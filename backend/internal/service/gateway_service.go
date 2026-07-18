package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	mathrand "math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/cespare/xxhash/v2"
	"github.com/google/uuid"
	gocache "github.com/patrickmn/go-cache"
	"github.com/tidwall/gjson"
	"golang.org/x/sync/singleflight"

	"github.com/gin-gonic/gin"
)

const (
	claudeAPIURL            = "https://api.anthropic.com/v1/messages?beta=true"
	claudeAPICountTokensURL = "https://api.anthropic.com/v1/messages/count_tokens?beta=true"
	stickySessionTTL        = time.Hour // 粘性会话TTL
	anthropicAffinityTTL    = 7 * 24 * time.Hour
	clientAffinityTTL       = anthropicAffinityTTL
	accountUserAffinityTTL  = anthropicAffinityTTL
	accountProxyExitIPTTL   = anthropicAffinityTTL
	defaultMaxLineSize      = 500 * 1024 * 1024
	// Canonical Claude Code banner. Keep it EXACT (no trailing whitespace/newlines)
	// to match real Claude CLI traffic as closely as possible. When we need a visual
	// separator between system blocks, we add "\n\n" at concatenation time.
	claudeCodeSystemPrompt          = "You are Claude Code, Anthropic's official CLI for Claude."
	claudeCodeSystemPromptExpansion = `You are an interactive agent that helps users with software engineering tasks. Use the instructions below and the tools available to you to assist the user.

IMPORTANT: Assist with authorized security testing, defensive security, CTF challenges, and educational contexts. Refuse requests for destructive techniques, DoS attacks, mass targeting, supply chain compromise, or detection evasion for malicious purposes. Dual-use security tools (C2 frameworks, credential testing, exploit development) require clear authorization context: pentesting engagements, CTF competitions, security research, or defensive use cases.
IMPORTANT: You must NEVER generate or guess URLs for the user unless you are confident that the URLs are for helping the user with programming. You may use URLs provided by the user in their messages or local files.

# Tone and style
 - Only use emojis if the user explicitly requests it. Avoid using emojis in all communication unless asked.
 - Your responses should be short and concise.
 - When referencing specific functions or pieces of code include the pattern file_path:line_number to allow the user to easily navigate to the source code location.
 - When referencing GitHub issues or pull requests, use the owner/repo#123 format (e.g. anthropics/claude-code#100) so they render as clickable links.
 - Do not use a colon before tool calls. Your tool calls may not be shown directly in the output, so text like "Let me read the file:" followed by a read tool call should just be "Let me read the file." with a period.`
	maxCacheControlBlocks = 4 // Anthropic API 允许的最大 cache_control 块数量

	defaultUserGroupRateCacheTTL           = 30 * time.Second
	defaultModelsListCacheTTL              = 15 * time.Second
	postUsageBillingTimeout                = 15 * time.Second
	claudeCodeNoopDeltaKeepaliveMinVersion = "2.1.193"
	debugGatewayBodyEnv                    = "SUB2API_DEBUG_GATEWAY_BODY"
	// Upstream error bodies only need enough data for structured error extraction and logging.
	gatewayUpstreamErrorBodyReadLimit int64 = 512 << 10
)

const (
	claudeMimicDebugInfoKey = "claude_mimic_debug_info"
)

const (
	clientAffinityKeyPrefix      = "client_affinity:"
	accountUserAffinityKeyPrefix = "account_user_affinity:"
	accountProxyExitIPKeyPrefix  = "account_proxy_exit_ip:"
)

const (
	cacheTTLTarget5m = "5m"
	cacheTTLTarget1h = "1h"
)

// ForceCacheBillingContextKey 强制缓存计费上下文键
// 用于粘性会话切换时，将 input_tokens 转为 cache_read_input_tokens 计费
type forceCacheBillingKeyType struct{}

// accountWithLoad 账号与负载信息的组合，用于负载感知调度
type accountWithLoad struct {
	account        *Account
	loadInfo       *AccountLoadInfo
	runtimePenalty int
}

var ForceCacheBillingContextKey = forceCacheBillingKeyType{}

var (
	windowCostPrefetchCacheHitTotal  atomic.Int64
	windowCostPrefetchCacheMissTotal atomic.Int64
	windowCostPrefetchBatchSQLTotal  atomic.Int64
	windowCostPrefetchFallbackTotal  atomic.Int64
	windowCostPrefetchErrorTotal     atomic.Int64

	userGroupRateCacheHitTotal      atomic.Int64
	userGroupRateCacheMissTotal     atomic.Int64
	userGroupRateCacheLoadTotal     atomic.Int64
	userGroupRateCacheSFSharedTotal atomic.Int64
	userGroupRateCacheFallbackTotal atomic.Int64

	modelsListCacheHitTotal   atomic.Int64
	modelsListCacheMissTotal  atomic.Int64
	modelsListCacheStoreTotal atomic.Int64

	userPlatformQuotaDBIncrErrorTotal           atomic.Int64
	userPlatformQuotaDBIncrLegacyErrorTotal     atomic.Int64
	userPlatformQuotaSentinelSetCacheErrorTotal atomic.Int64
)

func GatewayWindowCostPrefetchStats() (cacheHit, cacheMiss, batchSQL, fallback, errCount int64) {
	return windowCostPrefetchCacheHitTotal.Load(),
		windowCostPrefetchCacheMissTotal.Load(),
		windowCostPrefetchBatchSQLTotal.Load(),
		windowCostPrefetchFallbackTotal.Load(),
		windowCostPrefetchErrorTotal.Load()
}

func GatewayUserGroupRateCacheStats() (cacheHit, cacheMiss, load, singleflightShared, fallback int64) {
	return userGroupRateCacheHitTotal.Load(),
		userGroupRateCacheMissTotal.Load(),
		userGroupRateCacheLoadTotal.Load(),
		userGroupRateCacheSFSharedTotal.Load(),
		userGroupRateCacheFallbackTotal.Load()
}

func GatewayModelsListCacheStats() (cacheHit, cacheMiss, store int64) {
	return modelsListCacheHitTotal.Load(), modelsListCacheMissTotal.Load(), modelsListCacheStoreTotal.Load()
}

func GatewayUserPlatformQuotaIncrStats() (mainPathErr, legacyPathErr, sentinelSetErr int64) {
	return userPlatformQuotaDBIncrErrorTotal.Load(),
		userPlatformQuotaDBIncrLegacyErrorTotal.Load(),
		userPlatformQuotaSentinelSetCacheErrorTotal.Load()
}

var claudeCliUserAgentRe = regexp.MustCompile(`(?i)^claude-cli/\d+\.\d+\.\d+`)

func openAIStreamEventIsTerminal(data string) bool {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return false
	}
	if trimmed == "[DONE]" {
		return true
	}
	switch gjson.Get(trimmed, "type").String() {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
		return true
	default:
		return false
	}
}

func anthropicStreamEventIsTerminal(eventName, data string) bool {
	if strings.EqualFold(strings.TrimSpace(eventName), "message_stop") {
		return true
	}
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return false
	}
	if trimmed == "[DONE]" {
		return true
	}
	return gjson.Get(trimmed, "type").String() == "message_stop"
}

func cloneStringSlice(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

// IsForceCacheBilling 检查是否启用强制缓存计费
func IsForceCacheBilling(ctx context.Context) bool {
	v, _ := ctx.Value(ForceCacheBillingContextKey).(bool)
	return v
}

// WithForceCacheBilling 返回带有强制缓存计费标记的上下文
func WithForceCacheBilling(ctx context.Context) context.Context {
	return context.WithValue(ctx, ForceCacheBillingContextKey, true)
}

func (s *GatewayService) debugModelRoutingEnabled() bool {
	if s == nil {
		return false
	}
	return s.debugModelRouting.Load()
}

func (s *GatewayService) debugClaudeMimicEnabled() bool {
	if s == nil {
		return false
	}
	return s.debugClaudeMimic.Load()
}

func parseDebugEnvBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func shortSessionHash(sessionHash string) string {
	if sessionHash == "" {
		return ""
	}
	if len(sessionHash) <= 8 {
		return sessionHash
	}
	return sessionHash[:8]
}

func redactAuthHeaderValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	// Keep scheme for debugging, redact secret.
	if strings.HasPrefix(strings.ToLower(v), "bearer ") {
		return "Bearer [redacted]"
	}
	return "[redacted]"
}

func safeHeaderValueForLog(key string, v string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "authorization", "x-api-key":
		return redactAuthHeaderValue(v)
	default:
		return strings.TrimSpace(v)
	}
}

func extractSystemPreviewFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	sys := gjson.GetBytes(body, "system")
	if !sys.Exists() {
		return ""
	}

	switch {
	case sys.IsArray():
		for _, item := range sys.Array() {
			if !item.IsObject() {
				continue
			}
			if strings.EqualFold(item.Get("type").String(), "text") {
				if t := item.Get("text").String(); strings.TrimSpace(t) != "" {
					return t
				}
			}
		}
		return ""
	case sys.Type == gjson.String:
		return sys.String()
	default:
		return ""
	}
}

func buildClaudeMimicDebugLine(req *http.Request, body []byte, account *Account, tokenType string, mimicClaudeCode bool) string {
	if req == nil {
		return ""
	}

	// Only log a minimal fingerprint to avoid leaking user content.
	interesting := []string{
		"user-agent",
		"x-app",
		"anthropic-dangerous-direct-browser-access",
		"anthropic-version",
		"anthropic-beta",
		"x-stainless-lang",
		"x-stainless-package-version",
		"x-stainless-os",
		"x-stainless-arch",
		"x-stainless-runtime",
		"x-stainless-runtime-version",
		"x-stainless-retry-count",
		"x-stainless-timeout",
		"authorization",
		"x-api-key",
		"content-type",
		"accept",
		"x-stainless-helper-method",
	}

	h := make([]string, 0, len(interesting))
	for _, k := range interesting {
		if v := getHeaderRaw(req.Header, k); v != "" {
			h = append(h, fmt.Sprintf("%s=%q", k, safeHeaderValueForLog(k, v)))
		}
	}

	metaUserID := strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String())
	sysPreview := strings.TrimSpace(extractSystemPreviewFromBody(body))

	// Truncate preview to keep logs sane.
	if len(sysPreview) > 300 {
		sysPreview = sysPreview[:300] + "..."
	}
	sysPreview = strings.ReplaceAll(sysPreview, "\n", "\\n")
	sysPreview = strings.ReplaceAll(sysPreview, "\r", "\\r")

	aid := int64(0)
	aname := ""
	if account != nil {
		aid = account.ID
		aname = account.Name
	}

	return fmt.Sprintf(
		"url=%s account=%d(%s) tokenType=%s mimic=%t meta.user_id=%q system.preview=%q headers={%s}",
		req.URL.String(),
		aid,
		aname,
		tokenType,
		mimicClaudeCode,
		metaUserID,
		sysPreview,
		strings.Join(h, " "),
	)
}

func logClaudeMimicDebug(req *http.Request, body []byte, account *Account, tokenType string, mimicClaudeCode bool) {
	line := buildClaudeMimicDebugLine(req, body, account, tokenType, mimicClaudeCode)
	if line == "" {
		return
	}
	logger.LegacyPrintf("service.gateway", "[ClaudeMimicDebug] %s", line)
}

func isClaudeCodeCredentialScopeError(msg string) bool {
	m := strings.ToLower(strings.TrimSpace(msg))
	if m == "" {
		return false
	}
	return strings.Contains(m, "only authorized for use with claude code") &&
		strings.Contains(m, "cannot be used for other api requests")
}

