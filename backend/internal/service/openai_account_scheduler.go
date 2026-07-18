package service

import (
	"container/heap"
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"golang.org/x/sync/singleflight"
)

const (
	openAIAccountScheduleLayerPreviousResponse = "previous_response_id"
	openAIAccountScheduleLayerCleanRelay       = "clean_relay"
	openAIAccountScheduleLayerSessionSticky    = "session_hash"
	openAIAccountScheduleLayerLoadBalance      = "load_balance"
	openAIAdvancedSchedulerSettingKey          = "openai_advanced_scheduler_enabled"
	defaultOpenAIOAuthSchedulingRateMultiplier = 1.0
)

const (
	openAIAdvancedSchedulerSettingCacheTTL  = 5 * time.Second
	openAIAdvancedSchedulerSettingDBTimeout = 2 * time.Second
	// ponytail: cap probes added when cost ordering expands configured Top-K;
	// use bulk acquisition if a measured workload needs a higher ceiling.
	openAIAccountSelectionProbeLimit = 64
)

const (
	openAIHybridFairnessRatio             = 0.30
	openAIHybridMaxFairShare              = 0.50
	openAIHybridOverflowProbeMax          = 32
	openAIQuotaHeadroomNeutralFactor      = 0.5
	openAIQuotaHeadroomSecondaryLowRemain = 0.10
	openAIQuotaHeadroomSnapshotStaleAfter = 8 * time.Hour
	openAIUpstreamCostNeutralFactor       = 0.5
)

type cachedOpenAIAdvancedSchedulerSetting struct {
	lowUpstreamRatePriorityEnabled bool
	oauthSchedulingRateMultiplier  float64
	enabled                        bool
	stickyWeightedEnabled          bool
	subscriptionPriorityEnabled    bool
	lbTopKOverride                 int
	weightOverrides                map[string]float64
	expiresAt                      int64
}

type openAIAdvancedSchedulerRuntimeSettings struct {
	lowUpstreamRatePriorityEnabled bool
	oauthSchedulingRateMultiplier  float64
	enabled                        bool
	stickyWeightedEnabled          bool
	subscriptionPriorityEnabled    bool
	lbTopKOverride                 int
	weightOverrides                map[string]float64
}

var openAIAdvancedSchedulerSettingCache atomic.Value // *cachedOpenAIAdvancedSchedulerSetting
var openAIAdvancedSchedulerSettingSF singleflight.Group

type OpenAIAccountScheduleRequest struct {
	GroupID                 *int64
	Platform                string
	SessionHash             string
	StickyAccountID         int64
	StickyPreviousAccountID int64
	StickyWeighted          bool
	SubscriptionPriority    bool
	PreserveStickyBinding   bool
	PreviousResponseID      string
	PreviousResponseCanMove bool
	UseUpstreamTokenCost    bool
	RequestedModel          string
	RequiredTransport       OpenAIUpstreamTransport
	RequiredCapability      OpenAIEndpointCapability
	RequiredImageCapability OpenAIImagesCapability
	RequireCompact          bool
	ExcludedIDs             map[int64]struct{}
}

type OpenAIAccountScheduleDecision struct {
	Layer               string
	StickyPreviousHit   bool
	StickySessionHit    bool
	CandidateCount      int
	TopK                int
	LatencyMs           int64
	LoadSkew            float64
	SelectedAccountID   int64
	SelectedAccountType string
}

type OpenAIAccountSchedulerMetricsSnapshot struct {
	SelectTotal              int64
	StickyPreviousHitTotal   int64
	StickySessionHitTotal    int64
	LoadBalanceSelectTotal   int64
	AccountSwitchTotal       int64
	SchedulerLatencyMsTotal  int64
	SchedulerLatencyMsAvg    float64
	StickyHitRatio           float64
	AccountSwitchRate        float64
	LoadSkewAvg              float64
	RuntimeStatsAccountCount int
}

type OpenAIAccountScheduler interface {
	Select(ctx context.Context, req OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error)
	ReportResult(accountID int64, success bool, firstTokenMs *int)
	ReportSwitch()
	SnapshotMetrics() OpenAIAccountSchedulerMetricsSnapshot
}

type openAIAccountSchedulerMetrics struct {
	selectTotal            atomic.Int64
	stickyPreviousHitTotal atomic.Int64
	stickySessionHitTotal  atomic.Int64
	loadBalanceSelectTotal atomic.Int64
	accountSwitchTotal     atomic.Int64
	latencyMsTotal         atomic.Int64
	loadSkewMilliTotal     atomic.Int64
}

type openAIAccountLoadPlan struct {
	allCandidates             []openAIAccountCandidateScore
	candidates                []openAIAccountCandidateScore
	staleSnapshotCompactRetry []openAIAccountCandidateScore
	selectionOrder            []openAIAccountCandidateScore
	candidateCount            int
	topK                      int
	loadSkew                  float64
	includeOverflowFallback   bool
}

type openAIAccountLoadSelectionAttempt struct {
	result              *AccountSelectionResult
	selectionOrder      []openAIAccountCandidateScore
	candidateCount      int
	topK                int
	loadSkew            float64
	compactBlocked      bool
	noCompactCandidates bool
	err                 error
}

func (m *openAIAccountSchedulerMetrics) recordSelect(decision OpenAIAccountScheduleDecision) {
	if m == nil {
		return
	}
	m.selectTotal.Add(1)
	m.latencyMsTotal.Add(decision.LatencyMs)
	m.loadSkewMilliTotal.Add(int64(math.Round(decision.LoadSkew * 1000)))
	if decision.StickyPreviousHit {
		m.stickyPreviousHitTotal.Add(1)
	}
	if decision.StickySessionHit {
		m.stickySessionHitTotal.Add(1)
	}
	if decision.Layer == openAIAccountScheduleLayerLoadBalance {
		m.loadBalanceSelectTotal.Add(1)
	}
}

func (m *openAIAccountSchedulerMetrics) recordSwitch() {
	if m == nil {
		return
	}
	m.accountSwitchTotal.Add(1)
}

type openAIAccountRuntimeStats struct {
	accounts     sync.Map
	accountCount atomic.Int64
}

type openAIAccountRuntimeStat struct {
	errorRateEWMABits atomic.Uint64
	ttftEWMABits      atomic.Uint64
}

func newOpenAIAccountRuntimeStats() *openAIAccountRuntimeStats {
	return &openAIAccountRuntimeStats{}
}

func (s *openAIAccountRuntimeStats) loadOrCreate(accountID int64) *openAIAccountRuntimeStat {
	if value, ok := s.accounts.Load(accountID); ok {
		stat, _ := value.(*openAIAccountRuntimeStat)
		if stat != nil {
			return stat
		}
	}

	stat := &openAIAccountRuntimeStat{}
	stat.ttftEWMABits.Store(math.Float64bits(math.NaN()))
	actual, loaded := s.accounts.LoadOrStore(accountID, stat)
	if !loaded {
		s.accountCount.Add(1)
		return stat
	}
	existing, _ := actual.(*openAIAccountRuntimeStat)
	if existing != nil {
		return existing
	}
	return stat
}

func updateEWMAAtomic(target *atomic.Uint64, sample float64, alpha float64) {
	for {
		oldBits := target.Load()
		oldValue := math.Float64frombits(oldBits)
		newValue := alpha*sample + (1-alpha)*oldValue
		if target.CompareAndSwap(oldBits, math.Float64bits(newValue)) {
			return
		}
	}
}

func (s *openAIAccountRuntimeStats) report(accountID int64, success bool, firstTokenMs *int) {
	if s == nil || accountID <= 0 {
		return
	}
	const alpha = 0.2
	stat := s.loadOrCreate(accountID)

	errorSample := 1.0
	if success {
		errorSample = 0.0
	}
	updateEWMAAtomic(&stat.errorRateEWMABits, errorSample, alpha)

	if firstTokenMs != nil && *firstTokenMs > 0 {
		ttft := float64(*firstTokenMs)
		ttftBits := math.Float64bits(ttft)
		for {
			oldBits := stat.ttftEWMABits.Load()
			oldValue := math.Float64frombits(oldBits)
			if math.IsNaN(oldValue) {
				if stat.ttftEWMABits.CompareAndSwap(oldBits, ttftBits) {
					break
				}
				continue
			}
			newValue := alpha*ttft + (1-alpha)*oldValue
			if stat.ttftEWMABits.CompareAndSwap(oldBits, math.Float64bits(newValue)) {
				break
			}
		}
	}
}

