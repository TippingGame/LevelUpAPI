package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestParseGrokMediaVideoBillingMetadata(t *testing.T) {
	info := ParseGrokMediaRequest("application/json", []byte(`{
		"model":"grok-imagine-video-1.5",
		"prompt":"animate",
		"resolution":"720p",
		"duration":10,
		"image":{"url":"https://example.com/source.png"},
		"reference_images":[{"image_url":"https://example.com/reference.png"}]
	}`))

	require.Equal(t, "grok-imagine-video-1.5", info.Model)
	require.Equal(t, VideoBillingResolution720P, info.Resolution)
	require.Equal(t, 10, info.DurationSeconds)
	require.Equal(t, []string{"https://example.com/source.png", "https://example.com/reference.png"}, info.InputImageURLs)
	require.True(t, info.HasInputImage())
}

func TestNormalizeGrokMediaForwardBodyUsesOfficialURLField(t *testing.T) {
	body := []byte(`{"model":"grok-imagine-video-1.5","image":{"image_url":"https://example.com/source.png"}}`)

	out, contentType, err := normalizeGrokMediaForwardBody(GrokMediaEndpointVideosGenerations, body, "application/json")

	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	require.Equal(t, "grok-imagine-video-1.5", gjson.GetBytes(out, "model").String())
	require.Equal(t, "https://example.com/source.png", gjson.GetBytes(out, "image.url").String())
	require.False(t, gjson.GetBytes(out, "image.image_url").Exists())
}

func TestSanitizeGrokImageBodyRemovesUnsupportedSize(t *testing.T) {
	body := []byte(`{"model":"grok-imagine-image","prompt":"cat","size":"1024x1024"}`)

	out, _, err := sanitizeGrokMediaForwardBody(GrokMediaEndpointImagesGenerations, body, "application/json")

	require.NoError(t, err)
	require.False(t, gjson.GetBytes(out, "size").Exists())
	require.Equal(t, "cat", gjson.GetBytes(out, "prompt").String())
}

func TestCalculateGrokVideoCostBillsPerSecond(t *testing.T) {
	svc := &BillingService{}
	custom720P := 0.10

	cost := svc.CalculateVideoCost(
		"grok-imagine-video",
		"720p",
		2,
		5,
		&VideoPriceConfig{Price720P: &custom720P},
		1.5,
	)

	require.InDelta(t, 1.0, cost.TotalCost, 1e-12)
	require.InDelta(t, 1.5, cost.ActualCost, 1e-12)
	require.Equal(t, string(BillingModeVideo), cost.BillingMode)
}

func TestCalculateGrokVideoCostUsesEightSecondDefault(t *testing.T) {
	svc := &BillingService{}
	cost := svc.CalculateVideoCost("grok-imagine-video", "480p", 1, 0, nil, 1)

	require.InDelta(t, 0.40, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.40, cost.ActualCost, 1e-12)
}

func TestCalculateGrokVideoCostClampsDurationToProviderLimit(t *testing.T) {
	svc := &BillingService{}
	cost := svc.CalculateVideoCost("grok-imagine-video", "720p", 1, 999, nil, 1)

	require.InDelta(t, 0.07*15, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.07*15, cost.ActualCost, 1e-12)
}

func TestCalculateGrokImagineImageCostUsesDefaultRateCard(t *testing.T) {
	svc := &BillingService{}

	standard1K := svc.CalculateImageCost("grok-imagine-image", "1K", 1, nil, 1)
	standard2K := svc.CalculateImageCost("grok-imagine-image", "2K", 1, nil, 1)
	quality1K := svc.CalculateImageCost("grok-imagine-image-quality", "1K", 1, nil, 1)
	quality2K := svc.CalculateImageCost("grok-imagine-image-quality", "2K", 1, nil, 1)

	require.InDelta(t, 0.02, standard1K.TotalCost, 1e-12)
	require.InDelta(t, 0.02, standard2K.TotalCost, 1e-12)
	require.InDelta(t, 0.05, quality1K.TotalCost, 1e-12)
	require.InDelta(t, 0.07, quality2K.TotalCost, 1e-12)
}

func TestCalculateGrokImagineVideoCostUsesDefaultRateCard(t *testing.T) {
	svc := &BillingService{}

	standard480P := svc.CalculateVideoCost("grok-imagine-video", "480p", 1, 1, nil, 1)
	standard720P := svc.CalculateVideoCost("grok-imagine-video", "720p", 1, 1, nil, 1)
	video15_480P := svc.CalculateVideoCost("grok-imagine-video-1.5", "480p", 1, 1, nil, 1)
	video15_720P := svc.CalculateVideoCost("grok-imagine-video-1.5", "720p", 1, 1, nil, 1)
	video15_1080P := svc.CalculateVideoCost("grok-imagine-video-1.5", "1080p", 1, 1, nil, 1)

	require.InDelta(t, 0.05, standard480P.TotalCost, 1e-12)
	require.InDelta(t, 0.07, standard720P.TotalCost, 1e-12)
	require.InDelta(t, 0.08, video15_480P.TotalCost, 1e-12)
	require.InDelta(t, 0.14, video15_720P.TotalCost, 1e-12)
	require.InDelta(t, 0.25, video15_1080P.TotalCost, 1e-12)
}

func TestForwardGrokVideoReturnsBillingMetadata(t *testing.T) {
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"grok-imagine-video-1.5","prompt":"waves","resolution":"720p","duration":10}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/generations", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	account := &Account{
		ID: 63, Name: "grok", Platform: PlatformGrok, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "api-key", "base_url": "https://xai.test/v1"},
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"request_id":"video-request-123"}`)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideosGenerations, "", body, "application/json")

	require.NoError(t, err)
	require.Equal(t, "https://xai.test/v1/videos/generations", upstream.lastReq.URL.String())
	require.JSONEq(t, `{"model":"grok-imagine-video","prompt":"waves","resolution":"720p","duration":10}`, string(upstream.lastBody))
	require.Equal(t, "video-request-123", result.ResponseID)
	require.Equal(t, 1, result.VideoCount)
	require.Equal(t, VideoBillingResolution720P, result.VideoResolution)
	require.Equal(t, 10, result.VideoDurationSeconds)
	require.Equal(t, 1, result.ImageCount)
	require.Empty(t, result.ImageSize)
}
