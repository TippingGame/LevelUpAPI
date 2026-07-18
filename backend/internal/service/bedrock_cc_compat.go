package service

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const featureKeyBedrockCCCompat = "bedrock_cc_compat"

func (c *Channel) IsBedrockCCCompatEnabled(_ string) bool {
	if c == nil || c.FeaturesConfig == nil {
		return false
	}
	enabled, ok := c.FeaturesConfig[featureKeyBedrockCCCompat].(bool)
	return ok && enabled
}

var bedrockToolUseIDRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func isBedrockOpus47OrNewer(modelID string) bool {
	lower := strings.ToLower(modelID)
	if !strings.Contains(lower, "opus") {
		return false
	}
	matches := claudeVersionRe.FindStringSubmatch(lower)
	if matches == nil {
		return false
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	return major > 4 || (major == 4 && minor >= 7)
}

func isBedrockFable5(modelID string) bool {
	return strings.Contains(strings.ToLower(modelID), "claude-fable-5")
}

const defaultThinkingBudgetTokens = 10000

func sanitizeBedrockThinking(body []byte, modelID string) []byte {
	thinking := gjson.GetBytes(body, "thinking")
	if !thinking.Exists() || !thinking.IsObject() {
		return body
	}

	thinkingType := thinking.Get("type").String()
	if thinkingType == "" {
		return body
	}
	if isBedrockFable5(modelID) {
		if thinkingType == "enabled" {
			body, _ = sjson.SetBytes(body, "thinking.type", "adaptive")
		}
		if thinkingType == "enabled" || thinkingType == "adaptive" {
			body, _ = sjson.DeleteBytes(body, "thinking.budget_tokens")
		}
		return body
	}
	if isBedrockOpus47OrNewer(modelID) {
		if thinkingType == "enabled" {
			body, _ = sjson.SetBytes(body, "thinking.type", "adaptive")
			body, _ = sjson.DeleteBytes(body, "thinking.budget_tokens")
		}
		return body
	}
	if thinkingType == "enabled" && !thinking.Get("budget_tokens").Exists() {
		body, _ = sjson.SetBytes(body, "thinking.budget_tokens", defaultThinkingBudgetTokens)
	}
	return body
}

func sanitizeBedrockToolUseIDs(body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}
	for mi, msg := range messages.Array() {
		content := msg.Get("content")
		if !content.Exists() || !content.IsArray() {
			continue
		}
		for ci, block := range content.Array() {
			switch block.Get("type").String() {
			case "tool_use":
				body = sanitizeBedrockIDField(body, block.Get("id").String(), fmt.Sprintf("messages.%d.content.%d.id", mi, ci))
			case "tool_result":
				body = sanitizeBedrockIDField(body, block.Get("tool_use_id").String(), fmt.Sprintf("messages.%d.content.%d.tool_use_id", mi, ci))
			}
		}
	}
	return body
}

func sanitizeBedrockIDField(body []byte, id, path string) []byte {
	if id == "" {
		return body
	}
	sanitized := bedrockToolUseIDRe.ReplaceAllString(id, "_")
	if sanitized != id {
		body, _ = sjson.SetBytes(body, path, sanitized)
	}
	return body
}

const defaultCCMaxTokens = 81920

func sanitizeBedrockCCFields(body []byte) []byte {
	for _, field := range []string{"service_tier", "interface_geo", "context_management"} {
		if gjson.GetBytes(body, field).Exists() {
			body, _ = sjson.DeleteBytes(body, field)
		}
	}
	if !gjson.GetBytes(body, "max_tokens").Exists() {
		body, _ = sjson.SetBytes(body, "max_tokens", defaultCCMaxTokens)
	}
	if !gjson.GetBytes(body, "anthropic_version").Exists() {
		body, _ = sjson.SetBytes(body, "anthropic_version", "bedrock-2023-05-31")
	}
	return body
}

func sanitizeBedrockCCBetaTokens(body []byte, modelID string) []byte {
	betaField := gjson.GetBytes(body, "anthropic_beta")
	if !betaField.Exists() {
		return body
	}
	var tokens []string
	if betaField.IsArray() {
		for _, token := range betaField.Array() {
			if token.Type == gjson.String {
				tokens = append(tokens, token.String())
			}
		}
	}
	originalTokens := append([]string(nil), tokens...)
	tokens = autoInjectBedrockBetaTokens(tokens, body, modelID)
	tokens = filterBedrockBetaTokens(tokens)
	if len(tokens) == 0 {
		body, _ = sjson.DeleteBytes(body, "anthropic_beta")
		logger.LegacyPrintf("service.gateway", "[Bedrock CC Compat] Removed all beta tokens: original=%v", originalTokens)
		return body
	}
	body, _ = sjson.SetBytes(body, "anthropic_beta", tokens)
	if len(originalTokens) > 0 {
		logger.LegacyPrintf("service.gateway", "[Bedrock CC Compat] Filtered beta tokens: original=%v final=%v", originalTokens, tokens)
	} else {
		logger.LegacyPrintf("service.gateway", "[Bedrock CC Compat] Auto-injected beta tokens: %v", tokens)
	}
	return body
}
