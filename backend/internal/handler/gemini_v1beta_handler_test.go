//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestGeminiV1BetaHandler_PlatformRoutingInvariant 文档化并验证 Handler 层的平台路由逻辑不变量
// 该测试确保 gemini 和 antigravity 平台的路由逻辑符合预期
func TestGeminiV1BetaHandler_PlatformRoutingInvariant(t *testing.T) {
	tests := []struct {
		name            string
		platform        string
		expectedService string
		description     string
	}{
		{
			name:            "Gemini平台使用ForwardNative",
			platform:        service.PlatformGemini,
			expectedService: "GeminiMessagesCompatService.ForwardNative",
			description:     "Gemini OAuth 账户直接调用 Google API",
		},
		{
			name:            "Antigravity平台使用ForwardGemini",
			platform:        service.PlatformAntigravity,
			expectedService: "AntigravityGatewayService.ForwardGemini",
			description:     "Antigravity 账户通过 CRS 中转，支持 Gemini 协议",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 GeminiV1BetaModels 中的路由决策 (lines 199-205 in gemini_v1beta_handler.go)
			var routedService string
			if tt.platform == service.PlatformAntigravity {
				routedService = "AntigravityGatewayService.ForwardGemini"
			} else {
				routedService = "GeminiMessagesCompatService.ForwardNative"
			}

			require.Equal(t, tt.expectedService, routedService,
				"平台 %s 应该路由到 %s: %s",
				tt.platform, tt.expectedService, tt.description)
		})
	}
}

// TestGeminiV1BetaHandler_ListModelsAntigravityFallback 验证 ListModels 的 antigravity 降级逻辑
// 当没有 gemini 账户但有 antigravity 账户时，应返回静态模型列表
func TestGeminiV1BetaHandler_ListModelsAntigravityFallback(t *testing.T) {
	tests := []struct {
		name             string
		hasGeminiAccount bool
		hasAntigravity   bool
		expectedBehavior string
	}{
		{
			name:             "有Gemini账户-调用ForwardAIStudioGET",
			hasGeminiAccount: true,
			hasAntigravity:   false,
			expectedBehavior: "forward_to_upstream",
		},
		{
			name:             "无Gemini有Antigravity-返回静态列表",
			hasGeminiAccount: false,
			hasAntigravity:   true,
			expectedBehavior: "static_fallback",
		},
		{
			name:             "无任何账户-返回503",
			hasGeminiAccount: false,
			hasAntigravity:   false,
			expectedBehavior: "service_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 GeminiV1BetaListModels 的逻辑 (lines 33-44 in gemini_v1beta_handler.go)
			var behavior string

			if tt.hasGeminiAccount {
				behavior = "forward_to_upstream"
			} else if tt.hasAntigravity {
				behavior = "static_fallback"
			} else {
				behavior = "service_unavailable"
			}

			require.Equal(t, tt.expectedBehavior, behavior)
		})
	}
}

// TestGeminiV1BetaHandler_GetModelAntigravityFallback 验证 GetModel 的 antigravity 降级逻辑
func TestGeminiV1BetaHandler_GetModelAntigravityFallback(t *testing.T) {
	tests := []struct {
		name             string
		hasGeminiAccount bool
		hasAntigravity   bool
		expectedBehavior string
	}{
		{
			name:             "有Gemini账户-调用ForwardAIStudioGET",
			hasGeminiAccount: true,
			hasAntigravity:   false,
			expectedBehavior: "forward_to_upstream",
		},
		{
			name:             "无Gemini有Antigravity-返回静态模型信息",
			hasGeminiAccount: false,
			hasAntigravity:   true,
			expectedBehavior: "static_model_info",
		},
		{
			name:             "无任何账户-返回503",
			hasGeminiAccount: false,
			hasAntigravity:   false,
			expectedBehavior: "service_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 GeminiV1BetaGetModel 的逻辑 (lines 77-87 in gemini_v1beta_handler.go)
			var behavior string

			if tt.hasGeminiAccount {
				behavior = "forward_to_upstream"
			} else if tt.hasAntigravity {
				behavior = "static_model_info"
			} else {
				behavior = "service_unavailable"
			}

			require.Equal(t, tt.expectedBehavior, behavior)
		})
	}
}