func isClaudeCodeClientRestrictionError(statusCode int, upstreamMsg string, responseBody []byte) bool {
	if statusCode < 400 {
		return false
	}

	normalized := normalizedUpstreamErrorText(upstreamMsg, responseBody)
	if normalized == "" {
		return false
	}
	if isClaudeCodeCredentialScopeError(normalized) {
		return true
	}
	return strings.Contains(normalized, "only allows claude code") ||
		strings.Contains(normalized, "only supports claude code") ||
		strings.Contains(normalized, "restricted to claude code")
}

func logClaudeMimicDebugOnError(c *gin.Context, statusCode int, requestID string) {
	if c == nil {
		return
	}
	if v, ok := c.Get(claudeMimicDebugInfoKey); ok {
		if line, ok := v.(string); ok && strings.TrimSpace(line) != "" {
			logger.LegacyPrintf("service.gateway", "[ClaudeMimicDebugOnError] status=%d request_id=%s %s",
				statusCode,
				requestID,
				line,
			)
		}
	}
}

// sseDataRe matches SSE data lines with optional whitespace after colon.
// Some upstream APIs return non-standard "data:" without space (should be "data: ").
var (
	sseDataRe = regexp.MustCompile(`^data:\s*`)

	// claudeCodePromptPrefixes 用于检测 Claude Code 系统提示词的前缀列表
	// 支持多种变体：标准版、Agent SDK 版、Explore Agent 版、Compact 版等
	// 注意：前缀之间不应存在包含关系，否则会导致冗余匹配
	claudeCodePromptPrefixes = []string{
		"You are Claude Code, Anthropic's official CLI for Claude",             // 标准版 & Agent SDK 版（含 running within...）
		"You are a Claude agent, built on Anthropic's Claude Agent SDK",        // Agent SDK 变体
		"You are a file search specialist for Claude Code",                     // Explore Agent 版
		"You are a helpful AI assistant tasked with summarizing conversations", // Compact 版
	}
)

// ErrNoAvailableAccounts 表示没有可用的账号
var ErrNoAvailableAccounts = errors.New("no available accounts")

// ErrAccountRPMExceeded 表示账号级 RPM 调度限制已达到。
var ErrAccountRPMExceeded = infraerrors.TooManyRequests("ACCOUNT_RPM_EXCEEDED", "account requests-per-minute limit exceeded")

// ErrClaudeCodeOnly 表示分组仅允许 Claude Code 客户端访问
var ErrClaudeCodeOnly = errors.New("this group only allows Claude Code clients")

// allowedHeaders 白名单headers（参考CRS项目）
var allowedHeaders = map[string]bool{
	"accept":                                    true,
	"x-stainless-retry-count":                   true,
	"x-stainless-timeout":                       true,
	"x-stainless-lang":                          true,
	"x-stainless-package-version":               true,
	"x-stainless-os":                            true,
	"x-stainless-arch":                          true,
	"x-stainless-runtime":                       true,
	"x-stainless-runtime-version":               true,
	"x-stainless-helper-method":                 true,
	"anthropic-dangerous-direct-browser-access": true,
	"anthropic-version":                         true,
	"x-app":                                     true,
	"anthropic-beta":                            true,
	"accept-language":                           true,
	"sec-fetch-mode":                            true,
	"user-agent":                                true,
	"content-type":                              true,
	"accept-encoding":                           true,
	"x-claude-code-session-id":                  true,
	"x-client-request-id":                       true,
}

// GatewayCache 定义网关服务的缓存操作接口。
// 提供粘性会话（Sticky Session）的存储、查询、刷新和删除功能。
//
// GatewayCache defines cache operations for gateway service.
// Provides sticky session storage, retrieval, refresh and deletion capabilities.
type GatewayCache interface {
	// GetSessionAccountID 获取粘性会话绑定的账号 ID
	// Get the account ID bound to a sticky session
	GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error)
	// SetSessionAccountID 设置粘性会话与账号的绑定关系
	// Set the binding between sticky session and account
	SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error
	// RefreshSessionTTL 刷新粘性会话的过期时间
	// Refresh the expiration time of a sticky session
	RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error
	// DeleteSessionAccountID 删除粘性会话绑定，用于账号不可用时主动清理
	// Delete sticky session binding, used to proactively clean up when account becomes unavailable
	DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error
	// GetSessionString 获取会话维度字符串值，用于 WS 多实例状态索引。
	GetSessionString(ctx context.Context, groupID int64, sessionHash string) (string, error)
	// SetSessionString 设置会话维度字符串值，用于 WS 多实例状态索引。
	SetSessionString(ctx context.Context, groupID int64, sessionHash string, value string, ttl time.Duration) error
	// DeleteSessionString 删除会话维度字符串值。
	DeleteSessionString(ctx context.Context, groupID int64, sessionHash string) error
}

var ErrGatewaySessionStringNotFound = errors.New("gateway session string not found")

// derefGroupID safely dereferences *int64 to int64, returning 0 if nil
func derefGroupID(groupID *int64) int64 {
	if groupID == nil {
		return 0
	}
	return *groupID
}

func resolveUserGroupRateCacheTTL(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.Gateway.UserGroupRateCacheTTLSeconds <= 0 {
		return defaultUserGroupRateCacheTTL
	}
	return time.Duration(cfg.Gateway.UserGroupRateCacheTTLSeconds) * time.Second
}

func resolveModelsListCacheTTL(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.Gateway.ModelsListCacheTTLSeconds <= 0 {
		return defaultModelsListCacheTTL
	}
	return time.Duration(cfg.Gateway.ModelsListCacheTTLSeconds) * time.Second
}

func modelsListCacheKey(groupID *int64, platform string) string {
	return fmt.Sprintf("%d|%s", derefGroupID(groupID), strings.TrimSpace(platform))
}

func prefetchedStickyGroupIDFromContext(ctx context.Context) (int64, bool) {
	return PrefetchedStickyGroupIDFromContext(ctx)
}

func prefetchedStickyAccountIDFromContext(ctx context.Context, groupID *int64) int64 {
	prefetchedGroupID, ok := prefetchedStickyGroupIDFromContext(ctx)
	if !ok || prefetchedGroupID != derefGroupID(groupID) {
		return 0
	}
	if accountID, ok := PrefetchedStickyAccountIDFromContext(ctx); ok && accountID > 0 {
		return accountID
	}
	return 0
}

// shouldClearStickySession 检查账号是否处于不可调度状态，需要清理粘性会话绑定。
// 委托 IsSchedulable() 判断账号级可调度性（状态、配额、过载、限流等），
// 额外检查模型级限流。
//
// shouldClearStickySession checks if an account is in an unschedulable state
// and the sticky session binding should be cleared.
// Delegates to IsSchedulable() for account-level checks, plus model-level rate limiting.
func shouldClearStickySession(account *Account, requestedModel string) bool {
	if account == nil {
		return false
	}
	if !account.IsSchedulable() {
		return true
	}
	if remaining := account.GetRateLimitRemainingTimeWithContext(context.Background(), requestedModel); remaining > 0 {
		return true
	}
	return false
}

type AccountWaitPlan struct {
	AccountID      int64
	MaxConcurrency int
	Timeout        time.Duration
	MaxWaiting     int
}

type AccountSelectionResult struct {
	Account     *Account
	Acquired    bool
	ReleaseFunc func()
	WaitPlan    *AccountWaitPlan // nil means no wait allowed
}

// ClaudeUsage 表示Claude API返回的usage信息
type ClaudeUsage struct {
	InputTokens                 int  `json:"input_tokens"`
	OutputTokens                int  `json:"output_tokens"`
	CacheCreationInputTokens    int  `json:"cache_creation_input_tokens"`
	CacheReadInputTokens        int  `json:"cache_read_input_tokens"`
	CacheCreation5mTokens       int  // 5分钟缓存创建token（来自嵌套 cache_creation 对象）
	CacheCreation1hTokens       int  // 1小时缓存创建token（来自嵌套 cache_creation 对象）
	ImageOutputTokens           int  `json:"image_output_tokens,omitempty"`
	InputTokensIncludeCacheRead bool `json:"-"`
}

// ForwardResult 转发结果
type ForwardResult struct {
	RequestID string
	Usage     ClaudeUsage
	Model     string
	// UpstreamModel is the actual upstream model after mapping.
	// Prefer empty when it is identical to Model; persistence normalizes equal values away as no-op mappings.
	UpstreamModel    string
	Stream           bool
	Duration         time.Duration
	FirstTokenMs     *int // 首字时间（流式请求）
	ClientDisconnect bool // 客户端是否在流式传输过程中断开
	ReasoningEffort  *string

	// 图片生成计费字段（图片生成模型使用）
	ImageCount         int    // 生成的图片数量
	ImageSize          string // 最终计费尺寸 "1K", "2K", "4K"
	ImageInputSize     string // 请求中的原始图片尺寸
	ImageOutputSize    string // 上游响应中的图片尺寸
	ImageOutputSizes   []string
	ImageSizeSource    string
	ImageSizeBreakdown map[string]int
}

// BillableStreamUsageError 表示流式响应未完整结束，但上游已经返回了可计费 usage。
// 调用方应记录并扣除 result 中已收集的 usage，同时保留原始错误用于日志和异常处理。
type BillableStreamUsageError struct {
	Err error
}

func (e *BillableStreamUsageError) Error() string {
	if e == nil || e.Err == nil {
		return "billable stream usage incomplete"
	}
	return e.Err.Error()
}