func (s *openAIAccountRuntimeStats) snapshot(accountID int64) (errorRate float64, ttft float64, hasTTFT bool) {
	if s == nil || accountID <= 0 {
		return 0, 0, false
	}
	value, ok := s.accounts.Load(accountID)
	if !ok {
		return 0, 0, false
	}
	stat, _ := value.(*openAIAccountRuntimeStat)
	if stat == nil {
		return 0, 0, false
	}
	errorRate = clamp01(math.Float64frombits(stat.errorRateEWMABits.Load()))
	ttftValue := math.Float64frombits(stat.ttftEWMABits.Load())
	if math.IsNaN(ttftValue) {
		return errorRate, 0, false
	}
	return errorRate, ttftValue, true
}

func (s *openAIAccountRuntimeStats) size() int {
	if s == nil {
		return 0
	}
	return int(s.accountCount.Load())
}

type defaultOpenAIAccountScheduler struct {
	service *OpenAIGatewayService
	metrics openAIAccountSchedulerMetrics
	stats   *openAIAccountRuntimeStats
}

type openAISelectionProbeBudget struct {
	acquires  int
	rechecks  int
	attempted map[int64]struct{}
	limited   bool
}

func newOpenAISelectionProbeBudget() *openAISelectionProbeBudget {
	return &openAISelectionProbeBudget{attempted: make(map[int64]struct{})}
}

func (b *openAISelectionProbeBudget) enableLimit() {
	if b != nil {
		b.limited = true
	}
}

func (b *openAISelectionProbeBudget) recordAcquire(accountID int64) bool {
	if b == nil {
		return false
	}
	if !b.limited {
		return true
	}
	if b.acquires >= openAIAccountSelectionProbeLimit {
		return false
	}
	if b.attempted == nil {
		b.attempted = make(map[int64]struct{})
	}
	b.acquires++
	b.attempted[accountID] = struct{}{}
	return true
}

func (b *openAISelectionProbeBudget) recordRecheck() bool {
	if b == nil {
		return false
	}
	if !b.limited {
		return true
	}
	if b.rechecks >= openAIAccountSelectionProbeLimit {
		return false
	}
	b.rechecks++
	return true
}

func (b *openAISelectionProbeBudget) acquireExhausted() bool {
	return b != nil && b.limited && b.acquires >= openAIAccountSelectionProbeLimit
}

func (b *openAISelectionProbeBudget) wasAttempted(accountID int64) bool {
	if b == nil {
		return false
	}
	_, ok := b.attempted[accountID]
	return ok
}

func newDefaultOpenAIAccountScheduler(service *OpenAIGatewayService, stats *openAIAccountRuntimeStats) OpenAIAccountScheduler {
	if stats == nil {
		stats = newOpenAIAccountRuntimeStats()
	}
	return &defaultOpenAIAccountScheduler{
		service: service,
		stats:   stats,
	}
}

func (s *defaultOpenAIAccountScheduler) Select(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	decision := OpenAIAccountScheduleDecision{}
	start := time.Now()
	defer func() {
		decision.LatencyMs = time.Since(start).Milliseconds()
		s.metrics.recordSelect(decision)
	}()

	previousResponseID := strings.TrimSpace(req.PreviousResponseID)
	if previousResponseID != "" && req.Platform == PlatformOpenAI && (!req.StickyWeighted || !req.PreviousResponseCanMove) {
		selection, err := s.service.selectAccountByPreviousResponseIDForCapability(
			ctx,
			req.GroupID,
			previousResponseID,
			req.RequestedModel,
			req.ExcludedIDs,
			req.RequiredCapability,
			req.RequireCompact,
		)
		if err != nil {
			return nil, decision, err
		}
		if selection != nil && selection.Account != nil {
			if !s.isAccountTransportCompatible(selection.Account, req.RequiredTransport) {
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				selection = nil
			}
		}
		if selection != nil && selection.Account != nil {
			decision.Layer = openAIAccountScheduleLayerPreviousResponse
			decision.StickyPreviousHit = true
			decision.SelectedAccountID = selection.Account.ID
			decision.SelectedAccountType = selection.Account.Type
			if req.SessionHash != "" {
				_ = s.service.BindStickySession(ctx, req.GroupID, req.SessionHash, selection.Account.ID)
			}
			return selection, decision, nil
		}
	}

	if !req.StickyWeighted {
		selection, err := s.selectBySessionHash(ctx, req)
		if err != nil {
			return nil, decision, err
		}
		if selection != nil && selection.Account != nil {
			decision.Layer = openAIAccountScheduleLayerSessionSticky
			decision.StickySessionHit = true
			decision.SelectedAccountID = selection.Account.ID
			decision.SelectedAccountType = selection.Account.Type
			return selection, decision, nil
		}
	}

	selection, candidateCount, topK, loadSkew, err := s.selectByLoadBalance(ctx, req)
	decision.Layer = openAIAccountScheduleLayerLoadBalance
	decision.CandidateCount = candidateCount
	decision.TopK = topK
	decision.LoadSkew = loadSkew
	if err != nil {
		return nil, decision, err
	}
	if selection != nil && selection.Account != nil {
		decision.SelectedAccountID = selection.Account.ID
		decision.SelectedAccountType = selection.Account.Type
		if req.StickyWeighted {
			decision.StickyPreviousHit = req.StickyPreviousAccountID > 0 && selection.Account.ID == req.StickyPreviousAccountID
			decision.StickySessionHit = req.StickyAccountID > 0 && selection.Account.ID == req.StickyAccountID
		}
	}
	return selection, decision, nil
}

func (s *defaultOpenAIAccountScheduler) selectBySessionHash(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
) (*AccountSelectionResult, error) {
	sessionHash := strings.TrimSpace(req.SessionHash)
	if sessionHash == "" || s == nil || s.service == nil || s.service.cache == nil {
		return nil, nil
	}

	accountID := req.StickyAccountID
	if accountID <= 0 {
		var err error
		accountID, err = s.service.getStickySessionAccountID(ctx, req.GroupID, sessionHash)
		if err != nil || accountID <= 0 {
			return nil, nil
		}
	}
	if accountID <= 0 {
		return nil, nil
	}
	if req.ExcludedIDs != nil {
		if _, excluded := req.ExcludedIDs[accountID]; excluded {
			return nil, nil
		}
	}

	account, err := s.service.getSchedulableAccount(ctx, accountID)
	if err != nil || account == nil {
		_ = s.service.deleteStickySessionAccountID(ctx, req.GroupID, sessionHash)
		return nil, nil
	}
	if s.shouldClearSessionStickyForRequest(account, req) {
		_ = s.service.deleteStickySessionAccountID(ctx, req.GroupID, sessionHash)
		return nil, nil
	}
	if !s.isAccountRequestCompatible(ctx, account, req) {
		return nil, nil
	}
	if !s.isAccountTransportCompatible(account, req.RequiredTransport) {
		_ = s.service.deleteStickySessionAccountID(ctx, req.GroupID, sessionHash)
		return nil, nil
	}
	account = s.service.recheckSelectedOpenAIAccountFromDB(ctx, account, req.GroupID, req.Platform, req.RequestedModel, req.RequireCompact, req.RequiredCapability)
	if account == nil || !s.isAccountTransportCompatible(account, req.RequiredTransport) {
		_ = s.service.deleteStickySessionAccountID(ctx, req.GroupID, sessionHash)
		return nil, nil
	}

	result, acquireErr := s.service.tryAcquireAccountSlot(ctx, accountID, account.Concurrency)
	if acquireErr == nil && result.Acquired {
		_ = s.service.refreshStickySessionTTL(ctx, req.GroupID, sessionHash, s.service.openAIWSSessionStickyTTL())
		return &AccountSelectionResult{
			Account:     account,
			Acquired:    true,
			ReleaseFunc: result.ReleaseFunc,
		}, nil
	}

	cfg := s.service.schedulingConfig()
	// WaitPlan.MaxConcurrency 使用 Concurrency（非 EffectiveLoadFactor），因为 WaitPlan 控制的是 Redis 实际并发槽位等待。
	if s.service.concurrencyService != nil {
		waitingCount, _ := s.service.concurrencyService.GetAccountWaitingCount(ctx, accountID)
		if waitingCount >= cfg.StickySessionMaxWaiting {
			return nil, nil
		}
		return &AccountSelectionResult{
			Account: account,
			WaitPlan: &AccountWaitPlan{
				AccountID:      accountID,
				MaxConcurrency: account.Concurrency,
				Timeout:        cfg.StickySessionWaitTimeout,
				MaxWaiting:     cfg.StickySessionMaxWaiting,
			},
		}, nil
	}
	return nil, nil
}

