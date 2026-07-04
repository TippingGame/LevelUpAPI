package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestSchedulerSnapshotCacheUsesPartialCandidatesWhenProxyRepairFallbackUnavailable(t *testing.T) {
	ownerID := int64(10)
	proxyID := int64(20)
	groupID := int64(30)
	cache := &schedulerSnapshotQuotaCache{
		snapshotAccounts: []*Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
			},
			{
				ID:           2,
				Platform:     PlatformOpenAI,
				AccountLevel: AccountLevelPro,
				Type:         AccountTypeOAuth,
				OwnerUserID:  &ownerID,
				ShareMode:    AccountShareModePublic,
				ShareStatus:  AccountShareStatusApproved,
				ProxyID:      &proxyID,
				Status:       StatusActive,
				Schedulable:  true,
				Concurrency:  1,
			},
		},
	}
	repo := &schedulerSnapshotQuotaAccountRepo{}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.DbFallbackEnabled = false
	svc := NewSchedulerSnapshotService(cache, nil, repo, nil, cfg)

	accounts, _, err := svc.ListSchedulableAccounts(context.Background(), &groupID, PlatformOpenAI, false)
	if err != nil {
		t.Fatalf("ListSchedulableAccounts error: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != 1 {
		t.Fatalf("returned accounts = %+v, want already schedulable cached account", accounts)
	}
}

func TestFilterSchedulableAccountsExcludesCodexQuotaProtectedAccount(t *testing.T) {
	resetAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	accounts := []Account{
		{
			ID:          1,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Extra: map[string]any{
				"codex_5h_limit_percent": 80.0,
				"codex_5h_used_percent":  81.0,
				"codex_5h_reset_at":      resetAt,
			},
		},
		{
			ID:          2,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Extra: map[string]any{
				"codex_5h_limit_percent": 80.0,
				"codex_5h_used_percent":  79.9,
				"codex_5h_reset_at":      resetAt,
			},
		},
	}

	filtered := filterSchedulableAccounts(accounts)
	if len(filtered) != 1 || filtered[0].ID != 2 {
		t.Fatalf("filtered accounts = %+v, want only account 2", filtered)
	}
}

func TestFilterSchedulableAccountsForSnapshotKeepsCodexQuotaProtectedAccount(t *testing.T) {
	resetAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	accounts := []Account{
		{
			ID:          1,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Extra: map[string]any{
				"codex_5h_limit_percent": 80.0,
				"codex_5h_used_percent":  81.0,
				"codex_5h_reset_at":      resetAt,
			},
		},
	}

	filtered := filterSchedulableAccountsForSnapshot(accounts)
	if len(filtered) != 1 || filtered[0].ID != 1 {
		t.Fatalf("filtered accounts = %+v, want protected account retained in snapshot", filtered)
	}
}

func TestSchedulerSnapshotFallbackCachesButDoesNotReturnCodexQuotaProtectedAccount(t *testing.T) {
	resetAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	cache := &schedulerSnapshotQuotaCache{}
	repo := &schedulerSnapshotQuotaAccountRepo{
		accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				Status:      StatusActive,
				Schedulable: true,
				Extra: map[string]any{
					"codex_5h_limit_percent": 80.0,
					"codex_5h_used_percent":  81.0,
					"codex_5h_reset_at":      resetAt,
				},
			},
			{
				ID:          2,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				Status:      StatusActive,
				Schedulable: true,
				Extra: map[string]any{
					"codex_5h_limit_percent": 80.0,
					"codex_5h_used_percent":  79.0,
					"codex_5h_reset_at":      resetAt,
				},
			},
		},
	}
	svc := NewSchedulerSnapshotService(cache, nil, repo, nil, nil)

	accounts, _, err := svc.ListSchedulableAccounts(context.Background(), nil, PlatformOpenAI, false)
	if err != nil {
		t.Fatalf("ListSchedulableAccounts error: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != 2 {
		t.Fatalf("returned accounts = %+v, want only account 2", accounts)
	}
	if len(cache.cachedAccounts) != 2 {
		t.Fatalf("cached accounts = %+v, want both accounts retained for automatic recovery", cache.cachedAccounts)
	}
}