func (e *BillableStreamUsageError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func IsBillableStreamUsageError(err error) bool {
	var target *BillableStreamUsageError
	return errors.As(err, &target)
}

func ForwardResultHasBillableUsage(result *ForwardResult) bool {
	return result != nil && claudeUsageHasBillableTokens(&result.Usage)
}

func claudeUsageHasBillableTokens(usage *ClaudeUsage) bool {
	if usage == nil {
		return false
	}
	return usage.InputTokens > 0 ||
		usage.OutputTokens > 0 ||
		usage.CacheCreationInputTokens > 0 ||
		usage.CacheReadInputTokens > 0 ||
		usage.CacheCreation5mTokens > 0 ||
		usage.CacheCreation1hTokens > 0 ||
		usage.ImageOutputTokens > 0
}

func normalizeClaudeCompatibleUsageForBilling(usage *ClaudeUsage) {
	if usage == nil || !usage.InputTokensIncludeCacheRead {
		return
	}
	if usage.CacheReadInputTokens <= 0 || usage.InputTokens <= 0 {
		usage.InputTokensIncludeCacheRead = false
		return
	}
	usage.InputTokens -= usage.CacheReadInputTokens
	if usage.InputTokens < 0 {
		usage.InputTokens = 0
	}
	usage.InputTokensIncludeCacheRead = false
}

func streamingResultHasBillableUsage(result *streamingResult) bool {
	return result != nil && claudeUsageHasBillableTokens(result.usage)
}

// GatewayFailureStage identifies which request stage failed. The zero value is
// intentionally treated as inference so existing UpstreamFailoverError callers
// retain their current behavior.
type GatewayFailureStage string

const (
	GatewayFailureStageInference   GatewayFailureStage = "inference"
	GatewayFailureStageAccountAuth GatewayFailureStage = "account_auth"
)

// GatewayFailureScope identifies whether selecting another account can help.
type GatewayFailureScope string

const (
	GatewayFailureScopeAccount  GatewayFailureScope = "account"
	GatewayFailureScopeProvider GatewayFailureScope = "provider"
	GatewayFailureScopeRequest  GatewayFailureScope = "request"
)

// NextAccountAction is tri-state for backwards compatibility. The zero value
// means legacy retry behavior; only NextAccountStop explicitly short-circuits.
type NextAccountAction uint8

const (
	NextAccountLegacyRetry NextAccountAction = iota
	NextAccountRetry
	NextAccountStop
)

type GatewayFailureReason string

// UpstreamFailoverError indicates an upstream or credential error that may
// trigger account failover. Additive metadata keeps existing composite literals
// source-compatible and preserves their legacy retry-next-account behavior.
type UpstreamFailoverError struct {
	StatusCode               int
	ResponseBody             []byte      // 上游响应体，用于错误透传规则匹配
	ResponseHeaders          http.Header // 上游响应头，用于透传 cf-ray/cf-mitigated/content-type 等诊断信息
	ForceCacheBilling        bool        // Antigravity 粘性会话切换时设为 true
	RetryableOnSameAccount   bool        // 临时性错误（如 Google 间歇性 400、空响应），应在同一账号上重试 N 次再切换
	SafeToFailoverAfterWrite bool        // 仅写出 SSE 注释等非语义字节时，仍可在同一客户端流中切换账号
	Stage                    GatewayFailureStage
	Scope                    GatewayFailureScope
	Reason                   GatewayFailureReason
	NextAccountAction        NextAccountAction
	ClientStatusCode         int
	ClientMessage            string
}

func (e *UpstreamFailoverError) Error() string {
	if e != nil && e.Stage == GatewayFailureStageAccountAuth {
		return fmt.Sprintf("credential failure: %s (failover)", e.Reason)
	}
	return fmt.Sprintf("upstream error: %d (failover)", e.StatusCode)
}

func (e *UpstreamFailoverError) ShouldRetryNextAccount() bool {
	return e != nil && e.NextAccountAction != NextAccountStop
}

func (e *UpstreamFailoverError) IsCredentialFailure() bool {
	return e != nil && e.Stage == GatewayFailureStageAccountAuth
}

// ShouldReportAccountScheduleFailure prevents provider- and request-scoped
// credential failures from being attributed to the selected account.
func (e *UpstreamFailoverError) ShouldReportAccountScheduleFailure() bool {
	if e == nil {
		return false
	}
	return !e.IsCredentialFailure() || e.Scope == GatewayFailureScopeAccount
}

// sseStreamErrorEventError 表示上游 SSE 流内出现 event:error 帧。
// RawData 保留 data: 行原始 JSON，供 failover 与 ops 日志保留真实上游错误。
type sseStreamErrorEventError struct {
	RawData      string
	StatusCode   int
	ResponseBody []byte
	Message      string
}

func (e *sseStreamErrorEventError) Error() string { return "have error in stream" }

// TempUnscheduleRetryableError 对 RetryableOnSameAccount 类型的 failover 错误触发临时封禁。
// 由 handler 层在同账号重试全部用尽、切换账号时调用。
func (s *GatewayService) TempUnscheduleRetryableError(ctx context.Context, accountID int64, failoverErr *UpstreamFailoverError) {
	if failoverErr == nil || !failoverErr.RetryableOnSameAccount {
		return
	}
	account, ok := retryableFailoverTempUnscheduleAccount(ctx, s.accountRepo, accountID, failoverErr)
	if !ok {
		return
	}
	// 根据状态码选择封禁策略
	var tempUnschedCache TempUnschedCache
	if s.rateLimitService != nil {
		tempUnschedCache = s.rateLimitService.tempUnschedCache
	}
	switch failoverErr.StatusCode {
	case http.StatusBadRequest:
		tempUnscheduleGoogleConfigError(ctx, s.accountRepo, tempUnschedCache, account, "[handler]")
	case http.StatusBadGateway:
		tempUnscheduleEmptyResponse(ctx, s.accountRepo, tempUnschedCache, account, "[handler]")
	default:
		tempUnscheduleRetryableStatusError(ctx, s.accountRepo, tempUnschedCache, account, failoverErr.StatusCode, failoverErr.ResponseBody, "[handler]")
	}
}

func (s *GatewayService) notePrivacyRequiredAccountSkipped(account *Account, group *Group) {
	if account == nil || group == nil {
		return
	}
	slog.Info("gateway_privacy_required_account_skipped",
		"account_id", account.ID,
		"group_id", group.ID,
		"group_name", group.Name,
	)
}

// GatewayService handles API gateway operations
type GatewayService struct {
	accountRepo            AccountRepository
	accountSharePolicyRepo AccountSharePolicyRepository
	groupRepo              GroupRepository
	usageLogRepo           UsageLogRepository
	usageBillingRepo       UsageBillingRepository
	userRepo               UserRepository
	userSubRepo            UserSubscriptionRepository
	userGroupRateRepo      UserGroupRateRepository
	cache                  GatewayCache
	digestStore            *DigestSessionStore
	cfg                    *config.Config
	schedulerSnapshot      *SchedulerSnapshotService
	billingService         *BillingService
	rateLimitService       *RateLimitService
	billingCacheService    *BillingCacheService
	identityService        *IdentityService
	httpUpstream           HTTPUpstream
	deferredService        *DeferredService
	concurrencyService     *ConcurrencyService
	claudeTokenProvider    *ClaudeTokenProvider
	sessionLimitCache      SessionLimitCache // 会话数量限制缓存（仅 Anthropic OAuth/SetupToken）
	rpmCache               RPMCache          // RPM 计数缓存（仅 Anthropic OAuth/SetupToken）
	proxyLatencyCache      ProxyLatencyCache
	userGroupRateResolver  *userGroupRateResolver
	userGroupRateCache     *gocache.Cache
	userGroupRateSF        singleflight.Group
	modelsListCache        *gocache.Cache
	modelsListCacheTTL     time.Duration
	settingService         *SettingService
	responseHeaderFilter   *responseheaders.CompiledHeaderFilter
	debugModelRouting      atomic.Bool
	debugClaudeMimic       atomic.Bool
	channelService         *ChannelService
	resolver               *ModelPricingResolver
	debugGatewayBodyFile   atomic.Pointer[os.File] // non-nil when SUB2API_DEBUG_GATEWAY_BODY is set
	tlsFPProfileService    *TLSFingerprintProfileService
	balanceNotifyService   *BalanceNotifyService
	accountRuntimeStats    *accountRuntimeStats
	userPlatformQuotaRepo  UserPlatformQuotaRepository
}

// NewGatewayService creates a new GatewayService
func NewGatewayService(
	accountRepo AccountRepository,
	accountSharePolicyRepo AccountSharePolicyRepository,
	groupRepo GroupRepository,
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
	identityService *IdentityService,
	httpUpstream HTTPUpstream,
	deferredService *DeferredService,
	claudeTokenProvider *ClaudeTokenProvider,
	sessionLimitCache SessionLimitCache,
	rpmCache RPMCache,
	digestStore *DigestSessionStore,
	settingService *SettingService,
	tlsFPProfileService *TLSFingerprintProfileService,
	channelService *ChannelService,
	resolver *ModelPricingResolver,
	balanceNotifyService *BalanceNotifyService,
	userPlatformQuotaRepos ...UserPlatformQuotaRepository,
) *GatewayService {
	userGroupRateTTL := resolveUserGroupRateCacheTTL(cfg)
	modelsListTTL := resolveModelsListCacheTTL(cfg)

	svc := &GatewayService{
		accountRepo:            accountRepo,
		accountSharePolicyRepo: accountSharePolicyRepo,
		groupRepo:              groupRepo,
		usageLogRepo:           usageLogRepo,
		usageBillingRepo:       usageBillingRepo,
		userRepo:               userRepo,
		userSubRepo:            userSubRepo,
		userGroupRateRepo:      userGroupRateRepo,
		cache:                  cache,
		digestStore:            digestStore,
		cfg:                    cfg,
		schedulerSnapshot:      schedulerSnapshot,
		concurrencyService:     concurrencyService,
		billingService:         billingService,
		rateLimitService:       rateLimitService,
		billingCacheService:    billingCacheService,
		identityService:        identityService,
		httpUpstream:           httpUpstream,
		deferredService:        deferredService,
		claudeTokenProvider:    claudeTokenProvider,
		sessionLimitCache:      sessionLimitCache,
		rpmCache:               rpmCache,
		userGroupRateCache:     gocache.New(userGroupRateTTL, time.Minute),
		settingService:         settingService,
		modelsListCache:        gocache.New(modelsListTTL, time.Minute),
		modelsListCacheTTL:     modelsListTTL,
		responseHeaderFilter:   compileResponseHeaderFilter(cfg),
		tlsFPProfileService:    tlsFPProfileService,
		channelService:         channelService,
		resolver:               resolver,
		balanceNotifyService:   balanceNotifyService,
		accountRuntimeStats:    newAccountRuntimeStats(),
	}
	if len(userPlatformQuotaRepos) > 0 {
		svc.userPlatformQuotaRepo = userPlatformQuotaRepos[0]
	}
	svc.userGroupRateResolver = newUserGroupRateResolver(
		userGroupRateRepo,
		svc.userGroupRateCache,
		userGroupRateTTL,
		&svc.userGroupRateSF,
		"service.gateway",
	)
	svc.debugModelRouting.Store(parseDebugEnvBool(os.Getenv("SUB2API_DEBUG_MODEL_ROUTING")))
	svc.debugClaudeMimic.Store(parseDebugEnvBool(os.Getenv("SUB2API_DEBUG_CLAUDE_MIMIC")))
	if path := strings.TrimSpace(os.Getenv(debugGatewayBodyEnv)); path != "" {
		svc.initDebugGatewayBodyFile(path)
	}
	return svc
}

func (s *GatewayService) SetProxyLatencyCache(cache ProxyLatencyCache) {
	if s == nil {
		return
	}
	s.proxyLatencyCache = cache
}

// GenerateSessionHash 从预解析请求计算粘性会话 hash
func (s *GatewayService) GenerateSessionHash(parsed *ParsedRequest) string {
	if parsed == nil {
		return ""
	}

	// 1. 最高优先级：从 metadata.user_id 提取 session_xxx
	if parsed.MetadataUserID != "" {
		uid := ParseMetadataUserID(parsed.MetadataUserID)
		if uid != nil && uid.SessionID != "" {
			slog.Info("sticky.hash_source",
				"source", "metadata_user_id",
				"session_id", uid.SessionID,
				"device_id", uid.DeviceID,
				"is_new_format", uid.IsNewFormat,
			)
			return uid.SessionID
		}
		slog.Info("sticky.hash_metadata_parse_failed",
			"metadata_user_id", parsed.MetadataUserID,
			"parsed_nil", uid == nil,
		)
	}

	// 2. 提取带 cache_control: {type: "ephemeral"} 的内容
	cacheableContent := s.extractCacheableContent(parsed)
	if cacheableContent != "" {
		hash := s.hashContent(cacheableContent)
		slog.Info("sticky.hash_source",
			"source", "cacheable_content",
			"hash", hash,
		)
		return hash
	}

	// 3. 最后 fallback: 使用 session上下文 + system + 所有消息的完整摘要串
	var combined strings.Builder
	// 混入请求上下文区分因子，避免不同用户相同消息产生相同 hash
	if parsed.SessionContext != nil {
		_, _ = combined.WriteString(parsed.SessionContext.ClientIP)
		_, _ = combined.WriteString(":")
		_, _ = combined.WriteString(NormalizeSessionUserAgent(parsed.SessionContext.UserAgent))
		_, _ = combined.WriteString(":")
		_, _ = combined.WriteString(strconv.FormatInt(parsed.SessionContext.APIKeyID, 10))
		_, _ = combined.WriteString("|")
	}
	if systemText := extractTextFromSystemRaw(parsed.SystemRaw()); systemText != "" {
		_, _ = combined.WriteString(systemText)
	}
	contentStart := combined.Len()
	appendMessageTextsFromRaw(&combined, parsed.MessagesRaw())
	if combined.Len() == contentStart {
		appendResponsesSessionAnchorFromRaw(&combined, parsed.InputRaw())
	}
	if combined.Len() > 0 {
		hash := s.hashContent(combined.String())
		slog.Info("sticky.hash_source",
			"source", "message_content_fallback",
			"hash", hash,
			"content_len", combined.Len(),
		)
		return hash
	}

	return ""
}

func (s *GatewayService) extractResponsesSessionAnchor(input any) string {
	switch v := input.(type) {
	case string:
		return v
	case []any:
		var builder strings.Builder
		for _, item := range v {
			switch typed := item.(type) {
			case string:
				return typed
			case map[string]any:
				role, _ := typed["role"].(string)
				switch role {
				case "system", "developer":
					if text := s.extractResponsesContentText(typed["content"]); text != "" {
						_, _ = builder.WriteString(text)
					}
				case "user":
					if text := s.extractResponsesContentText(typed["content"]); text != "" {
						_, _ = builder.WriteString(text)
					}
					return builder.String()
				default:
					if itemType, _ := typed["type"].(string); itemType == "input_text" {
						if text, _ := typed["text"].(string); text != "" {
							_, _ = builder.WriteString(text)
						}
						return builder.String()
					}
				}
			}
		}
		return builder.String()
	case map[string]any:
		return s.extractResponsesContentText(v["content"])
	default:
		return ""
	}
}

func (s *GatewayService) extractResponsesContentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var builder strings.Builder
		for _, part := range v {
			partMap, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch partMap["type"] {
			case "input_text", "text":
				if text, ok := partMap["text"].(string); ok {
					_, _ = builder.WriteString(text)
				}
			}
		}
		return builder.String()
	default:
		return ""
	}
}

