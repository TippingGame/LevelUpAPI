//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type accountQuotaSchedulerRefresherStub struct {
	accounts []*Account
}

func (s *accountQuotaSchedulerRefresherStub) UpdateAccountInCache(ctx context.Context, account *Account) error {
	s.accounts = append(s.accounts, account)
	return nil
}

func TestAccountQuotaStateCrossedLimit(t *testing.T) {
	tests := []struct {
		name   string
		state  *AccountQuotaState
		amount float64
		want   bool
	}{
		{
			name:   "total crosses",
			state:  &AccountQuotaState{TotalUsed: 10, TotalLimit: 10},
			amount: 2,
			want:   true,
		},
		{
			name:   "daily crosses",
			state:  &AccountQuotaState{DailyUsed: 10, DailyLimit: 10},
			amount: 2,
			want:   true,
		},
		{
			name:   "weekly crosses",
			state:  &AccountQuotaState{WeeklyUsed: 50, WeeklyLimit: 50},
			amount: 5,
			want:   true,
		},
		{
			name:   "already exceeded before this request",
			state:  &AccountQuotaState{DailyUsed: 12, DailyLimit: 10},
			amount: 1,
			want:   false,
		},
		{
			name:   "still below limit",
			state:  &AccountQuotaState{TotalUsed: 9, TotalLimit: 10},
			amount: 1,
			want:   false,
		},
		{
			name:   "zero amount",
			state:  &AccountQuotaState{TotalUsed: 10, TotalLimit: 10},
			amount: 0,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, accountQuotaStateCrossedLimit(tt.state, tt.amount))
		})
	}
}

func TestSyncAccountQuotaSchedulerSnapshotOnQuotaCrossing(t *testing.T) {
	fresh := &Account{
		ID:          7,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"quota_daily_limit": 10.0,
			"quota_daily_used":  10.0,
		},
	}
	repo := &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{fresh.ID: fresh}}
	refresher := &accountQuotaSchedulerRefresherStub{}

	syncAccountQuotaSchedulerSnapshot(
		&postUsageBillingParams{
			Cost:                  &CostBreakdown{TotalCost: 2},
			Account:               &Account{ID: fresh.ID, Type: AccountTypeAPIKey},
			AccountRateMultiplier: 1,
		},
		&billingDeps{
			accountRepo:       repo,
			schedulerSnapshot: refresher,
		},
		&UsageBillingApplyResult{
			QuotaState: &AccountQuotaState{DailyUsed: 10, DailyLimit: 10},
		},
	)

	require.Equal(t, 1, repo.getByIDCalls)
	require.Len(t, refresher.accounts, 1)
	require.Same(t, fresh, refresher.accounts[0])
}

func TestSyncAccountQuotaSchedulerSnapshotSkipsWhenNoCrossing(t *testing.T) {
	repo := &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{7: &Account{ID: 7}}}
	refresher := &accountQuotaSchedulerRefresherStub{}

	syncAccountQuotaSchedulerSnapshot(
		&postUsageBillingParams{
			Cost:                  &CostBreakdown{TotalCost: 1},
			Account:               &Account{ID: 7, Type: AccountTypeAPIKey},
			AccountRateMultiplier: 1,
		},
		&billingDeps{
			accountRepo:       repo,
			schedulerSnapshot: refresher,
		},
		&UsageBillingApplyResult{
			QuotaState: &AccountQuotaState{DailyUsed: 9, DailyLimit: 10},
		},
	)

	require.Zero(t, repo.getByIDCalls)
	require.Empty(t, refresher.accounts)
}
