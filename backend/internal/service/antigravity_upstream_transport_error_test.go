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

type antigravityTransportAccountRepoStub struct {
	AccountRepository

	tempCalls      int
	lastAccountID  int64
	lastTempUntil  time.Time
	lastTempReason string
	tempErr        error
}

func (r *antigravityTransportAccountRepoStub) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	r.lastAccountID = id
	r.lastTempUntil = until
	r.lastTempReason = reason
	return r.tempErr
}

func TestClassifyAntigravityTransportError(t *testing.T) {
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
			require.Equal(t, tt.want, classifyAntigravityTransportError(tt.err).Persistent)
		})
	}
}

func TestHandleAntigravityUpstreamTransportError_PersistentEvictsAndFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &antigravityTransportAccountRepoStub{}
	tempCache := &runtimeTempUnschedCacheStub{}
	svc := &AntigravityGatewayService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: tempCache},
	}
	account := &Account{ID: 42, Name: "ag-proxy", Platform: PlatformAntigravity, Type: AccountTypeOAuth}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/antigravity/v1/messages", nil)

	before := time.Now()
	err := svc.handleAntigravityUpstreamTransportError(context.Background(), c, account, errors.New("proxy authentication required"), "https://antigravity.example/v1internal:streamGenerateContent")

	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, int64(42), repo.lastAccountID)
	require.Contains(t, repo.lastTempReason, "proxy/network")
	require.Contains(t, repo.lastTempReason, "proxy authentication required")
	require.True(t, repo.lastTempUntil.After(before.Add(antigravityTransportErrorTempUnschedDuration-time.Second)))
	require.True(t, repo.lastTempUntil.Before(time.Now().Add(antigravityTransportErrorTempUnschedDuration+time.Second)))
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Equal(t, repo.lastTempReason, account.TempUnschedulableReason)
	require.NotNil(t, tempCache.states[42])
	require.Equal(t, "antigravity_transport_error", tempCache.states[42].MatchedKeyword)
	require.Equal(t, 0, rec.Body.Len())
}

func TestTempUnscheduleAntigravityTransportError_PersistFailureUsesRuntimeFallback(t *testing.T) {
	repo := &antigravityTransportAccountRepoStub{tempErr: errors.New("db unavailable")}
	tempCache := &runtimeTempUnschedCacheStub{}
	svc := &AntigravityGatewayService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: tempCache},
	}
	account := &Account{ID: 47, Name: "ag-db-fail", Platform: PlatformAntigravity, Type: AccountTypeOAuth}

	svc.tempUnscheduleAntigravityTransportError(context.Background(), account, "proxy authentication required")

	require.Equal(t, 1, repo.tempCalls)
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Contains(t, account.TempUnschedulableReason, "proxy authentication required")
	require.NotNil(t, tempCache.states[47])
	require.Equal(t, "antigravity_transport_error", tempCache.states[47].MatchedKeyword)
}

func TestAntigravityRetryErrorToFailover(t *testing.T) {
	switchErr := &AntigravityAccountSwitchError{OriginalAccountID: 7, RateLimitedModel: "gemini-3.1-pro", IsStickySession: true}
	failoverErr, ok := antigravityRetryErrorToFailover(switchErr)
	require.True(t, ok)
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.StatusCode)
	require.True(t, failoverErr.ForceCacheBilling)

	original := &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: []byte(`{"error":"boom"}`)}
	failoverErr, ok = antigravityRetryErrorToFailover(original)
	require.True(t, ok)
	require.Same(t, original, failoverErr)

	failoverErr, ok = antigravityRetryErrorToFailover(errors.New("plain error"))
	require.False(t, ok)
	require.Nil(t, failoverErr)
}

func TestAntigravityRetryLoopTransportErrorReturnsFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &antigravityTransportAccountRepoStub{}
	upstream := &queuedHTTPUpstreamStub{
		errors: []error{
			errors.New("proxy authentication required"),
			errors.New("proxy authentication required"),
			errors.New("proxy authentication required"),
		},
	}
	svc := &AntigravityGatewayService{
		accountRepo:   repo,
		httpUpstream:  upstream,
		tokenProvider: &AntigravityTokenProvider{},
	}
	account := &Account{
		ID:          88,
		Name:        "ag-loop-proxy",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "token",
		},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/antigravity/v1beta/models/gemini-2.5-flash:generateContent", nil)

	result, err := svc.antigravityRetryLoop(antigravityRetryLoopParams{
		ctx:            context.Background(),
		prefix:         "[test-antigravity-transport]",
		account:        account,
		accessToken:    "token",
		action:         "streamGenerateContent",
		body:           []byte(`{"request":{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}}`),
		c:              c,
		httpUpstream:   upstream,
		accountRepo:    repo,
		requestedModel: "gemini-2.5-flash",
		handleError:    testConnectionHandleError,
	})

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, antigravityMaxRetries, upstream.callCount)
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, int64(88), repo.lastAccountID)
	require.Equal(t, 0, rec.Body.Len(), "retry loop must not write a hard response before handler can fail over")
}