// BindStickySession sets session -> account binding with standard TTL.
func (s *GatewayService) BindStickySession(ctx context.Context, groupID *int64, sessionHash string, accountID int64) error {
	return s.setStickySessionAccountID(ctx, groupID, sessionHash, accountID, stickySessionTTL)
}

func (s *GatewayService) setStickySessionAccountID(ctx context.Context, groupID *int64, sessionHash string, accountID int64, ttl time.Duration) error {
	if s == nil || sessionHash == "" || accountID <= 0 || s.cache == nil {
		return nil
	}
	writeCtx, cancel := rateLimitStateContext(ctx)
	defer cancel()
	return s.cache.SetSessionAccountID(writeCtx, derefGroupID(groupID), sessionHash, accountID, ttl)
}

func (s *GatewayService) deleteStickySessionAccountID(ctx context.Context, groupID *int64, sessionHash string) error {
	if s == nil || sessionHash == "" || s.cache == nil {
		return nil
	}
	writeCtx, cancel := rateLimitStateContext(ctx)
	defer cancel()
	return s.cache.DeleteSessionAccountID(writeCtx, derefGroupID(groupID), sessionHash)
}

func (s *GatewayService) refreshStickySessionTTL(ctx context.Context, groupID *int64, sessionHash string, ttl time.Duration) error {
	if s == nil || sessionHash == "" || s.cache == nil {
		return nil
	}
	writeCtx, cancel := rateLimitStateContext(ctx)
	defer cancel()
	return s.cache.RefreshSessionTTL(writeCtx, derefGroupID(groupID), sessionHash, ttl)
}

func (s *GatewayService) setGatewaySessionString(ctx context.Context, groupID int64, key string, value string, ttl time.Duration) error {
	if s == nil || key == "" || s.cache == nil {
		return nil
	}
	writeCtx, cancel := rateLimitStateContext(ctx)
	defer cancel()
	return s.cache.SetSessionString(writeCtx, groupID, key, value, ttl)
}

func (s *GatewayService) deleteGatewaySessionString(ctx context.Context, groupID int64, key string) error {
	if s == nil || key == "" || s.cache == nil {
		return nil
	}
	writeCtx, cancel := rateLimitStateContext(ctx)
	defer cancel()
	return s.cache.DeleteSessionString(writeCtx, groupID, key)
}

// ClearStickySessionIfBoundTo removes a sticky binding only when it still points
// at the failed account. This keeps a failover from erasing a newer binding made
// by another request.
func (s *GatewayService) ClearStickySessionIfBoundTo(ctx context.Context, groupID *int64, sessionHash string, accountID int64) (bool, error) {
	if sessionHash == "" || accountID <= 0 || s.cache == nil {
		return false, nil
	}
	writeCtx, cancel := rateLimitStateContext(ctx)
	defer cancel()

	currentAccountID, err := s.cache.GetSessionAccountID(writeCtx, derefGroupID(groupID), sessionHash)
	if err != nil || currentAccountID != accountID {
		return false, nil
	}
	if err := s.cache.DeleteSessionAccountID(writeCtx, derefGroupID(groupID), sessionHash); err != nil {
		return false, err
	}
	return true, nil
}

func (s *GatewayService) ReportAccountForwardResult(accountID int64, result *ForwardResult, err error) {
	if s == nil || s.accountRuntimeStats == nil || accountID <= 0 {
		return
	}
	s.accountRuntimeStats.report(accountID, result, err)
}

// GetCachedSessionAccountID retrieves the account ID bound to a sticky session.
// Returns 0 if no binding exists or on error.
func (s *GatewayService) GetCachedSessionAccountID(ctx context.Context, groupID *int64, sessionHash string) (int64, error) {
	if sessionHash == "" || s.cache == nil {
		return 0, nil
	}
	accountID, err := s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), sessionHash)
	if err != nil {
		return 0, err
	}
	return accountID, nil
}

func (s *GatewayService) buildClientAffinityKey(metadataUserID string, sub2apiUserID int64) string {
	if sub2apiUserID <= 0 {
		return ""
	}

	var parts []string
	parts = append(parts, "user", strconv.FormatInt(sub2apiUserID, 10))
	if uid := ParseMetadataUserID(metadataUserID); uid != nil {
		if deviceID := strings.TrimSpace(uid.DeviceID); deviceID != "" {
			parts = append(parts, "device", deviceID)
		}
		if accountUUID := strings.TrimSpace(uid.AccountUUID); accountUUID != "" {
			parts = append(parts, "account", accountUUID)
		}
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return clientAffinityKeyPrefix + fmt.Sprintf("%x", sum[:16])
}

func sessionLimitIDFromMetadataUserID(metadataUserID string) string {
	if uid := ParseMetadataUserID(metadataUserID); uid != nil {
		return strings.TrimSpace(uid.SessionID)
	}
	return ""
}

type accountSelectionFilterStats struct {
	eligibleBeforeRPM int
	rpmFiltered       int
}

func (s *GatewayService) accountRPMFiltered(ctx context.Context, account *Account, isSticky bool, stats *accountSelectionFilterStats) bool {
	if s.isAccountSchedulableForRPM(ctx, account, isSticky) {
		if !isSticky && stats != nil {
			stats.eligibleBeforeRPM++
		}
		return false
	}
	if !isSticky && stats != nil {
		stats.eligibleBeforeRPM++
		stats.rpmFiltered++
	}
	return true
}

func (stats accountSelectionFilterStats) onlyRPMFiltered() bool {
	return stats.eligibleBeforeRPM > 0 && stats.eligibleBeforeRPM == stats.rpmFiltered
}

func (s *GatewayService) getClientAffinityAccountID(ctx context.Context, groupID *int64, key string) int64 {
	if s == nil || s.cache == nil || key == "" {
		return 0
	}
	raw, err := s.cache.GetSessionString(ctx, derefGroupID(groupID), key)
	if err != nil {
		return 0
	}
	accountID, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || accountID <= 0 {
		_ = s.deleteGatewaySessionString(ctx, derefGroupID(groupID), key)
		return 0
	}
	return accountID
}

// ClearClientAffinityIfBoundTo removes a user/device affinity only when it
// still points at the failed account.
func (s *GatewayService) ClearClientAffinityIfBoundTo(ctx context.Context, groupID *int64, metadataUserID string, sub2apiUserID int64, accountID int64) (bool, error) {
	if s == nil || s.cache == nil || accountID <= 0 {
		return false, nil
	}
	key := s.buildClientAffinityKey(metadataUserID, sub2apiUserID)
	if key == "" {
		return false, nil
	}
	writeCtx, cancel := rateLimitStateContext(ctx)
	defer cancel()

	raw, err := s.cache.GetSessionString(writeCtx, derefGroupID(groupID), key)
	if err != nil {
		return false, nil
	}
	currentAccountID, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || currentAccountID != accountID {
		return false, nil
	}
	if err := s.cache.DeleteSessionString(writeCtx, derefGroupID(groupID), key); err != nil {
		return false, err
	}
	return true, nil
}

func (s *GatewayService) bindClientAffinityAccount(ctx context.Context, groupID *int64, key string, account *Account) {
	if s == nil || s.cache == nil || key == "" || account == nil || account.ID <= 0 {
		return
	}
	if !account.IsAnthropicOAuthOrSetupToken() {
		return
	}
	_ = s.setGatewaySessionString(ctx, derefGroupID(groupID), key, strconv.FormatInt(account.ID, 10), clientAffinityTTL)
}

func accountUserAffinityKey(accountID int64) string {
	if accountID <= 0 {
		return ""
	}
	return accountUserAffinityKeyPrefix + strconv.FormatInt(accountID, 10)
}

func (s *GatewayService) getAccountUserAffinityUserID(ctx context.Context, groupID *int64, accountID int64) int64 {
	if s == nil || s.cache == nil || accountID <= 0 {
		return 0
	}
	key := accountUserAffinityKey(accountID)
	if key == "" {
		return 0
	}
	raw, err := s.cache.GetSessionString(ctx, derefGroupID(groupID), key)
	if err != nil {
		return 0
	}
	userID, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || userID <= 0 {
		_ = s.deleteGatewaySessionString(ctx, derefGroupID(groupID), key)
		return 0
	}
	return userID
}

func (s *GatewayService) isAccountAllowedForUserAffinity(ctx context.Context, groupID *int64, account *Account, sub2apiUserID int64) bool {
	if s == nil || account == nil || !account.IsAnthropicOAuthOrSetupToken() {
		return true
	}
	if sub2apiUserID <= 0 || s.cache == nil {
		return true
	}
	boundUserID := s.getAccountUserAffinityUserID(ctx, groupID, account.ID)
	return boundUserID <= 0 || boundUserID == sub2apiUserID
}

func (s *GatewayService) bindAccountUserAffinity(ctx context.Context, groupID *int64, account *Account, sub2apiUserID int64) {
	if s == nil || s.cache == nil || account == nil || !account.IsAnthropicOAuthOrSetupToken() || sub2apiUserID <= 0 {
		return
	}
	key := accountUserAffinityKey(account.ID)
	if key == "" {
		return
	}
	_ = s.setGatewaySessionString(ctx, derefGroupID(groupID), key, strconv.FormatInt(sub2apiUserID, 10), accountUserAffinityTTL)
}

func accountProxyExitIPKey(accountID int64) string {
	if accountID <= 0 {
		return ""
	}
	return accountProxyExitIPKeyPrefix + strconv.FormatInt(accountID, 10)
}

func normalizeProxyExitIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if idx := strings.IndexByte(raw, ','); idx >= 0 {
		raw = strings.TrimSpace(raw[:idx])
	}
	if ip := net.ParseIP(strings.Trim(raw, "[]")); ip != nil {
		return ip.String()
	}
	return raw
}

func accountNeedsProxyExitIPStability(account *Account) bool {
	return account != nil && account.RequiresProxyForScheduling() && account.ProxyID != nil && *account.ProxyID > 0
}

func (s *GatewayService) observedProxyExitIP(ctx context.Context, account *Account) string {
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

func (s *GatewayService) boundAccountProxyExitIP(ctx context.Context, accountID int64) string {
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
		_ = s.deleteGatewaySessionString(ctx, 0, key)
	}
	return normalized
}

