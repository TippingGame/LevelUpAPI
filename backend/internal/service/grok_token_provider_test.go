//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type grokTokenCacheForProviderTest struct {
	token        string
	setKey       string
	setToken     string
	setTTL       time.Duration
	lockResult   bool
	releaseCalls int
	deletedKeys  []string
	deleteErr    error
	getCalls     int
	mu           sync.Mutex
}

type grokCredentialRaceRepo struct {
	*tokenRefreshAccountRepo
	mu sync.RWMutex
}

func (r *grokCredentialRaceRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tokenRefreshAccountRepo.GetByID(ctx, id)
}

func (r *grokCredentialRaceRepo) setAccount(account *Account) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accountsByID[account.ID] = account
}

func (c *grokTokenCacheForProviderTest) GetAccessToken(context.Context, string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getCalls++
	if c.token == "" {
		return "", errors.New("not cached")
	}
	return c.token, nil
}

func (c *grokTokenCacheForProviderTest) SetAccessToken(_ context.Context, key string, token string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setKey = key
	c.setToken = token
	c.setTTL = ttl
	return nil
}

func (c *grokTokenCacheForProviderTest) DeleteAccessToken(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deletedKeys = append(c.deletedKeys, key)
	return c.deleteErr
}

func (c *grokTokenCacheForProviderTest) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return c.lockResult, nil
}

func (c *grokTokenCacheForProviderTest) ReleaseRefreshLock(context.Context, string) error {
	c.releaseCalls++
	return nil
}

func TestGrokTokenProviderRefreshesExpiredTokenOnRequestPath(t *testing.T) {
	t.Setenv(xai.EnvBaseURL, xai.DefaultCLIBaseURL)

	expiredAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	account := &Account{
		ID:          54,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":  "expired-access-token",
			"refresh_token": "refresh-token",
			"expires_at":    expiredAt,
			"base_url":      xai.DefaultCLIBaseURL,
			"client_id":     "client-id",
		},
	}
	repo := &tokenRefreshAccountRepo{}
	repo.accountsByID = map[int64]*Account{54: account}
	cache := &grokTokenCacheForProviderTest{lockResult: true}
	oauthSvc := NewGrokOAuthService(nil, &grokOAuthClientStub{
		refreshResponse: &xai.TokenResponse{
			AccessToken: "new-access-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		},
	})
	defer oauthSvc.Stop()

	provider := NewGrokTokenProvider(repo, cache)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), NewGrokTokenRefresher(oauthSvc))

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "new-access-token", token)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Equal(t, "new-access-token", repo.accountsByID[54].GetGrokAccessToken())
	require.Equal(t, "refresh-token", repo.accountsByID[54].GetGrokRefreshToken())
	require.Equal(t, xai.DefaultCLIBaseURL, repo.accountsByID[54].GetGrokBaseURL())
	require.Equal(t, "grok:account:54", cache.setKey)
	require.Equal(t, "new-access-token", cache.setToken)
	require.Greater(t, cache.setTTL, time.Duration(0))
	require.Equal(t, 1, cache.releaseCalls)
}

func TestGrokTokenProviderRefreshFailureDefersAccountQuarantine(t *testing.T) {
	expiredAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	account := &Account{
		ID:          55,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":  "expired-access-token",
			"refresh_token": "refresh-token",
			"expires_at":    expiredAt,
			"base_url":      xai.DefaultCLIBaseURL,
		},
	}
	repo := &tokenRefreshAccountRepo{}
	repo.accountsByID = map[int64]*Account{55: account}
	cache := &grokTokenCacheForProviderTest{lockResult: true}
	provider := NewGrokTokenProvider(repo, cache)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &tokenRefresherStub{
		err: errors.New("temporary refresh failure access_token=leaked-access refresh_token=leaked-refresh"),
	})

	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Empty(t, token)
	require.Equal(t, 0, repo.setTempUnschedCalls)
	require.Equal(t, 0, repo.setErrorCalls)
}

// Manual connection tests must remain usable while production scheduling has
// temporarily excluded the account.
func TestGrokTokenProviderManualTestBypassesSchedulingGate(t *testing.T) {
	future := time.Now().Add(time.Hour)
	tests := []struct {
		name   string
		mutate func(*Account)
	}{
		{name: "not schedulable", mutate: func(account *Account) { account.Schedulable = false }},
		{name: "temporarily unschedulable", mutate: func(account *Account) { account.TempUnschedulableUntil = &future }},
		{name: "rate limited", mutate: func(account *Account) { account.RateLimitResetAt = &future }},
		{name: "overloaded", mutate: func(account *Account) { account.OverloadUntil = &future }},
		{name: "disabled by error", mutate: func(account *Account) { account.Status = StatusError }},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := expiredGrokOAuthAccountForCredentialTest(int64(120 + index))
			account.Credentials["access_token"] = "still-valid-token"
			account.Credentials["expires_at"] = time.Now().Add(2 * grokTokenRefreshSkew).UTC().Format(time.RFC3339)
			tt.mutate(account)
			provider := NewGrokTokenProvider(&tokenRefreshAccountRepo{}, &grokTokenCacheForProviderTest{})

			_, requestErr := provider.GetAccessToken(context.Background(), account)
			require.ErrorIs(t, requestErr, errOAuthRefreshAccountStateChanged)

			token, err := provider.GetAccessTokenForManualTest(context.Background(), account)
			require.NoError(t, err)
			require.Equal(t, "still-valid-token", token)
		})
	}
}