type openAIAccountCandidateScore struct {
	account   *Account
	loadInfo  *AccountLoadInfo
	loadKnown bool
	priority  int
	score     float64
	errorRate float64
	ttft      float64
	hasTTFT   bool
}

func openAICandidatePriority(candidate openAIAccountCandidateScore) int {
	if candidate.priority > 0 {
		return candidate.priority
	}
	if candidate.account != nil {
		return candidate.account.Priority
	}
	return 0
}

type openAIAccountCandidateHeap []openAIAccountCandidateScore

func (h openAIAccountCandidateHeap) Len() int {
	return len(h)
}

func (h openAIAccountCandidateHeap) Less(i, j int) bool {
	// 最小堆根节点保存“最差”候选，便于 O(log k) 维护 topK。
	return isOpenAIAccountCandidateBetter(h[j], h[i])
}

func (h openAIAccountCandidateHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *openAIAccountCandidateHeap) Push(x any) {
	candidate, ok := x.(openAIAccountCandidateScore)
	if !ok {
		panic("openAIAccountCandidateHeap: invalid element type")
	}
	*h = append(*h, candidate)
}

func (h *openAIAccountCandidateHeap) Pop() any {
	old := *h
	n := len(old)
	last := old[n-1]
	*h = old[:n-1]
	return last
}

func isOpenAIAccountCandidateBetter(left openAIAccountCandidateScore, right openAIAccountCandidateScore) bool {
	if left.score != right.score {
		return left.score > right.score
	}
	if left.account == nil || right.account == nil {
		return left.account != nil
	}
	leftPriority := openAICandidatePriority(left)
	rightPriority := openAICandidatePriority(right)
	if leftPriority != rightPriority {
		return leftPriority < rightPriority
	}
	if loadA, loadB := openAICandidateLoadRate(left.loadInfo), openAICandidateLoadRate(right.loadInfo); loadA != loadB {
		return loadA < loadB
	}
	if waitingA, waitingB := openAICandidateWaitingCount(left.loadInfo), openAICandidateWaitingCount(right.loadInfo); waitingA != waitingB {
		return waitingA < waitingB
	}
	if cmp := compareOpenAIAccountLastUsed(left.account, right.account); cmp != 0 {
		return cmp < 0
	}
	return left.account.ID < right.account.ID
}

func selectTopKOpenAICandidates(candidates []openAIAccountCandidateScore, topK int) []openAIAccountCandidateScore {
	if len(candidates) == 0 {
		return nil
	}
	if topK <= 0 {
		topK = 1
	}
	if topK >= len(candidates) {
		ranked := append([]openAIAccountCandidateScore(nil), candidates...)
		sort.Slice(ranked, func(i, j int) bool {
			return isOpenAIAccountCandidateBetter(ranked[i], ranked[j])
		})
		return ranked
	}

	best := make(openAIAccountCandidateHeap, 0, topK)
	for _, candidate := range candidates {
		if len(best) < topK {
			heap.Push(&best, candidate)
			continue
		}
		if isOpenAIAccountCandidateBetter(candidate, best[0]) {
			best[0] = candidate
			heap.Fix(&best, 0)
		}
	}

	ranked := make([]openAIAccountCandidateScore, len(best))
	copy(ranked, best)
	sort.Slice(ranked, func(i, j int) bool {
		return isOpenAIAccountCandidateBetter(ranked[i], ranked[j])
	})
	return ranked
}

func selectHybridTopKOpenAICandidates(candidates []openAIAccountCandidateScore, topK int, req OpenAIAccountScheduleRequest) []openAIAccountCandidateScore {
	if len(candidates) == 0 {
		return nil
	}
	if topK <= 0 {
		topK = 1
	}
	if topK >= len(candidates) {
		return selectTopKOpenAICandidates(candidates, topK)
	}

	fairCount := openAIHybridFairCandidateCount(topK, len(candidates))
	performanceCount := topK - fairCount
	if performanceCount <= 0 {
		performanceCount = 1
		fairCount = topK - performanceCount
	}

	performance := selectTopKOpenAICandidates(candidates, performanceCount)
	selectedIDs := make(map[int64]struct{}, len(performance))
	for _, candidate := range performance {
		if candidate.account != nil {
			selectedIDs[candidate.account.ID] = struct{}{}
		}
	}

	fairPool := make([]openAIAccountCandidateScore, 0, len(candidates)-len(performance))
	for _, candidate := range candidates {
		if candidate.account == nil {
			continue
		}
		if _, selected := selectedIDs[candidate.account.ID]; selected {
			continue
		}
		fairPool = append(fairPool, candidate)
	}

	fair := selectFairOpenAICandidates(fairPool, fairCount, deriveOpenAISelectionSeed(req))
	out := make([]openAIAccountCandidateScore, 0, len(performance)+len(fair))
	out = append(out, performance...)
	out = append(out, fair...)
	return out
}

func selectOpenAIOverflowProbeCandidates(candidates, selected []openAIAccountCandidateScore, limit int, req OpenAIAccountScheduleRequest) []openAIAccountCandidateScore {
	if limit <= 0 || len(candidates) == 0 {
		return nil
	}
	selectedIDs := make(map[int64]struct{}, len(selected))
	for _, candidate := range selected {
		if candidate.account != nil {
			selectedIDs[candidate.account.ID] = struct{}{}
		}
	}
	remaining := make([]openAIAccountCandidateScore, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.account == nil {
			continue
		}
		if _, ok := selectedIDs[candidate.account.ID]; ok {
			continue
		}
		remaining = append(remaining, candidate)
	}
	if len(remaining) == 0 {
		return nil
	}
	if limit > len(remaining) {
		limit = len(remaining)
	}
	return selectHybridTopKOpenAICandidates(remaining, limit, req)
}

func openAIHybridFairCandidateCount(topK, candidateCount int) int {
	if topK <= 1 || candidateCount <= topK {
		return 0
	}
	count := int(math.Floor(float64(topK) * openAIHybridFairnessRatio))
	if count < 1 {
		count = 1
	}
	maxFair := int(math.Floor(float64(topK) * openAIHybridMaxFairShare))
	if maxFair < 1 {
		maxFair = 1
	}
	if count > maxFair {
		count = maxFair
	}
	if count >= topK {
		count = topK - 1
	}
	return count
}

func openAIHybridOverflowProbeCount(topK, candidateCount int) int {
	if topK <= 0 || candidateCount <= topK {
		return 0
	}
	count := topK
	if count > openAIHybridOverflowProbeMax {
		count = openAIHybridOverflowProbeMax
	}
	remaining := candidateCount - topK
	if count > remaining {
		count = remaining
	}
	return count
}

