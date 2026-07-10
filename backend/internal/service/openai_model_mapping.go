package service

import "strings"

var openAIOAuthForeignModelPrefixes = []string{
	"deepseek",
	"glm-",
	"kimi-",
	"moonshot",
	"qwen",
	"qwq-",
	"minimax",
	"gemini-",
	"gemma-",
	"grok-",
	"doubao-",
	"hunyuan-",
	"llama",
	"meta-llama",
	"mistral",
	"mixtral",
	"baichuan",
	"ernie-",
	"step-",
	"seed-",
	"yi-",
}

// resolveOpenAIForwardModel determines the upstream model for OpenAI-compatible
// forwarding. The group-level messages default only applies to Claude-family
// dispatch requests that did not match an explicit model_mapping rule.
func resolveOpenAIForwardModel(account *Account, requestedModel, defaultMappedModel string) string {
	if account == nil {
		if defaultMappedModel != "" && claudeMessagesDispatchFamily(requestedModel) != "" {
			return defaultMappedModel
		}
		return requestedModel
	}

	mappedModel, matched := account.ResolveMappedModel(requestedModel)
	if !matched && defaultMappedModel != "" && claudeMessagesDispatchFamily(requestedModel) != "" {
		return defaultMappedModel
	}
	return mappedModel
}

// isOpenAIOAuthServableModel 对空 model_mapping 的 Codex OAuth 账号采用保守
// fail-open 策略：自定义别名仍允许，仅过滤确定不可能由 OpenAI 上游服务的厂商前缀。
func isOpenAIOAuthServableModel(requestedModel string) bool {
	model := strings.ToLower(lastOpenAIModelSegment(requestedModel))
	if model == "" {
		return true
	}
	for _, prefix := range openAIOAuthForeignModelPrefixes {
		if strings.HasPrefix(model, prefix) {
			return false
		}
	}
	return true
}

// resolveOpenAICompactForwardModel determines the compact-only upstream model
// for /responses/compact requests. It never affects normal /responses traffic.
// When no compact-specific mapping matches, the input model is returned as-is.
func resolveOpenAICompactForwardModel(account *Account, model string) string {
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" || account == nil {
		return trimmedModel
	}

	mappedModel, matched := account.ResolveCompactMappedModel(trimmedModel)
	if !matched {
		return trimmedModel
	}
	if trimmedMapped := strings.TrimSpace(mappedModel); trimmedMapped != "" {
		return trimmedMapped
	}
	return trimmedModel
}
