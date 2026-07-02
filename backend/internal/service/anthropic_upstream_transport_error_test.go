package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type anthropicTransportAccountRepoStub struct {
	AccountRepository

	tempCalls      int
	lastAccountID  int64
	lastTempUntil  time.Time
	lastTempReason string
}

func (r *anthropicTransportAccountRepoStub) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	r.lastAccountID = id
	r.lastTempUntil = until
	r.lastTempReason = reason
	return nil
}

func TestClassifyAnthropicTransportError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "proxy authentication required", err: errors.New("proxy authentication required"), want: true},
		{name: "connection refused", err: errors.New("dial tcp: connect: connection refused"), want: true},
		{name: "dns not found", err: errors.New("lookup proxy.local: no such host"), want: true},
		{name: "timeout remains transient", err: errors.New("i/o timeout"), want: false},
		{name: "deadline remains transient", err: context.DeadlineExceeded, want: false},
		{name: "canceled remains transient", err: context.Canceled, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyAnthropicTransportError(tt.err).Persistent)
		})
	}
}

func TestMaybeTempUnscheduleAnthropicTransportError(t *testing.T) {
	repo := &anthropicTransportAccountRepoStub{}
	tempCache := &runtimeTempUnschedCacheStub{}
	svc := &GatewayService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: tempCache},
	}

	account := &Account{
		ID:       42,
		Name:     "claude-oauth",
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
	}

	svc.maybeTempUnscheduleAnthropicTransportError(context.Background(), account, errors.New("proxy authentication required"), "proxy authentication required")

	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, int64(42), repo.lastAccountID)
	require.Contains(t, repo.lastTempReason, "proxy/network")
	require.Contains(t, repo.lastTempReason, "proxy authentication required")
	require.WithinDuration(t, time.Now().Add(anthropicTransportErrorTempUnschedDuration), repo.lastTempUntil, 2*time.Second)
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Equal(t, repo.lastTempReason, account.TempUnschedulableReason)
	require.NotNil(t, tempCache.states[42])
	require.Equal(t, "anthropic_transport_error", tempCache.states[42].MatchedKeyword)
}

func TestMaybeTempUnscheduleAnthropicTransportErrorSkipsNonPersistentAndAPIKey(t *testing.T) {
	repo := &anthropicTransportAccountRepoStub{}
	svc := &GatewayService{accountRepo: repo}

	svc.maybeTempUnscheduleAnthropicTransportError(context.Background(), &Account{
		ID:       43,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
	}, errors.New("i/o timeout"), "i/o timeout")

	svc.maybeTempUnscheduleAnthropicTransportError(context.Background(), &Account{
		ID:       44,
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
	}, errors.New("proxy authentication required"), "proxy authentication required")

	require.Zero(t, repo.tempCalls)
}

func TestHandleAnthropicUpstreamTransportError_PersistentEvictsAndFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	repo := &anthropicTransportAccountRepoStub{}
	svc := &GatewayService{accountRepo: repo}
	account := &Account{
		ID:       42,
		Name:     "claude-oauth",
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
	}

	err := svc.handleAnthropicUpstreamTransportError(context.Background(), c, account, errors.New("proxy authentication required"), "https://api.anthropic.com/v1/messages", true)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), "Upstream request failed")
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, int64(42), repo.lastAccountID)
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Equal(t, 0, rec.Body.Len())

	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := raw.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, PlatformAnthropic, events[0].Platform)
	require.Equal(t, int64(42), events[0].AccountID)
	require.Equal(t, "https://api.anthropic.com/v1/messages", events[0].UpstreamURL)
	require.True(t, events[0].Passthrough)
	require.Equal(t, "request_error", events[0].Kind)
}

func TestHandleAnthropicUpstreamTransportError_TransientFailsOverWithoutEviction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	repo := &anthropicTransportAccountRepoStub{}
	svc := &GatewayService{accountRepo: repo}
	account := &Account{
		ID:       43,
		Name:     "claude-oauth",
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
	}

	err := svc.handleAnthropicUpstreamTransportError(context.Background(), c, account, errors.New("i/o timeout"), "https://api.anthropic.com/v1/messages", false)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Zero(t, repo.tempCalls)
	require.Nil(t, account.TempUnschedulableUntil)
	require.Equal(t, 0, rec.Body.Len())
}

func TestHandleAnthropicUpstreamTransportError_ContextCanceledNoFailoverNoEviction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	repo := &anthropicTransportAccountRepoStub{}
	svc := &GatewayService{accountRepo: repo}
	account := &Account{
		ID:       44,
		Name:     "claude-oauth",
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
	}

	err := svc.handleAnthropicUpstreamTransportError(context.Background(), c, account, context.Canceled, "https://api.anthropic.com/v1/messages", false)

	require.ErrorIs(t, err, context.Canceled)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.Zero(t, repo.tempCalls)
	require.Nil(t, account.TempUnschedulableUntil)
	require.Equal(t, 0, rec.Body.Len())
}
