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
	tempErr   error
	tempCalls int
}

func (r *retryableTempUnschedRepoStub) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	return r.tempErr
}

func TestTempUnscheduleGoogleConfigErrorCachesWhenRepoWriteFails(t *testing.T) {
	repo := &retryableTempUnschedRepoStub{tempErr: errors.New("db timeout")}
	cache := &runtimeTempUnschedCacheStub{}

	tempUnscheduleGoogleConfigError(context.Background(), repo, cache, 101, "[test]")

	require.Equal(t, 1, repo.tempCalls)
	require.NotNil(t, cache.states[101])
	require.Equal(t, http.StatusBadRequest, cache.states[101].StatusCode)
	require.Equal(t, "invalid project resource name", cache.states[101].MatchedKeyword)
}

func TestTempUnscheduleEmptyResponseCachesWhenRepoWriteFails(t *testing.T) {
	repo := &retryableTempUnschedRepoStub{tempErr: errors.New("db timeout")}
	cache := &runtimeTempUnschedCacheStub{}

	tempUnscheduleEmptyResponse(context.Background(), repo, cache, 102, "[test]")

	require.Equal(t, 1, repo.tempCalls)
	require.NotNil(t, cache.states[102])
	require.Equal(t, http.StatusBadGateway, cache.states[102].StatusCode)
	require.Equal(t, "empty stream response", cache.states[102].MatchedKeyword)
}

func TestTempUnscheduleRetryableStatusErrorCachesWhenRepoWriteFails(t *testing.T) {
	repo := &retryableTempUnschedRepoStub{tempErr: errors.New("db timeout")}
	cache := &runtimeTempUnschedCacheStub{}

	tempUnscheduleRetryableStatusError(
		context.Background(),
		repo,
		cache,
		103,
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
