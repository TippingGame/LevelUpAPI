package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

func openAIAccountSchedulingPriority(account *Account) int {
	if account == nil {
		return 0
	}
	return account.Priority
}

// withOpenAIQuotaAutoPauseContext keeps the scheduler call boundary compatible
// with upstream. LevelUp's quota fence is currently enforced by Account
// schedulability and DB rechecks rather than context-carried settings.
func (s *OpenAIGatewayService) withOpenAIQuotaAutoPauseContext(ctx context.Context) context.Context {
	return ctx
}

func (s *OpenAIGatewayService) ResolveAccountIDByPreviousResponseIDForScheduler(
	ctx context.Context,
	groupID *int64,
	previousResponseID string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredCapability OpenAIEndpointCapability,
	requireCompact bool,
) int64 {
	if s == nil {
		return 0
	}
	responseID := strings.TrimSpace(previousResponseID)
	if responseID == "" {
		return 0
	}
	store := s.getOpenAIWSStateStore()
	if store == nil {
		return 0
	}
	accountID, err := store.GetResponseAccount(ctx, derefGroupID(groupID), responseID)
	if err != nil || accountID <= 0 {
		return 0
	}
	if _, excluded := excludedIDs[accountID]; excluded {
		return 0
	}
	account, err := s.getSchedulableAccount(ctx, accountID)
	if err != nil || account == nil {
		_ = store.DeleteResponseAccount(ctx, derefGroupID(groupID), responseID)
		return 0
	}
	if s.isOpenAICleanRelayActive(ctx, account) ||
		s.getOpenAIWSProtocolResolver().Resolve(account).Transport != OpenAIUpstreamTransportResponsesWebsocketV2 {
		return 0
	}
	if shouldClearStickySession(account, requestedModel) || !account.IsOpenAI() || !account.IsSchedulable() ||
		(requestedModel != "" && !account.IsModelSupported(requestedModel)) ||
		!account.SupportsOpenAIEndpointCapability(requiredCapability) {
		_ = store.DeleteResponseAccount(ctx, derefGroupID(groupID), responseID)
		return 0
	}
	account = s.recheckSelectedOpenAIAccountFromDB(ctx, account, groupID, PlatformOpenAI, requestedModel, requireCompact, requiredCapability)
	if account == nil || (requireCompact && openAICompactSupportTier(account) == 0) {
		_ = store.DeleteResponseAccount(ctx, derefGroupID(groupID), responseID)
		return 0
	}
	return accountID
}

func (s *OpenAIGatewayService) openAIAdvancedSchedulerRuntimeSettings(ctx context.Context) openAIAdvancedSchedulerRuntimeSettings {
	fromCache := func(cached *cachedOpenAIAdvancedSchedulerSetting) openAIAdvancedSchedulerRuntimeSettings {
		return openAIAdvancedSchedulerRuntimeSettings{
			lowUpstreamRatePriorityEnabled: cached.lowUpstreamRatePriorityEnabled,
			oauthSchedulingRateMultiplier:  cached.oauthSchedulingRateMultiplier,
			enabled:                        cached.enabled,
			stickyWeightedEnabled:          cached.stickyWeightedEnabled,
			subscriptionPriorityEnabled:    cached.subscriptionPriorityEnabled,
			lbTopKOverride:                 cached.lbTopKOverride,
			weightOverrides:                cloneOpenAIAdvancedSchedulerWeightOverrides(cached.weightOverrides),
		}
	}
	if cached, ok := openAIAdvancedSchedulerSettingCache.Load().(*cachedOpenAIAdvancedSchedulerSetting); ok && cached != nil && time.Now().UnixNano() < cached.expiresAt {
		return fromCache(cached)
	}

	result, _, _ := openAIAdvancedSchedulerSettingSF.Do(openAIAdvancedSchedulerSettingKey, func() (any, error) {
		if cached, ok := openAIAdvancedSchedulerSettingCache.Load().(*cachedOpenAIAdvancedSchedulerSetting); ok && cached != nil && time.Now().UnixNano() < cached.expiresAt {
			return fromCache(cached), nil
		}

		settings := openAIAdvancedSchedulerRuntimeSettings{
			oauthSchedulingRateMultiplier: defaultOpenAIOAuthSchedulingRateMultiplier,
			weightOverrides:               map[string]float64{},
		}
		if repo := s.openAIAdvancedSchedulerSettingRepo(); repo != nil {
			dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openAIAdvancedSchedulerSettingDBTimeout)
			defer cancel()

			values, err := repo.GetMultiple(dbCtx, openAIAdvancedSchedulerRuntimeSettingKeys())
			if err != nil {
				slog.Warn("openai_advanced_scheduler_settings_batch_load_failed", "error", err)
				values = make(map[string]string)
				for _, key := range openAIAdvancedSchedulerRuntimeSettingKeys() {
					if value, valueErr := repo.GetValue(dbCtx, key); valueErr == nil {
						values[key] = value
					}
				}
			}
			settings.lowUpstreamRatePriorityEnabled = strings.EqualFold(strings.TrimSpace(values[SettingKeyOpenAILowUpstreamRatePriorityEnabled]), "true")
			settings.oauthSchedulingRateMultiplier = parseOpenAIOAuthSchedulingRateMultiplier(values[SettingKeyOpenAIOAuthSchedulingRateMultiplier])
			settings.enabled = strings.EqualFold(strings.TrimSpace(values[openAIAdvancedSchedulerSettingKey]), "true")
			settings.stickyWeightedEnabled = strings.EqualFold(strings.TrimSpace(values[SettingKeyOpenAIAdvancedSchedulerStickyWeightedEnabled]), "true")
			settings.subscriptionPriorityEnabled = strings.EqualFold(strings.TrimSpace(values[SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled]), "true")
			settings.lbTopKOverride = parsePositiveIntOverride(values[SettingKeyOpenAIAdvancedSchedulerLBTopK])
			settings.weightOverrides = parseOpenAIAdvancedSchedulerWeightOverrides(values)
		}

		openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
			lowUpstreamRatePriorityEnabled: settings.lowUpstreamRatePriorityEnabled,
			oauthSchedulingRateMultiplier:  settings.oauthSchedulingRateMultiplier,
			enabled:                        settings.enabled,
			stickyWeightedEnabled:          settings.stickyWeightedEnabled,
			subscriptionPriorityEnabled:    settings.subscriptionPriorityEnabled,
			lbTopKOverride:                 settings.lbTopKOverride,
			weightOverrides:                cloneOpenAIAdvancedSchedulerWeightOverrides(settings.weightOverrides),
			expiresAt:                      time.Now().Add(openAIAdvancedSchedulerSettingCacheTTL).UnixNano(),
		})
		return settings, nil
	})
	settings, _ := result.(openAIAdvancedSchedulerRuntimeSettings)
	if settings.oauthSchedulingRateMultiplier == 0 {
		// Zero is a valid explicit reference multiplier. Only restore the default
		// when no setting repository was available and the zero value leaked out.
		if s == nil || s.openAIAdvancedSchedulerSettingRepo() == nil {
			settings.oauthSchedulingRateMultiplier = defaultOpenAIOAuthSchedulingRateMultiplier
		}
	}
	return settings
}