func (s *GatewayService) isAccountProxyExitIPStable(ctx context.Context, account *Account) bool {
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

func (s *GatewayService) bindAccountProxyExitIP(ctx context.Context, account *Account) {
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
	_ = s.setGatewaySessionString(ctx, 0, key, observed, accountProxyExitIPTTL)
}

// FindGeminiSession 查找 Gemini 会话（基于内容摘要链的 Fallback 匹配）
// 返回最长匹配的会话信息（uuid, accountID）
func (s *GatewayService) FindGeminiSession(_ context.Context, groupID int64, prefixHash, digestChain string) (uuid string, accountID int64, matchedChain string, found bool) {
	if digestChain == "" || s.digestStore == nil {
		return "", 0, "", false
	}
	return s.digestStore.Find(groupID, prefixHash, digestChain)
}

// SaveGeminiSession 保存 Gemini 会话。oldDigestChain 为 Find 返回的 matchedChain，用于删旧 key。
func (s *GatewayService) SaveGeminiSession(_ context.Context, groupID int64, prefixHash, digestChain, uuid string, accountID int64, oldDigestChain string) error {
	if digestChain == "" || s.digestStore == nil {
		return nil
	}
	s.digestStore.Save(groupID, prefixHash, digestChain, uuid, accountID, oldDigestChain)
	return nil
}

// FindAnthropicSession 查找 Anthropic 会话（基于内容摘要链的 Fallback 匹配）
func (s *GatewayService) FindAnthropicSession(_ context.Context, groupID int64, prefixHash, digestChain string) (uuid string, accountID int64, matchedChain string, found bool) {
	if digestChain == "" || s.digestStore == nil {
		return "", 0, "", false
	}
	return s.digestStore.Find(groupID, prefixHash, digestChain)
}

// SaveAnthropicSession 保存 Anthropic 会话
func (s *GatewayService) SaveAnthropicSession(_ context.Context, groupID int64, prefixHash, digestChain, uuid string, accountID int64, oldDigestChain string) error {
	if digestChain == "" || s.digestStore == nil {
		return nil
	}
	s.digestStore.Save(groupID, prefixHash, digestChain, uuid, accountID, oldDigestChain)
	return nil
}

func (s *GatewayService) extractCacheableContent(parsed *ParsedRequest) string {
	if parsed == nil {
		return ""
	}

	systemText := extractCacheableTextFromSystemRaw(parsed.SystemRaw())
	if messageText := extractCacheableTextFromMessagesRaw(parsed.MessagesRaw()); messageText != "" {
		return messageText
	}
	return systemText
}

func extractTextFromSystemRaw(raw []byte) string {
	system := parseRawJSONView(raw)
	switch system.Type {
	case gjson.String:
		return system.String()
	case gjson.JSON:
		if !system.IsArray() {
			return ""
		}
		var builder strings.Builder
		system.ForEach(func(_, part gjson.Result) bool {
			if text := part.Get("text").String(); text != "" {
				_, _ = builder.WriteString(text)
			}
			return true
		})
		return builder.String()
	}
	return ""
}

func extractTextFromContentRaw(content gjson.Result) string {
	switch content.Type {
	case gjson.String:
		return content.String()
	case gjson.JSON:
		if !content.IsArray() {
			return ""
		}
		var builder strings.Builder
		content.ForEach(func(_, part gjson.Result) bool {
			if part.Get("type").String() == "text" {
				if text := part.Get("text").String(); text != "" {
					_, _ = builder.WriteString(text)
				}
			}
			return true
		})
		return builder.String()
	}
	return ""
}

func appendMessageTextsFromRaw(builder *strings.Builder, raw []byte) {
	if builder == nil || len(raw) == 0 {
		return
	}
	messages := parseRawJSONView(raw)
	if !messages.IsArray() {
		return
	}
	messages.ForEach(func(_, msg gjson.Result) bool {
		if content := msg.Get("content"); content.Exists() {
			_, _ = builder.WriteString(extractTextFromContentRaw(content))
			return true
		}
		parts := msg.Get("parts")
		if parts.IsArray() {
			parts.ForEach(func(_, part gjson.Result) bool {
				if text := part.Get("text").String(); text != "" {
					_, _ = builder.WriteString(text)
				}
				return true
			})
		}
		return true
	})
}

func appendResponsesSessionAnchorFromRaw(builder *strings.Builder, raw []byte) {
	if builder == nil || len(raw) == 0 {
		return
	}
	input := parseRawJSONView(raw)
	if input.Type == gjson.String {
		_, _ = builder.WriteString(input.String())
		return
	}
	if !input.IsArray() {
		return
	}
	input.ForEach(func(_, item gjson.Result) bool {
		if item.Type == gjson.String {
			_, _ = builder.WriteString(item.String())
			return false
		}
		switch item.Get("role").String() {
		case "system", "developer":
			appendResponsesContentText(builder, item.Get("content"))
		case "user":
			appendResponsesContentText(builder, item.Get("content"))
			return false
		default:
			if item.Get("type").String() == "input_text" {
				if text := item.Get("text").String(); text != "" {
					_, _ = builder.WriteString(text)
				}
				return false
			}
		}
		return true
	})
}

func appendResponsesContentText(builder *strings.Builder, content gjson.Result) {
	if builder == nil || !content.Exists() {
		return
	}
	if content.Type == gjson.String {
		_, _ = builder.WriteString(content.String())
		return
	}
	if !content.IsArray() {
		return
	}
	content.ForEach(func(_, part gjson.Result) bool {
		switch part.Get("type").String() {
		case "input_text", "text":
			if text := part.Get("text").String(); text != "" {
				_, _ = builder.WriteString(text)
			}
		}
		return true
	})
}

func extractCacheableTextFromSystemRaw(raw []byte) string {
	system := parseRawJSONView(raw)
	if !system.IsArray() {
		return ""
	}
	var builder strings.Builder
	system.ForEach(func(_, part gjson.Result) bool {
		if part.Get("cache_control.type").String() == "ephemeral" {
			if text := part.Get("text").String(); text != "" {
				_, _ = builder.WriteString(text)
			}
		}
		return true
	})
	return builder.String()
}

func extractCacheableTextFromMessagesRaw(raw []byte) string {
	messages := parseRawJSONView(raw)
	if !messages.IsArray() {
		return ""
	}
	var text string
	messages.ForEach(func(_, msg gjson.Result) bool {
		content := msg.Get("content")
		if !content.IsArray() {
			return true
		}
		found := false
		content.ForEach(func(_, part gjson.Result) bool {
			if part.Get("cache_control.type").String() == "ephemeral" {
				found = true
				return false
			}
			return true
		})
		if found {
			text = extractTextFromContentRaw(content)
			return false
		}
		return true
	})
	return text
}

func (s *GatewayService) extractTextFromSystem(system any) string {
	switch v := system.(type) {
	case string:
		return v
	case []any:
		var texts []string
		for _, part := range v {
			if partMap, ok := part.(map[string]any); ok {
				if text, ok := partMap["text"].(string); ok {
					texts = append(texts, text)
				}
			}
		}
		return strings.Join(texts, "")
	}
	return ""
}

func (s *GatewayService) extractTextFromContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var texts []string
		for _, part := range v {
			if partMap, ok := part.(map[string]any); ok {
				if partMap["type"] == "text" {
					if text, ok := partMap["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
		}
		return strings.Join(texts, "")
	}
	return ""
}

func (s *GatewayService) hashContent(content string) string {
	h := xxhash.Sum64String(content)
	return strconv.FormatUint(h, 36)
}

// hashBodyForSessionSeed 为 sessionID 提供一个稳定但仅对本次请求特征化的种子。
// 复用 SHA-256 + 截断，与 generateSessionUUID 的输入格式对齐。
func hashBodyForSessionSeed(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum[:16])
}

func (s *GatewayService) isAccountTempUnschedulableCached(ctx context.Context, account *Account) bool {
	if s == nil || s.rateLimitService == nil || account == nil {
		return false
	}
	return s.rateLimitService.IsAccountTempUnschedulableCached(ctx, account)
}

type proxyHealthPrefetchContextKeyType struct{}

var proxyHealthPrefetchContextKey = proxyHealthPrefetchContextKeyType{}

func proxyHealthFromPrefetchContext(ctx context.Context, proxyID int64) (*ProxyLatencyInfo, bool) {
	if ctx == nil || proxyID <= 0 {
		return nil, false
	}
	m, ok := ctx.Value(proxyHealthPrefetchContextKey).(map[int64]*ProxyLatencyInfo)
	if !ok || len(m) == 0 {
		return nil, false
	}
	info, exists := m[proxyID]
	return info, exists
}

func (s *GatewayService) withProxyHealthPrefetch(ctx context.Context, accounts []Account) context.Context {
	if ctx == nil || s == nil || s.proxyLatencyCache == nil || len(accounts) == 0 {
		return ctx
	}

	seen := make(map[int64]struct{})
	ids := make([]int64, 0, len(accounts))
	for i := range accounts {
		acc := &accounts[i]
		if acc == nil || !acc.IsAnthropicOAuthOrSetupToken() || acc.ProxyID == nil || *acc.ProxyID <= 0 {
			continue
		}
		proxyID := *acc.ProxyID
		if _, ok := seen[proxyID]; ok {
			continue
		}
		seen[proxyID] = struct{}{}
		ids = append(ids, proxyID)
	}
	if len(ids) == 0 {
		return ctx
	}

	latencies, err := s.proxyLatencyCache.GetProxyLatencies(ctx, ids)
	if err != nil || len(latencies) == 0 {
		return ctx
	}
	return context.WithValue(ctx, proxyHealthPrefetchContextKey, latencies)
}

func proxyHealthBlocksProtectedProxyScheduling(info *ProxyLatencyInfo) bool {
	if info == nil {
		return false
	}
	if proxyCountryBlocksProtectedProxyScheduling(info.CountryCode) {
		return true
	}
	if !info.Success && (!info.UpdatedAt.IsZero() || strings.TrimSpace(info.Message) != "") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(info.QualityStatus)) {
	case "failed", "challenge":
		return true
	default:
		return false
	}
}

func proxyHealthBlocksAnthropicScheduling(info *ProxyLatencyInfo) bool {
	return proxyHealthBlocksProtectedProxyScheduling(info)
}

func proxyCountryBlocksProtectedProxyScheduling(countryCode string) bool {
	switch strings.ToUpper(strings.TrimSpace(countryCode)) {
	case "BY", "CN", "CU", "IR", "KP", "RU", "SY":
		return true
	default:
		return false
	}
}

func proxyCountryBlocksAnthropicScheduling(countryCode string) bool {
	return proxyCountryBlocksProtectedProxyScheduling(countryCode)
}

func (s *GatewayService) isAccountProxyHealthSchedulable(ctx context.Context, account *Account) bool {
	if s == nil || s.proxyLatencyCache == nil || account == nil || !account.IsAnthropicOAuthOrSetupToken() {
		return true
	}
	if account.ProxyID == nil || *account.ProxyID <= 0 {
		return true
	}
	proxyID := *account.ProxyID
	if info, ok := proxyHealthFromPrefetchContext(ctx, proxyID); ok {
		return !proxyHealthBlocksAnthropicScheduling(info)
	}
	latencies, err := s.proxyLatencyCache.GetProxyLatencies(ctx, []int64{proxyID})
	if err != nil {
		return true
	}
	return !proxyHealthBlocksAnthropicScheduling(latencies[proxyID])
}

func filterByMinPriorityForRequest(ctx context.Context, accounts []accountWithLoad) []accountWithLoad {
	if len(accounts) == 0 {
		return accounts
	}
	minPriority := accountWithLoadPriorityForRequest(ctx, accounts[0])
	for _, acc := range accounts[1:] {
		if priority := accountWithLoadPriorityForRequest(ctx, acc); priority < minPriority {
			minPriority = priority
		}
	}
	result := make([]accountWithLoad, 0, len(accounts))
	for _, acc := range accounts {
		if accountWithLoadPriorityForRequest(ctx, acc) == minPriority {
			result = append(result, acc)
		}
	}
	return result
}

