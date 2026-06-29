package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newGinContextForEndpoint(t *testing.T, endpoint string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, endpoint, nil)
	return c, w
}

func parseResponsesFailedSSE(t *testing.T, body string) (map[string]any, map[string]any) {
	t.Helper()
	require.True(t, strings.HasPrefix(body, "event: response.failed\n"), "got: %q", body)
	require.True(t, strings.HasSuffix(body, "\n\n"))

	lines := strings.SplitN(strings.TrimSuffix(body, "\n\n"), "\n", 2)
	require.Len(t, lines, 2)
	require.True(t, strings.HasPrefix(lines[1], "data: "))
	jsonStr := strings.TrimPrefix(lines[1], "data: ")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed), "data must be valid JSON: %s", jsonStr)

	assert.Equal(t, "response.failed", parsed["type"])
	_, hasSeq := parsed["sequence_number"]
	assert.False(t, hasSeq)

	resp, ok := parsed["response"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "response", resp["object"])
	assert.Equal(t, "failed", resp["status"])

	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok)

	return resp, errObj
}

func TestOpenAIHandleStreamingAwareError_ResponsesStreamingEmitsResponseFailed(t *testing.T) {
	c, w := newGinContextForEndpoint(t, EndpointResponses)
	h := &OpenAIGatewayHandler{}
	h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error",
		"Concurrency limit exceeded for user, please retry later", true)

	resp, errObj := parseResponsesFailedSSE(t, w.Body.String())

	id, _ := resp["id"].(string)
	assert.True(t, strings.HasPrefix(id, "resp_"))
	assert.Equal(t, "rate_limit_exceeded", errObj["code"])
	assert.Equal(t, "Concurrency limit exceeded for user, please retry later", errObj["message"])
}

func TestOpenAIHandleStreamingAwareError_ResponsesStreamingIncludesModel(t *testing.T) {
	c, w := newGinContextForEndpoint(t, EndpointResponses)
	setOpsRequestContext(c, "gpt-5.5", true, nil)

	h := &OpenAIGatewayHandler{}
	h.handleStreamingAwareError(c, http.StatusBadGateway, "upstream_error", "boom", true)

	resp, _ := parseResponsesFailedSSE(t, w.Body.String())
	assert.Equal(t, "gpt-5.5", resp["model"])
}

func TestOpenAIHandleStreamingAwareError_ResponsesStreamingOmitsEmptyModel(t *testing.T) {
	c, w := newGinContextForEndpoint(t, EndpointResponses)
	h := &OpenAIGatewayHandler{}
	h.handleStreamingAwareError(c, http.StatusBadGateway, "upstream_error", "boom", true)

	resp, _ := parseResponsesFailedSSE(t, w.Body.String())
	_, hasModel := resp["model"]
	assert.False(t, hasModel)
}

func TestOpenAIHandleStreamingAwareError_ResponsesStreamingReusesRequestID(t *testing.T) {
	c, w := newGinContextForEndpoint(t, EndpointResponses)
	c.Request = c.Request.WithContext(
		context.WithValue(c.Request.Context(), ctxkey.RequestID, "fd277bc5-ff7e-45d1-8aa9-f54e1df318f1"),
	)

	h := &OpenAIGatewayHandler{}
	h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "x", true)

	resp, _ := parseResponsesFailedSSE(t, w.Body.String())
	assert.Equal(t, "resp_fd277bc5ff7e45d18aa9f54e1df318f1", resp["id"])
}

func TestOpenAIHandleStreamingAwareError_ResponsesStreamingJSONEscaping(t *testing.T) {
	cases := []struct {
		name    string
		errType string
		message string
	}{
		{"quote", "server_error", `upstream returned "invalid" response`},
		{"backslash", "server_error", `path C:\Users\test\file.txt not found`},
		{"quote and backslash", "upstream_error", `error parsing "key\value": unexpected token`},
		{"newline and tab", "server_error", "line1\nline2\ttab"},
		{"plain", "upstream_error", "Upstream service temporarily unavailable"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := newGinContextForEndpoint(t, EndpointResponses)
			h := &OpenAIGatewayHandler{}
			h.handleStreamingAwareError(c, http.StatusBadGateway, tc.errType, tc.message, true)

			_, errObj := parseResponsesFailedSSE(t, w.Body.String())
			assert.Equal(t, tc.message, errObj["message"])
		})
	}
}

