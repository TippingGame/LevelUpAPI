package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
)

const TempUnschedKeywordUpstreamRelayPoolUnavailable = "upstream_relay_pool_unavailable"

// TempUnschedState 临时不可调度状态
type TempUnschedState struct {
	UntilUnix        int64  `json:"until_unix"`                  // 解除时间（Unix 时间戳）
	TriggeredAtUnix  int64  `json:"triggered_at_unix"`           // 触发时间（Unix 时间戳）
	StatusCode       int    `json:"status_code"`                 // 触发的错误码
	MatchedKeyword   string `json:"matched_keyword"`             // 匹配的关键词
	RuleIndex        int    `json:"rule_index"`                  // 触发的规则索引
	ErrorMessage     string `json:"error_message"`               // 错误消息
	ConsecutiveCount int    `json:"consecutive_count,omitempty"` // 连续命中次数
}

// TempUnschedCache 临时不可调度缓存接口
type TempUnschedCache interface {
	SetTempUnsched(ctx context.Context, accountID int64, state *TempUnschedState) error
	GetTempUnsched(ctx context.Context, accountID int64) (*TempUnschedState, error)
	DeleteTempUnsched(ctx context.Context, accountID int64) error
}

func newTempUnschedState(until time.Time, statusCode int, matchedKeyword string, errorMessage string) *TempUnschedState {
	now := time.Now()
	return &TempUnschedState{
		UntilUnix:       until.Unix(),
		TriggeredAtUnix: now.Unix(),
		StatusCode:      statusCode,
		MatchedKeyword:  matchedKeyword,
		RuleIndex:       -1,
		ErrorMessage:    errorMessage,
	}
}

func markTempUnschedRuntimeState(ctx context.Context, cache TempUnschedCache, account *Account, until time.Time, reason string, state *TempUnschedState, source string) {
	if account == nil || state == nil {
		return
	}
	if !shouldApplyRuntimeTempUnschedState(account, state) {
		slog.Info("runtime_temp_unsched_state_skipped", "account_id", account.ID, "source", source, "status_code", state.StatusCode)
		return
	}
	account.TempUnschedulableUntil = &until
	account.TempUnschedulableReason = reason
	setTempUnschedCacheBestEffort(ctx, cache, account.ID, state, source)
}

func markAccountErrorRuntimeEvicted(ctx context.Context, cache TempUnschedCache, account *Account, errorMsg string, source string) {
	if account == nil {
		return
	}
	if !shouldApplyLocalSystemErrorState(account) {
		slog.Info("runtime_account_error_eviction_skipped", "account_id", account.ID, "source", source)
		return
	}
	account.Status = StatusError
	account.ErrorMessage = errorMsg
	account.Schedulable = false

	until := time.Now().Add(runtimeAccountErrorEvictionTTL)
	state := newTempUnschedState(until, 0, "account_error", truncateTempUnschedMessage([]byte(errorMsg), tempUnschedMessageMaxBytes))
	setTempUnschedCacheBestEffort(ctx, cache, account.ID, state, source)
}

func shouldApplyRuntimeTempUnschedState(account *Account, state *TempUnschedState) bool {
	if shouldIgnoreTempUnschedulableForAccount(account, state, "") {
		return false
	}
	if state != nil && state.StatusCode > 0 {
		return shouldApplyLocalErrorState(account, state.StatusCode)
	}
	return shouldApplyLocalSystemErrorState(account)
}

func shouldIgnoreTempUnschedulableForAccount(account *Account, state *TempUnschedState, reason string) bool {
	if account == nil {
		return false
	}
	matchedKeyword := ""
	if state != nil {
		matchedKeyword = state.MatchedKeyword
	}
	return shouldIgnoreOpenAIOAuthTempUnschedulable(account.Platform, account.Type, matchedKeyword, reason)
}

func shouldIgnoreOpenAIOAuthTempUnschedulable(platform, accountType, matchedKeyword, reason string) bool {
	if platform != PlatformOpenAI || accountType != AccountTypeOAuth {
		return false
	}
	return tempUnschedReasonIsUpstreamRelayPoolUnavailable(matchedKeyword, reason)
}

func tempUnschedReasonIsUpstreamRelayPoolUnavailable(matchedKeyword, reason string) bool {
	if strings.TrimSpace(matchedKeyword) == TempUnschedKeywordUpstreamRelayPoolUnavailable {
		return true
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return false
	}
	var state TempUnschedState
	if err := json.Unmarshal([]byte(reason), &state); err == nil && strings.TrimSpace(state.MatchedKeyword) == TempUnschedKeywordUpstreamRelayPoolUnavailable {
		return true
	}
	normalized := strings.ToLower(reason)
	return strings.Contains(normalized, TempUnschedKeywordUpstreamRelayPoolUnavailable) ||
		strings.Contains(normalized, "upstream relay pool unavailable")
}

func setTempUnschedCacheBestEffort(ctx context.Context, cache TempUnschedCache, accountID int64, state *TempUnschedState, source string) {
	if cache == nil || accountID <= 0 || state == nil {
		return
	}
	if err := cache.SetTempUnsched(ctx, accountID, state); err != nil {
		slog.Warn("temp_unsched_cache_set_failed",
			"account_id", accountID,
			"source", source,
			"error", err,
		)
	}
}

func deleteTempUnschedCacheBestEffort(ctx context.Context, cache TempUnschedCache, accountID int64, source string) {
	if cache == nil || accountID <= 0 {
		return
	}
	if err := cache.DeleteTempUnsched(ctx, accountID); err != nil {
		slog.Warn("temp_unsched_cache_delete_failed",
			"account_id", accountID,
			"source", source,
			"error", err,
		)
	}
}

// TimeoutCounterCache 超时计数器缓存接口
type TimeoutCounterCache interface {
	// IncrementTimeoutCount 增加账户的超时计数，返回当前计数值
	// windowMinutes 是计数窗口时间（分钟），超过此时间计数器会自动重置
	IncrementTimeoutCount(ctx context.Context, accountID int64, windowMinutes int) (int64, error)
	// GetTimeoutCount 获取账户当前的超时计数
	GetTimeoutCount(ctx context.Context, accountID int64) (int64, error)
	// ResetTimeoutCount 重置账户的超时计数
	ResetTimeoutCount(ctx context.Context, accountID int64) error
	// GetTimeoutCountTTL 获取计数器剩余过期时间
	GetTimeoutCountTTL(ctx context.Context, accountID int64) (time.Duration, error)
}
