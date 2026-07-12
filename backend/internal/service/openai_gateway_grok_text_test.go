//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPatchGrokResponsesBodyKeepsTextFeaturesAndDropsUnsupportedFields(t *testing.T) {
	body := []byte(`{
		"model":"grok-latest",
		"input":"hello",
		"reasoning":{"effort":"high"},
		"prompt_cache_retention":"24h",
		"safety_identifier":"user-1",
		"presence_penalty":0.1,
		"external_web_access":true,
		"tools":[
			{"type":"namespace","namespace":"functions"},
			{"type":"function","name":"lookup","parameters":{"type":"object","external_web_access":true}}
		],
		"tool_choice":{"type":"function","name":"lookup"}
	}`)

	patched, err := patchGrokResponsesBody(body, "grok-4.5")
	require.NoError(t, err)
	require.True(t, json.Valid(patched))
	require.Equal(t, "grok-4.5", gjson.GetBytes(patched, "model").String())
	require.Equal(t, "high", gjson.GetBytes(patched, "reasoning.effort").String())
	require.False(t, gjson.GetBytes(patched, "prompt_cache_retention").Exists())
	require.False(t, gjson.GetBytes(patched, "safety_identifier").Exists())
	require.False(t, gjson.GetBytes(patched, "presence_penalty").Exists())
	require.NotContains(t, string(patched), "external_web_access")
	require.Len(t, gjson.GetBytes(patched, "tools").Array(), 1)
	require.Equal(t, "lookup", gjson.GetBytes(patched, "tool_choice.name").String())
}

func TestPatchGrokResponsesBodyDropsUnsupportedToolChoice(t *testing.T) {
	body := []byte(`{
		"model":"grok",
		"input":"hello",
		"tools":[{"type":"image_generation","model":"grok-imagine"}],
		"tool_choice":{"type":"image_generation"}
	}`)

	patched, err := patchGrokResponsesBody(body, "grok-4.3")
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(patched, "tools").Exists())
	require.False(t, gjson.GetBytes(patched, "tool_choice").Exists())
}

func TestPatchGrokResponsesBodySupportsAgentToolsAndDropsPrivateInputCarrier(t *testing.T) {
	body := []byte(`{
		"model":"grok-4.5",
		"instructions":"You are a coding agent.",
		"input":[
			{"type":"additional_tools","role":"developer","tools":[{"type":"custom","name":"exec"}]},
			{"role":"user","content":[{"type":"input_text","text":"Inspect the project"}]}
		],
		"stream":true,
		"store":false,
		"tools":[{
			"type":"function",
			"name":"read_file",
			"description":"Read a file.",
			"parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]},
			"strict":false
		}],
		"tool_choice":"auto"
	}`)

	patched, err := patchGrokResponsesBody(body, "grok-4.5")
	require.NoError(t, err)
	require.True(t, json.Valid(patched))
	require.False(t, gjson.GetBytes(patched, `input.#(type=="additional_tools")`).Exists())
	require.Len(t, gjson.GetBytes(patched, "input").Array(), 1)
	require.Equal(t, "read_file", gjson.GetBytes(patched, "tools.0.name").String())
	require.True(t, gjson.GetBytes(patched, "tools.0.strict").Exists())
	require.False(t, gjson.GetBytes(patched, "tools.0.strict").Bool())
	require.Equal(t, "auto", gjson.GetBytes(patched, "tool_choice").String())
	require.False(t, gjson.GetBytes(patched, "store").Bool())
}

func TestBuildGrokResponsesRequestUsesOfficialBearerEndpoint(t *testing.T) {
	account := &Account{Platform: PlatformGrok, Type: AccountTypeOAuth}
	req, err := buildGrokResponsesRequest(context.Background(), nil, account, []byte(`{"model":"grok-4.3"}`), "access-token")
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, xai.DefaultBaseURL+"/responses", req.URL.String())
	require.Equal(t, "Bearer access-token", req.Header.Get("Authorization"))
	require.True(t, strings.Contains(req.Header.Get("Accept"), "text/event-stream"))
}

func TestExtractGrokReasoningEffort(t *testing.T) {
	effort := extractOpenAIReasoningEffortFromBody([]byte(`{"model":"grok-4.3","reasoning_effort":"high"}`), "grok-4.3")
	require.NotNil(t, effort)
	require.Equal(t, "high", *effort)
}

func grokTextTestAccount(id int64) (*Account, *grokQuotaAccountRepo) {
	account := &Account{
		ID:          id,
		Name:        "grok",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "access-token",
			"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			"base_url":     xai.DefaultBaseURL,
		},
	}
	repo := &grokQuotaAccountRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
		accountsByID: map[int64]*Account{id: account},
	}}
	return account, repo
}

func grokTextTestConfig() *config.Config {
	return &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
		Enabled: false, AllowInsecureHTTP: true,
	}}}
}

func grokMessagesCompletedResponse() *http.Response {
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_grok_messages","object":"response","model":"grok-4.3","status":"completed","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"grok-messages-request"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestForwardAsChatCompletionsForGrokUsesXAIAndCapturesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"grok","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	account, repo := grokTextTestAccount(51)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":                   []string{"application/json"},
			"Xai-Request-Id":                 []string{"xai-chat-request"},
			"X-Ratelimit-Limit-Requests":     []string{"10"},
			"X-Ratelimit-Remaining-Requests": []string{"9"},
		},
		Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl","object":"chat.completion","model":"grok-4.5","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":1}}}`)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream, grokTokenProvider: NewGrokTokenProvider(repo, nil), accountRepo: repo}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer access-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "grok-4.5", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, 1, result.Usage.CacheReadInputTokens)
	require.NotNil(t, repo.updates[51][grokQuotaSnapshotExtraKey])
}

