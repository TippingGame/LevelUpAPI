package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/cespare/xxhash/v2"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const (
	// ChatGPT internal API for OAuth accounts
	chatgptCodexURL = "https://chatgpt.com/backend-api/codex/responses"
	// OpenAI Platform API for API Key accounts (fallback)
	openaiPlatformAPIURL            = "https://api.openai.com/v1/responses"
	openaiPlatformAPIInputTokensURL = "https://api.openai.com/v1/responses/input_tokens"
	openaiStickySessionTTL          = time.Hour // 绮樻€т細璇漈TL
	codexCLIUserAgent               = "codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color"
	// codex_cli_only 鎷掔粷鏃跺崟涓姹傚ご鏃ュ織闀垮害涓婇檺锛堝瓧绗︼級
	// codex_cli_only rejected request header values are truncated for diagnostics.
	codexCLIOnlyHeaderValueMaxBytes = 256

	// OpenAI WS Mode reconnect retry limit after the first failed attempt.
	openAIWSReconnectRetryLimit = 5
	// 上游错误体只需用于错误提取和日志摘要，默认限制为 512KiB。
	openAIUpstreamErrorBodyReadLimit int64 = 512 << 10
	// OpenAI WS Mode default retry backoff values.
	openAIWSRetryBackoffInitialDefault = 120 * time.Millisecond
	openAIWSRetryBackoffMaxDefault     = 2 * time.Second
	openAIWSRetryJitterRatioDefault    = 0.2
	openAICompactSessionSeedKey        = "openai_compact_session_seed"
	codexCLIVersion                    = "0.144.1"
	// Codex rate limit snapshots are throttled to avoid write amplification.
	openAICodexSnapshotPersistMinInterval = 30 * time.Second
	// A missing or stale Codex usage snapshot is refreshed before scheduling.
	openAICodexAutoPauseStaleAfter = 2 * time.Hour
)

// OpenAI allowed headers whitelist (for non-passthrough).
var openaiAllowedHeaders = map[string]bool{
	"accept-language":       true,
	"content-type":          true,
	"conversation_id":       true,
	"user-agent":            true,
	"originator":            true,
	"session_id":            true,
	"x-codex-beta-features": true,
	"x-codex-turn-state":    true,
	"x-codex-turn-metadata": true,
	responsesLiteHeaderKey:  true,
}

// OpenAI passthrough allowed headers whitelist.
// Only low-risk request headers are forwarded in passthrough mode.
var openaiPassthroughAllowedHeaders = map[string]bool{
	"accept":                true,
	"accept-language":       true,
	"content-type":          true,
	"conversation_id":       true,
	"openai-beta":           true,
	"user-agent":            true,
	"originator":            true,
	"session_id":            true,
	"x-codex-beta-features": true,
	"x-codex-turn-state":    true,
	"x-codex-turn-metadata": true,
	responsesLiteHeaderKey:  true,
}

// codex_cli_only debug header whitelist for rejection diagnostics.
var codexCLIOnlyDebugHeaderWhitelist = []string{
	"User-Agent",
	"Content-Type",
	"Accept",
	"Accept-Language",
	"OpenAI-Beta",
	"Originator",
	"Session_ID",
	"Conversation_ID",
	"X-Request-ID",
	"X-Client-Request-ID",
	"X-Forwarded-For",
	"X-Real-IP",
}

// OpenAICodexUsageSnapshot represents Codex API usage limits from response headers
type OpenAICodexUsageSnapshot struct {
	PrimaryUsedPercent          *float64 `json:"primary_used_percent,omitempty"`
	PrimaryResetAfterSeconds    *int     `json:"primary_reset_after_seconds,omitempty"`
	PrimaryWindowMinutes        *int     `json:"primary_window_minutes,omitempty"`
	SecondaryUsedPercent        *float64 `json:"secondary_used_percent,omitempty"`
	SecondaryResetAfterSeconds  *int     `json:"secondary_reset_after_seconds,omitempty"`
	SecondaryWindowMinutes      *int     `json:"secondary_window_minutes,omitempty"`
	PrimaryOverSecondaryPercent *float64 `json:"primary_over_secondary_percent,omitempty"`
	UpdatedAt                   string   `json:"updated_at,omitempty"`
}

// NormalizedCodexLimits contains normalized 5h/7d rate limit data
type NormalizedCodexLimits struct {
	Used5hPercent   *float64
	Reset5hSeconds  *int
	Window5hMinutes *int
	Used7dPercent   *float64
	Reset7dSeconds  *int
	Window7dMinutes *int
}

// Normalize converts primary/secondary fields to canonical 5h/7d fields.
// Strategy: Compare window_minutes to determine which is 5h vs 7d.
// Returns nil if snapshot is nil or has no useful data.
func (s *OpenAICodexUsageSnapshot) Normalize() *NormalizedCodexLimits {
	if s == nil {
		return nil
	}

	result := &NormalizedCodexLimits{}

	primaryMins := 0
	secondaryMins := 0
	hasPrimaryWindow := false
	hasSecondaryWindow := false

	if s.PrimaryWindowMinutes != nil {
		primaryMins = *s.PrimaryWindowMinutes
		hasPrimaryWindow = true
	}
	if s.SecondaryWindowMinutes != nil {
		secondaryMins = *s.SecondaryWindowMinutes
		hasSecondaryWindow = true
	}

	// Determine mapping based on window_minutes
	use5hFromPrimary := false
	use7dFromPrimary := false

	if hasPrimaryWindow && hasSecondaryWindow {
		// Both known: smaller window is 5h, larger is 7d
		if primaryMins < secondaryMins {
			use5hFromPrimary = true
		} else {
			use7dFromPrimary = true
		}
	} else if hasPrimaryWindow {
		// Only primary known: classify by threshold (<=360 min = 6h -> 5h window)
		if primaryMins <= 360 {
			use5hFromPrimary = true
		} else {
			use7dFromPrimary = true
		}
	} else if hasSecondaryWindow {
		// Only secondary known: classify by threshold
		if secondaryMins <= 360 {
			// 5h from secondary, so primary (if any data) is 7d
			use7dFromPrimary = true
		} else {
			// 7d from secondary, so primary (if any data) is 5h
			use5hFromPrimary = true
		}
	} else {
		// No window_minutes: fall back to legacy assumption (primary=7d, secondary=5h)
		use7dFromPrimary = true
	}

	// Assign values
	if use5hFromPrimary {
		result.Used5hPercent = s.PrimaryUsedPercent
		result.Reset5hSeconds = s.PrimaryResetAfterSeconds
		result.Window5hMinutes = s.PrimaryWindowMinutes
		result.Used7dPercent = s.SecondaryUsedPercent
		result.Reset7dSeconds = s.SecondaryResetAfterSeconds
		result.Window7dMinutes = s.SecondaryWindowMinutes
	} else if use7dFromPrimary {
		result.Used7dPercent = s.PrimaryUsedPercent
		result.Reset7dSeconds = s.PrimaryResetAfterSeconds
		result.Window7dMinutes = s.PrimaryWindowMinutes
		result.Used5hPercent = s.SecondaryUsedPercent
		result.Reset5hSeconds = s.SecondaryResetAfterSeconds
		result.Window5hMinutes = s.SecondaryWindowMinutes
	}

	return result
}