func (s *OpenAIGatewayService) isOpenAILowUpstreamRatePriorityEnabled(ctx context.Context) bool {
	settings := s.openAIAdvancedSchedulerRuntimeSettings(ctx)
	return !settings.enabled && settings.lowUpstreamRatePriorityEnabled
}

func (s *OpenAIGatewayService) openAIOAuthSchedulingRateMultiplier(ctx context.Context) float64 {
	return s.openAIAdvancedSchedulerRuntimeSettings(ctx).oauthSchedulingRateMultiplier
}

func (s *OpenAIGatewayService) isOpenAIAdvancedSchedulerStickyWeightedEnabled(ctx context.Context) bool {
	settings := s.openAIAdvancedSchedulerRuntimeSettings(ctx)
	return settings.enabled && settings.stickyWeightedEnabled
}

func (s *OpenAIGatewayService) isOpenAIAdvancedSchedulerSubscriptionPriorityEnabled(ctx context.Context) bool {
	settings := s.openAIAdvancedSchedulerRuntimeSettings(ctx)
	return settings.enabled && settings.subscriptionPriorityEnabled
}

func openAIAdvancedSchedulerRuntimeSettingKeys() []string {
	keys := []string{
		SettingKeyOpenAILowUpstreamRatePriorityEnabled,
		SettingKeyOpenAIOAuthSchedulingRateMultiplier,
		openAIAdvancedSchedulerSettingKey,
		SettingKeyOpenAIAdvancedSchedulerStickyWeightedEnabled,
		SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled,
		SettingKeyOpenAIAdvancedSchedulerLBTopK,
	}
	for _, spec := range openAIAdvancedSchedulerWeightOverrideSpecs() {
		keys = append(keys, spec.key)
	}
	return keys
}

type openAIAdvancedSchedulerWeightOverrideSpec struct {
	key  string
	name string
}

func openAIAdvancedSchedulerWeightOverrideSpecs() []openAIAdvancedSchedulerWeightOverrideSpec {
	return []openAIAdvancedSchedulerWeightOverrideSpec{
		{key: SettingKeyOpenAIAdvancedSchedulerWeightPriority, name: "priority"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightLoad, name: "load"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightQueue, name: "queue"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightErrorRate, name: "error_rate"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightTTFT, name: "ttft"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightReset, name: "reset"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightQuotaHeadroom, name: "quota_headroom"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightUpstreamCost, name: "upstream_cost"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightPreviousResponse, name: "previous_response"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightSessionSticky, name: "session_sticky"},
	}
}

