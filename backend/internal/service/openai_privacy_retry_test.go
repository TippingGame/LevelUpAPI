//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type privacyModeUpdateRepo struct {
	mockAccountRepoForGemini
	updateExtraCalls      int
	updateExtraContextErr error
	lastExtraUpdates      map[string]any
}

func (r *privacyModeUpdateRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	r.updateExtraCalls++
	r.updateExtraContextErr = ctx.Err()
	r.lastExtraUpdates = make(map[string]any, len(updates))
	for k, v := range updates {
		r.lastExtraUpdates[k] = v
	}
	return nil
}

func TestAdminService_EnsureOpenAIPrivacy_RetriesNonSuccessModes(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{PrivacyModeFailed, PrivacyModeCFBlocked} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			privacyCalls := 0
			svc := &adminServiceImpl{
				accountRepo: &mockAccountRepoForGemini{},
				privacyClientFactory: func(proxyURL string) (*req.Client, error) {
					privacyCalls++
					return nil, errors.New("factory failed")
				},
			}

			account := &Account{
				ID:       101,
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"access_token": "token-1",
				},
				Extra: map[string]any{
					"privacy_mode": mode,
				},
			}

			got := svc.EnsureOpenAIPrivacy(context.Background(), account)

			require.Equal(t, PrivacyModeFailed, got)
			require.Equal(t, 1, privacyCalls)
		})
	}
}

func TestAdminService_EnsureOpenAIPrivacy_PersistsPrivacyModeAfterRequestCancel(t *testing.T) {
	t.Parallel()

	repo := &privacyModeUpdateRepo{}
	svc := &adminServiceImpl{
		accountRepo: repo,
		privacyClientFactory: func(proxyURL string) (*req.Client, error) {
			return nil, errors.New("factory failed")
		},
	}

	account := &Account{
		ID:       101,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-1",
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := svc.EnsureOpenAIPrivacy(ctx, account)

	require.Equal(t, PrivacyModeFailed, got)
	require.Equal(t, 1, repo.updateExtraCalls)
	require.NoError(t, repo.updateExtraContextErr)
	require.Equal(t, PrivacyModeFailed, repo.lastExtraUpdates["privacy_mode"])
	require.Equal(t, PrivacyModeFailed, account.Extra["privacy_mode"])
}

func TestAdminService_ForceOpenAIPrivacy_PersistsPrivacyModeAfterRequestCancel(t *testing.T) {
	t.Parallel()

	repo := &privacyModeUpdateRepo{}
	svc := &adminServiceImpl{
		accountRepo: repo,
		privacyClientFactory: func(proxyURL string) (*req.Client, error) {
			return nil, errors.New("factory failed")
		},
	}

	account := &Account{
		ID:       102,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-2",
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := svc.ForceOpenAIPrivacy(ctx, account)

	require.Equal(t, PrivacyModeFailed, got)
	require.Equal(t, 1, repo.updateExtraCalls)
	require.NoError(t, repo.updateExtraContextErr)
	require.Equal(t, PrivacyModeFailed, repo.lastExtraUpdates["privacy_mode"])
	require.Equal(t, PrivacyModeFailed, account.Extra["privacy_mode"])
}

func TestAdminService_PersistAccountPrivacyMode_UpdatesAntigravityExtraAfterRequestCancel(t *testing.T) {
	t.Parallel()

	repo := &privacyModeUpdateRepo{}
	svc := &adminServiceImpl{accountRepo: repo}
	account := &Account{
		ID:       103,
		Platform: PlatformAntigravity,
		Extra: map[string]any{
			"kept": "yes",
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.persistAccountPrivacyMode(ctx, account, AntigravityPrivacySet)

	require.NoError(t, err)
	require.Equal(t, 1, repo.updateExtraCalls)
	require.NoError(t, repo.updateExtraContextErr)
	require.Equal(t, AntigravityPrivacySet, repo.lastExtraUpdates["privacy_mode"])
	require.Equal(t, "yes", account.Extra["kept"])
	require.Equal(t, AntigravityPrivacySet, account.Extra["privacy_mode"])
}

func TestTokenRefreshService_ensureOpenAIPrivacy_RetriesNonSuccessModes(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          1,
			RetryBackoffSeconds: 0,
		},
	}

	for _, mode := range []string{PrivacyModeFailed, PrivacyModeCFBlocked} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			service := NewTokenRefreshService(&tokenRefreshAccountRepo{}, nil, nil, nil, nil, nil, nil, cfg, nil)
			privacyCalls := 0
			service.SetPrivacyDeps(func(proxyURL string) (*req.Client, error) {
				privacyCalls++
				return nil, errors.New("factory failed")
			}, nil)

			account := &Account{
				ID:       202,
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"access_token": "token-2",
				},
				Extra: map[string]any{
					"privacy_mode": mode,
				},
			}

			service.ensureOpenAIPrivacy(context.Background(), account)

			require.Equal(t, 1, privacyCalls)
		})
	}
}
