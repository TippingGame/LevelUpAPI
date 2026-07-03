package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestForwardAsAnthropic_BufferedContextWindowFailureDoesNotFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(nil))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamSSE := strings.Join([]string{
		"event: response.failed",
		`data: {"type":"response.failed","response":{"id":"resp_failed","object":"response","model":"gpt-5.5","status":"failed","output":[],"error":{"code":"upstream_error","message":"input exceeds the context window"}}}`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_messages_context_window"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}

	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          1,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://api.openai.com"},
	}
	body := []byte(`{"model":"claude-sonnet-4-5-20250929","max_tokens":1024,"stream":false,"messages":[{"role":"user","content":"large prompt"}]}`)

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

	require.Error(t, err)
	require.NotNil(t, result)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.True(t, c.Writer.Written())
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "input exceeds the context window")
}

func TestForwardAsAnthropic_StreamAccessDeniedBeforeOutputReturnsFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(nil))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamSSE := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_access_denied","model":"gpt-5.5","status":"in_progress","output":[]}}`,
		"",
		"event: response.failed",
		`data: {"type":"response.failed","response":{"id":"resp_access_denied","object":"response","model":"gpt-5.5","status":"failed","output":[],"error":{"type":"access_denied","message":"workspace forbidden by policy","details":{"reason":"ip_blocked"}}}}`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_messages_access_denied"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	repo := &openAIPassthroughFailoverRepo{}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
		rateLimitService: &RateLimitService{
			accountRepo: repo,
			cfg:         &config.Config{},
		},
	}
	account := &Account{
		ID:          2,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-acc"},
	}
	body := []byte(`{"model":"claude-sonnet-4-5-20250929","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

	require.Error(t, err)
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
	require.Len(t, repo.tempCalls, 1)
	require.Contains(t, repo.tempReasons[0], "openai_403_counter_unavailable")
	require.Contains(t, repo.tempReasons[0], "workspace forbidden by policy")
}

func TestForwardAsAnthropic_StreamContextWindowFailureDoesNotFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(nil))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamSSE := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_context","model":"gpt-5.5","status":"in_progress","output":[]}}`,
		"",
		"event: response.failed",
		`data: {"type":"response.failed","response":{"id":"resp_context","object":"response","model":"gpt-5.5","status":"failed","output":[],"error":{"code":"upstream_error","message":"input exceeds the context window"}}}`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_messages_context_stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          3,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://api.openai.com"},
	}
	body := []byte(`{"model":"claude-sonnet-4-5-20250929","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"large prompt"}]}`)

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

	require.Error(t, err)
	require.NotNil(t, result)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.True(t, c.Writer.Written())
	require.Contains(t, rec.Body.String(), "event: error")
	require.Contains(t, rec.Body.String(), "input exceeds the context window")
}
