//go:build unit

package service

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCreateStandardClaudeTestPayload_DoesNotMimicClaudeCode(t *testing.T) {
	t.Parallel()

	payload := createStandardClaudeTestPayload("claude-opus-4-8")

	require.Equal(t, "claude-opus-4-8", payload["model"])
	require.NotContains(t, payload, "system")
	require.NotContains(t, payload, "metadata")

	body, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NotContains(t, string(body), "x-anthropic-billing-header")
	require.NotContains(t, string(body), "Claude Code")
	require.NotContains(t, string(body), "cache_control")
}

func TestCreateClaudeCodeTestPayload_IncludesClaudeCodeAttributionBlocks(t *testing.T) {
	t.Parallel()

	payload, err := createClaudeCodeTestPayload("claude-opus-4-8")
	require.NoError(t, err)

	system, ok := payload["system"].([]map[string]any)
	require.True(t, ok, "system should be Claude Code-style text blocks")
	require.Len(t, system, 3)

	billingBlock := system[0]
	require.Equal(t, "text", billingBlock["type"])
	billingText, ok := billingBlock["text"].(string)
	require.True(t, ok)
	require.Contains(t, billingText, "x-anthropic-billing-header:")
	require.Contains(t, billingText, "cc_version="+claude.CLICurrentVersion+".")
	require.Contains(t, billingText, "cc_entrypoint=cli")
	require.NotContains(t, billingText, "cch=")
	require.NotContains(t, billingBlock, "cache_control")

	promptBlock := system[1]
	require.Equal(t, "text", promptBlock["type"])
	require.Equal(t, claudeCodeSystemPrompt, promptBlock["text"])

	expansionBlock := system[2]
	require.Equal(t, "text", expansionBlock["type"])
	require.Equal(t, claudeCodeSystemPromptExpansion, expansionBlock["text"])
	cacheControl, ok := expansionBlock["cache_control"].(map[string]any)
	require.True(t, ok, "expansion block should keep cache_control")
	require.Equal(t, "ephemeral", cacheControl["type"])
}

func TestAccountTestService_ClaudeAPIKeyConnectionUsesStandardAnthropicRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := newTestContext()
	upstream := &httpUpstreamRecorder{resp: claudeTestSSEOKResponse()}
	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:          41,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test"},
	}

	err := svc.testClaudeAccountConnection(c, account, "claude-opus-4-8")

	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "sk-test", getHeaderRaw(upstream.lastReq.Header, "x-api-key"))
	require.Equal(t, "", getHeaderRaw(upstream.lastReq.Header, "anthropic-beta"))
	require.Equal(t, "", getHeaderRaw(upstream.lastReq.Header, "x-app"))
	require.Equal(t, "", getHeaderRaw(upstream.lastReq.Header, "x-client-request-id"))

	require.Equal(t, "claude-opus-4-8", gjson.GetBytes(upstream.lastBody, "model").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "system").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "metadata").Exists())
	require.NotContains(t, string(upstream.lastBody), "x-anthropic-billing-header")
	require.NotContains(t, string(upstream.lastBody), "Claude Code")
}

func TestAccountTestService_ClaudeOAuthConnectionUsesClaudeCodeMimicry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := newTestContext()
	upstream := &httpUpstreamRecorder{resp: claudeTestSSEOKResponse()}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:          42,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
	}

	err := svc.testClaudeAccountConnection(c, account, "claude-opus-4-8")

	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "Bearer oauth-token", getHeaderRaw(upstream.lastReq.Header, "authorization"))
	require.Contains(t, getHeaderRaw(upstream.lastReq.Header, "anthropic-beta"), claude.BetaClaudeCode)
	require.Contains(t, getHeaderRaw(upstream.lastReq.Header, "anthropic-beta"), claude.BetaOAuth)
	require.Equal(t, "cli", getHeaderRaw(upstream.lastReq.Header, "x-app"))
	require.NotEmpty(t, getHeaderRaw(upstream.lastReq.Header, "x-client-request-id"))
	require.Equal(t, "stream", getHeaderRaw(upstream.lastReq.Header, "x-stainless-helper-method"))

	system := gjson.GetBytes(upstream.lastBody, "system")
	require.True(t, system.IsArray())
	require.Contains(t, system.Array()[0].Get("text").String(), "x-anthropic-billing-header:")
	require.Equal(t, claudeCodeSystemPrompt, system.Array()[1].Get("text").String())
}

func claudeTestSSEOKResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"ok\"}}\n\ndata: [DONE]\n\n")),
	}
}
