package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type manualTempUnschedRepoStub struct {
	AccountRepository
	tempErr   error
	tempCalls int
}

func (r *manualTempUnschedRepoStub) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	return r.tempErr
}

func TestRateLimitService_SetTempUnschedulableUsesRuntimeFallbackOnRepoFailure(t *testing.T) {
	repo := &manualTempUnschedRepoStub{tempErr: errors.New("db timeout")}
	cache := &runtimeTempUnschedCacheStub{}
	svc := &RateLimitService{
		accountRepo:      repo,
		tempUnschedCache: cache,
	}
	account := &Account{ID: 77, Status: StatusActive, Schedulable: true}
	until := time.Now().Add(5 * time.Minute)

	err := svc.SetTempUnschedulable(context.Background(), account, until, "manual cooldown")

	require.Error(t, err)
	require.Equal(t, 1, repo.tempCalls)
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Equal(t, "manual cooldown", account.TempUnschedulableReason)
	require.NotNil(t, cache.states[77])
	require.Equal(t, "manual cooldown", cache.states[77].ErrorMessage)
}