func selectFairOpenAICandidates(candidates []openAIAccountCandidateScore, count int, seed uint64) []openAIAccountCandidateScore {
	if count <= 0 || len(candidates) == 0 {
		return nil
	}
	ranked := append([]openAIAccountCandidateScore(nil), candidates...)
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.account == nil || b.account == nil {
			return a.account != nil
		}
		priorityA := openAICandidatePriority(a)
		priorityB := openAICandidatePriority(b)
		if priorityA != priorityB {
			return priorityA < priorityB
		}
		if bucketA, bucketB := openAICandidateLoadBucket(a.loadInfo), openAICandidateLoadBucket(b.loadInfo); bucketA != bucketB {
			return bucketA < bucketB
		}
		if cmp := compareOpenAIAccountLastUsed(a.account, b.account); cmp != 0 {
			return cmp < 0
		}
		if waitingA, waitingB := openAICandidateWaitingCount(a.loadInfo), openAICandidateWaitingCount(b.loadInfo); waitingA != waitingB {
			return waitingA < waitingB
		}
		if loadA, loadB := openAICandidateLoadRate(a.loadInfo), openAICandidateLoadRate(b.loadInfo); loadA != loadB {
			return loadA < loadB
		}
		if a.score != b.score {
			return a.score > b.score
		}
		return openAIAccountSeedRank(seed, a.account.ID) < openAIAccountSeedRank(seed, b.account.ID)
	})
	if count > len(ranked) {
		count = len(ranked)
	}
	return ranked[:count]
}

func openAICandidateLoadBucket(loadInfo *AccountLoadInfo) int {
	if loadInfo == nil {
		return 0
	}
	switch {
	case loadInfo.LoadRate >= 100:
		return 2
	case loadInfo.LoadRate >= 80:
		return 1
	default:
		return 0
	}
}

func openAICandidateWaitingCount(loadInfo *AccountLoadInfo) int {
	if loadInfo == nil {
		return 0
	}
	return loadInfo.WaitingCount
}

func openAICandidateLoadRate(loadInfo *AccountLoadInfo) int {
	if loadInfo == nil {
		return 0
	}
	return loadInfo.LoadRate
}

func compareOpenAIAccountLastUsed(left, right *Account) int {
	switch {
	case left == nil && right == nil:
		return 0
	case left == nil:
		return 1
	case right == nil:
		return -1
	case left.LastUsedAt == nil && right.LastUsedAt != nil:
		return -1
	case left.LastUsedAt != nil && right.LastUsedAt == nil:
		return 1
	case left.LastUsedAt == nil && right.LastUsedAt == nil:
		return 0
	case left.LastUsedAt.Before(*right.LastUsedAt):
		return -1
	case right.LastUsedAt.Before(*left.LastUsedAt):
		return 1
	default:
		return 0
	}
}

func openAIAccountSeedRank(seed uint64, accountID int64) uint64 {
	x := seed ^ (uint64(accountID) + 0x9e3779b97f4a7c15)
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

func effectiveOpenAIHybridTopK(configuredTopK, candidateCount int) int {
	if candidateCount <= 0 {
		return 0
	}
	if configuredTopK <= 0 {
		configuredTopK = 1
	}
	topK := configuredTopK
	switch {
	case candidateCount > 500 && topK < 32:
		topK = 32
	case candidateCount > 100 && topK < 24:
		topK = 24
	case candidateCount > 20 && topK < 16:
		topK = 16
	case candidateCount > 12 && topK < 12:
		topK = 12
	}
	if topK > candidateCount {
		topK = candidateCount
	}
	return topK
}

type openAISelectionRNG struct {
	state uint64
}

func newOpenAISelectionRNG(seed uint64) openAISelectionRNG {
	if seed == 0 {
		seed = 0x9e3779b97f4a7c15
	}
	return openAISelectionRNG{state: seed}
}

func (r *openAISelectionRNG) nextUint64() uint64 {
	// xorshift64*
	x := r.state
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	r.state = x
	return x * 2685821657736338717
}

func (r *openAISelectionRNG) nextFloat64() float64 {
	// [0,1)
	return float64(r.nextUint64()>>11) / (1 << 53)
}

func deriveOpenAISelectionSeed(req OpenAIAccountScheduleRequest) uint64 {
	hasher := fnv.New64a()
	writeValue := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		_, _ = hasher.Write([]byte(trimmed))
		_, _ = hasher.Write([]byte{0})
	}

	writeValue(req.SessionHash)
	writeValue(req.PreviousResponseID)
	writeValue(req.RequestedModel)
	if req.GroupID != nil {
		_, _ = hasher.Write([]byte(strconv.FormatInt(*req.GroupID, 10)))
	}

	seed := hasher.Sum64()
	// 对“无会话锚点”的纯负载均衡请求引入时间熵，避免固定命中同一账号。
	if strings.TrimSpace(req.SessionHash) == "" && strings.TrimSpace(req.PreviousResponseID) == "" {
		seed ^= uint64(time.Now().UnixNano())
	}
	if seed == 0 {
		seed = uint64(time.Now().UnixNano()) ^ 0x9e3779b97f4a7c15
	}
	return seed
}

func buildOpenAIWeightedSelectionOrder(
	candidates []openAIAccountCandidateScore,
	req OpenAIAccountScheduleRequest,
) []openAIAccountCandidateScore {
	if len(candidates) <= 1 {
		return append([]openAIAccountCandidateScore(nil), candidates...)
	}

	pool := append([]openAIAccountCandidateScore(nil), candidates...)
	weights := make([]float64, len(pool))
	minScore := pool[0].score
	for i := 1; i < len(pool); i++ {
		if pool[i].score < minScore {
			minScore = pool[i].score
		}
	}
	for i := range pool {
		// 将 top-K 分值平移到正区间，避免“单一最高分账号”长期垄断。
		weight := (pool[i].score - minScore) + 1.0
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 {
			weight = 1.0
		}
		weights[i] = weight
	}

	order := make([]openAIAccountCandidateScore, 0, len(pool))
	rng := newOpenAISelectionRNG(deriveOpenAISelectionSeed(req))
	for len(pool) > 0 {
		total := 0.0
		for _, w := range weights {
			total += w
		}

		selectedIdx := 0
		if total > 0 {
			r := rng.nextFloat64() * total
			acc := 0.0
			for i, w := range weights {
				acc += w
				if r <= acc {
					selectedIdx = i
					break
				}
			}
		} else {
			selectedIdx = int(rng.nextUint64() % uint64(len(pool)))
		}

		order = append(order, pool[selectedIdx])
		pool = append(pool[:selectedIdx], pool[selectedIdx+1:]...)
		weights = append(weights[:selectedIdx], weights[selectedIdx+1:]...)
	}
	return order
}

func groupOpenAICandidatesByPriority(candidates []openAIAccountCandidateScore) [][]openAIAccountCandidateScore {
	if len(candidates) == 0 {
		return nil
	}
	groups := make(map[int][]openAIAccountCandidateScore)
	priorities := make([]int, 0)
	for _, candidate := range candidates {
		priority := openAICandidatePriority(candidate)
		if _, exists := groups[priority]; !exists {
			priorities = append(priorities, priority)
		}
		groups[priority] = append(groups[priority], candidate)
	}
	sort.Ints(priorities)

	ordered := make([][]openAIAccountCandidateScore, 0, len(priorities))
	for _, priority := range priorities {
		ordered = append(ordered, groups[priority])
	}
	return ordered
}

func buildOpenAIPriorityLayeredSelectionOrders(
	candidates []openAIAccountCandidateScore,
	topK int,
	req OpenAIAccountScheduleRequest,
) ([]openAIAccountCandidateScore, []openAIAccountCandidateScore) {
	if len(candidates) == 0 || topK <= 0 {
		return nil, nil
	}
	selectionOrder := make([]openAIAccountCandidateScore, 0, len(candidates))
	acquireOrder := make([]openAIAccountCandidateScore, 0, len(candidates))
	for _, priorityGroup := range groupOpenAICandidatesByPriority(candidates) {
		groupTopK := topK
		if groupTopK > len(priorityGroup) {
			groupTopK = len(priorityGroup)
		}
		ranked := selectHybridTopKOpenAICandidates(priorityGroup, groupTopK, req)
		layerSelection := buildOpenAIWeightedSelectionOrder(ranked, req)
		selectionOrder = append(selectionOrder, layerSelection...)
		acquireOrder = append(acquireOrder, layerSelection...)

		overflowLimit := openAIHybridOverflowProbeCount(groupTopK, len(priorityGroup))
		if overflowLimit <= 0 {
			continue
		}
		if overflow := selectOpenAIOverflowProbeCandidates(priorityGroup, layerSelection, overflowLimit, req); len(overflow) > 0 {
			acquireOrder = append(acquireOrder, overflow...)
		}
	}
	return selectionOrder, acquireOrder
}