func maxWaitingForStickyDecision(defaultMax int, decision accountRuntimeStickyDecision) int {
	if !decision.LimitWait {
		return defaultMax
	}
	if defaultMax <= 0 {
		return defaultMax
	}
	if defaultMax < accountRuntimeStickySlowMaxWait {
		return defaultMax
	}
	return accountRuntimeStickySlowMaxWait
}

func waitTimeoutForStickyDecision(defaultTimeout time.Duration, decision accountRuntimeStickyDecision) time.Duration {
	if !decision.LimitWait || defaultTimeout <= 0 || defaultTimeout <= accountRuntimeStickySlowWaitCap {
		return defaultTimeout
	}
	return accountRuntimeStickySlowWaitCap
}

func logStickyRuntimeDecision(accountID int64, sessionHash string, decision accountRuntimeStickyDecision, action string) {
	slog.Info("sticky.runtime_slow_account",
		"account_id", accountID,
		"session", shortSessionHash(sessionHash),
		"action", action,
		"penalty", decision.Penalty,
		"slow_score", decision.SlowScore,
		"slow_strike", decision.SlowStrike,
		"cache_protected", decision.CacheProtected,
	)
}

func (s *GatewayService) accountRuntimePenalty(accountID int64) int {
	if s == nil || s.accountRuntimeStats == nil {
		return 0
	}
	return s.accountRuntimeStats.loadPenalty(accountID)
}

func (s *GatewayService) stickyRuntimeDecision(accountID int64) accountRuntimeStickyDecision {
	if s == nil || s.accountRuntimeStats == nil {
		return accountRuntimeStickyDecision{}
	}
	return s.accountRuntimeStats.stickyDecision(accountID)
}

func effectiveLoadRate(acc accountWithLoad) int {
	if acc.loadInfo == nil {
		return acc.runtimePenalty
	}
	loadRate := acc.loadInfo.LoadRate
	if loadRate < 0 {
		loadRate = 0
	}
	return loadRate + acc.runtimePenalty
}

func accountPriorityForRequest(ctx context.Context, account *Account) int {
	if account == nil {
		return 0
	}
	return account.EffectivePriorityForRequest(ctx)
}

func accountWithLoadPriorityForRequest(ctx context.Context, acc accountWithLoad) int {
	return accountPriorityForRequest(ctx, acc.account)
}

func sortAccountsByPriorityAndLastUsedForRequest(ctx context.Context, accounts []*Account, preferOAuth bool) {
	sort.SliceStable(accounts, func(i, j int) bool {
		a, b := accounts[i], accounts[j]
		priorityA := accountPriorityForRequest(ctx, a)
		priorityB := accountPriorityForRequest(ctx, b)
		if priorityA != priorityB {
			return priorityA < priorityB
		}
		switch {
		case a.LastUsedAt == nil && b.LastUsedAt != nil:
			return true
		case a.LastUsedAt != nil && b.LastUsedAt == nil:
			return false
		case a.LastUsedAt == nil && b.LastUsedAt == nil:
			if preferOAuth && a.Type != b.Type {
				return a.Type == AccountTypeOAuth
			}
			return false
		default:
			return a.LastUsedAt.Before(*b.LastUsedAt)
		}
	})
	shuffleWithinPriorityAndLastUsedForRequest(ctx, accounts, preferOAuth)
}

func shuffleWithinSortGroupsForRequest(ctx context.Context, accounts []accountWithLoad) {
	if len(accounts) <= 1 {
		return
	}
	i := 0
	for i < len(accounts) {
		j := i + 1
		for j < len(accounts) && sameAccountWithLoadGroupForRequest(ctx, accounts[i], accounts[j]) {
			j++
		}
		if j-i > 1 {
			mathrand.Shuffle(j-i, func(a, b int) {
				accounts[i+a], accounts[i+b] = accounts[i+b], accounts[i+a]
			})
		}
		i = j
	}
}

func sameAccountWithLoadGroupForRequest(ctx context.Context, a, b accountWithLoad) bool {
	if accountWithLoadPriorityForRequest(ctx, a) != accountWithLoadPriorityForRequest(ctx, b) {
		return false
	}
	if effectiveLoadRate(a) != effectiveLoadRate(b) {
		return false
	}
	return sameLastUsedAt(a.account.LastUsedAt, b.account.LastUsedAt)
}

func shuffleWithinPriorityAndLastUsedForRequest(ctx context.Context, accounts []*Account, preferOAuth bool) {
	shuffleWithinPriorityAndLastUsedByGroup(accounts, preferOAuth, func(a, b *Account) bool {
		return sameAccountGroupForRequest(ctx, a, b)
	})
}

func shuffleWithinPriorityRuntimeAndLastUsed(ctx context.Context, accounts []*Account, preferOAuth bool, svc *GatewayService) {
	shuffleWithinPriorityAndLastUsedByGroup(accounts, preferOAuth, func(a, b *Account) bool {
		return sameAccountRuntimeGroupForRequest(ctx, a, b, svc)
	})
}

func shuffleWithinPriorityAndLastUsedByGroup(accounts []*Account, preferOAuth bool, sameGroup func(a, b *Account) bool) {
	if len(accounts) <= 1 {
		return
	}
	i := 0
	for i < len(accounts) {
		j := i + 1
		for j < len(accounts) && sameGroup(accounts[i], accounts[j]) {
			j++
		}
		if j-i > 1 {
			if preferOAuth {
				oauth := make([]*Account, 0, j-i)
				others := make([]*Account, 0, j-i)
				for _, acc := range accounts[i:j] {
					if acc.Type == AccountTypeOAuth {
						oauth = append(oauth, acc)
					} else {
						others = append(others, acc)
					}
				}
				if len(oauth) > 1 {
					mathrand.Shuffle(len(oauth), func(a, b int) { oauth[a], oauth[b] = oauth[b], oauth[a] })
				}
				if len(others) > 1 {
					mathrand.Shuffle(len(others), func(a, b int) { others[a], others[b] = others[b], others[a] })
				}
				copy(accounts[i:], oauth)
				copy(accounts[i+len(oauth):], others)
			} else {
				mathrand.Shuffle(j-i, func(a, b int) {
					accounts[i+a], accounts[i+b] = accounts[i+b], accounts[i+a]
				})
			}
		}
		i = j
	}
}

func sameAccountGroupForRequest(ctx context.Context, a, b *Account) bool {
	if accountPriorityForRequest(ctx, a) != accountPriorityForRequest(ctx, b) {
		return false
	}
	return sameLastUsedAt(a.LastUsedAt, b.LastUsedAt)
}

func sameAccountRuntimeGroup(a, b *Account, svc *GatewayService) bool {
	return sameAccountRuntimeGroupForRequest(nil, a, b, svc)
}

func sameAccountRuntimeGroupForRequest(ctx context.Context, a, b *Account, svc *GatewayService) bool {
	if accountPriorityForRequest(ctx, a) != accountPriorityForRequest(ctx, b) {
		return false
	}
	if svc != nil && svc.accountRuntimePenalty(a.ID) != svc.accountRuntimePenalty(b.ID) {
		return false
	}
	return sameLastUsedAt(a.LastUsedAt, b.LastUsedAt)
}

func (s *GatewayService) sortAccountsByPriorityRuntimeOnly(ctx context.Context, accounts []*Account, preferOAuth bool) {
	sort.SliceStable(accounts, func(i, j int) bool {
		a, b := accounts[i], accounts[j]
		priorityA := accountPriorityForRequest(ctx, a)
		priorityB := accountPriorityForRequest(ctx, b)
		if priorityA != priorityB {
			return priorityA < priorityB
		}
		if penaltyA, penaltyB := s.accountRuntimePenalty(a.ID), s.accountRuntimePenalty(b.ID); penaltyA != penaltyB {
			return penaltyA < penaltyB
		}
		if preferOAuth && a.Type != b.Type {
			return a.Type == AccountTypeOAuth
		}
		return false
	})
}

func (s *GatewayService) sortAccountsByPriorityRuntimeAndLastUsed(ctx context.Context, accounts []*Account, preferOAuth bool) {
	sort.SliceStable(accounts, func(i, j int) bool {
		a, b := accounts[i], accounts[j]
		return s.isBetterRuntimeAccountForRequest(ctx, a, b, preferOAuth)
	})
	shuffleWithinPriorityRuntimeAndLastUsed(ctx, accounts, preferOAuth, s)
}

func (s *GatewayService) isBetterRuntimeAccount(candidate, current *Account, preferOAuth bool) bool {
	return s.isBetterRuntimeAccountForRequest(nil, candidate, current, preferOAuth)
}

func (s *GatewayService) isBetterRuntimeAccountForRequest(ctx context.Context, candidate, current *Account, preferOAuth bool) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}
	priorityCandidate := accountPriorityForRequest(ctx, candidate)
	priorityCurrent := accountPriorityForRequest(ctx, current)
	if priorityCandidate != priorityCurrent {
		return priorityCandidate < priorityCurrent
	}
	if penaltyCandidate, penaltyCurrent := s.accountRuntimePenalty(candidate.ID), s.accountRuntimePenalty(current.ID); penaltyCandidate != penaltyCurrent {
		return penaltyCandidate < penaltyCurrent
	}
	switch {
	case candidate.LastUsedAt == nil && current.LastUsedAt != nil:
		return true
	case candidate.LastUsedAt != nil && current.LastUsedAt == nil:
		return false
	case candidate.LastUsedAt == nil && current.LastUsedAt == nil:
		if preferOAuth && candidate.Type != current.Type {
			return candidate.Type == AccountTypeOAuth
		}
		return false
	default:
		return candidate.LastUsedAt.Before(*current.LastUsedAt)
	}
}

func sortAccountsByPriorityOnlyForRequest(ctx context.Context, accounts []*Account, preferOAuth bool) {
	sort.SliceStable(accounts, func(i, j int) bool {
		a, b := accounts[i], accounts[j]
		priorityA := accountPriorityForRequest(ctx, a)
		priorityB := accountPriorityForRequest(ctx, b)
		if priorityA != priorityB {
			return priorityA < priorityB
		}
		if preferOAuth && a.Type != b.Type {
			return a.Type == AccountTypeOAuth
		}
		return false
	})
}

func shuffleWithinPriorityForRequest(ctx context.Context, accounts []*Account) {
	if len(accounts) <= 1 {
		return
	}
	r := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	start := 0
	for start < len(accounts) {
		priority := accountPriorityForRequest(ctx, accounts[start])
		end := start + 1
		for end < len(accounts) && accountPriorityForRequest(ctx, accounts[end]) == priority {
			end++
		}
		// 对 [start, end) 范围内的账户随机打乱
		if end-start > 1 {
			r.Shuffle(end-start, func(i, j int) {
				accounts[start+i], accounts[start+j] = accounts[start+j], accounts[start+i]
			})
		}
		start = end
	}
}

func shuffleWithinPriorityRuntime(ctx context.Context, accounts []*Account, svc *GatewayService) {
	if len(accounts) <= 1 {
		return
	}
	r := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	start := 0
	for start < len(accounts) {
		priority := accountPriorityForRequest(ctx, accounts[start])
		penalty := 0
		if svc != nil {
			penalty = svc.accountRuntimePenalty(accounts[start].ID)
		}
		end := start + 1
		for end < len(accounts) && accountPriorityForRequest(ctx, accounts[end]) == priority {
			if svc != nil && svc.accountRuntimePenalty(accounts[end].ID) != penalty {
				break
			}
			end++
		}
		if end-start > 1 {
			r.Shuffle(end-start, func(i, j int) {
				accounts[start+i], accounts[start+j] = accounts[start+j], accounts[start+i]
			})
		}
		start = end
	}
}

