package service

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

const (
	modelRateLimitsKey                 = "model_rate_limits"
	antigravityGeminiModelRateLimitKey = "antigravity:gemini"
	openAIImageGenerationRateLimitKey  = "openai:image_generation"
	// Anthropic 7d_oi is a Fable-family window; rate limiting this scope should
	// exclude all Fable variants without blocking the whole account.
	anthropicFableRateLimitKey = "claude-fable-5"
)

// isRateLimitActiveForKey 检查指定 key 的限流是否生效
func (a *Account) isRateLimitActiveForKey(key string) bool {
	resetAt := a.modelRateLimitResetAt(key)
	return resetAt != nil && time.Now().Before(*resetAt)
}

// getRateLimitRemainingForKey 获取指定 key 的限流剩余时间，0 表示未限流或已过期
func (a *Account) getRateLimitRemainingForKey(key string) time.Duration {
	resetAt := a.modelRateLimitResetAt(key)
	if resetAt == nil {
		return 0
	}
	remaining := time.Until(*resetAt)
	if remaining > 0 {
		return remaining
	}
	return 0
}

func (a *Account) isModelRateLimitedWithContext(ctx context.Context, requestedModel string) bool {
	if a == nil {
		return false
	}

	for _, modelKey := range a.modelRateLimitKeysForRequest(ctx, requestedModel) {
		if a.isRateLimitActiveForKey(modelKey) {
			return true
		}
	}
	return false
}

// GetModelRateLimitRemainingTime 获取模型限流剩余时间
// 返回 0 表示未限流或已过期
func (a *Account) GetModelRateLimitRemainingTime(requestedModel string) time.Duration {
	return a.GetModelRateLimitRemainingTimeWithContext(context.Background(), requestedModel)
}

func (a *Account) GetModelRateLimitRemainingTimeWithContext(ctx context.Context, requestedModel string) time.Duration {
	if a == nil {
		return 0
	}

	var maxRemaining time.Duration
	for _, modelKey := range a.modelRateLimitKeysForRequest(ctx, requestedModel) {
		if remaining := a.getRateLimitRemainingForKey(modelKey); remaining > maxRemaining {
			maxRemaining = remaining
		}
	}
	return maxRemaining
}

func (a *Account) modelRateLimitKeysForRequest(ctx context.Context, requestedModel string) []string {
	if a == nil {
		return nil
	}
	modelKey := a.GetMappedModel(requestedModel)
	if a.Platform == PlatformAntigravity {
		modelKey = resolveFinalAntigravityModelKey(ctx, a, requestedModel)
	}
	modelKey = strings.TrimSpace(modelKey)
	if modelKey == "" {
		return nil
	}
	keys := []string{modelKey}
	switch a.Platform {
	case PlatformAntigravity:
		if isAntigravityGeminiModel(modelKey) && modelKey != antigravityGeminiModelRateLimitKey {
			keys = append(keys, antigravityGeminiModelRateLimitKey)
		}
	case PlatformOpenAI:
		if openAIImageGenerationRateLimitApplies(ctx, requestedModel, modelKey) && modelKey != openAIImageGenerationRateLimitKey {
			keys = append(keys, openAIImageGenerationRateLimitKey)
		}
	case PlatformAnthropic:
		if isAnthropicFableModel(modelKey) && modelKey != anthropicFableRateLimitKey {
			keys = append(keys, anthropicFableRateLimitKey)
		}
	}
	return keys
}

func openAIImageGenerationRateLimitApplies(ctx context.Context, requestedModel, modelKey string) bool {
	return isOpenAIImageGenerationModel(requestedModel) || isOpenAIImageGenerationModel(modelKey) || OpenAIImageGenerationIntentFromContext(ctx)
}

func WithOpenAIImageGenerationIntent(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxkey.OpenAIImageGenerationIntent, true)
}

func OpenAIImageGenerationIntentFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, ok := ctx.Value(ctxkey.OpenAIImageGenerationIntent).(bool)
	return ok && enabled
}

func isAntigravityGeminiModel(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(normalized, "gemini")
}

func antigravityModelRateLimitKeys(model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	keys := []string{model}
	if isAntigravityGeminiModel(model) && model != antigravityGeminiModelRateLimitKey {
		keys = append(keys, antigravityGeminiModelRateLimitKey)
	}
	return keys
}

func isAnthropicFableModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "fable")
}

func resolveFinalAntigravityModelKey(ctx context.Context, account *Account, requestedModel string) string {
	modelKey := mapAntigravityModel(account, requestedModel)
	if modelKey == "" {
		return ""
	}
	// thinking 会影响 Antigravity 最终模型名（例如 claude-sonnet-4-5 -> claude-sonnet-4-5-thinking）
	if enabled, ok := ThinkingEnabledFromContext(ctx); ok {
		modelKey = applyThinkingModelSuffix(modelKey, enabled)
	}
	return modelKey
}

func (a *Account) modelRateLimitResetAt(scope string) *time.Time {
	if a == nil || a.Extra == nil || scope == "" {
		return nil
	}
	if !shouldApplyLocalSystemErrorState(a) {
		return nil
	}
	rawLimits, ok := a.Extra[modelRateLimitsKey].(map[string]any)
	if !ok {
		return nil
	}
	rawLimit, ok := rawLimits[scope].(map[string]any)
	if !ok {
		return nil
	}
	resetAtRaw, ok := rawLimit["rate_limit_reset_at"].(string)
	if !ok || strings.TrimSpace(resetAtRaw) == "" {
		return nil
	}
	resetAt, err := time.Parse(time.RFC3339, resetAtRaw)
	if err != nil {
		return nil
	}
	return &resetAt
}
