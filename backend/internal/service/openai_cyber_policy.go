package service

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const opsCyberPolicyKey = "ops_cyber_policy"
const openAICyberPolicyDefaultMessage = "Request blocked by upstream cyber-security policy"

type CyberPolicyMark struct {
	Code           string
	Message        string
	Body           string
	UpstreamStatus int
	UpstreamInTok  int
	UpstreamOutTok int
}

func MarkOpsCyberPolicy(c *gin.Context, mark CyberPolicyMark) {
	if c == nil {
		return
	}
	if GetOpsCyberPolicy(c) != nil {
		return
	}
	mark.Code = "cyber_policy"
	mark.Message = strings.TrimSpace(mark.Message)
	mark.Body = strings.TrimSpace(mark.Body)
	c.Set(opsCyberPolicyKey, &mark)
}

func GetOpsCyberPolicy(c *gin.Context) *CyberPolicyMark {
	if c == nil {
		return nil
	}
	if v, ok := c.Get(opsCyberPolicyKey); ok {
		if m, ok := v.(*CyberPolicyMark); ok && m != nil {
			return m
		}
	}
	return nil
}

func openAICyberPolicyClientMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return openAICyberPolicyDefaultMessage
	}
	return msg
}

func detectOpenAICyberPolicy(payload []byte) (bool, string, string) {
	code := gjson.GetBytes(payload, "error.code").String()
	if code == "" {
		code = gjson.GetBytes(payload, "response.error.code").String()
	}
	if !strings.EqualFold(strings.TrimSpace(code), "cyber_policy") {
		return false, "", ""
	}
	msg := gjson.GetBytes(payload, "error.message").String()
	if msg == "" {
		msg = gjson.GetBytes(payload, "response.error.message").String()
	}
	return true, "cyber_policy", strings.TrimSpace(msg)
}

func isOpenAIRequestPolicyError(payload []byte, upstreamMsg string) bool {
	if _, permanent := permanentAccountKeywordErrorMessage(
		&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		http.StatusForbidden,
		upstreamMsg,
		payload,
	); permanent {
		return false
	}
	if hit, _, _ := detectOpenAICyberPolicy(payload); hit {
		return true
	}

	code, errType, msg := openAIErrorPolicyFields(payload)
	normalizedCode := normalizeOpenAIPolicyToken(code)
	normalizedType := normalizeOpenAIPolicyToken(errType)
	switch normalizedCode {
	case "content_filter", "content_policy", "content_policy_violation", "policy_violation", "safety_violation":
		return true
	case "invalid_encrypted_content", "previous_response_not_found":
		return true
	}
	switch normalizedType {
	case "content_filter", "content_policy", "content_policy_error", "policy_violation", "safety_error", "safety_violation":
		return true
	}

	combinedMsg := strings.ToLower(strings.TrimSpace(msg + " " + upstreamMsg))
	combinedMsg = strings.Join(strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(combinedMsg)), " ")
	for _, marker := range []string{
		"content policy",
		"high risk cyber",
		"high-risk cyber",
		"safety policy",
		"safety system",
	} {
		if strings.Contains(combinedMsg, marker) {
			return true
		}
	}
	return false
}

func openAIErrorPolicyFields(payload []byte) (code string, errType string, msg string) {
	if len(payload) == 0 {
		return "", "", ""
	}
	code = firstNonEmptyTrimmed(
		gjson.GetBytes(payload, "error.code").String(),
		gjson.GetBytes(payload, "response.error.code").String(),
	)
	errType = firstNonEmptyTrimmed(
		gjson.GetBytes(payload, "error.type").String(),
		gjson.GetBytes(payload, "response.error.type").String(),
	)
	msg = firstNonEmptyTrimmed(
		gjson.GetBytes(payload, "error.message").String(),
		gjson.GetBytes(payload, "response.error.message").String(),
	)
	return code, errType, msg
}

func normalizeOpenAIPolicyToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("-", "_", " ", "_").Replace(value)
	return strings.Trim(value, "_")
}
