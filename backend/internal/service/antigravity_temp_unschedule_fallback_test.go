package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type retryableTempUnschedRepoStub struct {
	AccountRepository
	account   *Account
	tempErr   error
	tempCalls int
}

func (r *retryableTempUnschedRepoStub) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func (r *retryableTempUnschedRepoStub) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	return r.tempErr
}

func TestTempUnscheduleGoogleConfigErrorCachesWhenRepoWriteFails(t *testing.T) {
	repo := &retryableTempUnschedRepoStub{tempErr: errors.New("db timeout")}
	cache := &runtimeTempUnschedCacheStub{}
	account := &Account{ID: 101, Type: AccountTypeAPIKey, Platform: PlatformAntigravity}

	tempUnscheduleGoogleConfigError(context.Background(), repo, cache, account, "[test]")

	require.Equal(t, 1, repo.tempCalls)
	require.NotNil(t, cache.states[101])
	require.Equal(t, http.StatusBadRequest, cache.states[101].StatusCode)
	require.Equal(t, "invalid project resource name", cache.states[101].MatchedKeyword)
}

func TestTempUnscheduleEmptyResponseCachesWhenRepoWriteFails(t *testing.T) {
	repo := &retryableTempUnschedRepoStub{tempErr: errors.New("db timeout")}
	cache := &runtimeTempUnschedCacheStub{}
	account := &Account{ID: 102, Type: AccountTypeAPIKey, Platform: PlatformAntigravity}

	tempUnscheduleEmptyResponse(context.Background(), repo, cache, account, "[test]")

	require.Equal(t, 1, repo.tempCalls)
	require.NotNil(t, cache.states[102])
	require.Equal(t, http.StatusBadGateway, cache.states[102].StatusCode)
	require.Equal(t, "empty stream response", cache.states[102].MatchedKeyword)
}

func TestTempUnscheduleRetryableStatusErrorCachesWhenRepoWriteFails(t *testing.T) {
	repo := &retryableTempUnschedRepoStub{tempErr: errors.New("db timeout")}
	cache := &runtimeTempUnschedCacheStub{}
	account := &Account{ID: 103, Type: AccountTypeAPIKey, Platform: PlatformAntigravity}

	tempUnscheduleRetryableStatusError(
		context.Background(),
		repo,
		cache,
		account,
		http.StatusServiceUnavailable,
		[]byte(`{"error":{"message":"upstream unavailable"}}`),
		"[test]",
	)

	require.Equal(t, 1, repo.tempCalls)
	require.NotNil(t, cache.states[103])
	require.Equal(t, http.StatusServiceUnavailable, cache.states[103].StatusCode)
	require.Equal(t, "retryable_status_503", cache.states[103].MatchedKeyword)
	require.Contains(t, cache.states[103].ErrorMessage, "upstream unavailable")
}

func TestTempUnscheduleRetryableStatusErrorSkipsPoolModeWithoutActivePolicy(t *testing.T) {
	repo := &retryableTempUnschedRepoStub{}
	cache := &runtimeTempUnschedCacheStub{}
	account := &Account{
		ID:       104,
		Type:     AccountTypeAPIKey,
		Platform: PlatformAntigravity,
		Credentials: map[string]any{
			"pool_mode":                  true,
			"custom_error_codes_enabled": true,
		},
	}

	tempUnscheduleRetryableStatusError(
		context.Background(),
		repo,
		cache,
		account,
		http.StatusServiceUnavailable,
		[]byte(`{"error":{"message":"upstream unavailable"}}`),
		"[test]",
	)

	require.Zero(t, repo.tempCalls)
	require.Nil(t, cache.states[104])
}

func TestGatewayTempUnscheduleRetryableErrorSkipsPoolModeWithoutActivePolicy(t *testing.T) {
	account := &Account{
		ID:       105,
		Type:     AccountTypeAPIKey,
		Platform: PlatformAntigravity,
		Credentials: map[string]any{
			"pool_mode":                  true,
			"custom_error_codes_enabled": true,
		},
	}
	repo := &retryableTempUnschedRepoStub{account: account}
	svc := &GatewayService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{tempUnschedCache: &runtimeTempUnschedCacheStub{}},
	}

	svc.TempUnscheduleRetryableError(context.Background(), account.ID, &UpstreamFailoverError{
		StatusCode:             http.StatusServiceUnavailable,
		ResponseBody:           []byte(`{"error":{"message":"upstream unavailable"}}`),
		RetryableOnSameAccount: true,
	})

	require.Zero(t, repo.tempCalls)
}