// GetAccessToken 获取账号凭证
func (s *GatewayService) GetAccessToken(ctx context.Context, account *Account) (string, string, error) {
	switch account.Type {
	case AccountTypeOAuth, AccountTypeSetupToken:
		// Both oauth and setup-token use OAuth token flow
		return s.getOAuthToken(ctx, account)
	case AccountTypeAPIKey:
		apiKey := account.GetCredential("api_key")
		if apiKey == "" {
			return "", "", errors.New("api_key not found in credentials")
		}
		return apiKey, "apikey", nil
	case AccountTypeBedrock:
		return "", "bedrock", nil // Bedrock 使用 SigV4 签名或 API Key，由 forwardBedrock 处理
	case AccountTypeServiceAccount:
		if account.Platform != PlatformAnthropic {
			return "", "", fmt.Errorf("unsupported service account platform: %s", account.Platform)
		}
		if s.claudeTokenProvider == nil {
			return "", "", errors.New("claude token provider not configured")
		}
		accessToken, err := s.claudeTokenProvider.GetAccessToken(ctx, account)
		if err != nil {
			return "", "", err
		}
		return accessToken, "service_account", nil
	default:
		return "", "", fmt.Errorf("unsupported account type: %s", account.Type)
	}
}

func (s *GatewayService) getOAuthToken(ctx context.Context, account *Account) (string, string, error) {
	// 对于 Anthropic OAuth 账号，使用 ClaudeTokenProvider 获取缓存的 token
	if account.Platform == PlatformAnthropic && account.Type == AccountTypeOAuth && s.claudeTokenProvider != nil {
		accessToken, err := s.claudeTokenProvider.GetAccessToken(ctx, account)
		if err != nil {
			return "", "", err
		}
		return accessToken, "oauth", nil
	}

	// 其他情况（Gemini 有自己的 TokenProvider，setup-token 类型等）直接从账号读取
	accessToken := account.GetCredential("access_token")
	if accessToken == "" {
		return "", "", errors.New("access_token not found in credentials")
	}
	// Token刷新由后台 TokenRefreshService 处理，此处只返回当前token
	return accessToken, "oauth", nil
}

// 重试相关常量
const (
	// 首响应保护：上游长时间不返回响应头时，若尚未写客户端，则尽快切换账号。
	upstreamFirstResponseFailoverTimeout = 12 * time.Second
)

func (s *GatewayService) shouldFailoverGatewayUpstreamResponse(account *Account, statusCode int, upstreamMsg string, upstreamBody []byte) bool {
	if account != nil && account.Platform == PlatformAnthropic && isAnthropicRequestPolicyError(statusCode, upstreamBody, upstreamMsg) {
		return false
	}
	return s.shouldFailoverUpstreamError(statusCode)
}

func (s *GatewayService) shouldFailoverAnthropicStreamError(statusCode int, upstreamMsg string, upstreamBody []byte) bool {
	if isAnthropicRequestPolicyError(statusCode, upstreamBody, upstreamMsg) {
		return false
	}
	return s.shouldFailoverUpstreamError(statusCode)
}

func (s *GatewayService) shouldStopRetryForPermanentAccountError(ctx context.Context, account *Account, statusCode int, body []byte) bool {
	if s == nil || s.rateLimitService == nil {
		return false
	}
	return s.rateLimitService.HandlePermanentAccountError(ctx, account, statusCode, body)
}

type upstreamFirstResponseTimeoutError struct {
	err error
}

func (e *upstreamFirstResponseTimeoutError) Error() string {
	if e == nil || e.err == nil {
		return "upstream first response timeout"
	}
	return e.err.Error()
}

func (e *upstreamFirstResponseTimeoutError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func upstreamFirstResponseFailoverError(err error) *UpstreamFailoverError {
	var timeoutErr *upstreamFirstResponseTimeoutError
	if err == nil || !errors.As(err, &timeoutErr) {
		return nil
	}
	body, _ := json.Marshal(map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    "upstream_timeout",
			"message": "upstream did not return a response in time",
		},
	})
	return &UpstreamFailoverError{
		StatusCode:             http.StatusBadGateway,
		ResponseBody:           body,
		RetryableOnSameAccount: false,
	}
}

type upstreamDoFunc func(req *http.Request) (*http.Response, error)

func doWithFirstResponseFailover(ctx context.Context, req *http.Request, do upstreamDoFunc) (*http.Response, error) {
	if req == nil || do == nil || upstreamFirstResponseFailoverTimeout <= 0 {
		if do == nil {
			return nil, errors.New("nil upstream do function")
		}
		return do(req)
	}
	baseCtx := req.Context()
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	upstreamReqCtx, cancelUpstreamReq := context.WithCancel(baseCtx)
	upstreamReq := req.WithContext(upstreamReqCtx)
	var timedOut atomic.Bool
	resultCh := make(chan struct {
		resp *http.Response
		err  error
	}, 1)

	go func() {
		resp, err := do(upstreamReq)
		if timedOut.Load() {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			return
		}
		resultCh <- struct {
			resp *http.Response
			err  error
		}{resp: resp, err: err}
	}()

	timer := time.NewTimer(upstreamFirstResponseFailoverTimeout)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			cancelUpstreamReq()
			return result.resp, result.err
		}
		if result.resp == nil || result.resp.Body == nil {
			cancelUpstreamReq()
			return result.resp, result.err
		}
		result.resp.Body = cancelOnCloseReadCloser{
			ReadCloser: result.resp.Body,
			cancel:     cancelUpstreamReq,
		}
		return result.resp, result.err
	case <-baseCtx.Done():
		cancelUpstreamReq()
		return nil, baseCtx.Err()
	case <-timer.C:
		timedOut.Store(true)
		cancelUpstreamReq()
		return nil, &upstreamFirstResponseTimeoutError{err: context.DeadlineExceeded}
	}
}

type cancelOnCloseReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r cancelOnCloseReadCloser) Close() error {
	err := r.ReadCloser.Close()
	if r.cancel != nil {
		r.cancel()
	}
	return err
}

func (s *GatewayService) shouldBridgeClaudeCodeForAnthropicAPIKeyPassthrough(ctx context.Context, c *gin.Context, account *Account, body []byte) bool {
	if c == nil || c.Request == nil {
		return false
	}
	if !usesCustomAnthropicAPIKeyPassthroughBaseURL(account) {
		return false
	}
	if IsClaudeCodeClient(ctx) {
		return true
	}
	bodyMap := map[string]any(nil)
	if len(body) > 0 {
		_ = json.Unmarshal(body, &bodyMap)
	}
	validator := NewClaudeCodeValidator()
	return validator.Validate(c.Request, bodyMap) || validator.ValidateTransportSignature(c.Request, bodyMap)
}

func usesCustomAnthropicAPIKeyPassthroughBaseURL(account *Account) bool {
	if account == nil || account.Platform != PlatformAnthropic || account.Type != AccountTypeAPIKey {
		return false
	}
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" {
		return false
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return true
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return host != "" && host != "api.anthropic.com"
}

func (s *GatewayService) ensureAnthropicAPIKeyPassthroughClaudeCodeBody(body []byte, r *http.Request) []byte {
	if len(body) == 0 {
		return body
	}
	out := body
	if !bodyHasClaudeCodeSystemMarker(out) {
		var system any
		if sys := gjson.GetBytes(out, "system"); sys.Exists() && sys.Type != gjson.Null {
			_ = json.Unmarshal([]byte(sys.Raw), &system)
		}
		out = rewriteSystemForNonClaudeCodeWithPromptBlocks(out, system, "", "")
	}
	if strings.TrimSpace(gjson.GetBytes(out, "metadata.user_id").String()) == "" {
		if userID := buildClaudeCodePassthroughMetadataUserID(r); userID != "" {
			if next, ok := ensureClaudeOAuthMetadataUserID(out, userID); ok {
				out = next
			}
		}
	}
	return out
}

func bodyHasClaudeCodeSystemMarker(body []byte) bool {
	sys := gjson.GetBytes(body, "system")
	if !sys.Exists() || sys.Type == gjson.Null {
		return false
	}
	if sys.Type == gjson.String {
		return hasClaudeCodeSystemTextMarker(sys.String())
	}
	if !sys.IsArray() {
		return false
	}
	found := false
	sys.ForEach(func(_, item gjson.Result) bool {
		text := item.Get("text").String()
		if hasClaudeCodeSystemTextMarker(text) {
			found = true
			return false
		}
		return true
	})
	return found
}

func hasClaudeCodeSystemTextMarker(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	return hasClaudeCodePrefix(text) ||
		(strings.HasPrefix(text, claudeCodeBillingHeaderPrefix) && strings.Contains(text, claudeCodeEntrypointMarker))
}

func buildClaudeCodePassthroughMetadataUserID(r *http.Request) string {
	if r == nil {
		return ""
	}
	sessionID := strings.TrimSpace(getHeaderRaw(r.Header, "X-Claude-Code-Session-Id"))
	if !claudeCodeSessionIDPattern.MatchString(sessionID) {
		sessionID = uuid.NewString()
	}
	return FormatMetadataUserID(generateClientID(), "", sessionID, ExtractCLIVersion(r.Header.Get("User-Agent")))
}

func isAnthropicPassthroughStreamErrorData(eventName, data string) bool {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(eventName), "error") {
		return true
	}
	if !gjson.Valid(data) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(gjson.Get(data, "type").String()), "error")
}

type apiKeyAuthCacheUserInvalidator interface {
	InvalidateAuthCacheByUserID(ctx context.Context, userID int64)
}

func finalizeLegacyUsageBillingWallet(p *postUsageBillingParams, deps *billingDeps, result *UsageBillingApplyResult) {
	if p == nil || deps == nil || result == nil || p.User == nil {
		return
	}

	if result.PointsDeducted > 0 {
		if invalidator, ok := p.APIKeyService.(apiKeyAuthCacheUserInvalidator); ok {
			invalidator.InvalidateAuthCacheByUserID(context.Background(), p.User.ID)
		}
		if deps.billingCacheService != nil {
			_ = deps.billingCacheService.InvalidateUserBalance(context.Background(), p.User.ID)
		}
		return
	}

	if result.BalanceDeducted > 0 {
		if deps.billingCacheService != nil {
			_ = deps.billingCacheService.InvalidateUserBalance(context.Background(), p.User.ID)
		}
		return
	}

	if result.CommissionDeducted > 0 {
		if deps.billingCacheService != nil {
			_ = deps.billingCacheService.InvalidateUserBalance(context.Background(), p.User.ID)
		}
	}
}

func compactUsageRequestID(primary, secondaryKind, secondary string) string {
	primary = strings.TrimSpace(primary)
	secondaryKind = strings.TrimSpace(secondaryKind)
	secondary = strings.TrimSpace(secondary)
	raw := primary
	if secondaryKind != "" && secondary != "" {
		raw = primary + "|" + secondaryKind + ":" + secondary
	}
	if len(raw) <= 64 {
		return raw
	}

	sum := sha256.Sum256([]byte(raw))
	suffix := hex.EncodeToString(sum[:])[:16]
	const sep = "|h:"
	maxPrimaryLen := 64 - len(sep) - len(suffix)
	if maxPrimaryLen <= 0 {
		return suffix
	}
	if len(primary) > maxPrimaryLen {
		primary = primary[:maxPrimaryLen]
	}
	if primary == "" {
		return suffix
	}
	return primary + sep + suffix
}

