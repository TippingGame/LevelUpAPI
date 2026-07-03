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

type geminiTransportAccountRepoStub struct {
	AccountRepository

	tempCalls      int
	lastAccountID  int64
	lastTempUntil  time.Time
	lastTempReason string
	tempErr        error
}

func (r *geminiTransportAccountRepoStub) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	r.lastAccountID = id
	r.lastTempUntil = until
	r.lastTempReason = reason
	return r.tempErr
}

func TestClassifyGeminiTransportError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "proxy authentication required", err: errors.New("proxy authentication required"), want: true},
		{name: "connection refused", err: errors.New("dial tcp: connect: connection refused"), want: true},
		{name: "dns not found", err: errors.New("lookup proxy.local: no such host"), want: true},
		{name: "bad tls root", err: errors.New("x509: certificate signed by unknown authority"), want: true},
		{name: "plain eof remains transient", err: errors.New("EOF"), want: false},
		{name: "deadline remains transient", err: context.DeadlineExceeded, want: false},
		{name: "canceled remains transient", err: context.Canceled, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyGeminiTransportError(tt.err).Persistent)
		})
	}
}

func TestHandleGeminiUpstreamTransportError_PersistentEvictsAndFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &geminiTransportAccountRepoStub{}
	tempCache := &runtimeTempUnschedCacheStub{}
	svc := &GeminiMessagesCompatService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: tempCache},
	}
	account := &Account{ID: 42, Name: "gemini-proxy", Platform: PlatformGemini, Type: AccountTypeOAuth}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)

	before := time.Now()
	err := svc.handleGeminiUpstreamTransportError(context.Background(), c, account, errors.New("proxy authentication required"))

	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, int64(42), repo.lastAccountID)
	require.Contains(t, repo.lastTempReason, "proxy/network")
	require.Contains(t, repo.lastTempReason, "proxy authentication required")
	require.True(t, repo.lastTempUntil.After(before.Add(geminiTransportErrorTempUnschedDuration-time.Second)))
	require.True(t, repo.lastTempUntil.Before(time.Now().Add(geminiTransportErrorTempUnschedDuration+time.Second)))
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Equal(t, repo.lastTempReason, account.TempUnschedulableReason)
	require.NotNil(t, tempCache.states[42])
	require.Equal(t, "gemini_transport_error", tempCache.states[42].MatchedKeyword)
	require.Equal(t, 0, rec.Body.Len())
}

func TestTempUnscheduleGeminiTransportError_PersistFailureUsesRuntimeFallback(t *testing.T) {
	repo := &geminiTransportAccountRepoStub{tempErr: errors.New("db unavailable")}
	tempCache := &runtimeTempUnschedCacheStub{}
	svc := &GeminiMessagesCompatService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: tempCache},
	}
	account := &Account{ID: 46, Name: "gemini-db-fail", Platform: PlatformGemini, Type: AccountTypeOAuth}

	svc.tempUnscheduleGeminiTransportError(context.Background(), account, "proxy authentication required")

	require.Equal(t, 1, repo.tempCalls)
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Contains(t, account.TempUnschedulableReason, "proxy authentication required")
	require.NotNil(t, tempCache.states[46])
	require.Equal(t, "gemini_transport_error", tempCache.states[46].MatchedKeyword)
}

func TestTempUnscheduleGeminiTransportError_PoolModeDefaultSkipsLocalState(t *testing.T) {
	repo := &geminiTransportAccountRepoStub{}
	tempCache := &runtimeTempUnschedCacheStub{}
	svc := &GeminiMessagesCompatService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: tempCache},
	}
	account := &Account{
		ID:       47,
		Name:     "gemini-pool",
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"pool_mode": true,
		},
	}

	svc.tempUnscheduleGeminiTransportError(context.Background(), account, "proxy authentication required")

	require.Equal(t, 0, repo.tempCalls)
	require.Nil(t, account.TempUnschedulableUntil)
	require.Nil(t, tempCache.states[47])
}

func TestTempUnscheduleGeminiTransportError_PoolModeCustomPolicyStillWrites(t *testing.T) {
	repo := &geminiTransportAccountRepoStub{}
	tempCache := &runtimeTempUnschedCacheStub{}
	svc := &GeminiMessagesCompatService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: tempCache},
	}
	account := &Account{
		ID:       48,
		Name:     "gemini-pool-custom",
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"pool_mode":                  true,
			"custom_error_codes_enabled": true,
		},
	}

	svc.tempUnscheduleGeminiTransportError(context.Background(), account, "proxy authentication required")

	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, int64(48), repo.lastAccountID)
	require.NotNil(t, account.TempUnschedulableUntil)
	require.NotNil(t, tempCache.states[48])
	require.Equal(t, "gemini_transport_error", tempCache.states[48].MatchedKeyword)
}

func TestHandleGeminiUpstreamTransportError_TransientFailsOverWithoutEviction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &geminiTransportAccountRepoStub{}
	svc := &GeminiMessagesCompatService{accountRepo: repo}
	account := &Account{ID: 43, Name: "gemini-flaky", Platform: PlatformGemini, Type: AccountTypeAPIKey}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)

	err := svc.handleGeminiUpstreamTransportError(context.Background(), c, account, errors.New("EOF"))

	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Zero(t, repo.tempCalls)
	require.Equal(t, 0, rec.Body.Len())
}

func TestHandleGeminiUpstreamTransportError_ContextCanceledNoFailoverNoEviction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &geminiTransportAccountRepoStub{}
	svc := &GeminiMessagesCompatService{accountRepo: repo}
	account := &Account{ID: 44, Name: "gemini-canceled", Platform: PlatformGemini, Type: AccountTypeOAuth}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)

	err := svc.handleGeminiUpstreamTransportError(context.Background(), c, account, context.Canceled)

	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, repo.tempCalls)
	require.Equal(t, 0, rec.Body.Len())
}