func TestOpenAIHandleStreamingAwareError_ChatCompletionsStreamingKeepsLegacy(t *testing.T) {
	c, w := newGinContextForEndpoint(t, EndpointChatCompletions)
	h := &OpenAIGatewayHandler{}
	h.handleStreamingAwareError(c, http.StatusBadGateway, "upstream_error", "boom", true)

	body := w.Body.String()
	assert.True(t, strings.HasPrefix(body, "event: error\n"), "got: %q", body)
}

func TestGatewayHandleStreamingAwareError_ResponsesStreamingEmitsResponseFailed(t *testing.T) {
	c, w := newGinContextForEndpoint(t, EndpointResponses)
	h := &GatewayHandler{}
	h.handleStreamingAwareError(c, http.StatusBadGateway, "upstream_error", "upstream gone", true)

	_, errObj := parseResponsesFailedSSE(t, w.Body.String())
	assert.Equal(t, "upstream_error", errObj["code"])
	assert.Equal(t, "upstream gone", errObj["message"])
}

func TestGatewayHandleStreamingAwareError_MessagesStreamingKeepsLegacy(t *testing.T) {
	c, w := newGinContextForEndpoint(t, EndpointMessages)
	h := &GatewayHandler{}
	h.handleStreamingAwareError(c, http.StatusBadGateway, "upstream_error", "boom", true)

	body := w.Body.String()
	assert.True(t, strings.HasPrefix(body, `data: {"type":"error"`), "got: %q", body)
}

func TestInboundIsResponses_CoversAllRoutes(t *testing.T) {
	cases := []struct {
		route string
		want  bool
	}{
		{"/v1/responses", true},
		{"/v1/responses/compact", true},
		{"/responses", true},
		{"/responses/compact", true},
		{"/backend-api/codex/responses", true},
		{"/backend-api/codex/responses/compact", true},
		{"/v1/chat/completions", false},
		{"/v1/messages", false},
		{"/", false},
		{"/responses-fake", false},
	}
	for _, tc := range cases {
		t.Run(tc.route, func(t *testing.T) {
			c, _ := newGinContextForEndpoint(t, tc.route)
			assert.Equal(t, tc.want, inboundIsResponses(c))
		})
	}
}

func TestInboundIsResponses_FallsBackToURLPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/responses", nil)

	assert.True(t, inboundIsResponses(c))
}

func TestOpenAIHandleStreamingAwareError_BareResponsesRouteEmitsResponseFailed(t *testing.T) {
	c, w := newGinContextForEndpoint(t, "/responses")
	h := &OpenAIGatewayHandler{}
	h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error",
		"Concurrency limit exceeded for user, please retry later", true)

	resp, errObj := parseResponsesFailedSSE(t, w.Body.String())
	id, _ := resp["id"].(string)
	assert.True(t, strings.HasPrefix(id, "resp_"))
	assert.Equal(t, "rate_limit_exceeded", errObj["code"])
}

func TestSynthesizeResponseID_FallbackUUID(t *testing.T) {
	c, _ := newGinContextForEndpoint(t, EndpointResponses)
	id := synthesizeResponseID(c)
	assert.True(t, strings.HasPrefix(id, "resp_"))
	assert.Len(t, id, 37)
}

func TestMapResponsesErrorCode(t *testing.T) {
	cases := []struct{ in, out string }{
		{"rate_limit_error", "rate_limit_exceeded"},
		{"invalid_request_error", "invalid_request"},
		{"permission_error", "permission_denied"},
		{"authentication_error", "authentication_failed"},
		{"upstream_error", "upstream_error"},
		{"server_error", "server_error"},
		{"api_error", "server_error"},
		{"", "server_error"},
		{"custom_thing", "custom_thing"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.out, mapResponsesErrorCode(tc.in), "in=%q", tc.in)
	}
}