func (s *defaultOpenAIAccountScheduler) selectByLoadBalance(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
) (*AccountSelectionResult, int, int, float64, error) {
	budget := newOpenAISelectionProbeBudget()
	accounts, err := s.service.listSchedulableAccounts(ctx, req.GroupID, req.Platform)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	if len(accounts) == 0 {
		return nil, 0, 0, 0, noAvailableOpenAISelectionError(req.RequestedModel, false)
	}

	var schedGroup *Group
	if req.GroupID != nil && s.service.schedulerSnapshot != nil {
		schedGroup, _ = s.service.schedulerSnapshot.GetGroupByID(ctx, *req.GroupID)
	}

	filtered, loadReq := s.filterOpenAIAccountsForLoadBalance(ctx, accounts, req, schedGroup)
	if len(filtered) == 0 && s.service.shouldRetryOpenAISchedulerWithoutCandidateIndex(ctx, req.GroupID) {
		retryCtx := WithSchedulerCandidateIndexBypass(ctx)
		accounts, err = s.service.listSchedulableAccounts(retryCtx, req.GroupID, req.Platform)
		if err != nil {
			return nil, 0, 0, 0, err
		}
		filtered, loadReq = s.filterOpenAIAccountsForLoadBalance(retryCtx, accounts, req, schedGroup)
	}
	if len(filtered) == 0 {
		return nil, 0, 0, 0, noAvailableOpenAISelectionError(req.RequestedModel, false)
	}

	loadMap := map[int64]*AccountLoadInfo{}
	if s.service.concurrencyService != nil {
		if batchLoad, loadErr := s.service.concurrencyService.GetAccountsLoadBatch(ctx, loadReq); loadErr == nil {
			loadMap = batchLoad
		}
	}

	if req.SubscriptionPriority {
		subscriptionAccounts, regularAccounts := partitionOpenAIChatGPTSubscriptionAccounts(filtered)
		if len(subscriptionAccounts) > 0 {
			subscriptionAttempt := s.trySelectByLoadBalancePool(ctx, req, subscriptionAccounts, loadMap, budget)
			if subscriptionAttempt.err != nil && (!subscriptionAttempt.noCompactCandidates || len(regularAccounts) == 0) {
				return nil, subscriptionAttempt.candidateCount, subscriptionAttempt.topK, subscriptionAttempt.loadSkew, subscriptionAttempt.err
			}
			if subscriptionAttempt.result != nil {
				return subscriptionAttempt.result, subscriptionAttempt.candidateCount, subscriptionAttempt.topK, subscriptionAttempt.loadSkew, nil
			}
			if len(regularAccounts) > 0 {
				regularAttempt := s.trySelectByLoadBalancePool(ctx, req, regularAccounts, loadMap, budget)
				if regularAttempt.err != nil && !regularAttempt.noCompactCandidates {
					return nil, regularAttempt.candidateCount, regularAttempt.topK, regularAttempt.loadSkew, regularAttempt.err
				}
				if regularAttempt.result != nil {
					return regularAttempt.result, regularAttempt.candidateCount, regularAttempt.topK, regularAttempt.loadSkew, nil
				}
				var result *AccountSelectionResult
				candidateCount, topK, loadSkew := regularAttempt.candidateCount, regularAttempt.topK, regularAttempt.loadSkew
				fallbackErr := regularAttempt.err
				if regularAttempt.err == nil {
					result, candidateCount, topK, loadSkew, fallbackErr = s.finishLoadBalanceSelectionFallback(ctx, req, regularAttempt, budget)
					if fallbackErr == nil && result != nil {
						return result, candidateCount, topK, loadSkew, nil
					}
				}
				subResult, subCount, subTopK, subSkew, subErr := s.finishLoadBalanceSelectionFallback(ctx, req, subscriptionAttempt, budget)
				if subErr == nil && subResult != nil {
					return subResult, subCount, subTopK, subSkew, nil
				}
				return result, candidateCount, topK, loadSkew, fallbackErr
			}
			return s.finishLoadBalanceSelectionFallback(ctx, req, subscriptionAttempt, budget)
		}
	}

	attempt := s.trySelectByLoadBalancePool(ctx, req, filtered, loadMap, budget)
	if attempt.err != nil {
		return nil, attempt.candidateCount, attempt.topK, attempt.loadSkew, attempt.err
	}
	if attempt.result != nil {
		return attempt.result, attempt.candidateCount, attempt.topK, attempt.loadSkew, nil
	}
	return s.finishLoadBalanceSelectionFallback(ctx, req, attempt, budget)
}
func (s *defaultOpenAIAccountScheduler) filterOpenAIAccountsForLoadBalance(
	ctx context.Context,
	accounts []Account,
	req OpenAIAccountScheduleRequest,
	schedGroup *Group,
) ([]*Account, []AccountWithConcurrency) {
	filtered := make([]*Account, 0, len(accounts))
	loadReq := make([]AccountWithConcurrency, 0, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		if req.ExcludedIDs != nil {
			if _, excluded := req.ExcludedIDs[account.ID]; excluded {
				continue
			}
		}
		if !account.IsSchedulable() || !account.IsOpenAI() {
			continue
		}
		if !s.service.isOpenAIAccountProxyHealthSchedulable(ctx, account) {
			continue
		}
		// require_privacy_set: 只在当前分组调度中跳过 privacy 未设置的账号。
		if schedGroup != nil && schedGroup.RequirePrivacySet && !account.IsPrivacySet() {
			s.notePrivacyRequiredAccountSkipped(account, schedGroup)
			continue
		}
		if !s.isAccountRequestCompatible(ctx, account, req) {
			continue
		}
		if !s.isAccountTransportCompatible(account, req.RequiredTransport) {
			continue
		}
		filtered = append(filtered, account)
		loadReq = append(loadReq, AccountWithConcurrency{
			ID:             account.ID,
			MaxConcurrency: account.EffectiveLoadFactor(),
		})
	}
	return filtered, loadReq
}

func (s *defaultOpenAIAccountScheduler) notePrivacyRequiredAccountSkipped(account *Account, group *Group) {
	if account == nil || group == nil {
		return
	}
	slog.Info("openai_scheduler_privacy_required_account_skipped",
		"account_id", account.ID,
		"group_id", group.ID,
		"group_name", group.Name,
	)
}

func (s *defaultOpenAIAccountScheduler) isAccountTransportCompatible(account *Account, requiredTransport OpenAIUpstreamTransport) bool {
	if requiredTransport == OpenAIUpstreamTransportAny || requiredTransport == OpenAIUpstreamTransportHTTPSSE {
		return true
	}
	if s == nil || s.service == nil {
		return false
	}
	return s.service.isOpenAIAccountTransportCompatible(account, requiredTransport)
}

func (s *defaultOpenAIAccountScheduler) lookupShadowParentAccount(ctx context.Context, id int64) *Account {
	if s == nil || s.service == nil {
		return nil
	}
	if s.service.schedulerSnapshot != nil {
		if account, err := s.service.schedulerSnapshot.GetAccount(ctx, id); err == nil && account != nil {
			return account
		}
	}
	if s.service.accountRepo == nil {
		return nil
	}
	account, _ := s.service.accountRepo.GetByID(ctx, id)
	return account
}

func (s *defaultOpenAIAccountScheduler) isAccountRequestCompatible(ctx context.Context, account *Account, req OpenAIAccountScheduleRequest) bool {
	if account == nil {
		return false
	}
	if s != nil && s.service != nil && s.service.isOpenAIAccountRequestRuntimeBlocked(account, req.RequestedModel) {
		return false
	}
	if paused, _ := shouldAutoPauseOpenAIAccountByQuota(ctx, account); paused {
		return false
	}
	if !parentHealthyForShadow(account, func(id int64) *Account {
		return s.lookupShadowParentAccount(ctx, id)
	}) {
		return false
	}
	if req.RequestedModel != "" && !account.IsModelSupported(req.RequestedModel) {
		return false
	}
	if req.GroupID != nil && s != nil && s.service != nil &&
		s.service.needsUpstreamChannelRestrictionCheck(ctx, req.GroupID) &&
		s.service.isUpstreamModelRestrictedByChannel(ctx, *req.GroupID, account, req.RequestedModel, req.RequireCompact) {
		return false
	}
	return accountSupportsOpenAICapabilities(account, req.RequiredCapability, req.RequiredImageCapability)
}