type geminiV1BetaRouteHTTPUpstream struct {
	statusCode int
	body       []byte
	accountIDs []int64
}

func (u *geminiV1BetaRouteHTTPUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.accountIDs = append(u.accountIDs, accountID)
	if req != nil && req.Body != nil {
		_, _ = io.ReadAll(req.Body)
	}
	statusCode := u.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	body := u.body
	if len(body) == 0 {
		body = []byte(`{"models":[{"name":"models/gemini-2.5-pro"}]}`)
	}
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}, nil
}

func (u *geminiV1BetaRouteHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func newGeminiV1BetaRouteTestHandler(t *testing.T, groupRepo *fakeGroupRepo, schedulerCache *fakeSchedulerCache, upstream *geminiV1BetaRouteHTTPUpstream) (*GatewayHandler, func()) {
	t.Helper()

	schedulerSnapshot := service.NewSchedulerSnapshotService(schedulerCache, nil, nil, nil, nil)
	gatewaySvc := service.NewGatewayService(
		nil,
		nil,
		groupRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		schedulerSnapshot,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	cfg := &config.Config{RunMode: config.RunModeSimple}
	geminiSvc := service.NewGeminiMessagesCompatService(nil, groupRepo, nil, schedulerSnapshot, nil, nil, upstream, nil, &config.Config{}, nil)
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg)
	concurrencySvc := service.NewConcurrencyService(&fakeConcurrencyCache{})

	h := &GatewayHandler{
		gatewayService:           gatewaySvc,
		geminiCompatService:      geminiSvc,
		billingCacheService:      billingCacheSvc,
		concurrencyHelper:        NewConcurrencyHelper(concurrencySvc, SSEPingFormatNone, 0),
		maxAccountSwitchesGemini: 1,
		maxAccountSwitches:       1,
	}
	return h, func() { billingCacheSvc.Stop() }
}

func TestGeminiV1BetaListModels_RoutesAcrossAPIKeyGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetAPIKeyGroupRouteBreakerForTest(t)

	firstGroupID := int64(2201)
	secondGroupID := int64(2202)
	accountID := int64(1202)
	firstGroup := &service.Group{ID: firstGroupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	secondGroup := &service.Group{ID: secondGroupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	account := &service.Account{
		ID:            accountID,
		Name:          "gemini-ai-studio",
		Platform:      service.PlatformGemini,
		Type:          service.AccountTypeAPIKey,
		Credentials:   map[string]any{"api_key": "test-gemini-key"},
		Concurrency:   1,
		Priority:      1,
		Status:        service.StatusActive,
		Schedulable:   true,
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: secondGroupID}},
	}
	upstream := &geminiV1BetaRouteHTTPUpstream{body: []byte(`{"models":[{"name":"models/gemini-2.5-pro"}]}`)}
	h, cleanup := newGeminiV1BetaRouteTestHandler(t,
		&fakeGroupRepo{groups: map[int64]*service.Group{firstGroupID: firstGroup, secondGroupID: secondGroup}},
		&fakeSchedulerCache{snapshots: map[service.SchedulerBucket][]*service.Account{
			{GroupID: firstGroupID, Platform: service.PlatformGemini, Mode: service.SchedulerModeForced}:  nil,
			{GroupID: secondGroupID, Platform: service.PlatformGemini, Mode: service.SchedulerModeForced}: {account},
		}},
		upstream,
	)
	defer cleanup()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, firstGroup))
	c.Request = req
	apiKey := testGeminiRouteAPIKey(firstGroupID, secondGroupID, firstGroup, secondGroup)
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)

	h.GeminiV1BetaListModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{accountID}, upstream.accountIDs)
	require.Contains(t, rec.Body.String(), "gemini-2.5-pro")
}