// OpenAIUsage represents OpenAI API response usage
type OpenAIUsage struct {
	InputTokens               int    `json:"input_tokens"`
	TextInputTokens           int    `json:"text_input_tokens,omitempty"`
	ImageInputTokens          int    `json:"image_input_tokens,omitempty"`
	OutputTokens              int    `json:"output_tokens"`
	TextOutputTokens          int    `json:"text_output_tokens,omitempty"`
	CacheCreationInputTokens  int    `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens      int    `json:"cache_read_input_tokens,omitempty"`
	TextCacheReadInputTokens  int    `json:"text_cache_read_input_tokens,omitempty"`
	ImageCacheReadInputTokens int    `json:"image_cache_read_input_tokens,omitempty"`
	ImageOutputTokens         int    `json:"image_output_tokens,omitempty"`
	ImageCount                int    `json:"image_count,omitempty"`
	ResponseServiceTier       string `json:"-"`
	ResponseID                string `json:"-"`
	// Legacy aliases retained for LevelUp's direct response-handler tests.
	imageCount int
	usage      *OpenAIUsage
}

// OpenAIForwardResult represents the result of forwarding
type OpenAIForwardResult struct {
	RequestID  string
	ResponseID string
	Usage      OpenAIUsage
	Model      string // 鍘熷妯″瀷锛堢敤浜庡搷搴斿拰鏃ュ織鏄剧ず锛?	// BillingModel is the model used for cost calculation.
	// When non-empty, CalculateCost uses this instead of Model.
	// This is set by the Anthropic Messages conversion path where
	// the mapped upstream model differs from the client-facing model.
	BillingModel string
	// UpstreamModel is the actual model sent to the upstream provider after mapping.
	// Empty when no mapping was applied (requested model was used as-is).
	UpstreamModel string
	// UpstreamEndpoint is the normalized endpoint selected for this attempt.
	UpstreamEndpoint string
	// ServiceTier records the OpenAI Responses API service tier, e.g. "priority" / "flex".
	// Nil means the request did not specify a recognized tier.
	ServiceTier *string
	// ResponseServiceTier is kept in memory only so billing can prefer the
	// tier echoed by the upstream response when it is available.
	ResponseServiceTier string
	// ReasoningEffort is extracted from request body (reasoning.effort) or derived from model suffix.
	// Stored for usage records display; nil means not provided / not applicable.
	ReasoningEffort *string
	Stream          bool
	OpenAIWSMode    bool
	// UpstreamTerminalEvent 是 Responses WebSocket turn 的规范化终态事件。
	// 空值保持非 WS 及旧调用方的成功语义。
	UpstreamTerminalEvent string
	ResponseHeaders       http.Header
	Duration              time.Duration
	FirstTokenMs          *int
	ClientDisconnect      bool
	ImageCount            int
	ImageSize             string
	ImageInputSize        string
	ImageOutputSize       string
	ImageOutputSizes      []string
	ImageSizeSource       string
	ImageSizeBreakdown    map[string]int
	VideoCount            int
	VideoResolution       string
	// VideoDurationSeconds 是提交时请求的生成时长，已归一化到 1-15 秒。
	VideoDurationSeconds int
	// WebSearchCalls 是 Codex alpha/search 网页搜索调用次数（成功请求为 1）。
	// 上游不返回 token usage，>0 时按分组单价、次数和倍率计费。
	WebSearchCalls int

	wsReplayInput       []json.RawMessage
	wsReplayInputExists bool
}

// SucceededForScheduling reports whether the upstream turn may clear
// model-scoped transient scheduling state.
func (r *OpenAIForwardResult) SucceededForScheduling() bool {
	if r == nil || !r.OpenAIWSMode || r.UpstreamTerminalEvent == "" {
		return true
	}
	switch r.UpstreamTerminalEvent {
	case "response.completed", "response.done":
		return true
	default:
		return false
	}
}

type openAIResponseImageBillingConfig struct {
	Intent    bool
	Model     string
	Size      string
	InputSize string
}

func resolveOpenAIResponseImageBillingConfig(endpoint, requestedModel string, reqBody map[string]any) openAIResponseImageBillingConfig {
	intent := IsImageGenerationIntentMap(endpoint, requestedModel, reqBody)
	imageConfig, err := resolveOpenAIResponsesImageBillingConfigDetailed(reqBody, requestedModel)
	if err != nil {
		return openAIResponseImageBillingConfig{Intent: intent}
	}
	if intent && strings.TrimSpace(imageConfig.Model) == "" {
		imageConfig.Model = "gpt-image-2"
	}
	return openAIResponseImageBillingConfig{
		Intent:    intent,
		Model:     strings.TrimSpace(imageConfig.Model),
		Size:      strings.TrimSpace(imageConfig.SizeTier),
		InputSize: strings.TrimSpace(imageConfig.InputSize),
	}
}

func resolveOpenAIResponseImageBillingConfigFromBody(endpoint, requestedModel string, body []byte) openAIResponseImageBillingConfig {
	reqBody := cloneRequestMapForImageIntent(body)
	return resolveOpenAIResponseImageBillingConfig(endpoint, requestedModel, reqBody)
}

func applyOpenAIResponseImageAccounting(result *OpenAIForwardResult, cfg openAIResponseImageBillingConfig) {
	if result == nil {
		return
	}
	imageCount := result.ImageCount
	if imageCount <= 0 {
		imageCount = result.Usage.ImageCount
	}
	if imageCount <= 0 {
		return
	}
	imageModel := ""
	if isOpenAIImageBillingModelAlias(cfg.Model) {
		imageModel = strings.TrimSpace(cfg.Model)
	}
	if imageModel == "" && isOpenAIImageBillingModelAlias(result.BillingModel) {
		imageModel = strings.TrimSpace(result.BillingModel)
	}
	if imageModel == "" && isOpenAIImageBillingModelAlias(result.Model) {
		imageModel = strings.TrimSpace(result.Model)
	}
	if imageModel == "" && cfg.Intent {
		imageModel = "gpt-image-2"
	}
	if imageModel == "" {
		imageModel = "gpt-image-2"
	}
	imageSize := strings.TrimSpace(result.ImageSize)
	if imageSize == "" {
		imageSize = strings.TrimSpace(cfg.Size)
	}
	if imageSize == "" {
		imageSize = NormalizeImageBillingTierOrDefault("")
	}
	result.ImageCount = imageCount
	if result.ImageInputSize == "" {
		result.ImageInputSize = strings.TrimSpace(cfg.InputSize)
	}
	result.ImageSize = imageSize
	result.BillingModel = imageModel
	result.Model = imageModel
}

type OpenAIWSRetryMetricsSnapshot struct {
	RetryAttemptsTotal            int64 `json:"retry_attempts_total"`
	RetryBackoffMsTotal           int64 `json:"retry_backoff_ms_total"`
	RetryExhaustedTotal           int64 `json:"retry_exhausted_total"`
	NonRetryableFastFallbackTotal int64 `json:"non_retryable_fast_fallback_total"`
}

type OpenAICompatibilityFallbackMetricsSnapshot struct {
	SessionHashLegacyReadFallbackTotal int64   `json:"session_hash_legacy_read_fallback_total"`
	SessionHashLegacyReadFallbackHit   int64   `json:"session_hash_legacy_read_fallback_hit"`
	SessionHashLegacyDualWriteTotal    int64   `json:"session_hash_legacy_dual_write_total"`
	SessionHashLegacyReadHitRate       float64 `json:"session_hash_legacy_read_hit_rate"`

	MetadataLegacyFallbackIsMaxTokensOneHaikuTotal int64 `json:"metadata_legacy_fallback_is_max_tokens_one_haiku_total"`
	MetadataLegacyFallbackThinkingEnabledTotal     int64 `json:"metadata_legacy_fallback_thinking_enabled_total"`
	MetadataLegacyFallbackPrefetchedStickyAccount  int64 `json:"metadata_legacy_fallback_prefetched_sticky_account_total"`
	MetadataLegacyFallbackPrefetchedStickyGroup    int64 `json:"metadata_legacy_fallback_prefetched_sticky_group_total"`
	MetadataLegacyFallbackSingleAccountRetryTotal  int64 `json:"metadata_legacy_fallback_single_account_retry_total"`
	MetadataLegacyFallbackAccountSwitchCountTotal  int64 `json:"metadata_legacy_fallback_account_switch_count_total"`
	MetadataLegacyFallbackTotal                    int64 `json:"metadata_legacy_fallback_total"`
}

type openAIWSRetryMetrics struct {
	retryAttempts            atomic.Int64
	retryBackoffMs           atomic.Int64
	retryExhausted           atomic.Int64
	nonRetryableFastFallback atomic.Int64
}

type accountWriteThrottle struct {
	minInterval time.Duration
	mu          sync.Mutex
	lastByID    map[int64]time.Time
}

func newAccountWriteThrottle(minInterval time.Duration) *accountWriteThrottle {
	return &accountWriteThrottle{
		minInterval: minInterval,
		lastByID:    make(map[int64]time.Time),
	}
}

func (t *accountWriteThrottle) Allow(id int64, now time.Time) bool {
	if t == nil || id <= 0 || t.minInterval <= 0 {
		return true
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if last, ok := t.lastByID[id]; ok && now.Sub(last) < t.minInterval {
		return false
	}
	t.lastByID[id] = now

	if len(t.lastByID) > 4096 {
		cutoff := now.Add(-4 * t.minInterval)
		for accountID, writtenAt := range t.lastByID {
			if writtenAt.Before(cutoff) {
				delete(t.lastByID, accountID)
			}
		}
	}

	return true
}

var defaultOpenAICodexSnapshotPersistThrottle = newAccountWriteThrottle(openAICodexSnapshotPersistMinInterval)

// ErrNoAvailableCompactAccounts indicates the request needs /responses/compact
// support but no compatible account is available.
var ErrNoAvailableCompactAccounts = errors.New("no available OpenAI accounts support /responses/compact")

// OpenAIGatewayService handles OpenAI API gateway operations
type OpenAIGatewayService struct {
	accountRepo            AccountRepository
	accountSharePolicyRepo AccountSharePolicyRepository
	usageLogRepo           UsageLogRepository
	usageBillingRepo       UsageBillingRepository
	userRepo               UserRepository
	userSubRepo            UserSubscriptionRepository
	cache                  GatewayCache
	cfg                    *config.Config
	codexDetector          CodexClientRestrictionDetector
	schedulerSnapshot      *SchedulerSnapshotService
	concurrencyService     *ConcurrencyService
	billingService         *BillingService
	rateLimitService       *RateLimitService
	billingCacheService    *BillingCacheService
	userGroupRateResolver  *userGroupRateResolver
	httpUpstream           HTTPUpstream
	deferredService        *DeferredService
	openAITokenProvider    *OpenAITokenProvider
	grokTokenProvider      *GrokTokenProvider
	toolCorrector          *CodexToolCorrector
	openaiWSResolver       OpenAIWSProtocolResolver
	resolver               *ModelPricingResolver
	channelService         *ChannelService
	balanceNotifyService   *BalanceNotifyService
	settingService         *SettingService
	accountService         *AccountService
	proxyLatencyCache      ProxyLatencyCache

	openaiWSPoolOnce              sync.Once
	openaiWSStateStoreOnce        sync.Once
	openaiSchedulerOnce           sync.Once
	openaiWSPassthroughDialerOnce sync.Once
	openaiModelTransientOnce      sync.Once
	agentIdentityTaskMu           sync.Mutex
	openaiWSPool                  *openAIWSConnPool
	openaiWSStateStore            OpenAIWSStateStore
	openaiScheduler               OpenAIAccountScheduler
	openaiWSPassthroughDialer     openAIWSClientDialer
	openaiAccountStats            *openAIAccountRuntimeStats
	openaiModelTransient          *openAIAccountModelTransientState

	openaiWSFallbackUntil               sync.Map // key: int64(accountID), value: time.Time
	codexModelsManifestCache            codexModelsManifestCache
	openaiAccountRuntimeBlockUntil      sync.Map // key: int64(accountID), value: time.Time
	openaiAccountRuntimeBlockLocks      sync.Map // key: int64(accountID), value: *sync.Mutex
	openaiAccountRuntimeBlockGeneration sync.Map // key: int64(accountID), value: uint64
	openaiAccountRuntimeBlockSequence   atomic.Uint64
	grokCredentialMutationLocks         sync.Map // key: int64(accountID), value: *sync.Mutex
	openaiOAuth429WindowStartUnixNano   atomic.Int64
	openaiOAuth429WindowCount           atomic.Int64
	openaiWSRetryMetrics                openAIWSRetryMetrics
	responseHeaderFilter                *responseheaders.CompiledHeaderFilter
	codexSnapshotThrottle               *accountWriteThrottle
	openaiCompatSessionResponses        sync.Map
	openaiCompatAnthropicDigestSessions sync.Map
}

// NewOpenAIGatewayService creates a new OpenAIGatewayService
func NewOpenAIGatewayService(
	accountRepo AccountRepository,
	accountSharePolicyRepo AccountSharePolicyRepository,
	usageLogRepo UsageLogRepository,
	usageBillingRepo UsageBillingRepository,
	userRepo UserRepository,
	userSubRepo UserSubscriptionRepository,
	userGroupRateRepo UserGroupRateRepository,
	cache GatewayCache,
	cfg *config.Config,
	schedulerSnapshot *SchedulerSnapshotService,
	concurrencyService *ConcurrencyService,
	billingService *BillingService,
	rateLimitService *RateLimitService,
	billingCacheService *BillingCacheService,
	httpUpstream HTTPUpstream,
	deferredService *DeferredService,
	openAITokenProvider *OpenAITokenProvider,
	resolver *ModelPricingResolver,
	channelService *ChannelService,
	balanceNotifyService *BalanceNotifyService,
	settingService *SettingService,
	accountService *AccountService,
	grokTokenProviders ...*GrokTokenProvider,
) *OpenAIGatewayService {
	var grokTokenProvider *GrokTokenProvider
	if len(grokTokenProviders) > 0 {
		grokTokenProvider = grokTokenProviders[0]
	}
	svc := &OpenAIGatewayService{
		accountRepo:            accountRepo,
		accountSharePolicyRepo: accountSharePolicyRepo,
		usageLogRepo:           usageLogRepo,
		usageBillingRepo:       usageBillingRepo,
		userRepo:               userRepo,
		userSubRepo:            userSubRepo,
		cache:                  cache,
		cfg:                    cfg,
		codexDetector:          NewOpenAICodexClientRestrictionDetector(cfg),
		schedulerSnapshot:      schedulerSnapshot,
		concurrencyService:     concurrencyService,
		billingService:         billingService,
		rateLimitService:       rateLimitService,
		billingCacheService:    billingCacheService,
		userGroupRateResolver: newUserGroupRateResolver(
			userGroupRateRepo,
			nil,
			resolveUserGroupRateCacheTTL(cfg),
			nil,
			"service.openai_gateway",
		),
		httpUpstream:          httpUpstream,
		deferredService:       deferredService,
		openAITokenProvider:   openAITokenProvider,
		grokTokenProvider:     grokTokenProvider,
		toolCorrector:         NewCodexToolCorrector(),
		openaiWSResolver:      NewOpenAIWSProtocolResolver(cfg),
		resolver:              resolver,
		channelService:        channelService,
		balanceNotifyService:  balanceNotifyService,
		settingService:        settingService,
		accountService:        accountService,
		responseHeaderFilter:  compileResponseHeaderFilter(cfg),
		codexSnapshotThrottle: newAccountWriteThrottle(openAICodexSnapshotPersistMinInterval),
		openaiModelTransient:  newOpenAIAccountModelTransientState(openAIModelTransientDefaultMax),
	}
	svc.logOpenAIWSModeBootstrap()
	return svc
}

func (s *OpenAIGatewayService) SetProxyLatencyCache(cache ProxyLatencyCache) {
	if s == nil {
		return
	}
	s.proxyLatencyCache = cache
}

func (s *OpenAIGatewayService) SetGrokTokenProvider(provider *GrokTokenProvider) {
	if s != nil {
		s.grokTokenProvider = provider
	}
}

// ResolveChannelMapping resolves channel-level model mapping.
func (s *OpenAIGatewayService) ResolveChannelMapping(ctx context.Context, groupID int64, model string) ChannelMappingResult {
	if s.channelService == nil {
		return ChannelMappingResult{MappedModel: model}
	}
	return s.channelService.ResolveChannelMapping(ctx, groupID, model)
}

// IsModelRestricted checks channel model restrictions.
func (s *OpenAIGatewayService) IsModelRestricted(ctx context.Context, groupID int64, model string) bool {
	if s.channelService == nil {
		return false
	}
	return s.channelService.IsModelRestricted(ctx, groupID, model)
}

// ResolveChannelMappingAndRestrict resolves channel mapping and restriction state.
func (s *OpenAIGatewayService) ResolveChannelMappingAndRestrict(ctx context.Context, groupID *int64, model string) (ChannelMappingResult, bool) {
	if s.channelService == nil {
		return ChannelMappingResult{MappedModel: model}, false
	}
	return s.channelService.ResolveChannelMappingAndRestrict(ctx, groupID, model)
}

func (s *OpenAIGatewayService) isCodexImageGenerationBridgeEnabled(ctx context.Context, account *Account, apiKey *APIKey) bool {
	if override := account.CodexImageGenerationBridgeOverride(); override != nil {
		return *override
	}
	if s != nil && s.channelService != nil && apiKey != nil && apiKey.GroupID != nil {
		ch, err := s.channelService.GetChannelForGroup(ctx, *apiKey.GroupID)
		if err != nil {
			slog.Warn("failed to resolve codex image generation bridge channel override", "group_id", *apiKey.GroupID, "error", err)
		} else if override := ch.CodexImageGenerationBridgeOverride(PlatformOpenAI); override != nil {
			return *override
		}
	}
	return s != nil && s.cfg != nil && s.cfg.Gateway.CodexImageGenerationBridgeEnabled
}

func (s *OpenAIGatewayService) checkChannelPricingRestriction(ctx context.Context, groupID *int64, requestedModel string) bool {
	if groupID == nil || s.channelService == nil || requestedModel == "" {
		return false
	}
	mapping := s.channelService.ResolveChannelMapping(ctx, *groupID, requestedModel)
	billingModel := billingModelForRestriction(mapping.BillingModelSource, requestedModel, mapping.MappedModel)
	if billingModel == "" {
		return false
	}
	return s.channelService.IsModelRestricted(ctx, *groupID, billingModel)
}

func (s *OpenAIGatewayService) isUpstreamModelRestrictedByChannel(ctx context.Context, groupID int64, account *Account, requestedModel string, requireCompact bool) bool {
	if s.channelService == nil {
		return false
	}
	upstreamModel := resolveOpenAIAccountUpstreamModelForRequest(account, requestedModel, requireCompact)
	if upstreamModel == "" {
		return false
	}
	return s.channelService.IsModelRestricted(ctx, groupID, upstreamModel)
}

func (s *OpenAIGatewayService) needsUpstreamChannelRestrictionCheck(ctx context.Context, groupID *int64) bool {
	if groupID == nil || s.channelService == nil {
		return false
	}
	ch, err := s.channelService.GetChannelForGroup(ctx, *groupID)
	if err != nil {
		slog.Warn("failed to check openai channel upstream restriction", "group_id", *groupID, "error", err)
		return false
	}
	if ch == nil || !ch.RestrictModels {
		return false
	}
	return ch.BillingModelSource == BillingModelSourceUpstream
}

// ReplaceModelInBody replaces the JSON model field in a request body.
func (s *OpenAIGatewayService) ReplaceModelInBody(body []byte, newModel string) []byte {
	return ReplaceModelInBody(body, newModel)
}

func (s *OpenAIGatewayService) getCodexSnapshotThrottle() *accountWriteThrottle {
	if s != nil && s.codexSnapshotThrottle != nil {
		return s.codexSnapshotThrottle
	}
	return defaultOpenAICodexSnapshotPersistThrottle
}

func (s *OpenAIGatewayService) billingDeps() *billingDeps {
	return &billingDeps{
		accountRepo:            s.accountRepo,
		accountSharePolicyRepo: s.accountSharePolicyRepo,
		userRepo:               s.userRepo,
		userSubRepo:            s.userSubRepo,
		billingCacheService:    s.billingCacheService,
		deferredService:        s.deferredService,
		balanceNotifyService:   s.balanceNotifyService,
		schedulerSnapshot:      s.schedulerSnapshot,
	}
}

// CloseOpenAIWSPool closes the OpenAI WebSocket connection pool.
func (s *OpenAIGatewayService) CloseOpenAIWSPool() {
	if s != nil && s.openaiWSPool != nil {
		s.openaiWSPool.Close()
	}
}

func (s *OpenAIGatewayService) InvalidateAgentIdentityWSConnections(accountID int64) {
	if pool := s.getOpenAIWSConnPool(); pool != nil {
		pool.ClearAccount(accountID)
	}
}

func (s *OpenAIGatewayService) logOpenAIWSModeBootstrap() {
	if s == nil || s.cfg == nil {
		return
	}
	wsCfg := s.cfg.Gateway.OpenAIWS
	logOpenAIWSModeInfo(
		"bootstrap enabled=%v oauth_enabled=%v apikey_enabled=%v force_http=%v responses_websockets_v2=%v responses_websockets=%v payload_log_sample_rate=%.3f event_flush_batch_size=%d event_flush_interval_ms=%d prewarm_cooldown_ms=%d retry_backoff_initial_ms=%d retry_backoff_max_ms=%d retry_jitter_ratio=%.3f retry_total_budget_ms=%d ws_read_limit_bytes=%d",
		wsCfg.Enabled,
		wsCfg.OAuthEnabled,
		wsCfg.APIKeyEnabled,
		wsCfg.ForceHTTP,
		wsCfg.ResponsesWebsocketsV2,
		wsCfg.ResponsesWebsockets,
		wsCfg.PayloadLogSampleRate,
		wsCfg.EventFlushBatchSize,
		wsCfg.EventFlushIntervalMS,
		wsCfg.PrewarmCooldownMS,
		wsCfg.RetryBackoffInitialMS,
		wsCfg.RetryBackoffMaxMS,
		wsCfg.RetryJitterRatio,
		wsCfg.RetryTotalBudgetMS,
		openAIWSMessageReadLimitBytes,
	)
}

func (s *OpenAIGatewayService) getCodexClientRestrictionDetector() CodexClientRestrictionDetector {
	if s != nil && s.codexDetector != nil {
		return s.codexDetector
	}
	var cfg *config.Config
	if s != nil {
		cfg = s.cfg
	}
	return NewOpenAICodexClientRestrictionDetector(cfg)
}

func (s *OpenAIGatewayService) getOpenAIWSProtocolResolver() OpenAIWSProtocolResolver {
	if s != nil && s.openaiWSResolver != nil {
		return s.openaiWSResolver
	}
	var cfg *config.Config
	if s != nil {
		cfg = s.cfg
	}
	return NewOpenAIWSProtocolResolver(cfg)
}

func classifyOpenAIWSReconnectReason(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	var fallbackErr *openAIWSFallbackError
	if !errors.As(err, &fallbackErr) || fallbackErr == nil {
		return "", false
	}
	reason := strings.TrimSpace(fallbackErr.Reason)
	if reason == "" {
		return "", false
	}

	baseReason := strings.TrimPrefix(reason, "prewarm_")

	switch baseReason {
	case "policy_violation",
		"message_too_big",
		"upgrade_required",
		"ws_unsupported",
		"auth_failed",
		"invalid_encrypted_content",
		"previous_response_not_found":
		return reason, false
	}

	switch baseReason {
	case "read_event",
		"write_request",
		"write",
		"acquire_timeout",
		"acquire_conn",
		"conn_queue_full",
		"dial_failed",
		"upstream_5xx",
		"event_error",
		"error_event",
		"upstream_error_event",
		"ws_connection_limit_reached",
		"missing_final_response":
		return reason, true
	default:
		return reason, false
	}
}

func resolveOpenAIWSFallbackErrorResponse(err error) (statusCode int, errType string, clientMessage string, upstreamMessage string, ok bool) {
	if err == nil {
		return 0, "", "", "", false
	}
	var fallbackErr *openAIWSFallbackError
	if !errors.As(err, &fallbackErr) || fallbackErr == nil {
		return 0, "", "", "", false
	}

	reason := strings.TrimSpace(fallbackErr.Reason)
	reason = strings.TrimPrefix(reason, "prewarm_")
	if reason == "" {
		return 0, "", "", "", false
	}

	var dialErr *openAIWSDialError
	if fallbackErr.Err != nil && errors.As(fallbackErr.Err, &dialErr) && dialErr != nil {
		if dialErr.StatusCode > 0 {
			statusCode = dialErr.StatusCode
		}
		if dialErr.Err != nil {
			upstreamMessage = sanitizeUpstreamErrorMessage(strings.TrimSpace(dialErr.Err.Error()))
		}
	}

	switch reason {
	case "invalid_encrypted_content":
		if statusCode == 0 {
			statusCode = http.StatusBadRequest
		}
		errType = "invalid_request_error"
		if upstreamMessage == "" {
			upstreamMessage = "encrypted content could not be verified"
		}
	case "previous_response_not_found":
		if statusCode == 0 {
			statusCode = http.StatusBadRequest
		}
		errType = "invalid_request_error"
		if upstreamMessage == "" {
			upstreamMessage = "previous response not found"
		}
	case "upgrade_required":
		if statusCode == 0 {
			statusCode = http.StatusUpgradeRequired
		}
	case "ws_unsupported":
		if statusCode == 0 {
			statusCode = http.StatusBadRequest
		}
	case "auth_failed":
		if statusCode == 0 {
			statusCode = http.StatusUnauthorized
		}
	case "upstream_rate_limited":
		if statusCode == 0 {
			statusCode = http.StatusTooManyRequests
		}
	case "upstream_capacity":
		if statusCode == 0 {
			statusCode = http.StatusServiceUnavailable
		}
	default:
		if statusCode == 0 {
			return 0, "", "", "", false
		}
	}

	if upstreamMessage == "" && fallbackErr.Err != nil {
		upstreamMessage = sanitizeUpstreamErrorMessage(strings.TrimSpace(fallbackErr.Err.Error()))
	}
	if upstreamMessage == "" {
		switch reason {
		case "upgrade_required":
			upstreamMessage = "upstream websocket upgrade required"
		case "ws_unsupported":
			upstreamMessage = "upstream websocket not supported"
		case "auth_failed":
			upstreamMessage = "upstream authentication failed"
		case "upstream_rate_limited":
			upstreamMessage = "upstream rate limit exceeded, please retry later"
		case "upstream_capacity":
			upstreamMessage = "upstream model capacity is temporarily unavailable"
		default:
			upstreamMessage = "Upstream request failed"
		}
	}

	if errType == "" {
		if statusCode == http.StatusTooManyRequests {
			errType = "rate_limit_error"
		} else {
			errType = "upstream_error"
		}
	}
	clientMessage = upstreamMessage
	return statusCode, errType, clientMessage, upstreamMessage, true
}

func (s *OpenAIGatewayService) writeOpenAIWSFallbackErrorResponse(c *gin.Context, account *Account, wsErr error) bool {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return false
	}
	statusCode, errType, clientMessage, upstreamMessage, ok := resolveOpenAIWSFallbackErrorResponse(wsErr)
	if !ok {
		return false
	}
	if strings.TrimSpace(clientMessage) == "" {
		clientMessage = "Upstream request failed"
	}
	if strings.TrimSpace(upstreamMessage) == "" {
		upstreamMessage = clientMessage
	}

	if s.rateLimitService != nil && account != nil {
		body, _ := json.Marshal(gin.H{
			"error": gin.H{
				"type":    errType,
				"message": upstreamMessage,
			},
		})
		ctx := context.Background()
		if c.Request != nil {
			ctx = c.Request.Context()
		}
		s.rateLimitService.HandlePermanentAccountError(ctx, account, statusCode, body)
	}

	setOpsUpstreamError(c, statusCode, upstreamMessage, "")
	if account != nil {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: statusCode,
			Kind:               "ws_error",
			Message:            upstreamMessage,
		})
	}
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": clientMessage,
		},
	})
	return true
}

func (s *OpenAIGatewayService) openAIWSCapacityFailoverError(c *gin.Context, account *Account, wsErr error) *UpstreamFailoverError {
	if c != nil && c.Writer != nil && c.Writer.Written() {
		return nil
	}
	var fallbackErr *openAIWSFallbackError
	if !errors.As(wsErr, &fallbackErr) || fallbackErr == nil {
		return nil
	}
	reason := strings.TrimPrefix(strings.TrimSpace(fallbackErr.Reason), "prewarm_")
	if reason != "upstream_capacity" {
		return nil
	}

	statusCode, _, _, upstreamMessage, ok := resolveOpenAIWSFallbackErrorResponse(wsErr)
	if !ok || statusCode <= 0 {
		statusCode = http.StatusServiceUnavailable
	}
	upstreamMessage = sanitizeUpstreamErrorMessage(strings.TrimSpace(upstreamMessage))
	if upstreamMessage == "" {
		upstreamMessage = "upstream model capacity is temporarily unavailable"
	}

	body, _ := json.Marshal(gin.H{
		"error": gin.H{
			"type":    "upstream_error",
			"message": upstreamMessage,
		},
	})
	setOpsUpstreamError(c, statusCode, upstreamMessage, "")
	if account != nil {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: statusCode,
			Kind:               "failover",
			Message:            upstreamMessage,
		})
	}
	return &UpstreamFailoverError{
		StatusCode:   statusCode,
		ResponseBody: body,
	}
}

func (s *OpenAIGatewayService) openAIWSRetryBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	initial := openAIWSRetryBackoffInitialDefault
	maxBackoff := openAIWSRetryBackoffMaxDefault
	jitterRatio := openAIWSRetryJitterRatioDefault
	if s != nil && s.cfg != nil {
		wsCfg := s.cfg.Gateway.OpenAIWS
		if wsCfg.RetryBackoffInitialMS > 0 {
			initial = time.Duration(wsCfg.RetryBackoffInitialMS) * time.Millisecond
		}
		if wsCfg.RetryBackoffMaxMS > 0 {
			maxBackoff = time.Duration(wsCfg.RetryBackoffMaxMS) * time.Millisecond
		}
		if wsCfg.RetryJitterRatio >= 0 {
			jitterRatio = wsCfg.RetryJitterRatio
		}
	}
	if initial <= 0 {
		return 0
	}
	if maxBackoff <= 0 {
		maxBackoff = initial
	}
	if maxBackoff < initial {
		maxBackoff = initial
	}
	if jitterRatio < 0 {
		jitterRatio = 0
	}
	if jitterRatio > 1 {
		jitterRatio = 1
	}

	shift := attempt - 1
	if shift < 0 {
		shift = 0
	}
	backoff := initial
	if shift > 0 {
		backoff = initial * time.Duration(1<<shift)
	}
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	if jitterRatio <= 0 {
		return backoff
	}
	jitter := time.Duration(float64(backoff) * jitterRatio)
	if jitter <= 0 {
		return backoff
	}
	delta := time.Duration(rand.Int63n(int64(jitter)*2+1)) - jitter
	withJitter := backoff + delta
	if withJitter < 0 {
		return 0
	}
	return withJitter
}

func (s *OpenAIGatewayService) openAIWSRetryTotalBudget() time.Duration {
	if s != nil && s.cfg != nil {
		ms := s.cfg.Gateway.OpenAIWS.RetryTotalBudgetMS
		if ms <= 0 {
			return 0
		}
		return time.Duration(ms) * time.Millisecond
	}
	return 0
}

func (s *OpenAIGatewayService) recordOpenAIWSRetryAttempt(backoff time.Duration) {
	if s == nil {
		return
	}
	s.openaiWSRetryMetrics.retryAttempts.Add(1)
	if backoff > 0 {
		s.openaiWSRetryMetrics.retryBackoffMs.Add(backoff.Milliseconds())
	}
}

func (s *OpenAIGatewayService) recordOpenAIWSRetryExhausted() {
	if s == nil {
		return
	}
	s.openaiWSRetryMetrics.retryExhausted.Add(1)
}

func (s *OpenAIGatewayService) recordOpenAIWSNonRetryableFastFallback() {
	if s == nil {
		return
	}
	s.openaiWSRetryMetrics.nonRetryableFastFallback.Add(1)
}

func (s *OpenAIGatewayService) SnapshotOpenAIWSRetryMetrics() OpenAIWSRetryMetricsSnapshot {
	if s == nil {
		return OpenAIWSRetryMetricsSnapshot{}
	}
	return OpenAIWSRetryMetricsSnapshot{
		RetryAttemptsTotal:            s.openaiWSRetryMetrics.retryAttempts.Load(),
		RetryBackoffMsTotal:           s.openaiWSRetryMetrics.retryBackoffMs.Load(),
		RetryExhaustedTotal:           s.openaiWSRetryMetrics.retryExhausted.Load(),
		NonRetryableFastFallbackTotal: s.openaiWSRetryMetrics.nonRetryableFastFallback.Load(),
	}
}

func SnapshotOpenAICompatibilityFallbackMetrics() OpenAICompatibilityFallbackMetricsSnapshot {
	legacyReadFallbackTotal, legacyReadFallbackHit, legacyDualWriteTotal := openAIStickyCompatStats()
	isMaxTokensOneHaiku, thinkingEnabled, prefetchedStickyAccount, prefetchedStickyGroup, singleAccountRetry, accountSwitchCount := RequestMetadataFallbackStats()

	readHitRate := float64(0)
	if legacyReadFallbackTotal > 0 {
		readHitRate = float64(legacyReadFallbackHit) / float64(legacyReadFallbackTotal)
	}
	metadataFallbackTotal := isMaxTokensOneHaiku + thinkingEnabled + prefetchedStickyAccount + prefetchedStickyGroup + singleAccountRetry + accountSwitchCount

	return OpenAICompatibilityFallbackMetricsSnapshot{
		SessionHashLegacyReadFallbackTotal: legacyReadFallbackTotal,
		SessionHashLegacyReadFallbackHit:   legacyReadFallbackHit,
		SessionHashLegacyDualWriteTotal:    legacyDualWriteTotal,
		SessionHashLegacyReadHitRate:       readHitRate,

		MetadataLegacyFallbackIsMaxTokensOneHaikuTotal: isMaxTokensOneHaiku,
		MetadataLegacyFallbackThinkingEnabledTotal:     thinkingEnabled,
		MetadataLegacyFallbackPrefetchedStickyAccount:  prefetchedStickyAccount,
		MetadataLegacyFallbackPrefetchedStickyGroup:    prefetchedStickyGroup,
		MetadataLegacyFallbackSingleAccountRetryTotal:  singleAccountRetry,
		MetadataLegacyFallbackAccountSwitchCountTotal:  accountSwitchCount,
		MetadataLegacyFallbackTotal:                    metadataFallbackTotal,
	}
}

func (s *OpenAIGatewayService) detectCodexClientRestriction(c *gin.Context, account *Account, bodies ...[]byte) CodexClientRestrictionDetectionResult {
	policy := CodexRestrictionPolicy{EngineFingerprintSignals: openai.DefaultEngineFingerprintSignals}
	if account != nil && account.IsCodexCLIOnlyEnabled() && s != nil && s.settingService != nil {
		ctx := context.Background()
		if c != nil && c.Request != nil {
			ctx = c.Request.Context()
		}
		policy = s.settingService.GetCodexRestrictionPolicy(ctx)
	}
	var body []byte
	if len(bodies) > 0 {
		body = bodies[0]
	}
	return s.getCodexClientRestrictionDetector().Detect(c, account, policy, body)
}

func getAPIKeyIDFromContext(c *gin.Context) int64 {
	if c == nil {
		return 0
	}
	v, exists := c.Get("api_key")
	if !exists {
		return 0
	}
	apiKey, ok := v.(*APIKey)
	if !ok || apiKey == nil {
		return 0
	}
	return apiKey.ID
}

// isolateOpenAISessionID 灏?apiKeyID 娣峰叆 session 鏍囪瘑绗︼紝
// isolateOpenAISessionID scopes a client session identifier by API key.
func isolateOpenAISessionID(apiKeyID int64, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	h := xxhash.New()
	_, _ = fmt.Fprintf(h, "k%d:", apiKeyID)
	_, _ = h.WriteString(raw)
	return fmt.Sprintf("%016x", h.Sum64())
}

func logCodexCLIOnlyDetection(ctx context.Context, c *gin.Context, account *Account, apiKeyID int64, result CodexClientRestrictionDetectionResult, body []byte) {
	if !result.Enabled {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	accountID := int64(0)
	if account != nil {
		accountID = account.ID
	}
	fields := []zap.Field{
		zap.String("component", "service.openai_gateway"),
		zap.Int64("account_id", accountID),
		zap.Bool("codex_cli_only_enabled", result.Enabled),
		zap.Bool("codex_official_client_match", result.Matched),
		zap.String("reject_reason", result.Reason),
	}
	if apiKeyID > 0 {
		fields = append(fields, zap.Int64("api_key_id", apiKeyID))
	}
	if !result.Matched {
		fields = appendCodexCLIOnlyRejectedRequestFields(fields, c, body)
	}
	log := logger.FromContext(ctx).With(fields...)
	if result.Matched {
		return
	}
	log.Warn("OpenAI codex_cli_only 拒绝非官方客户端请求")
}

func appendCodexCLIOnlyRejectedRequestFields(fields []zap.Field, c *gin.Context, body []byte) []zap.Field {
	if c == nil || c.Request == nil {
		return fields
	}

	req := c.Request
	requestModel, requestStream, promptCacheKey := extractOpenAIRequestMetaFromBody(body)
	fields = append(fields,
		zap.String("request_method", strings.TrimSpace(req.Method)),
		zap.String("request_path", strings.TrimSpace(req.URL.Path)),
		zap.String("request_query", strings.TrimSpace(req.URL.RawQuery)),
		zap.String("request_host", strings.TrimSpace(req.Host)),
		zap.String("request_client_ip", strings.TrimSpace(ip.GetClientIP(c))),
		zap.String("request_remote_addr", strings.TrimSpace(req.RemoteAddr)),
		zap.String("request_user_agent", strings.TrimSpace(req.Header.Get("User-Agent"))),
		zap.String("request_content_type", strings.TrimSpace(req.Header.Get("Content-Type"))),
		zap.Int64("request_content_length", req.ContentLength),
		zap.Bool("request_stream", requestStream),
	)
	if requestModel != "" {
		fields = append(fields, zap.String("request_model", requestModel))
	}
	if promptCacheKey != "" {
		fields = append(fields, zap.String("request_prompt_cache_key_sha256", hashSensitiveValueForLog(promptCacheKey)))
	}

	if headers := snapshotCodexCLIOnlyHeaders(req.Header); len(headers) > 0 {
		fields = append(fields, zap.Any("request_headers", headers))
	}
	fields = append(fields, zap.Int("request_body_size", len(body)))
	return fields
}

func snapshotCodexCLIOnlyHeaders(header http.Header) map[string]string {
	if len(header) == 0 {
		return nil
	}
	result := make(map[string]string, len(codexCLIOnlyDebugHeaderWhitelist))
	for _, key := range codexCLIOnlyDebugHeaderWhitelist {
		value := strings.TrimSpace(header.Get(key))
		if value == "" {
			continue
		}
		result[strings.ToLower(key)] = truncateString(value, codexCLIOnlyHeaderValueMaxBytes)
	}
	return result
}

func hashSensitiveValueForLog(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func isOpenAIModelCapacityError(upstreamStatusCode int, upstreamMsg string, upstreamBody []byte) bool {
	if upstreamStatusCode > 0 && upstreamStatusCode < http.StatusBadRequest {
		return false
	}

	parts := make([]string, 0, 8)
	if upstreamMsg != "" {
		parts = append(parts, upstreamMsg)
	}
	if len(upstreamBody) > 0 {
		for _, path := range []string{
			"error.message",
			"error.code",
			"error.type",
			"response.error.message",
			"response.error.code",
			"response.error.type",
			"message",
			"code",
			"type",
		} {
			if value := strings.TrimSpace(gjson.GetBytes(upstreamBody, path).String()); value != "" {
				parts = append(parts, value)
			}
		}
		parts = append(parts, string(upstreamBody))
	}
	return isOpenAIModelCapacityText(strings.Join(parts, " "))
}

func isOpenAIModelCapacityText(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "selected model is at capacity") ||
		strings.Contains(lower, "model is at capacity") {
		return true
	}
	if strings.Contains(lower, "try a different model") && strings.Contains(lower, "capacity") {
		return true
	}
	if strings.Contains(lower, "capacity_exhaust") ||
		(strings.Contains(lower, "model_capacity") && strings.Contains(lower, "exhaust")) {
		return true
	}
	return strings.Contains(lower, "no capacity available") && strings.Contains(lower, "model")
}

// ClearStickySessionIfBoundTo removes an OpenAI sticky binding only when the
// current binding still points at the failed account.
func (s *OpenAIGatewayService) ClearStickySessionIfBoundTo(ctx context.Context, groupID *int64, sessionHash string, accountID int64) (bool, error) {
	if sessionHash == "" || accountID <= 0 || s == nil || s.cache == nil {
		return false, nil
	}
	currentAccountID, err := s.getStickySessionAccountID(ctx, groupID, sessionHash)
	if err != nil || currentAccountID != accountID {
		return false, nil
	}
	if err := s.deleteStickySessionAccountID(ctx, groupID, sessionHash); err != nil {
		return false, err
	}
	return true, nil
}

type noAvailableOpenAIAccountsError struct {
	message string
}

func (e *noAvailableOpenAIAccountsError) Error() string {
	if e == nil || strings.TrimSpace(e.message) == "" {
		return ErrNoAvailableAccounts.Error()
	}
	return e.message
}

func (e *noAvailableOpenAIAccountsError) Unwrap() error {
	return ErrNoAvailableAccounts
}

// isOpenAIAccountEligibleForRequest centralises the schedulable / OpenAI / model /
// compact-support checks used during account selection.
func isOpenAIAccountEligibleForRequest(account *Account, requestedModel string, requireCompact bool, requiredCapability OpenAIEndpointCapability) bool {
	if account == nil || !account.IsSchedulable() || !account.IsOpenAI() {
		return false
	}
	if requestedModel != "" && !account.IsModelSupported(requestedModel) {
		return false
	}
	if !account.SupportsOpenAIEndpointCapability(requiredCapability) {
		return false
	}
	if requireCompact && openAICompactSupportTier(account) == 0 {
		return false
	}
	return true
}

func shouldClearOpenAISessionStickyForRequest(account *Account, requestedModel string, requireCompact bool, requiredCapability OpenAIEndpointCapability) bool {
	if account == nil {
		return false
	}
	if shouldClearStickySession(account, requestedModel) || !account.IsOpenAI() {
		return true
	}
	if requestedModel != "" && !account.IsModelSupported(requestedModel) {
		return true
	}
	if !account.SupportsOpenAIEndpointCapability(requiredCapability) {
		return true
	}
	if requireCompact && openAICompactSupportTier(account) == 0 {
		return true
	}
	return false
}

func (s *OpenAIGatewayService) isOpenAIAccountProxyHealthSchedulable(ctx context.Context, account *Account) bool {
	if s == nil || s.proxyLatencyCache == nil || account == nil || !account.IsOpenAI() || !account.RequiresProxyForScheduling() {
		return true
	}
	if account.ProxyID == nil || *account.ProxyID <= 0 {
		return false
	}
	proxyID := *account.ProxyID
	if info, ok := proxyHealthFromPrefetchContext(ctx, proxyID); ok {
		return !proxyHealthBlocksProtectedProxyScheduling(info)
	}
	latencies, err := s.proxyLatencyCache.GetProxyLatencies(ctx, []int64{proxyID})
	if err != nil {
		return true
	}
	return !proxyHealthBlocksProtectedProxyScheduling(latencies[proxyID])
}

func (s *OpenAIGatewayService) observedProxyExitIP(ctx context.Context, account *Account) string {
	if s == nil || !accountNeedsProxyExitIPStability(account) {
		return ""
	}
	proxyID := *account.ProxyID
	if info, ok := proxyHealthFromPrefetchContext(ctx, proxyID); ok && info != nil {
		return normalizeProxyExitIP(info.IPAddress)
	}
	if s.proxyLatencyCache == nil {
		return ""
	}
	latencies, err := s.proxyLatencyCache.GetProxyLatencies(ctx, []int64{proxyID})
	if err != nil {
		return ""
	}
	if info := latencies[proxyID]; info != nil {
		return normalizeProxyExitIP(info.IPAddress)
	}
	return ""
}

func (s *OpenAIGatewayService) boundAccountProxyExitIP(ctx context.Context, accountID int64) string {
	if s == nil || s.cache == nil || accountID <= 0 {
		return ""
	}
	key := accountProxyExitIPKey(accountID)
	if key == "" {
		return ""
	}
	raw, err := s.cache.GetSessionString(ctx, 0, key)
	if err != nil {
		return ""
	}
	normalized := normalizeProxyExitIP(raw)
	if normalized == "" {
		writeCtx, cancel := rateLimitStateContext(ctx)
		defer cancel()
		_ = s.cache.DeleteSessionString(writeCtx, 0, key)
	}
	return normalized
}

func (s *OpenAIGatewayService) isOpenAIAccountProxyExitIPStable(ctx context.Context, account *Account) bool {
	if s == nil || !accountNeedsProxyExitIPStability(account) || s.cache == nil {
		return true
	}
	observed := s.observedProxyExitIP(ctx, account)
	if observed == "" {
		return true
	}
	bound := s.boundAccountProxyExitIP(ctx, account.ID)
	return bound == "" || bound == observed
}

func (s *OpenAIGatewayService) bindOpenAIAccountProxyExitIP(ctx context.Context, account *Account) {
	if s == nil || s.cache == nil || !accountNeedsProxyExitIPStability(account) {
		return
	}
	observed := s.observedProxyExitIP(ctx, account)
	if observed == "" {
		return
	}
	key := accountProxyExitIPKey(account.ID)
	if key == "" {
		return
	}
	writeCtx, cancel := rateLimitStateContext(ctx)
	defer cancel()
	_ = s.cache.SetSessionString(writeCtx, 0, key, observed, accountProxyExitIPTTL)
}

func (s *OpenAIGatewayService) isOpenAIAccountInRequestGroup(account *Account, groupID *int64) bool {
	if account == nil {
		return false
	}
	if s != nil && s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return true
	}
	if groupID == nil {
		return len(account.AccountGroups) == 0 && len(account.GroupIDs) == 0
	}
	for _, ag := range account.AccountGroups {
		if ag.GroupID == *groupID {
			return true
		}
	}
	for _, gid := range account.GroupIDs {
		if gid == *groupID {
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) isAccountTempUnschedulableCached(ctx context.Context, account *Account) bool {
	if s == nil || s.rateLimitService == nil || account == nil {
		return false
	}
	return s.rateLimitService.IsAccountTempUnschedulableCached(ctx, account)
}

// GetAccessToken gets the bearer credential for an OpenAI-compatible account.
func (s *OpenAIGatewayService) GetAccessToken(ctx context.Context, account *Account) (string, string, error) {
	if account == nil {
		return "", "", errors.New("account is nil")
	}
	if account.IsShadow() {
		credentialAccount, err := resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			return "", "", err
		}
		account = credentialAccount
	}
	switch account.Type {
	case AccountTypeOAuth:
		if account.IsOpenAIAgentIdentity() {
			return "", OpenAIAuthModeAgentIdentity, nil
		}
		if account.Platform == PlatformGrok {
			if s.grokTokenProvider != nil {
				accessToken, err := s.grokTokenProvider.GetAccessToken(ctx, account)
				if err != nil {
					return "", "", err
				}
				return accessToken, "oauth", nil
			}
			accessToken := account.GetGrokAccessToken()
			if accessToken == "" {
				return "", "", errors.New("access_token not found in credentials")
			}
			return accessToken, "oauth", nil
		}
		// 浣跨敤 TokenProvider 鑾峰彇缂撳瓨鐨?token
		if s.openAITokenProvider != nil {
			accessToken, err := s.openAITokenProvider.GetAccessToken(ctx, account)
			if err != nil {
				return "", "", err
			}
			return accessToken, "oauth", nil
		}
		// 闄嶇骇锛歍okenProvider 鏈厤缃椂鐩存帴浠庤处鍙疯鍙?
		accessToken := account.GetOpenAIAccessToken()
		if accessToken == "" {
			return "", "", errors.New("access_token not found in credentials")
		}
		return accessToken, "oauth", nil
	case AccountTypeAPIKey:
		apiKey := account.GetOpenAIApiKey()
		if account.Platform == PlatformGrok {
			apiKey = account.GetGrokAPIKey()
		}
		if apiKey == "" {
			return "", "", errors.New("api_key not found in credentials")
		}
		return apiKey, "apikey", nil
	default:
		return "", "", fmt.Errorf("unsupported account type: %s", account.Type)
	}
}

func (s *OpenAIGatewayService) handleFailoverSideEffectsForModel(ctx context.Context, resp *http.Response, account *Account, requestedModel string) {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	s.handleFailoverSideEffects(ctx, resp, account, body, requestedModel)
}

func (s *OpenAIGatewayService) handleOpenAIModelCapacitySignal(ctx context.Context, account *Account, statusCode int, headers http.Header, payload []byte, message string) bool {
	if s == nil || s.rateLimitService == nil || account == nil || account.Platform != PlatformOpenAI {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if statusCode <= 0 {
		statusCode = http.StatusServiceUnavailable
	}
	if !isOpenAIModelCapacityError(statusCode, message, payload) {
		return false
	}
	cooldownBody := payload
	if len(cooldownBody) == 0 {
		cooldownBody = []byte(message)
	}
	_ = s.rateLimitService.HandleUpstreamError(ctx, account, statusCode, headers, cooldownBody)
	return true
}

func (s *OpenAIGatewayService) handleStreamingResponseLegacy(ctx context.Context, resp *http.Response, c *gin.Context, account *Account, startTime time.Time, originalModel, mappedModel string) (*openaiStreamingResult, error) {
	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}

	// Set SSE response headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Pass through other headers
	if v := resp.Header.Get("x-request-id"); v != "" {
		c.Header("x-request-id", v)
	}

	w := c.Writer
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("streaming not supported")
	}
	bufferedWriter := bufio.NewWriterSize(w, 4*1024)
	flushBuffered := func() error {
		if err := bufferedWriter.Flush(); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	usage := &OpenAIUsage{}
	var firstTokenMs *int
	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	scanBuf := getSSEScannerBuf64K()
	scanner.Buffer(scanBuf[:0], maxLineSize)

	streamInterval := time.Duration(0)
	if s.cfg != nil && s.cfg.Gateway.StreamDataIntervalTimeout > 0 {
		streamInterval = time.Duration(s.cfg.Gateway.StreamDataIntervalTimeout) * time.Second
	}
	// 浠呯洃鎺т笂娓告暟鎹棿闅旇秴鏃讹紝涓嶈涓嬫父鍐欏叆闃诲褰卞搷
	var intervalTicker *time.Ticker
	if streamInterval > 0 {
		intervalTicker = time.NewTicker(streamInterval)
		defer intervalTicker.Stop()
	}
	var intervalCh <-chan time.Time
	if intervalTicker != nil {
		intervalCh = intervalTicker.C
	}

	keepaliveInterval := time.Duration(0)
	if s.cfg != nil && s.cfg.Gateway.StreamKeepaliveInterval > 0 {
		keepaliveInterval = time.Duration(s.cfg.Gateway.StreamKeepaliveInterval) * time.Second
	}
	// 涓嬫父 keepalive 浠呯敤浜庨槻姝唬鐞嗙┖闂叉柇寮€
	var keepaliveTicker *time.Ticker
	if keepaliveInterval > 0 {
		keepaliveTicker = time.NewTicker(keepaliveInterval)
		defer keepaliveTicker.Stop()
	}
	var keepaliveCh <-chan time.Time
	if keepaliveTicker != nil {
		keepaliveCh = keepaliveTicker.C
	}
	// Track downstream writes separately from upstream reads: pre-output failover
	// can buffer response.created / response.in_progress, so keepalive must be
	// based on downstream idle time.
	lastDownstreamWriteAt := time.Now()

	// 浠呭彂閫佷竴娆￠敊璇簨浠讹紝閬垮厤澶氭鍐欏叆瀵艰嚧鍗忚娣蜂贡銆?	// 娉ㄦ剰锛歄penAI `/v1/responses` streaming 浜嬩欢蹇呴』绗﹀悎 OpenAI Responses schema锛?	// 鍚﹀垯涓嬫父 SDK锛堜緥濡?OpenCode锛変細鍥犱负绫诲瀷鏍￠獙澶辫触鑰屾姤閿欍€?
	errorEventSent := false
	clientDisconnected := false // 瀹㈡埛绔柇寮€鍚庣户缁?drain 涓婃父浠ユ敹闆?usage
	sawTerminalEvent := false
	sawFailedEvent := false
	failedMessage := ""
	clientOutputStarted := false
	upstreamRequestID := strings.TrimSpace(resp.Header.Get("x-request-id"))
	var streamFailoverErr error
	sendErrorEvent := func(reason string) {
		if errorEventSent || clientDisconnected {
			return
		}
		errorEventSent = true
		payload := `{"type":"error","sequence_number":0,"error":{"type":"upstream_error","message":` + strconv.Quote(reason) + `,"code":` + strconv.Quote(reason) + `}}`
		if err := flushBuffered(); err != nil {
			clientDisconnected = true
			return
		}
		if _, err := bufferedWriter.WriteString("data: " + payload + "\n\n"); err != nil {
			clientDisconnected = true
			return
		}
		if err := flushBuffered(); err != nil {
			clientDisconnected = true
			return
		}
		clientOutputStarted = true
		lastDownstreamWriteAt = time.Now()
	}

	needModelReplace := originalModel != mappedModel
	imageCounter := newOpenAIImageOutputCounter()
	streamOutputAccumulator := apicompat.NewBufferedResponseAccumulator()
	streamImageOutputs := make([]json.RawMessage, 0, 1)
	streamSeenImages := make(map[string]struct{})
	responseID := ""
	resultWithUsage := func() *openaiStreamingResult {
		usage.ImageCount = imageCounter.Count()
		usage.imageCount = usage.ImageCount
		usage.usage = usage
		return &openaiStreamingResult{usage: usage, imageCount: usage.ImageCount, firstTokenMs: firstTokenMs, responseServiceTier: usage.ResponseServiceTier, responseID: responseID}
	}
	finalizeStream := func() (*openaiStreamingResult, error) {
		if !sawTerminalEvent {
			if !openAIStreamClientOutputStarted(c, clientOutputStarted) {
				return resultWithUsage(), s.newOpenAIStreamFailoverError(
					c,
					account,
					false,
					upstreamRequestID,
					nil,
					"OpenAI stream ended before a terminal event",
				)
			}
			return resultWithUsage(), fmt.Errorf("stream usage incomplete: missing terminal event")
		}
		if sawFailedEvent {
			return resultWithUsage(), fmt.Errorf("upstream response failed: %s", failedMessage)
		}
		if !clientDisconnected {
			hadBufferedData := bufferedWriter.Buffered() > 0
			if err := flushBuffered(); err != nil {
				clientDisconnected = true
				logger.LegacyPrintf("service.openai_gateway", "Client disconnected during final flush, returning collected usage")
			} else if hadBufferedData {
				clientOutputStarted = true
				lastDownstreamWriteAt = time.Now()
			}
		}
		return resultWithUsage(), nil
	}
	handleScanErr := func(scanErr error) (*openaiStreamingResult, error, bool) {
		if scanErr == nil {
			return nil, nil, false
		}
		if sawTerminalEvent && !sawFailedEvent {
			logger.LegacyPrintf("service.openai_gateway", "Upstream scan ended after terminal event: %v", scanErr)
			return resultWithUsage(), nil, true
		}
		if sawFailedEvent {
			return resultWithUsage(), fmt.Errorf("upstream response failed: %s", failedMessage), true
		}
		// 瀹㈡埛绔柇寮€/鍙栨秷璇锋眰鏃讹紝涓婃父璇诲彇寰€寰€浼氳繑鍥?context canceled銆?		// /v1/responses 鐨?SSE 浜嬩欢蹇呴』绗﹀悎 OpenAI 鍗忚锛涜繖閲屼笉娉ㄥ叆鑷畾涔?error event锛岄伩鍏嶄笅娓?SDK 瑙ｆ瀽澶辫触銆?
		if errors.Is(scanErr, context.Canceled) || errors.Is(scanErr, context.DeadlineExceeded) {
			return resultWithUsage(), fmt.Errorf("stream usage incomplete: %w", scanErr), true
		}
		if errors.Is(scanErr, bufio.ErrTooLong) {
			logger.LegacyPrintf("service.openai_gateway", "SSE line too long: account=%d max_size=%d error=%v", account.ID, maxLineSize, scanErr)
			sendErrorEvent("response_too_large")
			return resultWithUsage(), scanErr, true
		}
		if !openAIStreamClientOutputStarted(c, clientOutputStarted) {
			msg := "OpenAI stream disconnected before completion"
			if errText := strings.TrimSpace(scanErr.Error()); errText != "" {
				msg += ": " + errText
			}
			return resultWithUsage(), s.newOpenAIStreamFailoverError(c, account, false, upstreamRequestID, nil, msg), true
		}
		// 瀹㈡埛绔凡鏂紑鏃讹紝涓婃父鍑洪敊浠呭奖鍝嶄綋楠岋紝涓嶅奖鍝嶈璐癸紱杩斿洖宸叉敹闆?usage
		if clientDisconnected {
			return resultWithUsage(), fmt.Errorf("stream usage incomplete after disconnect: %w", scanErr), true
		}
		sendErrorEvent("stream_read_error")
		return resultWithUsage(), fmt.Errorf("stream read error: %w", scanErr), true
	}
	processSSELine := func(line string, queueDrained bool) {
		if streamFailoverErr != nil {
			return
		}
		// Extract data from SSE line (supports both "data: " and "data:" formats)
		if data, ok := extractOpenAISSEDataLine(line); ok {
			dataBytes := []byte(data)
			if openAIStreamEventIsTerminal(data) {
				sawTerminalEvent = true
			}
			eventType := strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
			if responseID == "" {
				responseID = extractOpenAIResponseIDFromJSONBytes(dataBytes)
			}
			forceFlushFailedEvent := false
			if eventType == "response.failed" {
				failedMessage = extractOpenAISSEErrorMessage(dataBytes)
				s.parseSSEUsageBytes(dataBytes, usage)
				if hit, code, msg := detectOpenAICyberPolicy(dataBytes); hit {
					MarkOpsCyberPolicy(c, CyberPolicyMark{
						Code:           code,
						Message:        msg,
						Body:           truncateString(string(dataBytes), 4096),
						UpstreamStatus: http.StatusOK,
						UpstreamInTok:  usage.InputTokens,
						UpstreamOutTok: usage.OutputTokens,
					})
				} else {
					s.handleOpenAIResponsesStreamErrorSideEffect(ctx, account, resp.Header, dataBytes, failedMessage, openAIStreamClientOutputStarted(c, clientOutputStarted))
					if !openAIStreamClientOutputStarted(c, clientOutputStarted) && openAIStreamFailedEventShouldFailover(dataBytes, failedMessage) {
						sawFailedEvent = true
						streamFailoverErr = s.newOpenAIStreamFailoverError(c, account, false, upstreamRequestID, dataBytes, failedMessage)
						return
					}
				}
				forceFlushFailedEvent = true
				sawFailedEvent = true
			}

			// Correct Codex tool calls if needed (apply_patch -> edit, etc.)
			if correctedData, corrected := s.toolCorrector.CorrectToolCallsInSSEBytes(dataBytes); corrected {
				dataBytes = correctedData
				data = string(correctedData)
				line = "data: " + data
				eventType = strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
			}
			if normalizedData, normalized := normalizeOpenAIResponsesFunctionCallArguments(dataBytes); normalized {
				dataBytes = normalizedData
				data = string(normalizedData)
				line = "data: " + data
				eventType = strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
			}
			if normalizedData, normalized := normalizeCompletedImageGenerationStatus(dataBytes); normalized {
				dataBytes = normalizedData
				data = string(normalizedData)
				line = "data: " + data
				eventType = strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
			}
			restoredData, restoreErr := restoreOpenAIResponsesNamespacePayload(c, dataBytes)
			if restoreErr != nil {
				streamFailoverErr = fmt.Errorf("restore OpenAI namespace response: %w", restoreErr)
				return
			}
			if !bytes.Equal(restoredData, dataBytes) {
				dataBytes = restoredData
				data = string(restoredData)
				line = "data: " + data
				eventType = strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
			}
			if imageOutput, ok := extractImageGenerationOutputFromSSEData(dataBytes, streamSeenImages); ok {
				streamImageOutputs = append(streamImageOutputs, imageOutput)
			}
			if responsesStreamEventMayContributeToOutput(eventType) {
				var streamEvent apicompat.ResponsesStreamEvent
				if err := json.Unmarshal(dataBytes, &streamEvent); err == nil {
					streamOutputAccumulator.ProcessEvent(&streamEvent)
				}
			}
			if normalizedData, normalized := normalizeResponsesStreamingTerminalOutput(dataBytes, streamOutputAccumulator, streamImageOutputs); normalized {
				dataBytes = normalizedData
				data = string(normalizedData)
				line = "data: " + data
				eventType = strings.TrimSpace(gjson.GetBytes(dataBytes, "type").String())
			}
			if sanitizedData, sanitized := sanitizeOpenAIResponseFailedEventForClient(
				dataBytes,
				eventType,
				openAIStreamClientOutputStarted(c, clientOutputStarted),
			); sanitized {
				dataBytes = sanitizedData
				data = string(sanitizedData)
				line = "data: " + data
			}
			// Replace model in response if needed.
			// Fast path: most events do not contain model field values.
			if needModelReplace && mappedModel != "" && strings.Contains(line, mappedModel) {
				line = s.replaceModelInSSELine(line, mappedModel, originalModel)
			}
			startsClientOutput := forceFlushFailedEvent || openAIStreamDataStartsClientOutput(data, eventType)

			// 鍐欏叆瀹㈡埛绔紙瀹㈡埛绔柇寮€鍚庣户缁?drain 涓婃父锛?
			if !clientDisconnected {
				shouldFlush := queueDrained && (clientOutputStarted || startsClientOutput)
				if firstTokenMs == nil && startsClientOutput {
					// 淇濊瘉棣栦釜 token 浜嬩欢灏藉揩鍑虹珯锛岄伩鍏嶅奖鍝?TTFT銆?
					shouldFlush = true
				}
				if _, err := bufferedWriter.WriteString(line); err != nil {
					clientDisconnected = true
					logger.LegacyPrintf("service.openai_gateway", "Client disconnected during streaming, continuing to drain upstream for billing")
				} else if _, err := bufferedWriter.WriteString("\n"); err != nil {
					clientDisconnected = true
					logger.LegacyPrintf("service.openai_gateway", "Client disconnected during streaming, continuing to drain upstream for billing")
				} else if shouldFlush {
					if err := flushBuffered(); err != nil {
						clientDisconnected = true
						logger.LegacyPrintf("service.openai_gateway", "Client disconnected during streaming flush, continuing to drain upstream for billing")
					} else {
						clientOutputStarted = true
						lastDownstreamWriteAt = time.Now()
					}
				}
			}

			// Record first token time
			if firstTokenMs == nil && startsClientOutput {
				ms := int(time.Since(startTime).Milliseconds())
				firstTokenMs = &ms
			}
			s.parseSSEUsageBytes(dataBytes, usage)
			imageCounter.AddSSEData(dataBytes)
			return
		}

		// Forward non-data lines as-is
		if !clientDisconnected {
			if _, err := bufferedWriter.WriteString(line); err != nil {
				clientDisconnected = true
				logger.LegacyPrintf("service.openai_gateway", "Client disconnected during streaming, continuing to drain upstream for billing")
			} else if _, err := bufferedWriter.WriteString("\n"); err != nil {
				clientDisconnected = true
				logger.LegacyPrintf("service.openai_gateway", "Client disconnected during streaming, continuing to drain upstream for billing")
			} else if queueDrained && clientOutputStarted {
				if err := flushBuffered(); err != nil {
					clientDisconnected = true
					logger.LegacyPrintf("service.openai_gateway", "Client disconnected during streaming flush, continuing to drain upstream for billing")
				} else {
					clientOutputStarted = true
					lastDownstreamWriteAt = time.Now()
				}
			}
		}
	}

	// 鏃犺秴鏃?鏃?keepalive 鐨勫父瑙佽矾寰勮蛋鍚屾鎵弿锛屽噺灏?goroutine 涓?channel 寮€閿€銆?
	if streamInterval <= 0 && keepaliveInterval <= 0 {
		defer putSSEScannerBuf64K(scanBuf)
		for scanner.Scan() {
			processSSELine(scanner.Text(), true)
			if streamFailoverErr != nil {
				return resultWithUsage(), streamFailoverErr
			}
		}
		if result, err, done := handleScanErr(scanner.Err()); done {
			return result, err
		}
		return finalizeStream()
	}

	type scanEvent struct {
		line string
		err  error
	}
	// 鐙珛 goroutine 璇诲彇涓婃父锛岄伩鍏嶈鍙栭樆濉炲奖鍝?keepalive/瓒呮椂澶勭悊
	events := make(chan scanEvent, 16)
	done := make(chan struct{})
	sendEvent := func(ev scanEvent) bool {
		select {
		case events <- ev:
			return true
		case <-done:
			return false
		}
	}
	var lastReadAt int64
	atomic.StoreInt64(&lastReadAt, time.Now().UnixNano())
	go func(scanBuf *sseScannerBuf64K) {
		defer putSSEScannerBuf64K(scanBuf)
		defer close(events)
		for scanner.Scan() {
			atomic.StoreInt64(&lastReadAt, time.Now().UnixNano())
			if !sendEvent(scanEvent{line: scanner.Text()}) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			_ = sendEvent(scanEvent{err: err})
		}
	}(scanBuf)
	defer close(done)

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return finalizeStream()
			}
			if result, err, done := handleScanErr(ev.err); done {
				return result, err
			}
			processSSELine(ev.line, len(events) == 0)
			if streamFailoverErr != nil {
				return resultWithUsage(), streamFailoverErr
			}

		case <-intervalCh:
			lastRead := time.Unix(0, atomic.LoadInt64(&lastReadAt))
			if time.Since(lastRead) < streamInterval {
				continue
			}
			if clientDisconnected {
				return resultWithUsage(), fmt.Errorf("stream usage incomplete after timeout")
			}
			logger.LegacyPrintf("service.openai_gateway", "Stream data interval timeout: account=%d model=%s interval=%s", account.ID, originalModel, streamInterval)
			// 澶勭悊娴佽秴鏃讹紝鍙兘鏍囪璐︽埛涓轰复鏃朵笉鍙皟搴︽垨閿欒鐘舵€?
			if s.rateLimitService != nil {
				s.rateLimitService.HandleStreamTimeout(ctx, account, originalModel)
			}
			sendErrorEvent("stream_timeout")
			return resultWithUsage(), fmt.Errorf("stream data interval timeout")

		case <-keepaliveCh:
			if clientDisconnected {
				continue
			}
			if time.Since(lastDownstreamWriteAt) < keepaliveInterval {
				continue
			}
			if _, err := bufferedWriter.WriteString(":\n\n"); err != nil {
				clientDisconnected = true
				logger.LegacyPrintf("service.openai_gateway", "Client disconnected during streaming, continuing to drain upstream for billing")
				continue
			}
			if err := flushBuffered(); err != nil {
				clientDisconnected = true
				logger.LegacyPrintf("service.openai_gateway", "Client disconnected during keepalive flush, continuing to drain upstream for billing")
			} else {
				lastDownstreamWriteAt = time.Now()
			}
		}
	}

}

func openAICacheCreationTokensFromGJSON(value gjson.Result) int {
	// OpenAI-compatible providers expose cache-write usage under several names.
	// Prefer nested detail fields because they are the canonical Responses/Chat shape.
	for _, path := range []string{
		"input_tokens_details.cache_write_tokens",
		"prompt_tokens_details.cache_write_tokens",
		"input_tokens_details.cache_creation_tokens",
		"prompt_tokens_details.cache_creation_tokens",
		"cache_creation_input_tokens",
		"cache_write_input_tokens",
		"cache_creation_tokens",
		"cache_write_tokens",
	} {
		result := value.Get(path)
		if !result.Exists() {
			continue
		}
		tokens := int(result.Int())
		if tokens < 0 {
			return 0
		}
		return tokens
	}
	return 0
}

func openAIUsageTokens(usage OpenAIUsage) (UsageTokens, int) {
	cacheReadTokens := usage.CacheReadInputTokens
	if cacheReadTokens == 0 {
		cacheReadTokens = usage.TextCacheReadInputTokens + usage.ImageCacheReadInputTokens
	}
	if cacheReadTokens < 0 {
		cacheReadTokens = 0
	}

	cacheCreationTokens := nonNegativeOpenAITokenCount(usage.CacheCreationInputTokens)
	actualInputTokens := usage.InputTokens - cacheReadTokens - cacheCreationTokens
	if actualInputTokens < 0 {
		actualInputTokens = 0
	}

	textInputTokens := nonNegativeOpenAITokenCount(usage.TextInputTokens)
	imageInputTokens := nonNegativeOpenAITokenCount(usage.ImageInputTokens)
	textCacheReadTokens := nonNegativeOpenAITokenCount(usage.TextCacheReadInputTokens)
	imageCacheReadTokens := nonNegativeOpenAITokenCount(usage.ImageCacheReadInputTokens)

	if textCacheReadTokens > textInputTokens {
		textCacheReadTokens = textInputTokens
	}
	if imageCacheReadTokens > imageInputTokens {
		imageCacheReadTokens = imageInputTokens
	}
	textInputTokens -= textCacheReadTokens
	imageInputTokens -= imageCacheReadTokens

	remainingCached := cacheReadTokens - textCacheReadTokens - imageCacheReadTokens
	if remainingCached > 0 {
		if textInputTokens >= remainingCached {
			textInputTokens -= remainingCached
			remainingCached = 0
		} else {
			remainingCached -= textInputTokens
			textInputTokens = 0
		}
		if remainingCached > 0 {
			if imageInputTokens >= remainingCached {
				imageInputTokens -= remainingCached
			} else {
				imageInputTokens = 0
			}
		}
	}

	// Responses input_tokens includes cache-write tokens. Remove them from the
	// normal input buckets as well, otherwise they are billed once as input and
	// again as cache creation.
	remainingCacheCreation := cacheCreationTokens
	if remainingCacheCreation > 0 {
		if textInputTokens >= remainingCacheCreation {
			textInputTokens -= remainingCacheCreation
			remainingCacheCreation = 0
		} else {
			remainingCacheCreation -= textInputTokens
			textInputTokens = 0
		}
		if remainingCacheCreation > 0 {
			if imageInputTokens >= remainingCacheCreation {
				imageInputTokens -= remainingCacheCreation
			} else {
				imageInputTokens = 0
			}
		}
	}

	classifiedInputTokens := textInputTokens + imageInputTokens
	unclassifiedInputTokens := actualInputTokens
	if classifiedInputTokens > 0 {
		unclassifiedInputTokens -= classifiedInputTokens
		if unclassifiedInputTokens < 0 {
			unclassifiedInputTokens = 0
		}
	}

	if imageCacheReadTokens > cacheReadTokens {
		imageCacheReadTokens = cacheReadTokens
	}

	return UsageTokens{
		InputTokens:          unclassifiedInputTokens,
		TextInputTokens:      textInputTokens,
		ImageInputTokens:     imageInputTokens,
		OutputTokens:         usage.OutputTokens,
		CacheCreationTokens:  cacheCreationTokens,
		CacheReadTokens:      cacheReadTokens,
		ImageCacheReadTokens: imageCacheReadTokens,
		ImageOutputTokens:    usage.ImageOutputTokens,
	}, actualInputTokens
}

func nonNegativeOpenAITokenCount(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func hasBillableOpenAITokens(tokens UsageTokens) bool {
	return tokens.InputTokens > 0 ||
		tokens.TextInputTokens > 0 ||
		tokens.ImageInputTokens > 0 ||
		tokens.OutputTokens > 0 ||
		tokens.CacheCreationTokens > 0 ||
		tokens.CacheReadTokens > 0 ||
		tokens.ImageOutputTokens > 0
}

func (s *OpenAIGatewayService) autoRepairOpenAIFreeAccountFromCodexSnapshot(ctx context.Context, accountID int64) {
	if s == nil || s.settingService == nil || s.accountService == nil || accountID <= 0 {
		return
	}
	enabled, threshold := s.settingService.GetOpenAIFreeAccountRepairSettings(ctx)
	if !enabled || threshold <= 0 {
		return
	}
	reason := fmt.Sprintf("OpenAI Codex 7d quota exhausted with weekly limit <= %.2f USD; account level repaired to free and public sharing suspended", threshold)
	account, repaired, err := s.accountService.AutoRepairSuspectedOpenAIFreeAccount(ctx, accountID, threshold, reason)
	if err != nil {
		slog.Warn("failed to auto repair suspected OpenAI free account", "account_id", accountID, "threshold_usd", threshold, "error", err)
		return
	}
	if repaired && account != nil {
		slog.Info("auto repaired suspected OpenAI free account", "account_id", accountID, "threshold_usd", threshold, "share_status", account.ShareStatus)
	}
}

func extractOpenAIModelFromRequestBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	return strings.TrimSpace(gjson.GetBytes(body, "model").String())
}