func (s *defaultOpenAIAccountScheduler) shouldClearSessionStickyForRequest(account *Account, req OpenAIAccountScheduleRequest) bool {
	if shouldClearOpenAISessionStickyForRequest(account, req.RequestedModel, req.RequireCompact, req.RequiredCapability) {
		return true
	}
	return account != nil && !account.SupportsOpenAIImageCapability(req.RequiredImageCapability)
}

func (s *defaultOpenAIAccountScheduler) ReportResult(accountID int64, success bool, firstTokenMs *int) {
	if s == nil || s.stats == nil {
		return
	}
	s.stats.report(accountID, success, firstTokenMs)
}

func (s *defaultOpenAIAccountScheduler) ReportSwitch() {
	if s == nil {
		return
	}
	s.metrics.recordSwitch()
}

func (s *defaultOpenAIAccountScheduler) SnapshotMetrics() OpenAIAccountSchedulerMetricsSnapshot {
	if s == nil {
		return OpenAIAccountSchedulerMetricsSnapshot{}
	}

	selectTotal := s.metrics.selectTotal.Load()
	prevHit := s.metrics.stickyPreviousHitTotal.Load()
	sessionHit := s.metrics.stickySessionHitTotal.Load()
	switchTotal := s.metrics.accountSwitchTotal.Load()
	latencyTotal := s.metrics.latencyMsTotal.Load()
	loadSkewTotal := s.metrics.loadSkewMilliTotal.Load()

	snapshot := OpenAIAccountSchedulerMetricsSnapshot{
		SelectTotal:              selectTotal,
		StickyPreviousHitTotal:   prevHit,
		StickySessionHitTotal:    sessionHit,
		LoadBalanceSelectTotal:   s.metrics.loadBalanceSelectTotal.Load(),
		AccountSwitchTotal:       switchTotal,
		SchedulerLatencyMsTotal:  latencyTotal,
		RuntimeStatsAccountCount: s.stats.size(),
	}
	if selectTotal > 0 {
		snapshot.SchedulerLatencyMsAvg = float64(latencyTotal) / float64(selectTotal)
		snapshot.StickyHitRatio = float64(prevHit+sessionHit) / float64(selectTotal)
		snapshot.AccountSwitchRate = float64(switchTotal) / float64(selectTotal)
		snapshot.LoadSkewAvg = float64(loadSkewTotal) / 1000 / float64(selectTotal)
	}
	return snapshot
}

func (s *OpenAIGatewayService) openAIAdvancedSchedulerSettingRepo() SettingRepository {
	if s == nil || s.rateLimitService == nil || s.rateLimitService.settingService == nil {
		return nil
	}
	return s.rateLimitService.settingService.settingRepo
}

func (s *OpenAIGatewayService) isOpenAIAdvancedSchedulerEnabled(ctx context.Context) bool {
	return s.openAIAdvancedSchedulerRuntimeSettings(ctx).enabled
}

func (s *OpenAIGatewayService) getOpenAIAccountScheduler(ctx context.Context) OpenAIAccountScheduler {
	if s == nil {
		return nil
	}
	if !s.isOpenAIAdvancedSchedulerEnabled(ctx) {
		return nil
	}
	s.openaiSchedulerOnce.Do(func() {
		if s.openaiAccountStats == nil {
			s.openaiAccountStats = newOpenAIAccountRuntimeStats()
		}
		if s.openaiScheduler == nil {
			s.openaiScheduler = newDefaultOpenAIAccountScheduler(s, s.openaiAccountStats)
		}
	})
	return s.openaiScheduler
}

func resetOpenAIAdvancedSchedulerSettingCacheForTest() {
	openAIAdvancedSchedulerSettingCache = atomic.Value{}
	openAIAdvancedSchedulerSettingSF = singleflight.Group{}
}

func (s *OpenAIGatewayService) SelectAccountWithScheduler(
	ctx context.Context,
	groupID *int64,
	previousResponseID string,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredTransport OpenAIUpstreamTransport,
	requireCompact bool,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	return s.selectAccountWithScheduler(ctx, groupID, previousResponseID, sessionHash, requestedModel, excludedIDs, requiredTransport, "", "", requireCompact, PlatformOpenAI, false, true)
}

func (s *OpenAIGatewayService) SelectAccountWithSchedulerForCapability(
	ctx context.Context,
	groupID *int64,
	previousResponseID string,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredTransport OpenAIUpstreamTransport,
	requiredCapability OpenAIEndpointCapability,
	requireCompact bool,
	options ...any,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	platform := PlatformOpenAI
	previousResponseCanMove := false
	useUpstreamTokenCost := true
	boolIndex := 0
	for _, option := range options {
		switch value := option.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				platform = strings.ToLower(strings.TrimSpace(value))
			}
		case bool:
			if boolIndex == 0 {
				previousResponseCanMove = value
			} else if boolIndex == 1 {
				useUpstreamTokenCost = value
			}
			boolIndex++
		}
	}
	if platform == PlatformGrok {
		return s.selectGrokAccountWithSession(ctx, groupID, sessionHash, requestedModel, excludedIDs, requiredCapability)
	}
	return s.selectAccountWithScheduler(ctx, groupID, previousResponseID, sessionHash, requestedModel, excludedIDs, requiredTransport, requiredCapability, "", requireCompact, platform, previousResponseCanMove, useUpstreamTokenCost)
}

func (s *OpenAIGatewayService) selectGrokAccount(
	ctx context.Context,
	groupID *int64,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredCapability OpenAIEndpointCapability,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	return s.selectGrokAccountWithSession(ctx, groupID, "", requestedModel, excludedIDs, requiredCapability)
}

func (s *OpenAIGatewayService) selectGrokAccountWithSession(
	ctx context.Context,
	groupID *int64,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredCapability OpenAIEndpointCapability,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	decision := OpenAIAccountScheduleDecision{Layer: openAIAccountScheduleLayerLoadBalance}
	var (
		accounts []Account
		err      error
	)
	if s.schedulerSnapshot != nil {
		accounts, _, err = s.schedulerSnapshot.ListSchedulableAccounts(ctx, groupID, PlatformGrok, false)
	} else if s.cfg != nil && s.cfg.RunMode == "simple" {
		accounts, err = s.accountRepo.ListSchedulableByPlatform(ctx, PlatformGrok)
	} else if groupID != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupIDAndPlatform(ctx, *groupID, PlatformGrok)
	} else {
		accounts, err = s.accountRepo.ListSchedulableUngroupedByPlatform(ctx, PlatformGrok)
	}
	if err != nil {
		return nil, decision, fmt.Errorf("query Grok accounts failed: %w", err)
	}
	accounts = FilterAccountsVisibleToRequestUser(ctx, accounts)
	stickyAccountID := int64(0)
	if strings.TrimSpace(sessionHash) != "" {
		if accountID, stickyErr := s.getStickySessionAccountID(ctx, groupID, sessionHash); stickyErr == nil {
			stickyAccountID = accountID
		}
	}
	eligible := make([]Account, 0, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		if _, excluded := excludedIDs[account.ID]; excluded {
			continue
		}
		if s.isOpenAIAccountRuntimeBlocked(account) ||
			!isSchedulableGrokAccountType(account) ||
			!account.IsSchedulable() ||
			!account.HasCompleteRequiredProxyForScheduling() {
			continue
		}
		if requiredCapability != "" && !account.SupportsOpenAIEndpointCapability(requiredCapability) {
			if account.IsGrok() && requiredCapability == OpenAIEndpointCapabilityGrokMediaGeneration {
				_, reason := account.GrokMediaGenerationEligibility()
				slog.Debug("grok_media_account_ineligible", "account_id", account.ID, "reason", reason)
			}
			continue
		}
		if strings.TrimSpace(requestedModel) != "" && !account.IsModelSupported(requestedModel) {
			continue
		}
		eligible = append(eligible, *account)
	}
	decision.CandidateCount = len(eligible)
	sort.SliceStable(eligible, func(i, j int) bool {
		if stickyAccountID > 0 {
			iSticky := eligible[i].ID == stickyAccountID
			jSticky := eligible[j].ID == stickyAccountID
			if iSticky != jSticky {
				return iSticky
			}
		}
		return s.isBetterAccount(ctx, &eligible[i], &eligible[j])
	})
	for i := range eligible {
		account := &eligible[i]
		result, acquireErr := s.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
		if acquireErr != nil || result == nil || !result.Acquired {
			continue
		}
		hydrated, hydrateErr := s.rehydrateSelectedGrokAccount(ctx, account, groupID, requestedModel, requiredCapability)
		if hydrateErr != nil {
			if result.ReleaseFunc != nil {
				result.ReleaseFunc()
			}
			continue
		}
		decision.SelectedAccountID = account.ID
		decision.SelectedAccountType = account.Type
		if account.ID == stickyAccountID {
			decision.Layer = openAIAccountScheduleLayerSessionSticky
			decision.StickySessionHit = true
		}
		return &AccountSelectionResult{Account: hydrated, Acquired: true, ReleaseFunc: result.ReleaseFunc}, decision, nil
	}
	return nil, decision, ErrNoAvailableAccounts
}

