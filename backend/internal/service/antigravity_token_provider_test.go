//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type antigravityTokenProviderRepoStub struct {
	AccountRepository
	tempErr    error
	tempCalls  int
	lastID     int64
	lastUntil  time.Time
	lastReason string
}

func (r *antigravityTokenProviderRepoStub) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	r.lastID = id
	r.lastUntil = until
	r.lastReason = reason
	return r.tempErr
}

func TestAntigravityTokenProvider_GetAccessToken_Upstream(t *testing.T) {
	provider := &AntigravityTokenProvider{}

	t.Run("upstream account with valid api_key", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAntigravity,
			Type:     AccountTypeUpstream,
			Credentials: map[string]any{
				"api_key": "sk-test-key-12345",
			},
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.NoError(t, err)
		require.Equal(t, "sk-test-key-12345", token)
	})

	t.Run("upstream account missing api_key", func(t *testing.T) {
		account := &Account{
			Platform:    PlatformAntigravity,
			Type:        AccountTypeUpstream,
			Credentials: map[string]any{},
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "upstream account missing api_key")
		require.Empty(t, token)
	})

	t.Run("upstream account with empty api_key", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAntigravity,
			Type:     AccountTypeUpstream,
			Credentials: map[string]any{
				"api_key": "",
			},
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "upstream account missing api_key")
		require.Empty(t, token)
	})

	t.Run("upstream account with nil credentials", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAntigravity,
			Type:     AccountTypeUpstream,
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "upstream account missing api_key")
		require.Empty(t, token)
	})
}

func TestAntigravityTokenProvider_GetAccessToken_Guards(t *testing.T) {
	provider := &AntigravityTokenProvider{}

	t.Run("nil account", func(t *testing.T) {
		token, err := provider.GetAccessToken(context.Background(), nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "account is nil")
		require.Empty(t, token)
	})

	t.Run("non-antigravity platform", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAnthropic,
			Type:     AccountTypeOAuth,
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not an antigravity account")
		require.Empty(t, token)
	})

	t.Run("unsupported account type", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAntigravity,
			Type:     AccountTypeAPIKey,
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not an antigravity oauth account")
		require.Empty(t, token)
	})
}

func TestAntigravityTokenProvider_MarkTempUnschedulableSurvivesRepoWriteFailure(t *testing.T) {
	repo := &antigravityTokenProviderRepoStub{tempErr: errors.New("db timeout")}
	tempCache := &runtimeTempUnschedCacheStub{}
	provider := &AntigravityTokenProvider{
		accountRepo:      repo,
		tempUnschedCache: tempCache,
	}
	account := &Account{
		ID:          42,
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}

	provider.markTempUnschedulable(account, errors.New("refresh timeout"))

	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, int64(42), repo.lastID)
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Contains(t, account.TempUnschedulableReason, "refresh timeout")
	require.NotNil(t, tempCache.states[42])
	require.Equal(t, "antigravity_token_refresh", tempCache.states[42].MatchedKeyword)
	require.Contains(t, tempCache.states[42].ErrorMessage, "refresh timeout")
}

func TestAntigravityTokenProvider_MarkTempUnschedulablePoolModeSkipsLocalState(t *testing.T) {
	repo := &antigravityTokenProviderRepoStub{}
	tempCache := &runtimeTempUnschedCacheStub{}
	provider := &AntigravityTokenProvider{
		accountRepo:      repo,
		tempUnschedCache: tempCache,
	}
	account := &Account{
		ID:          43,
		Platform:    PlatformAntigravity,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"pool_mode": true,
		},
	}

	provider.markTempUnschedulable(account, errors.New("refresh timeout"))

	require.Equal(t, 0, repo.tempCalls)
	require.Nil(t, account.TempUnschedulableUntil)
	require.Nil(t, tempCache.states[43])
}
