package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayService_ForwardAsChatCompletions_AnthropicTransportErrorFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	upstream := &anthropicHTTPUpstreamRecorder{
		err: errors.New("dial tcp: connect: connection refused"),
	}
	svc := &GatewayService{
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{Enabled: false},
			},
		},
		httpUpstream: upstream,
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, newAnthropicAPIKeyAccountForTest(), []byte(`{"model":"claude-3-5-sonnet-latest","messages":[{"role":"user","content":"hi"}]}`), nil)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, 0, rec.Body.Len())

	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := raw.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "request_error", events[0].Kind)
	require.Equal(t, "https://api.anthropic.com/v1/messages", events[0].UpstreamURL)
}

func TestGatewayService_ForwardAsResponses_AnthropicStreamRateLimitErrorFailsOverBeforeOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	resetAt := time.Now().Add(time.Hour).Unix()
	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":                         []string{"text/event-stream"},
				"X-Request-Id":                         []string{"rid-bridge-responses-rate-limit"},
				"Anthropic-Ratelimit-Unified-5h-Reset": []string{fmt.Sprint(resetAt)},
				"Anthropic-Ratelimit-Unified-5h-Surpassed-Threshold": []string{"true"},
				"Anthropic-Ratelimit-Unified-5h-Utilization":         []string{"1.0"},
			},
			Body: io.NopCloser(strings.NewReader(strings.Join([]string{
				"event: error",
				`data: {"type":"error","error":{"type":"rate_limit_error","message":"rate limit exceeded"}}`,
				"",
			}, "\n"))),
		},
	}
	repo := &openAIPassthroughFailoverRepo{}
	svc := &GatewayService{
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{Enabled: false},
			},
		},
		httpUpstream:     upstream,
		rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil),
	}

	result, err := svc.ForwardAsResponses(context.Background(), c, newAnthropicAPIKeyAccountForTest(), []byte(`{"model":"claude-3-5-sonnet-latest","stream":true,"input":"hi"}`), nil)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), "rate_limit_error")
	require.Equal(t, 0, rec.Body.Len(), "pre-output bridge stream error must not commit a 200 response before failover")
	require.Len(t, repo.rateLimitCalls, 1)
}

func TestGatewayService_ForwardAsChatCompletions_AnthropicStreamInvalidRequestDoesNotFailoverOrCoolAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
				"X-Request-Id": []string{"rid-bridge-cc-invalid"},
			},
			Body: io.NopCloser(strings.NewReader(strings.Join([]string{
				"event: error",
				`data: {"type":"error","error":{"type":"invalid_request_error","message":"messages: text is required"}}`,
				"",
			}, "\n"))),
		},
	}
	repo := &openAIPassthroughFailoverRepo{}
	svc := &GatewayService{
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{Enabled: false},
			},
		},
		httpUpstream:     upstream,
		rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil),
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, newAnthropicAPIKeyAccountForTest(), []byte(`{"model":"claude-3-5-sonnet-latest","stream":true,"messages":[{"role":"user","content":"hi"}]}`), nil)

	require.Error(t, err)
	require.NotNil(t, result)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "invalid request bridge stream errors must not switch accounts")
	require.Empty(t, repo.rateLimitCalls)
	require.Empty(t, repo.tempCalls)
	require.Contains(t, rec.Body.String(), "event: error")
	require.Contains(t, rec.Body.String(), "invalid_request_error")
}

func TestGatewayService_ForwardAsResponses_AnthropicTransportErrorFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	upstream := &anthropicHTTPUpstreamRecorder{
		err: errors.New("dial tcp: connect: connection refused"),
	}
	svc := &GatewayService{
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{Enabled: false},
			},
		},
		httpUpstream: upstream,
	}

	result, err := svc.ForwardAsResponses(context.Background(), c, newAnthropicAPIKeyAccountForTest(), []byte(`{"model":"claude-3-5-sonnet-latest","input":"hi"}`), nil)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, 0, rec.Body.Len())

	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := raw.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "request_error", events[0].Kind)
	require.Equal(t, "https://api.anthropic.com/v1/messages", events[0].UpstreamURL)
}