func parsePositiveIntOverride(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func parseOpenAIAdvancedSchedulerWeightOverrides(values map[string]string) map[string]float64 {
	overrides := map[string]float64{}
	for _, spec := range openAIAdvancedSchedulerWeightOverrideSpecs() {
		raw := strings.TrimSpace(values[spec.key])
		if raw == "" {
			continue
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		overrides[spec.name] = value
	}
	return overrides
}

func cloneOpenAIAdvancedSchedulerWeightOverrides(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (s *OpenAIGatewayService) openAIWSLBTopKForRequest(ctx context.Context) int {
	base := s.openAIWSLBTopK()
	settings := s.openAIAdvancedSchedulerRuntimeSettings(ctx)
	if settings.enabled && settings.lbTopKOverride > 0 {
		return settings.lbTopKOverride
	}
	return base
}

func (s *OpenAIGatewayService) openAIWSSchedulerWeightsForRequest(ctx context.Context) GatewayOpenAIWSSchedulerScoreWeightsView {
	weights := s.openAIWSSchedulerWeights()
	settings := s.openAIAdvancedSchedulerRuntimeSettings(ctx)
	if !settings.enabled {
		return weights
	}
	overridden := applyOpenAIAdvancedSchedulerWeightOverrides(weights, settings.weightOverrides)
	if !overridden.configWeights().IsValid() {
		return weights
	}
	return overridden
}

func applyOpenAIAdvancedSchedulerWeightOverrides(weights GatewayOpenAIWSSchedulerScoreWeightsView, overrides map[string]float64) GatewayOpenAIWSSchedulerScoreWeightsView {
	for key, value := range overrides {
		switch key {
		case "priority":
			weights.Priority = value
		case "load":
			weights.Load = value
		case "queue":
			weights.Queue = value
		case "error_rate":
			weights.ErrorRate = value
		case "ttft":
			weights.TTFT = value
		case "reset":
			weights.Reset = value
		case "quota_headroom":
			weights.QuotaHeadroom = value
		case "upstream_cost":
			weights.UpstreamCost = value
		case "previous_response":
			weights.Previous = value
		case "session_sticky":
			weights.SessionSticky = value
		}
	}
	return weights
}

func (s *defaultOpenAIAccountScheduler) buildOpenAIAccountLoadPlan(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	filtered []*Account,
	loadMap map[int64]*AccountLoadInfo,
) openAIAccountLoadPlan {
	allCandidates := make([]openAIAccountCandidateScore, 0, len(filtered))
	for _, account := range filtered {
		loadInfo, loadKnown := loadMap[account.ID]
		if !loadKnown || loadInfo == nil {
			loadInfo = &AccountLoadInfo{AccountID: account.ID}
			loadKnown = false
		}
		errorRate, ttft, hasTTFT := 0.0, 0.0, false
		if s.stats != nil {
			errorRate, ttft, hasTTFT = s.stats.snapshot(account.ID)
		}
		allCandidates = append(allCandidates, openAIAccountCandidateScore{
			account: account, loadInfo: loadInfo, loadKnown: loadKnown,
			errorRate: errorRate, ttft: ttft, hasTTFT: hasTTFT,
		})
	}

	candidates := allCandidates
	staleSnapshotCompactRetry := make([]openAIAccountCandidateScore, 0, len(allCandidates))
	if req.RequireCompact {
		candidates = make([]openAIAccountCandidateScore, 0, len(allCandidates))
		for _, candidate := range allCandidates {
			if openAICompactSupportTier(candidate.account) == 0 {
				staleSnapshotCompactRetry = append(staleSnapshotCompactRetry, candidate)
				continue
			}
			candidates = append(candidates, candidate)
		}
	}

	plan := openAIAccountLoadPlan{
		allCandidates: allCandidates, candidates: candidates,
		staleSnapshotCompactRetry: staleSnapshotCompactRetry,
		candidateCount:            len(candidates),
	}
	if len(candidates) == 0 {
		plan.selectionOrder = s.buildOpenAISelectionOrder(req, plan)
		return plan
	}

	minPriority := accountPriorityForRequest(ctx, candidates[0].account)
	maxPriority := minPriority
	maxWaiting := 1
	loadRateSum, loadRateSumSquares := 0.0, 0.0
	minTTFT, maxTTFT := 0.0, 0.0
	hasTTFTSample := false
	for i := range candidates {
		candidate := &candidates[i]
		candidate.priority = accountPriorityForRequest(ctx, candidate.account)
		if candidate.priority < minPriority {
			minPriority = candidate.priority
		}
		if candidate.priority > maxPriority {
			maxPriority = candidate.priority
		}
		if candidate.loadInfo.WaitingCount > maxWaiting {
			maxWaiting = candidate.loadInfo.WaitingCount
		}
		if candidate.hasTTFT && candidate.ttft > 0 {
			if !hasTTFTSample {
				minTTFT, maxTTFT, hasTTFTSample = candidate.ttft, candidate.ttft, true
			} else {
				if candidate.ttft < minTTFT {
					minTTFT = candidate.ttft
				}
				if candidate.ttft > maxTTFT {
					maxTTFT = candidate.ttft
				}
			}
		}
		loadRate := float64(candidate.loadInfo.LoadRate)
		loadRateSum += loadRate
		loadRateSumSquares += loadRate * loadRate
	}
	plan.loadSkew = calcLoadSkewByMoments(loadRateSum, loadRateSumSquares, len(candidates))

	weights := s.service.openAIWSSchedulerWeightsForRequest(ctx)
	now := time.Now()
	upstreamCostFactors := map[int64]float64(nil)
	if req.UseUpstreamTokenCost && weights.UpstreamCost > 0 {
		costAccounts := make([]*Account, 0, len(candidates))
		for _, candidate := range candidates {
			costAccounts = append(costAccounts, candidate.account)
		}
		upstreamCostFactors = openAIUpstreamCostFactors(costAccounts, now, s.service.openAIOAuthSchedulingRateMultiplier(ctx))
		for _, factor := range upstreamCostFactors {
			if factor != openAIUpstreamCostNeutralFactor {
				plan.includeOverflowFallback = true
				break
			}
		}
	}

	minResetRemaining, maxResetRemaining := 0.0, 0.0
	hasResetSample := false
	if weights.Reset > 0 {
		for _, candidate := range candidates {
			end := candidate.account.SessionWindowEnd
			if end == nil || !now.Before(*end) {
				continue
			}
			remaining := end.Sub(now).Seconds()
			if !hasResetSample {
				minResetRemaining, maxResetRemaining, hasResetSample = remaining, remaining, true
				continue
			}
			if remaining < minResetRemaining {
				minResetRemaining = remaining
			}
			if remaining > maxResetRemaining {
				maxResetRemaining = remaining
			}
		}
	}

	for i := range candidates {
		item := &candidates[i]
		priorityFactor := 1.0
		if maxPriority > minPriority {
			priorityFactor = 1 - float64(item.priority-minPriority)/float64(maxPriority-minPriority)
		}
		loadFactor := 1 - clamp01(float64(item.loadInfo.LoadRate)/100.0)
		queueFactor := 1 - clamp01(float64(item.loadInfo.WaitingCount)/float64(maxWaiting))
		errorFactor := 1 - clamp01(item.errorRate)
		ttftFactor := 0.5
		if item.hasTTFT && hasTTFTSample && maxTTFT > minTTFT {
			ttftFactor = 1 - clamp01((item.ttft-minTTFT)/(maxTTFT-minTTFT))
		}
		resetFactor := 0.0
		if weights.Reset > 0 && hasResetSample {
			if end := item.account.SessionWindowEnd; end != nil && now.Before(*end) {
				if maxResetRemaining > minResetRemaining {
					resetFactor = 1 - clamp01((end.Sub(now).Seconds()-minResetRemaining)/(maxResetRemaining-minResetRemaining))
				} else {
					resetFactor = 1
				}
			}
		}
		quotaHeadroomFactor := 0.0
		if weights.QuotaHeadroom > 0 {
			quotaHeadroomFactor = openAIQuotaHeadroomFactor(item.account, now)
		}
		upstreamCostFactor := openAIUpstreamCostNeutralFactor
		if factor, ok := upstreamCostFactors[item.account.ID]; ok {
			upstreamCostFactor = factor
		}
		item.score = weights.Priority*priorityFactor +
			weights.Load*loadFactor + weights.Queue*queueFactor +
			weights.ErrorRate*errorFactor + weights.TTFT*ttftFactor +
			weights.Reset*resetFactor + weights.QuotaHeadroom*quotaHeadroomFactor +
			weights.UpstreamCost*(upstreamCostFactor-openAIUpstreamCostNeutralFactor)
		if req.StickyWeighted {
			if req.PreviousResponseCanMove && req.StickyPreviousAccountID > 0 && item.account.ID == req.StickyPreviousAccountID {
				item.score += weights.Previous
			}
			if req.StickyAccountID > 0 && item.account.ID == req.StickyAccountID {
				item.score += weights.SessionSticky
			}
		}
	}
	plan.candidates = candidates
	plan.topK = effectiveOpenAIHybridTopK(s.service.openAIWSLBTopKForRequest(ctx), len(candidates))
	plan.selectionOrder = s.buildOpenAISelectionOrder(req, plan)
	return plan
}

func (s *defaultOpenAIAccountScheduler) buildOpenAISelectionOrder(req OpenAIAccountScheduleRequest, plan openAIAccountLoadPlan) []openAIAccountCandidateScore {
	buildSelectionOrder := func(pool []openAIAccountCandidateScore) []openAIAccountCandidateScore {
		if len(pool) == 0 || plan.topK <= 0 {
			return nil
		}
		groupTopK := plan.topK
		if groupTopK > len(pool) {
			groupTopK = len(pool)
		}
		ranked := selectTopKOpenAICandidates(pool, groupTopK)
		var primary []openAIAccountCandidateScore
		if req.StickyWeighted {
			for _, stickyID := range []int64{req.StickyPreviousAccountID, req.StickyAccountID} {
				if stickyID <= 0 {
					continue
				}
				for i, candidate := range ranked {
					if candidate.account != nil && candidate.account.ID == stickyID {
						primary = append([]openAIAccountCandidateScore{candidate}, ranked[:i]...)
						primary = append(primary, ranked[i+1:]...)
						break
					}
				}
				if len(primary) > 0 {
					break
				}
			}
		}
		if len(primary) == 0 {
			primary = buildOpenAIWeightedSelectionOrder(ranked, req)
		}
		if !plan.includeOverflowFallback || groupTopK >= len(pool) {
			return primary
		}
		selected := make(map[int64]struct{}, len(primary))
		for _, candidate := range primary {
			selected[candidate.account.ID] = struct{}{}
		}
		overflow := make([]openAIAccountCandidateScore, 0, len(pool)-len(primary))
		for _, candidate := range pool {
			if _, ok := selected[candidate.account.ID]; !ok {
				overflow = append(overflow, candidate)
			}
		}
		sort.Slice(overflow, func(i, j int) bool { return isOpenAIAccountCandidateBetter(overflow[i], overflow[j]) })
		return append(primary, overflow...)
	}

	if req.RequireCompact {
		supported := make([]openAIAccountCandidateScore, 0, len(plan.candidates))
		unknown := make([]openAIAccountCandidateScore, 0, len(plan.candidates))
		for _, candidate := range plan.candidates {
			switch openAICompactSupportTier(candidate.account) {
			case 2:
				supported = append(supported, candidate)
			case 1:
				unknown = append(unknown, candidate)
			}
		}
		selectionOrder := make([]openAIAccountCandidateScore, 0, len(plan.allCandidates))
		selectionOrder = append(selectionOrder, buildSelectionOrder(supported)...)
		selectionOrder = append(selectionOrder, buildSelectionOrder(unknown)...)
		if len(plan.staleSnapshotCompactRetry) > 0 && s.service != nil && s.service.schedulerSnapshot != nil {
			selectionOrder = append(selectionOrder, sortOpenAICompactRetryCandidates(plan.staleSnapshotCompactRetry)...)
		}
		return selectionOrder
	}
	return buildSelectionOrder(plan.candidates)
}

func sortOpenAICompactRetryCandidates(pool []openAIAccountCandidateScore) []openAIAccountCandidateScore {
	if len(pool) == 0 {
		return nil
	}
	ordered := append([]openAIAccountCandidateScore(nil), pool...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if openAICandidatePriority(a) != openAICandidatePriority(b) {
			return openAICandidatePriority(a) < openAICandidatePriority(b)
		}
		if a.loadInfo.LoadRate != b.loadInfo.LoadRate {
			return a.loadInfo.LoadRate < b.loadInfo.LoadRate
		}
		if a.loadInfo.WaitingCount != b.loadInfo.WaitingCount {
			return a.loadInfo.WaitingCount < b.loadInfo.WaitingCount
		}
		return compareOpenAIAccountLastUsed(a.account, b.account) < 0
	})
	return ordered
}

func buildOpenAIRuntimeSelectionOrder(req OpenAIAccountScheduleRequest, plan openAIAccountLoadPlan) []openAIAccountCandidateScore {
	if plan.includeOverflowFallback {
		return plan.selectionOrder
	}
	appendPool := func(dst []openAIAccountCandidateScore, pool []openAIAccountCandidateScore) []openAIAccountCandidateScore {
		if len(pool) == 0 || plan.topK <= 0 {
			return dst
		}
		_, acquireOrder := buildOpenAIPriorityLayeredSelectionOrders(pool, plan.topK, req)
		return append(dst, acquireOrder...)
	}
	if req.RequireCompact {
		supported := make([]openAIAccountCandidateScore, 0, len(plan.candidates))
		unknown := make([]openAIAccountCandidateScore, 0, len(plan.candidates))
		for _, candidate := range plan.candidates {
			switch openAICompactSupportTier(candidate.account) {
			case 2:
				supported = append(supported, candidate)
			case 1:
				unknown = append(unknown, candidate)
			}
		}
		order := make([]openAIAccountCandidateScore, 0, len(plan.allCandidates))
		order = appendPool(order, supported)
		order = appendPool(order, unknown)
		if len(plan.staleSnapshotCompactRetry) > 0 {
			order = append(order, sortOpenAICompactRetryCandidates(plan.staleSnapshotCompactRetry)...)
		}
		return order
	}
	return appendPool(nil, plan.candidates)
}

func partitionOpenAIChatGPTSubscriptionAccounts(accounts []*Account) ([]*Account, []*Account) {
	subscriptionAccounts := make([]*Account, 0, len(accounts))
	regularAccounts := make([]*Account, 0, len(accounts))
	for _, account := range accounts {
		if account != nil && account.IsOpenAIChatGPTSubscription() {
			subscriptionAccounts = append(subscriptionAccounts, account)
			continue
		}
		regularAccounts = append(regularAccounts, account)
	}
	return subscriptionAccounts, regularAccounts
}

func (s *defaultOpenAIAccountScheduler) trySelectByLoadBalancePool(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	filtered []*Account,
	loadMap map[int64]*AccountLoadInfo,
	budget *openAISelectionProbeBudget,
) openAIAccountLoadSelectionAttempt {
	plan := s.buildOpenAIAccountLoadPlan(ctx, req, filtered, loadMap)
	if openAICostOverflowExpanded(req, plan) {
		budget.enableLimit()
	}
	runtimeOrder := buildOpenAIRuntimeSelectionOrder(req, plan)
	attempt := openAIAccountLoadSelectionAttempt{
		selectionOrder: runtimeOrder,
		candidateCount: plan.candidateCount,
		topK:           plan.topK,
		loadSkew:       plan.loadSkew,
	}
	if req.RequireCompact && len(plan.candidates) == 0 && len(plan.staleSnapshotCompactRetry) == 0 {
		attempt.noCompactCandidates = true
		attempt.err = ErrNoAvailableCompactAccounts
		return attempt
	}
	if req.RequireCompact && len(runtimeOrder) == 0 && s.service.schedulerSnapshot == nil {
		attempt.noCompactCandidates = true
		attempt.err = ErrNoAvailableCompactAccounts
		return attempt
	}
	if len(runtimeOrder) == 0 {
		attempt.compactBlocked = req.RequireCompact && len(plan.allCandidates) > 0
		return attempt
	}

	result, compactBlocked, acquireErr := s.tryAcquireOpenAISelectionOrderWithBudget(ctx, req, runtimeOrder, budget)
	attempt.compactBlocked = compactBlocked
	if acquireErr != nil {
		attempt.err = acquireErr
		return attempt
	}
	if result != nil {
		attempt.result = result
		return attempt
	}

	if s.service.concurrencyService != nil && !budget.acquireExhausted() {
		loadReq := buildOpenAIAccountLoadRequest(filtered)
		if freshLoadMap, loadErr := s.service.concurrencyService.GetAccountsLoadBatchFresh(ctx, loadReq); loadErr == nil {
			freshPlan := s.buildOpenAIAccountLoadPlan(ctx, req, filtered, freshLoadMap)
			if openAICostOverflowExpanded(req, freshPlan) {
				budget.enableLimit()
			}
			freshOrder := buildOpenAIRuntimeSelectionOrder(req, freshPlan)
			if len(freshOrder) > 0 {
				freshResult, freshCompactBlocked, freshAcquireErr := s.tryAcquireOpenAISelectionOrderWithBudget(ctx, req, freshOrder, budget)
				if freshAcquireErr != nil {
					attempt.err = freshAcquireErr
					return attempt
				}
				attempt.compactBlocked = attempt.compactBlocked || freshCompactBlocked
				attempt.selectionOrder = freshOrder
				attempt.candidateCount = freshPlan.candidateCount
				attempt.topK = freshPlan.topK
				attempt.loadSkew = freshPlan.loadSkew
				if freshResult != nil {
					attempt.result = freshResult
					return attempt
				}
			}
		}
	}
	return attempt
}

func (s *defaultOpenAIAccountScheduler) tryAcquireOpenAISelectionOrder(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	selectionOrder []openAIAccountCandidateScore,
) (*AccountSelectionResult, bool, error) {
	budget := newOpenAISelectionProbeBudget()
	budget.enableLimit()
	return s.tryAcquireOpenAISelectionOrderWithBudget(ctx, req, selectionOrder, budget)
}

func (s *defaultOpenAIAccountScheduler) tryAcquireOpenAISelectionOrderWithBudget(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	selectionOrder []openAIAccountCandidateScore,
	budget *openAISelectionProbeBudget,
) (*AccountSelectionResult, bool, error) {
	compactBlocked := false
	release := func(result *AcquireResult) {
		if result != nil && result.ReleaseFunc != nil {
			result.ReleaseFunc()
		}
	}
	for i := 0; i < len(selectionOrder); i++ {
		candidate := selectionOrder[i]
		if candidate.account == nil {
			continue
		}
		if candidate.loadKnown && candidate.account.Concurrency > 0 &&
			candidate.loadInfo != nil && candidate.loadInfo.CurrentConcurrency >= candidate.account.Concurrency {
			continue
		}

		result, attempted, acquireErr := s.tryAcquireOpenAIAccountSlot(ctx, candidate.account.ID, candidate.account.Concurrency, budget)
		if !attempted {
			break
		}
		if acquireErr != nil {
			return nil, compactBlocked, acquireErr
		}
		if result == nil || !result.Acquired {
			continue
		}

		fresh := s.service.resolveFreshSchedulableOpenAIAccount(ctx, candidate.account, req.RequestedModel, false, req.RequiredCapability, req.Platform)
		if fresh == nil || !s.isAccountTransportCompatible(fresh, req.RequiredTransport) || !s.isAccountRequestCompatible(ctx, fresh, req) {
			release(result)
			continue
		}
		if !s.consumeOpenAISelectionDBRecheck(budget) {
			release(result)
			break
		}
		fresh = s.service.recheckSelectedOpenAIAccountFromDB(ctx, fresh, req.GroupID, req.Platform, req.RequestedModel, false, req.RequiredCapability)
		if fresh == nil || !s.isAccountTransportCompatible(fresh, req.RequiredTransport) || !s.isAccountRequestCompatible(ctx, fresh, req) {
			release(result)
			continue
		}
		if req.RequireCompact && openAICompactSupportTier(fresh) == 0 {
			compactBlocked = true
			release(result)
			continue
		}

		if fresh.Concurrency != candidate.account.Concurrency {
			release(result)
			result, attempted, acquireErr = s.tryAcquireOpenAIAccountSlot(ctx, fresh.ID, fresh.Concurrency, budget)
			if !attempted {
				continue
			}
			if acquireErr != nil {
				return nil, compactBlocked, acquireErr
			}
			if result == nil || !result.Acquired {
				continue
			}
		}
		if req.SessionHash != "" && !req.PreserveStickyBinding {
			_ = s.service.BindStickySession(ctx, req.GroupID, req.SessionHash, fresh.ID)
		}
		return &AccountSelectionResult{
			Account:     fresh,
			Acquired:    true,
			ReleaseFunc: result.ReleaseFunc,
		}, compactBlocked, nil
	}
	return nil, compactBlocked, nil
}

func (s *defaultOpenAIAccountScheduler) tryAcquireOpenAIAccountSlot(
	ctx context.Context,
	accountID int64,
	maxConcurrency int,
	budget *openAISelectionProbeBudget,
) (*AcquireResult, bool, error) {
	if s.service.concurrencyService != nil && maxConcurrency > 0 && !budget.recordAcquire(accountID) {
		return nil, false, nil
	}
	result, err := s.service.tryAcquireAccountSlot(ctx, accountID, maxConcurrency)
	return result, true, err
}

func (s *defaultOpenAIAccountScheduler) consumeOpenAISelectionDBRecheck(budget *openAISelectionProbeBudget) bool {
	if s.service.schedulerSnapshot == nil || s.service.accountRepo == nil {
		return true
	}
	return budget.recordRecheck()
}

func (s *defaultOpenAIAccountScheduler) tryFallbackToWeightedSticky(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
) (*AccountSelectionResult, error) {
	if !req.StickyWeighted {
		return nil, nil
	}
	for _, accountID := range []int64{req.StickyPreviousAccountID, req.StickyAccountID} {
		if accountID <= 0 {
			continue
		}
		if req.ExcludedIDs != nil {
			if _, excluded := req.ExcludedIDs[accountID]; excluded {
				continue
			}
		}
		account, err := s.service.getSchedulableAccount(ctx, accountID)
		if err != nil || account == nil {
			continue
		}
		if !s.isAccountRequestCompatible(ctx, account, req) || !s.isAccountTransportCompatible(account, req.RequiredTransport) {
			continue
		}
		account = s.service.recheckSelectedOpenAIAccountFromDB(ctx, account, req.GroupID, req.Platform, req.RequestedModel, req.RequireCompact, req.RequiredCapability)
		if account == nil || !s.service.isOpenAIAccountInRequestGroup(account, req.GroupID) {
			if accountID == req.StickyAccountID && strings.TrimSpace(req.SessionHash) != "" {
				_ = s.service.deleteStickySessionAccountID(ctx, req.GroupID, req.SessionHash)
			}
			continue
		}
		if !s.isAccountRequestCompatible(ctx, account, req) || !s.isAccountTransportCompatible(account, req.RequiredTransport) {
			continue
		}
		if req.RequireCompact && openAICompactSupportTier(account) == 0 {
			continue
		}
		result, acquireErr := s.service.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
		if acquireErr != nil {
			return nil, acquireErr
		}
		if result != nil && result.Acquired {
			if req.SessionHash != "" && !req.PreserveStickyBinding {
				_ = s.service.BindStickySession(ctx, req.GroupID, req.SessionHash, account.ID)
			}
			return &AccountSelectionResult{Account: account, Acquired: true, ReleaseFunc: result.ReleaseFunc}, nil
		}
		if s.service.concurrencyService != nil {
			cfg := s.service.schedulingConfig()
			return &AccountSelectionResult{
				Account: account,
				WaitPlan: &AccountWaitPlan{
					AccountID:      account.ID,
					MaxConcurrency: account.Concurrency,
					Timeout:        cfg.StickySessionWaitTimeout,
					MaxWaiting:     cfg.StickySessionMaxWaiting,
				},
			}, nil
		}
	}
	return nil, nil
}

func openAICostOverflowExpanded(req OpenAIAccountScheduleRequest, plan openAIAccountLoadPlan) bool {
	if !plan.includeOverflowFallback || plan.topK <= 0 {
		return false
	}
	if !req.RequireCompact {
		return len(plan.candidates) > plan.topK
	}
	supported, unknown := 0, 0
	for _, candidate := range plan.candidates {
		switch openAICompactSupportTier(candidate.account) {
		case 2:
			supported++
		case 1:
			unknown++
		}
	}
	return supported > plan.topK || unknown > plan.topK
}

func buildOpenAIAccountLoadRequest(accounts []*Account) []AccountWithConcurrency {
	loadReq := make([]AccountWithConcurrency, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		loadReq = append(loadReq, AccountWithConcurrency{ID: account.ID, MaxConcurrency: account.EffectiveLoadFactor()})
	}
	return loadReq
}

func (s *defaultOpenAIAccountScheduler) finishLoadBalanceSelectionFallback(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	attempt openAIAccountLoadSelectionAttempt,
	budget *openAISelectionProbeBudget,
) (*AccountSelectionResult, int, int, float64, error) {
	candidateCount, topK, loadSkew := attempt.candidateCount, attempt.topK, attempt.loadSkew
	if len(attempt.selectionOrder) == 0 {
		return nil, candidateCount, topK, loadSkew, noAvailableOpenAISelectionError(req.RequestedModel, attempt.compactBlocked)
	}
	if stickyFallback, stickyErr := s.tryFallbackToWeightedSticky(ctx, req); stickyErr != nil {
		return nil, candidateCount, topK, loadSkew, stickyErr
	} else if stickyFallback != nil {
		return stickyFallback, candidateCount, topK, loadSkew, nil
	}

	cfg := s.service.schedulingConfig()
	compactBlocked := attempt.compactBlocked
	passes := 1
	if budget != nil && budget.limited {
		passes = 4
	}
	for pass := 0; pass < passes; pass++ {
		wantAttempted := pass == 1 || pass == 3
		wantKnownFull := pass >= 2
		for _, candidate := range attempt.selectionOrder {
			if candidate.account == nil {
				continue
			}
			if budget != nil && budget.limited {
				knownFull := candidate.loadKnown && candidate.account.Concurrency > 0 && candidate.loadInfo != nil &&
					candidate.loadInfo.CurrentConcurrency >= candidate.account.Concurrency
				if budget.wasAttempted(candidate.account.ID) != wantAttempted || knownFull != wantKnownFull {
					continue
				}
			}
			fresh := s.service.resolveFreshSchedulableOpenAIAccount(ctx, candidate.account, req.RequestedModel, false, req.RequiredCapability, req.Platform)
			if fresh == nil || !s.isAccountTransportCompatible(fresh, req.RequiredTransport) || !s.isAccountRequestCompatible(ctx, fresh, req) {
				continue
			}
			if !s.consumeOpenAISelectionDBRecheck(budget) {
				return nil, candidateCount, topK, loadSkew, noAvailableOpenAISelectionError(req.RequestedModel, compactBlocked)
			}
			fresh = s.service.recheckSelectedOpenAIAccountFromDB(ctx, fresh, req.GroupID, req.Platform, req.RequestedModel, false, req.RequiredCapability)
			if fresh == nil || !s.isAccountTransportCompatible(fresh, req.RequiredTransport) || !s.isAccountRequestCompatible(ctx, fresh, req) {
				continue
			}
			if req.RequireCompact && openAICompactSupportTier(fresh) == 0 {
				compactBlocked = true
				continue
			}
			return &AccountSelectionResult{
				Account: fresh,
				WaitPlan: &AccountWaitPlan{
					AccountID:      fresh.ID,
					MaxConcurrency: fresh.Concurrency,
					Timeout:        cfg.FallbackWaitTimeout,
					MaxWaiting:     cfg.FallbackMaxWaiting,
				},
			}, candidateCount, topK, loadSkew, nil
		}
	}
	return nil, candidateCount, topK, loadSkew, noAvailableOpenAISelectionError(req.RequestedModel, compactBlocked)
}

type OpenAIAccountSchedulerScoreSnapshot struct {
	BaseScore             float64
	StickyScore           float64
	StickyScoreInfinity   bool
	StickyWeightedEnabled bool
}

func (s *RateLimitService) BuildOpenAIAccountSchedulerScoreSnapshot(
	ctx context.Context,
	accounts []*Account,
	loadMap map[int64]*AccountLoadInfo,
) map[int64]OpenAIAccountSchedulerScoreSnapshot {
	gateway := &OpenAIGatewayService{cfg: nil, rateLimitService: s}
	if s != nil {
		gateway.cfg = s.cfg
	}
	return buildOpenAIAccountSchedulerScoreSnapshot(
		accounts,
		loadMap,
		gateway.openAIWSSchedulerWeightsForRequest(ctx),
		gateway.isOpenAIAdvancedSchedulerStickyWeightedEnabled(ctx),
		gateway.openAIOAuthSchedulingRateMultiplier(ctx),
	)
}

func BuildOpenAIAccountSchedulerScoreSnapshot(
	accounts []*Account,
	loadMap map[int64]*AccountLoadInfo,
) map[int64]OpenAIAccountSchedulerScoreSnapshot {
	gateway := &OpenAIGatewayService{}
	return buildOpenAIAccountSchedulerScoreSnapshot(accounts, loadMap, gateway.openAIWSSchedulerWeights(), false, defaultOpenAIOAuthSchedulingRateMultiplier)
}

func buildOpenAIAccountSchedulerScoreSnapshot(
	accounts []*Account,
	loadMap map[int64]*AccountLoadInfo,
	weights GatewayOpenAIWSSchedulerScoreWeightsView,
	stickyWeightedEnabled bool,
	oauthSchedulingRateMultiplier float64,
) map[int64]OpenAIAccountSchedulerScoreSnapshot {
	if len(accounts) == 0 {
		return nil
	}
	candidates := make([]openAIAccountCandidateScore, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		loadInfo := loadMap[account.ID]
		if loadInfo == nil {
			loadInfo = &AccountLoadInfo{AccountID: account.ID}
		}
		candidates = append(candidates, openAIAccountCandidateScore{account: account, loadInfo: loadInfo})
	}
	if len(candidates) == 0 {
		return nil
	}

	minPriority := openAIAccountSchedulingPriority(candidates[0].account)
	maxPriority := minPriority
	maxWaiting := 1
	for i := range candidates {
		candidate := &candidates[i]
		candidate.priority = openAIAccountSchedulingPriority(candidate.account)
		if candidate.priority < minPriority {
			minPriority = candidate.priority
		}
		if candidate.priority > maxPriority {
			maxPriority = candidate.priority
		}
		if candidate.loadInfo.WaitingCount > maxWaiting {
			maxWaiting = candidate.loadInfo.WaitingCount
		}
	}

	minResetRemaining, maxResetRemaining := 0.0, 0.0
	hasResetSample := false
	now := time.Now()
	upstreamCostFactors := map[int64]float64(nil)
	if weights.UpstreamCost > 0 {
		costAccounts := make([]*Account, 0, len(candidates))
		for _, candidate := range candidates {
			costAccounts = append(costAccounts, candidate.account)
		}
		upstreamCostFactors = openAIUpstreamCostFactors(costAccounts, now, oauthSchedulingRateMultiplier)
	}
	if weights.Reset > 0 {
		for _, candidate := range candidates {
			end := candidate.account.SessionWindowEnd
			if end == nil || !now.Before(*end) {
				continue
			}
			remaining := end.Sub(now).Seconds()
			if !hasResetSample {
				minResetRemaining, maxResetRemaining = remaining, remaining
				hasResetSample = true
				continue
			}
			if remaining < minResetRemaining {
				minResetRemaining = remaining
			}
			if remaining > maxResetRemaining {
				maxResetRemaining = remaining
			}
		}
	}

	result := make(map[int64]OpenAIAccountSchedulerScoreSnapshot, len(candidates))
	for _, candidate := range candidates {
		priorityFactor := 1.0
		if maxPriority > minPriority {
			priorityFactor = 1 - float64(candidate.priority-minPriority)/float64(maxPriority-minPriority)
		}
		loadFactor := 1 - clamp01(float64(candidate.loadInfo.LoadRate)/100.0)
		queueFactor := 1 - clamp01(float64(candidate.loadInfo.WaitingCount)/float64(maxWaiting))
		resetFactor := 0.0
		if weights.Reset > 0 && hasResetSample {
			if end := candidate.account.SessionWindowEnd; end != nil && now.Before(*end) {
				if maxResetRemaining > minResetRemaining {
					resetFactor = 1 - clamp01((end.Sub(now).Seconds()-minResetRemaining)/(maxResetRemaining-minResetRemaining))
				} else {
					resetFactor = 1
				}
			}
		}
		quotaHeadroomFactor := 0.0
		if weights.QuotaHeadroom > 0 {
			quotaHeadroomFactor = openAIQuotaHeadroomFactor(candidate.account, now)
		}
		upstreamCostFactor := openAIUpstreamCostNeutralFactor
		if factor, ok := upstreamCostFactors[candidate.account.ID]; ok {
			upstreamCostFactor = factor
		}
		baseScore := weights.Priority*priorityFactor +
			weights.Load*loadFactor +
			weights.Queue*queueFactor +
			weights.ErrorRate +
			weights.TTFT*0.5 +
			weights.Reset*resetFactor +
			weights.QuotaHeadroom*quotaHeadroomFactor +
			weights.UpstreamCost*(upstreamCostFactor-openAIUpstreamCostNeutralFactor)
		score := OpenAIAccountSchedulerScoreSnapshot{
			BaseScore:             baseScore,
			StickyWeightedEnabled: stickyWeightedEnabled,
			StickyScoreInfinity:   !stickyWeightedEnabled,
		}
		if stickyWeightedEnabled {
			score.StickyScore = baseScore + weights.Previous + weights.SessionSticky
		}
		result[candidate.account.ID] = score
	}
	return result
}

func openAIUpstreamCostFactors(accounts []*Account, now time.Time, oauthSchedulingRateMultiplier float64) map[int64]float64 {
	type rateSample struct {
		accountID int64
		rate      float64
	}

	factors := make(map[int64]float64, len(accounts))
	samples := make([]rateSample, 0, len(accounts))
	eligibleCount := 0
	for _, account := range accounts {
		if account == nil {
			continue
		}
		factors[account.ID] = openAIUpstreamCostNeutralFactor
		if !account.IsOpenAIApiKey() && !account.IsOpenAIOAuth() {
			continue
		}
		eligibleCount++
		if rate, ok := openAISchedulingRate(account, now, oauthSchedulingRateMultiplier); ok {
			samples = append(samples, rateSample{accountID: account.ID, rate: rate})
		}
	}
	if len(samples) < 2 || eligibleCount == 0 {
		return factors
	}

	allEqual := true
	positiveLogs := make([]float64, 0, len(samples))
	for i, sample := range samples {
		if i > 0 && sample.rate != samples[0].rate {
			allEqual = false
		}
		if sample.rate > 0 {
			positiveLogs = append(positiveLogs, math.Log(sample.rate))
		}
	}
	if allEqual || len(positiveLogs) == 0 {
		return factors
	}

	sort.Float64s(positiveLogs)
	middle := len(positiveLogs) / 2
	medianLog := positiveLogs[middle]
	if len(positiveLogs)%2 == 0 {
		medianLog = (positiveLogs[middle-1] + positiveLogs[middle]) / 2
	}
	center := math.Exp(medianLog)
	if center <= 0 || math.IsNaN(center) || math.IsInf(center, 0) {
		return factors
	}

	coverage := float64(len(samples)) / float64(eligibleCount)
	for _, sample := range samples {
		rawFactor := 1.0
		if sample.rate > 0 {
			rawFactor = 1 / (1 + sample.rate/center)
		}
		factors[sample.accountID] = clamp01(openAIUpstreamCostNeutralFactor + coverage*(rawFactor-openAIUpstreamCostNeutralFactor))
	}
	return factors
}

type openAILegacyUpstreamRateOrder struct {
	enabled bool
	rates   map[int64]float64
}

func newOpenAILegacyUpstreamRateOrder(accounts []*Account, now time.Time, oauthSchedulingRateMultiplier float64) openAILegacyUpstreamRateOrder {
	rates := make(map[int64]float64, len(accounts))
	var first float64
	distinct := false
	for _, account := range accounts {
		rate, ok := openAISchedulingRate(account, now, oauthSchedulingRateMultiplier)
		if !ok {
			continue
		}
		if len(rates) == 0 {
			first = rate
		} else if rate != first {
			distinct = true
		}
		rates[account.ID] = rate
	}
	return openAILegacyUpstreamRateOrder{enabled: len(rates) >= 2 && distinct, rates: rates}
}

func openAISchedulingRate(account *Account, now time.Time, oauthSchedulingRateMultiplier float64) (float64, bool) {
	if account != nil && account.IsOpenAIOAuth() {
		return oauthSchedulingRateMultiplier, true
	}
	return openAIFreshUpstreamBillingRate(account, now)
}

func (o openAILegacyUpstreamRateOrder) compare(a, b *Account) int {
	if !o.enabled || a == nil || b == nil {
		return 0
	}
	aRate, aKnown := o.rates[a.ID]
	bRate, bKnown := o.rates[b.ID]
	if aKnown != bKnown {
		if aKnown {
			return -1
		}
		return 1
	}
	if !aKnown || aRate == bRate {
		return 0
	}
	if aRate < bRate {
		return -1
	}
	return 1
}

func openAIFreshUpstreamBillingRate(account *Account, now time.Time) (float64, bool) {
	if !isUpstreamBillingProbeAccount(account) {
		return 0, false
	}
	snapshot := decodeUpstreamBillingProbeSnapshot(account.Extra)
	if snapshot == nil || (snapshot.Status != UpstreamBillingProbeStatusOK && snapshot.Status != UpstreamBillingProbeStatusFailed) ||
		snapshot.ReceivedAt == nil || snapshot.ReceivedAt.IsZero() {
		return 0, false
	}
	receivedAt := *snapshot.ReceivedAt
	freshUntil := snapshot.FreshUntil
	if freshUntil == nil && snapshot.Status == UpstreamBillingProbeStatusOK {
		interval := snapshot.NextProbeAt.Sub(receivedAt)
		if interval > 0 {
			freshUntil = probeTimePtr(receivedAt.Add(2 * interval))
		}
	}
	if freshUntil == nil || !freshUntil.After(receivedAt) || now.Before(receivedAt) || now.After(*freshUntil) {
		return 0, false
	}
	return upstreamBillingRateAt(snapshot.Data, now)
}

func openAIQuotaHeadroomFactor(account *Account, now time.Time) float64 {
	if account == nil || len(account.Extra) == 0 || openAIQuotaHeadroomSnapshotStale(account.Extra, now) {
		return openAIQuotaHeadroomNeutralFactor
	}
	primaryUsedPercent, ok := resolveAccountExtraNumber(account.Extra, "codex_primary_used_percent", "codex_7d_used_percent")
	if !ok || openAIQuotaWindowResetAny(account.Extra, now, "primary", "7d") {
		return openAIQuotaHeadroomNeutralFactor
	}
	factor := 1 - clamp01(primaryUsedPercent/100)
	if secondaryUsedPercent, ok := resolveAccountExtraNumber(account.Extra, "codex_secondary_used_percent", "codex_5h_used_percent"); ok &&
		!openAIQuotaWindowResetAny(account.Extra, now, "secondary", "5h") {
		secondaryRemaining := 1 - clamp01(secondaryUsedPercent/100)
		if secondaryRemaining < openAIQuotaHeadroomSecondaryLowRemain {
			factor *= openAIQuotaHeadroomNeutralFactor
		}
	}
	return factor
}

func openAIQuotaHeadroomSnapshotStale(extra map[string]any, now time.Time) bool {
	updatedRaw, ok := extra["codex_usage_updated_at"]
	if !ok {
		return true
	}
	updatedAt, err := parseTime(fmt.Sprint(updatedRaw))
	if err != nil {
		return true
	}
	return now.Sub(updatedAt) >= openAIQuotaHeadroomSnapshotStaleAfter
}

func openAIQuotaWindowResetAny(extra map[string]any, now time.Time, windows ...string) bool {
	for _, window := range windows {
		if openAIQuotaWindowReset(extra, window, now) {
			return true
		}
	}
	return false
}

func openAIQuotaWindowReset(extra map[string]any, window string, now time.Time) bool {
	if len(extra) == 0 {
		return false
	}
	if resetAtRaw, ok := extra["codex_"+window+"_reset_at"]; ok {
		if resetAt, err := parseTime(fmt.Sprint(resetAtRaw)); err == nil {
			return !now.Before(resetAt)
		}
	}
	resetAfter := parseExtraInt(extra["codex_"+window+"_reset_after_seconds"])
	if resetAfter <= 0 {
		return false
	}
	base := now
	if updatedRaw, ok := extra["codex_usage_updated_at"]; ok {
		if updatedAt, err := parseTime(fmt.Sprint(updatedRaw)); err == nil {
			base = updatedAt
		}
	}
	resetAt := base.Add(time.Duration(resetAfter) * time.Second)
	return !now.Before(resetAt)
}