// rehydrateSelectedGrokAccount performs a final authoritative read after the
// scheduler chooses a shared-pool candidate. Snapshot metadata is intentionally
// sparse and must never be allowed to turn a required account proxy into a
// direct xAI connection.
func (s *OpenAIGatewayService) rehydrateSelectedGrokAccount(
	ctx context.Context,
	account *Account,
	groupID *int64,
	requestedModel string,
	requiredCapability OpenAIEndpointCapability,
) (*Account, error) {
	if account == nil || s.accountRepo == nil {
		return nil, ErrNoAvailableAccounts
	}
	hydrated, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || hydrated == nil {
		return nil, ErrNoAvailableAccounts
	}
	if !IsAccountVisibleToRequestUser(ctx, hydrated) ||
		s.isOpenAIAccountRuntimeBlocked(hydrated) ||
		!isSchedulableGrokAccountType(hydrated) ||
		!hydrated.IsSchedulable() ||
		!hydrated.HasCompleteRequiredProxyForScheduling() ||
		!s.isOpenAIAccountInRequestGroup(hydrated, groupID) {
		return nil, ErrNoAvailableAccounts
	}
	if requiredCapability != "" && !hydrated.SupportsOpenAIEndpointCapability(requiredCapability) {
		return nil, ErrNoAvailableAccounts
	}
	if strings.TrimSpace(requestedModel) != "" && !hydrated.IsModelSupported(requestedModel) {
		return nil, ErrNoAvailableAccounts
	}
	if s.schedulerSnapshot != nil {
		_ = s.schedulerSnapshot.UpdateAccountInCache(context.WithoutCancel(ctx), hydrated)
	}
	return hydrated, nil
}

func isSchedulableGrokAccountType(account *Account) bool {
	if account == nil {
		return false
	}
	if account.IsGrokOAuth() {
		return true
	}
	// User-owned Grok accounts remain OAuth-only. API-key accounts are
	// schedulable only in the administrator-managed shared pool.
	return account.IsGrokAPIKey() && account.OwnerUserID == nil
}

func (s *OpenAIGatewayService) SelectAccountWithSchedulerForImages(
	ctx context.Context,
	groupID *int64,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredCapability OpenAIImagesCapability,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	selection, decision, err := s.selectAccountWithScheduler(ctx, groupID, "", sessionHash, requestedModel, excludedIDs, OpenAIUpstreamTransportHTTPSSE, "", requiredCapability, false, PlatformOpenAI, false, false)
	if err == nil && selection != nil && selection.Account != nil {
		return selection, decision, nil
	}
	// 如果要求 native 能力（如指定了模型）但没有可用的 APIKey 账号，回退到 basic（OAuth 账号）
	if requiredCapability == OpenAIImagesCapabilityNative {
		return s.selectAccountWithScheduler(ctx, groupID, "", sessionHash, requestedModel, excludedIDs, OpenAIUpstreamTransportHTTPSSE, "", OpenAIImagesCapabilityBasic, false, PlatformOpenAI, false, false)
	}
	return selection, decision, err
}

func (s *OpenAIGatewayService) selectAccountWithScheduler(
	ctx context.Context,
	groupID *int64,
	previousResponseID string,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredTransport OpenAIUpstreamTransport,
	requiredCapability OpenAIEndpointCapability,
	requiredImageCapability OpenAIImagesCapability,
	requireCompact bool,
	platform string,
	previousResponseCanMove bool,
	useUpstreamTokenCost bool,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	ctx = s.withOpenAIQuotaAutoPauseContext(ctx)
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" {
		platform = PlatformOpenAI
	}
	decision := OpenAIAccountScheduleDecision{}
	scheduler := s.getOpenAIAccountScheduler(ctx)
	if scheduler == nil {
		decision.Layer = openAIAccountScheduleLayerLoadBalance
		if requiredTransport == OpenAIUpstreamTransportAny || requiredTransport == OpenAIUpstreamTransportHTTPSSE {
			effectiveExcludedIDs := cloneExcludedAccountIDs(excludedIDs)
			for {
				selection, err := s.selectAccountWithLoadAwareness(ctx, groupID, sessionHash, requestedModel, effectiveExcludedIDs, requireCompact, requiredCapability, useUpstreamTokenCost, platform)
				if err != nil {
					return nil, decision, err
				}
				if selection == nil || selection.Account == nil {
					return selection, decision, nil
				}
				if accountSupportsOpenAICapabilities(selection.Account, requiredCapability, requiredImageCapability) {
					return selection, decision, nil
				}
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				if effectiveExcludedIDs == nil {
					effectiveExcludedIDs = make(map[int64]struct{})
				}
				if _, exists := effectiveExcludedIDs[selection.Account.ID]; exists {
					return nil, decision, ErrNoAvailableAccounts
				}
				effectiveExcludedIDs[selection.Account.ID] = struct{}{}
			}
		}

		effectiveExcludedIDs := cloneExcludedAccountIDs(excludedIDs)
		for {
			selection, err := s.selectAccountWithLoadAwareness(ctx, groupID, sessionHash, requestedModel, effectiveExcludedIDs, requireCompact, requiredCapability, useUpstreamTokenCost, platform)
			if err != nil {
				return nil, decision, err
			}
			if selection == nil || selection.Account == nil {
				return selection, decision, nil
			}
			if s.isOpenAIAccountTransportCompatible(selection.Account, requiredTransport) &&
				accountSupportsOpenAICapabilities(selection.Account, requiredCapability, requiredImageCapability) {
				return selection, decision, nil
			}
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			if effectiveExcludedIDs == nil {
				effectiveExcludedIDs = make(map[int64]struct{})
			}
			if _, exists := effectiveExcludedIDs[selection.Account.ID]; exists {
				return nil, decision, ErrNoAvailableAccounts
			}
			effectiveExcludedIDs[selection.Account.ID] = struct{}{}
		}
	}

	var stickyAccountID int64
	if sessionHash != "" && s.cache != nil {
		if accountID, err := s.getStickySessionAccountID(ctx, groupID, sessionHash); err == nil && accountID > 0 {
			stickyAccountID = accountID
		}
	}
	stickyWeighted := s.isOpenAIAdvancedSchedulerStickyWeightedEnabled(ctx)
	subscriptionPriority := s.isOpenAIAdvancedSchedulerSubscriptionPriorityEnabled(ctx)
	stickyPreviousAccountID := int64(0)
	if stickyWeighted && previousResponseCanMove && strings.TrimSpace(previousResponseID) != "" && platform == PlatformOpenAI {
		stickyPreviousAccountID = s.ResolveAccountIDByPreviousResponseIDForScheduler(ctx, groupID, previousResponseID, requestedModel, excludedIDs, requiredCapability, requireCompact)
	}

	selection, decision, err := scheduler.Select(ctx, OpenAIAccountScheduleRequest{
		GroupID:                 groupID,
		Platform:                platform,
		SessionHash:             sessionHash,
		StickyAccountID:         stickyAccountID,
		StickyPreviousAccountID: stickyPreviousAccountID,
		StickyWeighted:          stickyWeighted,
		SubscriptionPriority:    subscriptionPriority,
		PreviousResponseID:      previousResponseID,
		PreviousResponseCanMove: previousResponseCanMove,
		UseUpstreamTokenCost:    useUpstreamTokenCost,
		RequestedModel:          requestedModel,
		RequiredTransport:       requiredTransport,
		RequiredCapability:      requiredCapability,
		RequiredImageCapability: requiredImageCapability,
		RequireCompact:          requireCompact,
		ExcludedIDs:             excludedIDs,
	})
	if err == nil && selection != nil && selection.Account != nil {
		s.bindOpenAIAccountProxyExitIP(ctx, selection.Account)
	}
	return selection, decision, err
}

