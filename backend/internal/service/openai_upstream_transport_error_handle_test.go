//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openaiTransportAccountRepoStub struct {
	AccountRepository
	accountsByID     map[int64]*Account
	tempUnschedCalls []tempUnschedCall
	tempErr          error
}

func (r *openaiTransportAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	if acc, ok := r.accountsByID[id]; ok {
		return acc, nil
	}
	return nil, fmt.Errorf("account %d not found", id)
}

func (r *openaiTransportAccountRepoStub) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.tempUnschedCalls = append(r.tempUnschedCalls, tempUnschedCall{accountID: id, until: until, reason: reason})
	return r.tempErr
}

func newOpenAITransportErrTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, rec
}

type failingOpenAIHTTPUpstream struct {
	err   error
	calls int
}

func (u *failingOpenAIHTTPUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	return nil, u.err
}

func (u *failingOpenAIHTTPUpstream) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.calls++
	return nil, u.err
}

func TestHandleOpenAIUpstreamTransportError_PersistentEvictsAndFailsOver(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	tempCache := &runtimeTempUnschedCacheStub{}
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: tempCache},
	}
	account := &Account{ID: 4627, Name: "proxy-expired", Platform: PlatformOpenAI}
	c, rec := newOpenAITransportErrTestContext()

	before := time.Now()
	err := svc.handleOpenAIUpstreamTransportError(context.Background(), c, account,
		errors.New(`Post "https://chatgpt.com/backend-api/codex/responses": socks connect tcp 1.2.3.4:1234->chatgpt.com:443: username/password authentication failed`), false)
	after := time.Now()

	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)

	require.Len(t, repo.tempUnschedCalls, 1)
	call := repo.tempUnschedCalls[0]
	require.Equal(t, int64(4627), call.accountID)
	require.Contains(t, call.reason, "authentication failed")
	require.True(t, call.until.After(before.Add(openAITransportErrorTempUnschedDuration-time.Second)))
	require.True(t, call.until.Before(after.Add(openAITransportErrorTempUnschedDuration+time.Second)))
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Equal(t, call.reason, account.TempUnschedulableReason)
	require.NotNil(t, tempCache.states[4627])
	require.Equal(t, "openai_transport_error", tempCache.states[4627].MatchedKeyword)
	require.Equal(t, 0, rec.Body.Len())
}

func TestTempUnscheduleOpenAITransportError_PersistFailureUsesRuntimeFallback(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{tempErr: errors.New("db unavailable")}
	tempCache := &runtimeTempUnschedCacheStub{}
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: tempCache},
	}
	account := &Account{ID: 4628, Name: "proxy-db-fail", Platform: PlatformOpenAI}

	svc.tempUnscheduleOpenAITransportError(context.Background(), account, "proxy authentication required")

	require.Len(t, repo.tempUnschedCalls, 1)
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Contains(t, account.TempUnschedulableReason, "proxy authentication required")
	require.NotNil(t, tempCache.states[4628])
	require.Equal(t, "openai_transport_error", tempCache.states[4628].MatchedKeyword)
}

func TestTempUnscheduleOpenAITransportError_PoolModeDefaultSkipsLocalState(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	tempCache := &runtimeTempUnschedCacheStub{}
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: tempCache},
	}
	account := &Account{
		ID:       4629,
		Name:     "openai-pool",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"pool_mode": true,
		},
	}

	svc.tempUnscheduleOpenAITransportError(context.Background(), account, "proxy authentication required")

	require.Empty(t, repo.tempUnschedCalls)
	require.Nil(t, account.TempUnschedulableUntil)
	require.Nil(t, tempCache.states[4629])
}

func TestTempUnscheduleOpenAITransportError_PoolModeCustomPolicyStillWrites(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	tempCache := &runtimeTempUnschedCacheStub{}
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: tempCache},
	}
	account := &Account{
		ID:       4630,
		Name:     "openai-pool-custom",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"pool_mode":                  true,
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(http.StatusBadGateway)},
		},
	}

	svc.tempUnscheduleOpenAITransportError(context.Background(), account, "proxy authentication required")

	require.Len(t, repo.tempUnschedCalls, 1)
	require.Equal(t, int64(4630), repo.tempUnschedCalls[0].accountID)
	require.NotNil(t, account.TempUnschedulableUntil)
	require.NotNil(t, tempCache.states[4630])
	require.Equal(t, "openai_transport_error", tempCache.states[4630].MatchedKeyword)
}