func TestGrokTokenProviderManualTestRefreshesExpiredTokenWhileUnschedulable(t *testing.T) {
	t.Setenv(xai.EnvBaseURL, xai.DefaultCLIBaseURL)

	account := &Account{
		ID:          130,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: false,
		Credentials: map[string]any{
			"access_token":  "expired-access-token",
			"refresh_token": "refresh-token",
			"expires_at":    time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
			"base_url":      xai.DefaultCLIBaseURL,
			"client_id":     "client-id",
		},
	}
	repo := &tokenRefreshAccountRepo{}
	repo.accountsByID = map[int64]*Account{130: account}
	cache := &grokTokenCacheForProviderTest{lockResult: true}
	oauthSvc := NewGrokOAuthService(nil, &grokOAuthClientStub{
		refreshResponse: &xai.TokenResponse{AccessToken: "manual-test-refreshed-token", TokenType: "Bearer", ExpiresIn: 3600},
	})
	defer oauthSvc.Stop()

	provider := NewGrokTokenProvider(repo, cache)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), NewGrokTokenRefresher(oauthSvc))

	token, err := provider.GetAccessTokenForManualTest(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "manual-test-refreshed-token", token)
	require.Equal(t, 1, repo.updateCredentialsCalls)
}

func TestGrokTokenProviderManualTestFallsBackToValidTokenOnRefreshFailure(t *testing.T) {
	account := &Account{
		ID:          131,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: false,
		Credentials: map[string]any{
			"access_token":  "near-expiry-token",
			"refresh_token": "refresh-token",
			"expires_at":    time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
		},
	}
	repo := &tokenRefreshAccountRepo{}
	repo.accountsByID = map[int64]*Account{131: account}
	cache := &grokTokenCacheForProviderTest{lockResult: true}
	provider := NewGrokTokenProvider(repo, cache)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &tokenRefresherStub{err: errors.New("upstream refresh unavailable")})

	token, err := provider.GetAccessTokenForManualTest(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "near-expiry-token", token)
}

func TestGrokTokenProviderManualTestReportsRefreshFailureWhenTokenExpired(t *testing.T) {
	account := expiredGrokOAuthAccountForCredentialTest(132)
	account.Schedulable = false
	repo := &tokenRefreshAccountRepo{}
	repo.accountsByID = map[int64]*Account{account.ID: account}
	cache := &grokTokenCacheForProviderTest{lockResult: true}
	provider := NewGrokTokenProvider(repo, cache)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &tokenRefresherStub{err: errors.New("invalid_client: client credentials rejected")})

	token, err := provider.GetAccessTokenForManualTest(context.Background(), account)
	require.Error(t, err)
	require.Empty(t, token)
	require.Contains(t, err.Error(), "invalid_client")
}

func TestGrokTokenProviderManualTestLockHeldWithExpiredTokenReturnsSpecificError(t *testing.T) {
	account := expiredGrokOAuthAccountForCredentialTest(133)
	repo := &tokenRefreshAccountRepo{}
	repo.accountsByID = map[int64]*Account{account.ID: account}
	cache := &grokTokenCacheForProviderTest{lockResult: false}
	provider := NewGrokTokenProvider(repo, cache)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &tokenRefresherStub{})

	token, err := provider.GetAccessTokenForManualTest(context.Background(), account)
	require.Error(t, err)
	require.Empty(t, token)
	require.Contains(t, err.Error(), "refresh is already in progress")
}

func TestGrokTokenProviderManualTestRequiresRefreshToken(t *testing.T) {
	account := expiredGrokOAuthAccountForCredentialTest(134)
	delete(account.Credentials, "refresh_token")
	provider := NewGrokTokenProvider(&tokenRefreshAccountRepo{}, &grokTokenCacheForProviderTest{})

	token, err := provider.GetAccessTokenForManualTest(context.Background(), account)
	require.ErrorIs(t, err, errGrokOAuthRefreshTokenMissing)
	require.Empty(t, token)
}
