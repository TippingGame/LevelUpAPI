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
		3: {UntilUnix: now.Add(time.Minute).Unix(), StatusCode: 401},
		4: {UntilUnix: now.Add(time.Minute).Unix(), StatusCode: 429},
		5: {UntilUnix: now.Add(time.Minute).Unix(), StatusCode: 401},
	}}
	svc := &RateLimitService{tempUnschedCache: cache}

	require.True(t, svc.IsTempUnschedulableCached(context.Background(), 1))
	require.False(t, svc.IsTempUnschedulableCached(context.Background(), 2))
	require.Equal(t, 1, cache.deleteCalls[2])
	require.False(t, svc.IsTempUnschedulableCached(context.Background(), 6))

	poolDefault := &Account{
		ID:       3,
		Type:     AccountTypeAPIKey,
		Platform: PlatformOpenAI,
		Credentials: map[string]any{
			"pool_mode": true,
		},
	}
	require.False(t, svc.IsAccountTempUnschedulableCached(context.Background(), poolDefault))
	require.Equal(t, 1, cache.deleteCalls[3])

	poolCustomHit := &Account{
		ID:       4,
		Type:     AccountTypeAPIKey,
		Platform: PlatformOpenAI,
		Credentials: map[string]any{
			"pool_mode":                  true,
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(429)},
		},
	}
	require.True(t, svc.IsAccountTempUnschedulableCached(context.Background(), poolCustomHit))
	require.Zero(t, cache.deleteCalls[4])

	poolCustomMiss := &Account{
		ID:       5,
		Type:     AccountTypeAPIKey,
		Platform: PlatformOpenAI,
		Credentials: map[string]any{
			"pool_mode":                  true,
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(429)},
		},
	}
	require.False(t, svc.IsAccountTempUnschedulableCached(context.Background(), poolCustomMiss))
	require.Equal(t, 1, cache.deleteCalls[5])
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

func TestOpenAISelectAccountForModel_IgnoresTempUnschedCacheForDefaultPoolMode(t *testing.T) {
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
			Credentials: map[string]any{
				"pool_mode": true,
			},
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
		1: {UntilUnix: time.Now().Add(time.Minute).Unix(), StatusCode: 401},
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
	require.Equal(t, int64(1), account.ID)
	require.Equal(t, 1, cache.deleteCalls[1])
}

func TestGeminiFilterTempUnschedCache_IgnoresDefaultPoolMode(t *testing.T) {
	accounts := []Account{
		{
			ID:          1,
			Platform:    PlatformAntigravity,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"pool_mode": true,
			},
		},
		{
			ID:          2,
			Platform:    PlatformGemini,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
		},
	}
	cache := &runtimeTempUnschedCacheStub{states: map[int64]*TempUnschedState{
		1: {UntilUnix: time.Now().Add(time.Minute).Unix(), StatusCode: 401},
		2: {UntilUnix: time.Now().Add(time.Minute).Unix(), StatusCode: 401},
	}}
	svc := &GeminiMessagesCompatService{
		rateLimitService: &RateLimitService{
			tempUnschedCache: cache,
		},
	}

	filtered := svc.filterTempUnschedulableCachedAccounts(context.Background(), accounts)

	require.Len(t, filtered, 1)
	require.Equal(t, int64(1), filtered[0].ID)
	require.Equal(t, 1, cache.deleteCalls[1])
	require.Zero(t, cache.deleteCalls[2])
}