func calculatePrivateGroupCommissionCost(p *postUsageBillingParams) float64 {
	if p == nil || p.Cost == nil || !p.IsSubscriptionBill || p.APIKey == nil || p.APIKey.Group == nil {
		return 0
	}
	if !p.APIKey.Group.IsUserPrivateScope() || p.Cost.ActualCost <= 0 {
		return 0
	}
	rate := p.PrivateGroupCommissionRate
	if rate <= 0 {
		return 0
	}
	if rate > 1 {
		rate = 1
	}
	return p.Cost.ActualCost * rate
}

func attachAccountShareBillingSnapshot(ctx context.Context, cmd *UsageBillingCommand, p *postUsageBillingParams, deps *billingDeps) error {
	if cmd == nil || p == nil || p.Account == nil || p.User == nil {
		return nil
	}
	account := p.Account
	shareMode := NormalizeAccountShareMode(account.ShareMode)
	shareStatus := NormalizeAccountShareStatus(account.ShareStatus)

	cmd.ShareSnapshotCaptured = true
	cmd.ShareModeSnapshot = shareMode
	cmd.ShareStatusSnapshot = shareStatus
	cmd.SharePlatform = strings.TrimSpace(account.Platform)
	if account.OwnerUserID != nil && *account.OwnerUserID > 0 {
		ownerUserID := *account.OwnerUserID
		cmd.ShareOwnerUserID = &ownerUserID
	}
	if account.SharePolicyID != nil && *account.SharePolicyID > 0 {
		sharePolicyID := *account.SharePolicyID
		cmd.SharePolicyID = &sharePolicyID
	}

	if account.OwnerUserID == nil || *account.OwnerUserID <= 0 || *account.OwnerUserID == p.User.ID {
		return nil
	}
	if shareMode != AccountShareModePublic || shareStatus != AccountShareStatusApproved {
		return nil
	}

	if deps == nil || deps.accountSharePolicyRepo == nil {
		return nil
	}

	var groupID *int64
	if p.APIKey != nil {
		groupID = p.APIKey.GroupID
	}
	policy, err := deps.accountSharePolicyRepo.ResolveEnabledAccountSharePolicy(ctx, account.ID, groupID, account.Platform, account.SharePolicyID)
	if err != nil {
		return fmt.Errorf("resolve account share policy snapshot: %w", err)
	}
	if policy == nil || (policy.OwnerShareRatio <= 0 && policy.InviteShareRatio <= 0) {
		return nil
	}
	sharePolicyID := policy.ID
	cmd.SharePolicyID = &sharePolicyID
	cmd.SharePolicyVersion = policy.Version
	cmd.OwnerShareRatio = policy.OwnerShareRatio
	cmd.InviteShareRatio = policy.InviteShareRatio
	return nil
}

func syncAccountQuotaSchedulerSnapshot(p *postUsageBillingParams, deps *billingDeps, result *UsageBillingApplyResult) {
	if p == nil || p.Cost == nil || p.Account == nil || deps == nil ||
		deps.accountRepo == nil || deps.schedulerSnapshot == nil || result == nil || result.QuotaState == nil {
		return
	}
	if !p.Account.IsAPIKeyOrBedrock() {
		return
	}
	accountCost := p.Cost.TotalCost * p.AccountRateMultiplier
	if !accountQuotaStateCrossedLimit(result.QuotaState, accountCost) {
		return
	}

	ctx, cancel := detachedBillingContext(context.Background())
	defer cancel()
	account, err := deps.accountRepo.GetByID(ctx, p.Account.ID)
	if err != nil {
		slog.Warn("account_quota_scheduler_sync_reload_failed", "account_id", p.Account.ID, "error", err)
		return
	}
	if err := deps.schedulerSnapshot.UpdateAccountInCache(ctx, account); err != nil {
		slog.Warn("account_quota_scheduler_sync_failed", "account_id", p.Account.ID, "error", err)
	}
}

func accountQuotaStateCrossedLimit(state *AccountQuotaState, amount float64) bool {
	if state == nil || amount <= 0 {
		return false
	}
	return quotaDimensionCrossedLimit(state.TotalUsed, state.TotalLimit, amount) ||
		quotaDimensionCrossedLimit(state.DailyUsed, state.DailyLimit, amount) ||
		quotaDimensionCrossedLimit(state.WeeklyUsed, state.WeeklyLimit, amount)
}

func quotaDimensionCrossedLimit(used, limit, amount float64) bool {
	return limit > 0 && used >= limit && used-amount < limit
}

type usageBillingWalletAdjuster interface {
	AdjustUsageBillingWallet(ctx context.Context, userID int64, amount float64, preferPoints bool, metadata map[string]any) (*UsageBillingApplyResult, error)
}

type accountSchedulerSnapshotRefresher interface {
	UpdateAccountInCache(ctx context.Context, account *Account) error
}

// GetAvailableModels returns the list of models available for a group
// It aggregates model_mapping keys from all schedulable accounts in the group
func (s *GatewayService) GetAvailableModels(ctx context.Context, groupID *int64, platform string) []string {
	cacheKey := modelsListCacheKey(groupID, platform)
	if s.modelsListCache != nil {
		if cached, found := s.modelsListCache.Get(cacheKey); found {
			if models, ok := cached.([]string); ok {
				modelsListCacheHitTotal.Add(1)
				return cloneStringSlice(models)
			}
		}
	}
	modelsListCacheMissTotal.Add(1)

	var accounts []Account
	var err error

	if groupID != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupID(ctx, *groupID)
	} else {
		accounts, err = s.accountRepo.ListSchedulable(ctx)
	}

	if err != nil || len(accounts) == 0 {
		return nil
	}

	// Filter by platform if specified
	if platform != "" {
		filtered := make([]Account, 0)
		for _, acc := range accounts {
			if acc.Platform == platform {
				filtered = append(filtered, acc)
			}
		}
		accounts = filtered
	}

	// Collect unique models from all accounts
	modelSet := make(map[string]struct{})
	hasAnyMapping := false

	for _, acc := range accounts {
		mapping := acc.GetModelMapping()
		if len(mapping) > 0 {
			hasAnyMapping = true
			for model := range mapping {
				modelSet[model] = struct{}{}
			}
		}
	}

	// If no account has model_mapping, return nil (use default)
	if !hasAnyMapping {
		if s.modelsListCache != nil {
			s.modelsListCache.Set(cacheKey, []string(nil), s.modelsListCacheTTL)
			modelsListCacheStoreTotal.Add(1)
		}
		return nil
	}

	// Convert to slice
	models := make([]string, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, model)
	}
	sort.Strings(models)

	if s.modelsListCache != nil {
		s.modelsListCache.Set(cacheKey, cloneStringSlice(models), s.modelsListCacheTTL)
		modelsListCacheStoreTotal.Add(1)
	}
	return cloneStringSlice(models)
}

func (s *GatewayService) InvalidateAvailableModelsCache(groupID *int64, platform string) {
	if s == nil || s.modelsListCache == nil {
		return
	}

	normalizedPlatform := strings.TrimSpace(platform)
	// 完整匹配时精准失效；否则按维度批量失效。
	if groupID != nil && normalizedPlatform != "" {
		s.modelsListCache.Delete(modelsListCacheKey(groupID, normalizedPlatform))
		return
	}

	targetGroup := derefGroupID(groupID)
	for key := range s.modelsListCache.Items() {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		groupPart, parseErr := strconv.ParseInt(parts[0], 10, 64)
		if parseErr != nil {
			continue
		}
		if groupID != nil && groupPart != targetGroup {
			continue
		}
		if normalizedPlatform != "" && parts[1] != normalizedPlatform {
			continue
		}
		s.modelsListCache.Delete(key)
	}
}

func compatCachedTokensFromUsageNode(usage gjson.Result) (int, bool) {
	if !usage.Exists() {
		return 0, false
	}
	for _, path := range []string{
		"cached_tokens",
		"input_tokens_details.cached_tokens",
		"prompt_tokens_details.cached_tokens",
	} {
		if v := usage.Get(path); v.Exists() && v.Int() > 0 {
			return int(v.Int()), true
		}
	}
	return 0, false
}

func compatCachedTokensFromUsageMap(usage map[string]any) (int, bool) {
	if usage == nil {
		return 0, false
	}
	if v, ok := parseSSEUsageInt(usage["cached_tokens"]); ok && v > 0 {
		return v, true
	}
	for _, key := range []string{"input_tokens_details", "prompt_tokens_details"} {
		details, _ := usage[key].(map[string]any)
		if v, ok := parseSSEUsageInt(details["cached_tokens"]); ok && v > 0 {
			return v, true
		}
	}
	return 0, false
}

const debugGatewayBodyDefaultFilename = "gateway_debug.log"

// initDebugGatewayBodyFile 初始化网关调试日志文件。
//
//   - "1"/"true" 等布尔值 → 当前目录下 gateway_debug.log
//   - 已有目录路径        → 该目录下 gateway_debug.log
//   - 其他               → 视为完整文件路径
func (s *GatewayService) initDebugGatewayBodyFile(path string) {
	if parseDebugEnvBool(path) {
		path = debugGatewayBodyDefaultFilename
	}

	// 如果 path 指向一个已存在的目录，自动追加默认文件名
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, debugGatewayBodyDefaultFilename)
	}

	// 确保父目录存在
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			slog.Error("failed to create gateway debug log directory", "dir", dir, "error", err)
			return
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		slog.Error("failed to open gateway debug log file", "path", path, "error", err)
		return
	}
	s.debugGatewayBodyFile.Store(f)
	slog.Info("gateway debug logging enabled", "path", path)
}

// debugLogGatewaySnapshot 将网关请求的完整快照（headers + body）写入独立的调试日志文件，
// 用于对比客户端原始请求和上游转发请求。
//
// 启用方式（环境变量）：
//
//	SUB2API_DEBUG_GATEWAY_BODY=1                          # 写入 gateway_debug.log
//	SUB2API_DEBUG_GATEWAY_BODY=/tmp/gateway_debug.log     # 写入指定路径
//
// tag: "CLIENT_ORIGINAL" 或 "UPSTREAM_FORWARD"
func (s *GatewayService) debugLogGatewaySnapshot(tag string, headers http.Header, body []byte, extra map[string]string) {
	f := s.debugGatewayBodyFile.Load()
	if f == nil {
		return
	}

	var buf strings.Builder
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	fmt.Fprintf(&buf, "\n========== [%s] %s ==========\n", ts, tag)

	// 1. context
	if len(extra) > 0 {
		fmt.Fprint(&buf, "--- context ---\n")
		extraKeys := make([]string, 0, len(extra))
		for k := range extra {
			extraKeys = append(extraKeys, k)
		}
		sort.Strings(extraKeys)
		for _, k := range extraKeys {
			fmt.Fprintf(&buf, "  %s: %s\n", k, extra[k])
		}
	}

	// 2. headers（按真实 Claude CLI wire 顺序排列，便于与抓包对比；auth 脱敏）
	fmt.Fprint(&buf, "--- headers ---\n")
	for _, k := range sortHeadersByWireOrder(headers) {
		for _, v := range headers[k] {
			fmt.Fprintf(&buf, "  %s: %s\n", k, safeHeaderValueForLog(k, v))
		}
	}

	// 3. body（完整输出，格式化 JSON 便于 diff）
	fmt.Fprint(&buf, "--- body ---\n")
	if len(body) == 0 {
		fmt.Fprint(&buf, "  (empty)\n")
	} else {
		var pretty bytes.Buffer
		if json.Indent(&pretty, body, "  ", "  ") == nil {
			fmt.Fprintf(&buf, "  %s\n", pretty.Bytes())
		} else {
			// JSON 格式化失败时原样输出
			fmt.Fprintf(&buf, "  %s\n", body)
		}
	}

	// 写入文件（调试用，并发写入可能交错但不影响可读性）
	_, _ = f.WriteString(buf.String())
}