func accountSupportsOpenAICapabilities(account *Account, requiredCapability OpenAIEndpointCapability, requiredImageCapability OpenAIImagesCapability) bool {
	if account == nil {
		return false
	}
	return account.SupportsOpenAIEndpointCapability(requiredCapability) &&
		account.SupportsOpenAIImageCapability(requiredImageCapability)
}

func cloneExcludedAccountIDs(excludedIDs map[int64]struct{}) map[int64]struct{} {
	if len(excludedIDs) == 0 {
		return nil
	}
	cloned := make(map[int64]struct{}, len(excludedIDs))
	for id := range excludedIDs {
		cloned[id] = struct{}{}
	}
	return cloned
}

func (s *OpenAIGatewayService) isOpenAIAccountTransportCompatible(account *Account, requiredTransport OpenAIUpstreamTransport) bool {
	if requiredTransport == OpenAIUpstreamTransportAny || requiredTransport == OpenAIUpstreamTransportHTTPSSE {
		return true
	}
	if s == nil || account == nil {
		return false
	}
	if requiredTransport == OpenAIUpstreamTransportResponsesWebsocketV2Ingress {
		if s.cfg == nil || !s.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled {
			return s.getOpenAIWSProtocolResolver().Resolve(account).Transport == OpenAIUpstreamTransportResponsesWebsocketV2
		}
		mode := account.ResolveOpenAIResponsesWebSocketV2Mode(s.cfg.Gateway.OpenAIWS.IngressModeDefault)
		switch mode {
		case OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge, OpenAIWSIngressModeShared, OpenAIWSIngressModeDedicated:
			return true
		default:
			return false
		}
	}
	return s.getOpenAIWSProtocolResolver().Resolve(account).Transport == requiredTransport
}

func (s *OpenAIGatewayService) ReportOpenAIAccountScheduleResult(accountID int64, args ...any) {
	var model string
	var success bool
	var firstTokenMs *int
	switch len(args) {
	case 2:
		success, _ = args[0].(bool)
		firstTokenMs, _ = args[1].(*int)
	case 3:
		model, _ = args[0].(string)
		success, _ = args[1].(bool)
		firstTokenMs, _ = args[2].(*int)
	default:
		return
	}
	if success {
		s.clearOpenAIAccountModelTransientState(accountID, normalizeOpenAIAccountModelTransientModel(model))
	}
	scheduler := s.getOpenAIAccountScheduler(context.Background())
	if scheduler == nil {
		return
	}
	scheduler.ReportResult(accountID, success, firstTokenMs)
}

func (s *OpenAIGatewayService) RecordOpenAIAccountSwitch() {
	scheduler := s.getOpenAIAccountScheduler(context.Background())
	if scheduler == nil {
		return
	}
	scheduler.ReportSwitch()
}

func (s *OpenAIGatewayService) SnapshotOpenAIAccountSchedulerMetrics() OpenAIAccountSchedulerMetricsSnapshot {
	scheduler := s.getOpenAIAccountScheduler(context.Background())
	if scheduler == nil {
		return OpenAIAccountSchedulerMetricsSnapshot{}
	}
	return scheduler.SnapshotMetrics()
}

func (s *OpenAIGatewayService) openAIWSSessionStickyTTL() time.Duration {
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds > 0 {
		return time.Duration(s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds) * time.Second
	}
	return openaiStickySessionTTL
}

func (s *OpenAIGatewayService) openAIWSLBTopK() int {
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.LBTopK > 0 {
		return s.cfg.Gateway.OpenAIWS.LBTopK
	}
	return 7
}

func (s *OpenAIGatewayService) shouldRetryOpenAISchedulerWithoutCandidateIndex(ctx context.Context, groupID *int64) bool {
	if s == nil || s.schedulerSnapshot == nil || IsSchedulerCandidateIndexBypassed(ctx) {
		return false
	}
	cfg := s.schedulingConfig()
	if len(cfg.IndexedBuckets) == 0 {
		return false
	}
	bucket := SchedulerBucket{GroupID: 0, Platform: PlatformOpenAI, Mode: SchedulerModeSingle}
	if groupID != nil && *groupID > 0 {
		bucket.GroupID = *groupID
	}
	for _, raw := range cfg.IndexedBuckets {
		if raw == bucket.String() {
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) openAIWSSchedulerWeights() GatewayOpenAIWSSchedulerScoreWeightsView {
	if s != nil && s.cfg != nil {
		return GatewayOpenAIWSSchedulerScoreWeightsView{
			Priority:      s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority,
			Load:          s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Load,
			Queue:         s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Queue,
			ErrorRate:     s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.ErrorRate,
			TTFT:          s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT,
			Reset:         s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Reset,
			QuotaHeadroom: s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.QuotaHeadroom,
			UpstreamCost:  s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.UpstreamCost,
			Previous:      s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.PreviousResponse,
			SessionSticky: s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.SessionSticky,
		}
	}
	return GatewayOpenAIWSSchedulerScoreWeightsView{
		Priority:      1.0,
		Load:          1.0,
		Queue:         0.7,
		ErrorRate:     0.8,
		TTFT:          0.5,
		Previous:      5.0,
		SessionSticky: 3.0,
	}
}

type GatewayOpenAIWSSchedulerScoreWeightsView struct {
	Priority      float64
	Load          float64
	Queue         float64
	ErrorRate     float64
	TTFT          float64
	Reset         float64
	QuotaHeadroom float64
	UpstreamCost  float64
	Previous      float64
	SessionSticky float64
}

func (w GatewayOpenAIWSSchedulerScoreWeightsView) configWeights() config.GatewayOpenAIWSSchedulerScoreWeights {
	return config.GatewayOpenAIWSSchedulerScoreWeights{
		Priority:         w.Priority,
		Load:             w.Load,
		Queue:            w.Queue,
		ErrorRate:        w.ErrorRate,
		TTFT:             w.TTFT,
		Reset:            w.Reset,
		QuotaHeadroom:    w.QuotaHeadroom,
		UpstreamCost:     w.UpstreamCost,
		PreviousResponse: w.Previous,
		SessionSticky:    w.SessionSticky,
	}
}

func clamp01(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func calcLoadSkewByMoments(sum float64, sumSquares float64, count int) float64 {
	if count <= 1 {
		return 0
	}
	mean := sum / float64(count)
	variance := sumSquares/float64(count) - mean*mean
	if variance < 0 {
		variance = 0
	}
	return math.Sqrt(variance)
}

func openAIStickyAccountMatchesGroup(account *Account, groupID *int64) bool {
	if account == nil {
		return false
	}
	if groupID == nil {
		return len(account.AccountGroups) == 0 && len(account.GroupIDs) == 0
	}
	for _, accountGroupID := range account.GroupIDs {
		if accountGroupID == *groupID {
			return true
		}
	}
	for _, accountGroup := range account.AccountGroups {
		if accountGroup.GroupID == *groupID {
			return true
		}
	}
	return false
}