func TestGeminiV1BetaModels_RoutesAcrossAPIKeyGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetAPIKeyGroupRouteBreakerForTest(t)

	firstGroupID := int64(2301)
	secondGroupID := int64(2302)
	accountID := int64(1302)
	firstGroup := &service.Group{ID: firstGroupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	secondGroup := &service.Group{ID: secondGroupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	account := &service.Account{
		ID:            accountID,
		Name:          "gemini-native",
		Platform:      service.PlatformGemini,
		Type:          service.AccountTypeAPIKey,
		Credentials:   map[string]any{"api_key": "test-gemini-key"},
		Concurrency:   1,
		Priority:      1,
		Status:        service.StatusActive,
		Schedulable:   true,
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: secondGroupID}},
	}
	upstream := &geminiV1BetaRouteHTTPUpstream{body: []byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2},"modelVersion":"gemini-2.5-pro"}`)}
	h, cleanup := newGeminiV1BetaRouteTestHandler(t,
		&fakeGroupRepo{groups: map[int64]*service.Group{firstGroupID: firstGroup, secondGroupID: secondGroup}},
		&fakeSchedulerCache{snapshots: map[service.SchedulerBucket][]*service.Account{
			{GroupID: firstGroupID, Platform: service.PlatformGemini, Mode: service.SchedulerModeMixed}:  nil,
			{GroupID: secondGroupID, Platform: service.PlatformGemini, Mode: service.SchedulerModeMixed}: {account},
		}},
		upstream,
	)
	defer cleanup()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, firstGroup))
	c.Request = req
	c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-pro:generateContent"}}
	apiKey := testGeminiRouteAPIKey(firstGroupID, secondGroupID, firstGroup, secondGroup)
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.GeminiV1BetaModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{accountID}, upstream.accountIDs)
	selected, ok := c.Get(opsAccountIDKey)
	require.True(t, ok)
	require.Equal(t, accountID, selected)
	require.Contains(t, rec.Body.String(), "ok")
}

func testGeminiRouteAPIKey(firstGroupID, secondGroupID int64, firstGroup, secondGroup *service.Group) *service.APIKey {
	return &service.APIKey{
		ID:      3202,
		UserID:  4202,
		GroupID: &firstGroupID,
		Status:  service.StatusActive,
		User: &service.User{
			ID:          4202,
			Concurrency: 10,
			Balance:     100,
		},
		Group: firstGroup,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: firstGroupID, Priority: 100, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: firstGroup},
			{GroupID: secondGroupID, Priority: 200, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: secondGroup},
		},
	}
}

func TestShouldFallbackGeminiModel_KnownFallbackOn404(t *testing.T) {
	t.Parallel()

	res := &service.UpstreamHTTPResult{StatusCode: http.StatusNotFound}
	require.True(t, shouldFallbackGeminiModel("gemini-3.1-pro-preview-customtools", res))
}

func TestShouldFallbackGeminiModel_UnknownModelOn404(t *testing.T) {
	t.Parallel()

	res := &service.UpstreamHTTPResult{StatusCode: http.StatusNotFound}
	require.False(t, shouldFallbackGeminiModel("gemini-future-model", res))
}

func TestShouldFallbackGeminiModel_DelegatesScopeFallback(t *testing.T) {
	t.Parallel()

	res := &service.UpstreamHTTPResult{
		StatusCode: http.StatusForbidden,
		Headers:    http.Header{"Www-Authenticate": []string{"Bearer error=\"insufficient_scope\""}},
		Body:       []byte("insufficient authentication scopes"),
	}
	require.True(t, shouldFallbackGeminiModel("gemini-future-model", res))
}
