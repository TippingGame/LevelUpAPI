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

type countTokensRouteHTTPUpstream struct {
	accountIDs []int64
}

func (u *countTokensRouteHTTPUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.accountIDs = append(u.accountIDs, accountID)
	if req != nil && req.Body != nil {
		_, _ = io.ReadAll(req.Body)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"input_tokens":7}`))),
	}, nil
}

func (u *countTokensRouteHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func newCountTokensRouteTestHandler(t *testing.T, groupRepo *fakeGroupRepo, schedulerCache *fakeSchedulerCache, upstream *countTokensRouteHTTPUpstream) (*GatewayHandler, func()) {
	t.Helper()

	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Security.URLAllowlist.Enabled = false
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
		cfg,
		schedulerSnapshot,
		nil,
		nil,
		nil,
		nil,
		nil,
		upstream,
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
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg)

	h := &GatewayHandler{
		gatewayService:      gatewaySvc,
		billingCacheService: billingCacheSvc,
	}
	return h, func() { billingCacheSvc.Stop() }
}

func TestGatewayHandlerCountTokens_RoutesAcrossAPIKeyGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetAPIKeyGroupRouteBreakerForTest(t)

	firstGroupID := int64(2401)
	secondGroupID := int64(2402)
	accountID := int64(1402)
	firstGroup := &service.Group{ID: firstGroupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive}
	secondGroup := &service.Group{ID: secondGroupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive}
	account := &service.Account{
		ID:            accountID,
		Name:          "anthropic-count-tokens",
		Platform:      service.PlatformAnthropic,
		Type:          service.AccountTypeAPIKey,
		Credentials:   map[string]any{"api_key": "sk-ant-test"},
		Concurrency:   1,
		Priority:      1,
		Status:        service.StatusActive,
		Schedulable:   true,
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: secondGroupID}},
	}
	upstream := &countTokensRouteHTTPUpstream{}
	h, cleanup := newCountTokensRouteTestHandler(t,
		&fakeGroupRepo{groups: map[int64]*service.Group{firstGroupID: firstGroup, secondGroupID: secondGroup}},
		&fakeSchedulerCache{snapshots: map[service.SchedulerBucket][]*service.Account{
			{GroupID: firstGroupID, Platform: service.PlatformAnthropic, Mode: service.SchedulerModeMixed}:  nil,
			{GroupID: secondGroupID, Platform: service.PlatformAnthropic, Mode: service.SchedulerModeMixed}: {account},
		}},
		upstream,
	)
	defer cleanup()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, firstGroup))
	c.Request = req
	apiKey := &service.APIKey{
		ID:      3402,
		UserID:  4402,
		GroupID: &firstGroupID,
		Status:  service.StatusActive,
		User: &service.User{
			ID:          4402,
			Concurrency: 10,
			Balance:     100,
		},
		Group: firstGroup,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: firstGroupID, Priority: 100, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: firstGroup},
			{GroupID: secondGroupID, Priority: 200, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: secondGroup},
		},
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.CountTokens(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{accountID}, upstream.accountIDs)
	require.JSONEq(t, `{"input_tokens":7}`, rec.Body.String())
	selected, ok := c.Get(opsAccountIDKey)
	require.True(t, ok)
	require.Equal(t, accountID, selected)
}
