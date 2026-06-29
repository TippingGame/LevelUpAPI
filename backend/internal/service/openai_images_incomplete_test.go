//go:build unit

package service

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExtractImagesUpstreamError_IncompleteIsRetryable(t *testing.T) {
	body := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n" +
		"data: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_1\",\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"}}}\n\n"

	got := extractOpenAIImagesUpstreamError([]byte(body))
	require.NotNil(t, got)
	require.Equal(t, http.StatusBadGateway, got.StatusCode)
	require.Equal(t, "response_incomplete", got.Code)
	require.Contains(t, got.Message, "max_output_tokens")
}

func TestExtractImagesUpstreamError_IncompleteContentFilterNotRetryable(t *testing.T) {
	body := "data: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"r\",\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"content_filter\"}}}\n\n"

	got := extractOpenAIImagesUpstreamError([]byte(body))
	require.NotNil(t, got)
	require.Equal(t, http.StatusBadRequest, got.StatusCode)
}

func TestSummarizeOpenAIImagesNoOutputBody(t *testing.T) {
	body := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}\n\n" +
		"data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"}}}\n\n"

	summary := summarizeOpenAIImagesNoOutputBody([]byte(body))
	require.Contains(t, summary, "no_image_output")
	require.Contains(t, summary, "last_event=response.incomplete")
	require.Contains(t, summary, "status=incomplete")
	require.Contains(t, summary, "incomplete_reason=max_output_tokens")
}

func TestImagesOAuthNonStreaming_CompletedNoImageTriggersSameAccountRetry(t *testing.T) {
	upstreamSSE := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_x\",\"status\":\"in_progress\",\"model\":\"gpt-5.4-mini-2026-03-17\",\"output\":[]}}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_x\",\"status\":\"completed\",\"model\":\"gpt-5.4-mini-2026-03-17\",\"output\":[],\"tool_usage\":{\"image_gen\":{\"output_tokens\":0}}}}\n\n"

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}

	svc := &OpenAIGatewayService{}
	_, _, err := svc.handleOpenAIImagesOAuthNonStreamingResponse(resp, c, "b64_json", "gpt-image-2")

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "expected *UpstreamFailoverError, got %T: %v", err, err)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.True(t, failoverErr.RetryableOnSameAccount)
}

func TestImagesOAuthNonStreaming_ContentRefusalReturns400NoRetry(t *testing.T) {
	upstreamSSE := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\",\"status\":\"in_progress\",\"model\":\"gpt-5.4-mini\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"抱歉，这个请求因涉及违规内容被安全系统判定为不适合生成。\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"model\":\"gpt-5.4-mini\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"抱歉，这个请求因涉及违规内容被安全系统判定为不适合生成。\"}]}],\"tool_usage\":{\"image_gen\":{\"output_tokens\":0}}}}\n\n"

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(upstreamSSE))}
	svc := &OpenAIGatewayService{}
	_, _, err := svc.handleOpenAIImagesOAuthNonStreamingResponse(resp, c, "b64_json", "gpt-image-2")

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "content refusal must not trigger retryable failover")
	var imgErr *OpenAIImagesUpstreamError
	require.True(t, errors.As(err, &imgErr), "expected *OpenAIImagesUpstreamError, got %T: %v", err, err)
	require.Equal(t, http.StatusBadRequest, imgErr.StatusCode)
	require.Equal(t, "content_policy_violation", imgErr.Code)
	require.Contains(t, imgErr.Message, "安全系统")
}

func TestExtractOpenAIImagesModelRefusal_EmptyWhenNoText(t *testing.T) {
	body := []byte("data: {\"type\":\"response.completed\",\"response\":{\"output\":[],\"tool_usage\":{\"image_gen\":{\"output_tokens\":0}}}}\n\n")
	require.Empty(t, extractOpenAIImagesModelRefusal(body))
}