func TestHandleOpenAIUpstreamTransportError_TransientFailsOverWithoutEviction(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 99, Name: "flaky", Platform: PlatformOpenAI}
	c, rec := newOpenAITransportErrTestContext()

	err := svc.handleOpenAIUpstreamTransportError(context.Background(), c, account,
		errors.New(`Post "https://chatgpt.com/...": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`), false)

	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Empty(t, repo.tempUnschedCalls)
	require.Equal(t, 0, rec.Body.Len())
}

func TestHandleOpenAIUpstreamTransportError_ContextCanceledNoFailoverNoEviction(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 77, Name: "healthy", Platform: PlatformOpenAI}
	c, rec := newOpenAITransportErrTestContext()

	err := svc.handleOpenAIUpstreamTransportError(context.Background(), c, account, context.Canceled, false)

	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, repo.tempUnschedCalls)
	require.Equal(t, 0, rec.Body.Len())
}

func TestOpenAIGatewayServiceTempUnscheduleRetryableErrorSkipsPoolModeDefault(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{accountsByID: map[int64]*Account{
		88: {
			ID:       88,
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Credentials: map[string]any{
				"pool_mode": true,
			},
		},
	}}
	cache := &runtimeTempUnschedCacheStub{}
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: cache},
	}

	svc.TempUnscheduleRetryableError(context.Background(), 88, &UpstreamFailoverError{
		StatusCode:             http.StatusBadGateway,
		RetryableOnSameAccount: true,
	})

	require.Empty(t, repo.tempUnschedCalls)
	require.Nil(t, cache.states[88])
}

func TestOpenAIGatewayServiceTempUnscheduleRetryableErrorPoolModeCustomHitWrites(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{accountsByID: map[int64]*Account{
		89: {
			ID:       89,
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Credentials: map[string]any{
				"pool_mode":                  true,
				"custom_error_codes_enabled": true,
				"custom_error_codes":         []any{float64(http.StatusBadGateway)},
			},
		},
	}}
	cache := &runtimeTempUnschedCacheStub{}
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: cache},
	}

	svc.TempUnscheduleRetryableError(context.Background(), 89, &UpstreamFailoverError{
		StatusCode:             http.StatusBadGateway,
		RetryableOnSameAccount: true,
	})

	require.Len(t, repo.tempUnschedCalls, 1)
	require.Equal(t, int64(89), repo.tempUnschedCalls[0].accountID)
	require.NotNil(t, cache.states[89])
	require.Equal(t, http.StatusBadGateway, cache.states[89].StatusCode)
	require.Equal(t, "empty stream response", cache.states[89].MatchedKeyword)
}

func TestHandleOpenAIUpstreamTransportError_WrappedContextCanceledNoFailover(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 78, Name: "healthy2", Platform: PlatformOpenAI}
	c, _ := newOpenAITransportErrTestContext()

	err := svc.handleOpenAIUpstreamTransportError(context.Background(), c, account,
		fmt.Errorf("http request failed: %w", context.Canceled), false)

	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.Empty(t, repo.tempUnschedCalls)
}

func TestHandleOpenAIUpstreamTransportError_DeadlineExceededStillFailsOver(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 79, Name: "slow", Platform: PlatformOpenAI}
	c, _ := newOpenAITransportErrTestContext()

	err := svc.handleOpenAIUpstreamTransportError(context.Background(), c, account, context.DeadlineExceeded, false)

	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Empty(t, repo.tempUnschedCalls)
}

func TestForwardAsRawChatCompletions_TransportErrorFailsOver(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	upstream := &failingOpenAIHTTPUpstream{
		err: errors.New(`Post "https://opencode.ai/zen/v1/chat/completions": EOF`),
	}
	svc := &OpenAIGatewayService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{Enabled: false},
			},
		},
	}
	account := &Account{
		ID:          81,
		Name:        "oc-20053",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://opencode.ai/zen/v1"},
	}
	c, rec := newOpenAITransportErrTestContext()
	body := []byte(`{"model":"deepseek-v4-flash-free","messages":[{"role":"user","content":"hello"}]}`)

	_, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")

	require.Equal(t, 1, upstream.calls)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "transport error must trigger account failover")
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Empty(t, repo.tempUnschedCalls, "plain EOF is transient: fail over but do not evict")
	require.Equal(t, 0, rec.Body.Len(), "service must not write a hard 502 before handler can fail over")
}