func TestSchedulerSnapshotCacheHydratesRequiredProxyMetadata(t *testing.T) {
	ownerID := int64(10)
	proxyID := int64(20)
	groupID := int64(30)
	cached := &Account{
		ID:           1,
		Platform:     PlatformOpenAI,
		AccountLevel: AccountLevelPro,
		Type:         AccountTypeOAuth,
		OwnerUserID:  &ownerID,
		ShareMode:    AccountShareModePublic,
		ShareStatus:  AccountShareStatusApproved,
		ProxyID:      &proxyID,
		Status:       StatusActive,
		Schedulable:  true,
		Concurrency:  1,
	}
	full := *cached
	full.Proxy = &Proxy{ID: proxyID, Status: StatusActive}
	cache := &schedulerSnapshotQuotaCache{
		snapshotAccounts: []*Account{cached},
		fullAccounts: map[int64]*Account{
			full.ID: &full,
		},
	}
	repo := &schedulerSnapshotQuotaAccountRepo{}
	svc := NewSchedulerSnapshotService(cache, nil, repo, nil, nil)

	accounts, _, err := svc.ListSchedulableAccounts(context.Background(), &groupID, PlatformOpenAI, false)
	if err != nil {
		t.Fatalf("ListSchedulableAccounts error: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != full.ID {
		t.Fatalf("returned accounts = %+v, want hydrated Pro account", accounts)
	}
	if len(cache.setAccountIDs) != 1 || cache.setAccountIDs[0] != full.ID {
		t.Fatalf("set account IDs = %+v, want metadata refresh for account %d", cache.setAccountIDs, full.ID)
	}
}

type schedulerSnapshotQuotaCache struct {
	cachedAccounts   []Account
	snapshotAccounts []*Account
	fullAccounts     map[int64]*Account
	setAccountIDs    []int64
}

func (c *schedulerSnapshotQuotaCache) GetSnapshot(context.Context, SchedulerBucket) ([]*Account, bool, error) {
	if c.snapshotAccounts != nil {
		return c.snapshotAccounts, true, nil
	}
	return nil, false, nil
}

func (c *schedulerSnapshotQuotaCache) SetSnapshot(_ context.Context, _ SchedulerBucket, accounts []Account) error {
	c.cachedAccounts = append([]Account(nil), accounts...)
	return nil
}

func (c *schedulerSnapshotQuotaCache) GetAccount(_ context.Context, id int64) (*Account, error) {
	if c.fullAccounts == nil {
		return nil, nil
	}
	return c.fullAccounts[id], nil
}

func (c *schedulerSnapshotQuotaCache) SetAccount(_ context.Context, account *Account) error {
	if account != nil {
		c.setAccountIDs = append(c.setAccountIDs, account.ID)
		if c.fullAccounts == nil {
			c.fullAccounts = map[int64]*Account{}
		}
		copyAccount := *account
		c.fullAccounts[account.ID] = &copyAccount
	}
	return nil
}

func (c *schedulerSnapshotQuotaCache) DeleteAccount(context.Context, int64) error {
	return nil
}

func (c *schedulerSnapshotQuotaCache) UpdateLastUsed(context.Context, map[int64]time.Time) error {
	return nil
}

func (c *schedulerSnapshotQuotaCache) TryLockBucket(context.Context, SchedulerBucket, time.Duration) (bool, error) {
	return true, nil
}

func (c *schedulerSnapshotQuotaCache) UnlockBucket(context.Context, SchedulerBucket) error {
	return nil
}

func (c *schedulerSnapshotQuotaCache) ListBuckets(context.Context) ([]SchedulerBucket, error) {
	return nil, nil
}

func (c *schedulerSnapshotQuotaCache) GetOutboxWatermark(context.Context) (int64, error) {
	return 0, nil
}

func (c *schedulerSnapshotQuotaCache) SetOutboxWatermark(context.Context, int64) error {
	return nil
}

type schedulerSnapshotQuotaAccountRepo struct {
	AccountRepository
	accounts []Account
}

func (r *schedulerSnapshotQuotaAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]Account, error) {
	out := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out, nil
}