func TestForwardGrokResponsesStreamingCapturesReasoningCacheAndQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"grok","input":"hi","stream":true,"reasoning_effort":"high"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("OpenAI-Beta", "responses=experimental")

	account, repo := grokTextTestAccount(52)
	upstreamBody := strings.Join([]string{
		`data: {"type":"response.output_text.delta","sequence_number":0,"delta":"ok"}`,
		"",
		`data: {"type":"response.completed","sequence_number":1,"response":{"id":"resp_grok","model":"grok-4.5","usage":{"input_tokens":5,"output_tokens":3,"input_tokens_details":{"cached_tokens":2}}}}`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":                   []string{"text/event-stream"},
			"Xai-Request-Id":                 []string{"xai-stream-request"},
			"X-Ratelimit-Limit-Requests":     []string{"10"},
			"X-Ratelimit-Remaining-Requests": []string{"8"},
		},
		Body: io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream, grokTokenProvider: NewGrokTokenProvider(repo, nil), accountRepo: repo}

	result, err := svc.forwardGrokResponses(context.Background(), c, account, body, "grok", true, time.Now())
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/responses", upstream.lastReq.URL.String())
	require.Equal(t, "responses=experimental", upstream.lastReq.Header.Get("OpenAI-Beta"))
	require.Equal(t, "grok-4.5", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "resp_grok", result.ResponseID)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Equal(t, 2, result.Usage.CacheReadInputTokens)
	require.NotNil(t, result.ReasoningEffort)
	require.Equal(t, "high", *result.ReasoningEffort)
	require.Contains(t, recorder.Body.String(), "response.output_text.delta")
	require.NotNil(t, repo.updates[52][grokQuotaSnapshotExtraKey])
}

func TestForwardAsChatCompletionsForGrokStreamingCapturesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"grok","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	account, repo := grokTextTestAccount(53)
	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_grok","object":"chat.completion.chunk","model":"grok-4.5","choices":[{"index":0,"delta":{"content":"ok"}}]}`,
		"",
		`data: {"id":"chatcmpl_grok","object":"chat.completion.chunk","model":"grok-4.5","choices":[],"usage":{"prompt_tokens":6,"completion_tokens":4,"total_tokens":10,"prompt_tokens_details":{"cached_tokens":1}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":                   []string{"text/event-stream"},
			"X-Request-Id":                   []string{"chat-stream-request"},
			"X-Ratelimit-Limit-Requests":     []string{"10"},
			"X-Ratelimit-Remaining-Requests": []string{"7"},
		},
		Body: io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{cfg: grokTextTestConfig(), httpUpstream: upstream, grokTokenProvider: NewGrokTokenProvider(repo, nil), accountRepo: repo}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Bool())
	require.Equal(t, 6, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Equal(t, 1, result.Usage.CacheReadInputTokens)
	require.Contains(t, recorder.Body.String(), "data: [DONE]")
	require.NotNil(t, repo.updates[53][grokQuotaSnapshotExtraKey])
}

func TestForwardAsAnthropicForGrokUsesResponsesConversion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"grok","max_tokens":32,"stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	account, repo := grokTextTestAccount(54)
	upstream := &httpUpstreamRecorder{resp: grokMessagesCompletedResponse()}
	svc := &OpenAIGatewayService{httpUpstream: upstream, grokTokenProvider: NewGrokTokenProvider(repo, nil), accountRepo: repo}

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/responses", upstream.lastReq.URL.String())
	require.Equal(t, "sub2api-grok/1.0", upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, "grok-4.5", gjson.GetBytes(upstream.lastBody, "model").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Contains(t, recorder.Body.String(), `"type":"message"`)
	require.Contains(t, recorder.Body.String(), "ok")
}

func TestHandleGrokAccountUpstreamErrorAppliesCooldown(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		headers    http.Header
		wantReason string
		cooldown   time.Duration
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, wantReason: "grok oauth token unauthorized", cooldown: 10 * time.Minute},
		{name: "forbidden", status: http.StatusForbidden, wantReason: "grok entitlement or subscription tier denied", cooldown: 30 * time.Minute},
		{name: "rate limited", status: http.StatusTooManyRequests, headers: http.Header{"Retry-After": []string{"45"}}, wantReason: "grok rate limited", cooldown: 45 * time.Second},
		{name: "server error", status: http.StatusBadGateway, wantReason: "grok upstream temporary error", cooldown: 2 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{ID: 61, Platform: PlatformGrok, Type: AccountTypeOAuth}
			repo := &grokQuotaAccountRepo{}
			svc := &OpenAIGatewayService{accountRepo: repo}
			before := time.Now()

			svc.handleGrokAccountUpstreamError(context.Background(), account, tt.status, tt.headers, nil)

			require.Equal(t, 1, repo.tempUnschedCalls)
			require.Equal(t, tt.wantReason, repo.lastTempUnschedReason)
			require.WithinDuration(t, before.Add(tt.cooldown), repo.lastTempUnschedUntil, time.Second)
		})
	}
}
