package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type runtimeTempUnschedCacheStub struct {
	states      map[int64]*TempUnschedState
	deleteCalls map[int64]int
}

func (s *runtimeTempUnschedCacheStub) SetTempUnsched(ctx context.Context, accountID int64, state *TempUnschedState) error {
	if s.states == nil {
		s.states = make(map[int64]*TempUnschedState)
	}
	s.states[accountID] = state
	return nil
}

func (s *runtimeTempUnschedCacheStub) GetTempUnsched(ctx context.Context, accountID int64) (*TempUnschedState, error) {
	if s.states == nil {
		return nil, nil
	}
	return s.states[accountID], nil
}

func (s *runtimeTempUnschedCacheStub) DeleteTempUnsched(ctx context.Context, accountID int64) error {
	if s.deleteCalls == nil {
		s.deleteCalls = make(map[int64]int)
	}
	s.deleteCalls[accountID]++
	if s.states != nil {
		delete(s.states, accountID)
	}
	return nil
}

func TestRateLimitService_IsTempUnschedulableCached(t *testing.T) {
	now := time.Now()
	cache := &runtimeTempUnschedCacheStub{states: map[int64]*TempUnschedState{
		1: {UntilUnix: now.Add(time.Minute).Unix()},
		2: {UntilUnix: now.Add(-time.Minute).Unix()},
	}}
	svc := &RateLimitService{tempUnschedCache: cache}

	require.True(t, svc.IsTempUnschedulableCached(context.Background(), 1))
	require.False(t, svc.IsTempUnschedulableCached(context.Background(), 2))
	require.Equal(t, 1, cache.deleteCalls[2])
	require.False(t, svc.IsTempUnschedulableCached(context.Background(), 3))
}

func TestOpenAISelectAccountForModel_SkipsTempUnschedCache(t *testing.T) {
	groupID := int64(9)
	accounts := []Account{
		{
			ID:          1,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Priority:    1,
			GroupIDs:    []int64{groupID},
		},
		{
			ID:          2,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Priority:    2,
			GroupIDs:    []int64{groupID},
		},
	}
	repo := stubOpenAIAccountRepo{accounts: accounts}
	cache := &runtimeTempUnschedCacheStub{states: map[int64]*TempUnschedState{
		1: {UntilUnix: time.Now().Add(time.Minute).Unix()},
	}}
	svc := &OpenAIGatewayService{
		accountRepo: repo,
		cfg:         &config.Config{RunMode: config.RunModeStandard},
		rateLimitService: &RateLimitService{
			tempUnschedCache: cache,
		},
	}

	account, err := svc.SelectAccountForModelWithExclusions(context.Background(), &groupID, "", "", nil)

	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(2), account.ID)
}
