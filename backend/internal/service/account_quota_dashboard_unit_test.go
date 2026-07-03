package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountQuotaDashboardGroupUsesEffectiveOpenAIProLevel(t *testing.T) {
	now := time.Date(2026, 7, 4, 6, 0, 0, 0, time.UTC)
	proGroupID := int64(1001)
	account := Account{
		ID:           2001,
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		AccountLevel: AccountLevelPlus,
		Credentials:  map[string]any{"plan_type": "chatgpt_pro"},
		Status:       StatusActive,
		Schedulable:  true,
		Groups: []*Group{{
			ID:                   proGroupID,
			Name:                 "PRO shared pool",
			Platform:             PlatformOpenAI,
			Status:               StatusActive,
			Scope:                GroupScopePublic,
			SubscriptionType:     SubscriptionTypeStandard,
			RequiredAccountLevel: AccountLevelPro,
		}},
	}

	builder := newAccountQuotaGroupDashboardBuilder(now)
	builder.addAccountWithGroupFilter(account, isPlatformSharedQuotaGroup)
	summaries := builder.finalize()

	require.Len(t, summaries, 1)
	require.Equal(t, proGroupID, *summaries[0].GroupID)
	require.Equal(t, 1, summaries[0].AccountCount)
	require.Equal(t, 1, summaries[0].SchedulableAccountCount)
}

func TestAccountQuotaDashboardIgnoresOpenAIOAuthRelayPoolTempState(t *testing.T) {
	now := time.Date(2026, 7, 4, 6, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	account := Account{
		ID:                      2002,
		Platform:                PlatformOpenAI,
		Type:                    AccountTypeOAuth,
		AccountLevel:            AccountLevelPro,
		Status:                  StatusActive,
		Schedulable:             true,
		TempUnschedulableUntil:  &future,
		TempUnschedulableReason: `{"matched_keyword":"upstream_relay_pool_unavailable","error_message":"No available accounts"}`,
	}

	builder := newAccountQuotaDashboardBuilder(now)
	builder.addAccount(account)
	dashboard := builder.finalize()

	require.Equal(t, 1, dashboard.Totals.AccountCount)
	require.Equal(t, 1, dashboard.Totals.SchedulableAccountCount)
	require.Equal(t, 0, dashboard.Totals.RateLimitedAccountCount)
}

type quotaPoolDashboardRepoStub struct {
	AccountRepository
	accounts      []Account
	repairChanged bool
	repairCalls   int
	listCalls     int
}

func (r *quotaPoolDashboardRepoStub) RepairQuotaPoolOwnerOpenAISharedPoolBindings(context.Context, int64) (bool, error) {
	r.repairCalls++
	return r.repairChanged, nil
}

func (r *quotaPoolDashboardRepoStub) ListQuotaPoolAccounts(context.Context, int64) ([]Account, error) {
	r.listCalls++
	out := make([]Account, len(r.accounts))
	copy(out, r.accounts)
	return out, nil
}

func TestGetQuotaPoolDashboardBypassesCachedViewAfterProBindingRepair(t *testing.T) {
	ctx := context.Background()
	ownerID := int64(42)
	plusGroupID := int64(1001)
	proGroupID := int64(1002)
	stalePlusGroup := &Group{
		ID:                   plusGroupID,
		Name:                 "PLUS shared pool",
		Platform:             PlatformOpenAI,
		Status:               StatusActive,
		Scope:                GroupScopePublic,
		SubscriptionType:     SubscriptionTypeStandard,
		RequiredAccountLevel: AccountLevelPlus,
	}
	repairedProGroup := &Group{
		ID:                   proGroupID,
		Name:                 "PRO shared pool",
		Platform:             PlatformOpenAI,
		Status:               StatusActive,
		Scope:                GroupScopePublic,
		SubscriptionType:     SubscriptionTypeStandard,
		RequiredAccountLevel: AccountLevelPro,
	}
	baseAccount := Account{
		ID:           2001,
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		AccountLevel: AccountLevelPlus,
		Credentials:  map[string]any{"plan_type": "chatgpt_pro"},
		OwnerUserID:  &ownerID,
		ShareMode:    AccountShareModePublic,
		ShareStatus:  AccountShareStatusApproved,
		Status:       StatusActive,
		Schedulable:  true,
		Concurrency:  3,
	}

	repo := &quotaPoolDashboardRepoStub{
		accounts: []Account{accountWithQuotaPoolGroups(baseAccount, stalePlusGroup)},
	}
	svc := &AccountService{accountRepo: repo}

	first, err := svc.GetQuotaPoolDashboard(ctx, ownerID)
	require.NoError(t, err)
	require.Equal(t, 1, repo.listCalls)
	requireQuotaPoolGroup(t, first.Mine.GroupSummaries, plusGroupID, 1, 0)

	repo.accounts = []Account{accountWithQuotaPoolGroups(baseAccount, repairedProGroup)}
	repo.repairChanged = true

	second, err := svc.GetQuotaPoolDashboard(ctx, ownerID)
	require.NoError(t, err)
	require.Equal(t, 2, repo.repairCalls)
	require.Equal(t, 2, repo.listCalls, "changed binding repair must bypass the stale dashboard cache")
	requireQuotaPoolGroup(t, second.Mine.GroupSummaries, proGroupID, 1, 1)
}

func TestGetQuotaPoolDashboardKeepsCacheWhenRepairDoesNotChangeBindings(t *testing.T) {
	ctx := context.Background()
	ownerID := int64(43)
	proGroupID := int64(1003)
	proGroup := &Group{
		ID:                   proGroupID,
		Name:                 "PRO shared pool",
		Platform:             PlatformOpenAI,
		Status:               StatusActive,
		Scope:                GroupScopePublic,
		SubscriptionType:     SubscriptionTypeStandard,
		RequiredAccountLevel: AccountLevelPro,
	}
	account := accountWithQuotaPoolGroups(Account{
		ID:           2002,
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		AccountLevel: AccountLevelPro,
		OwnerUserID:  &ownerID,
		ShareMode:    AccountShareModePublic,
		ShareStatus:  AccountShareStatusApproved,
		Status:       StatusActive,
		Schedulable:  true,
		Concurrency:  2,
	}, proGroup)
	repo := &quotaPoolDashboardRepoStub{accounts: []Account{account}}
	svc := &AccountService{accountRepo: repo}

	_, err := svc.GetQuotaPoolDashboard(ctx, ownerID)
	require.NoError(t, err)
	_, err = svc.GetQuotaPoolDashboard(ctx, ownerID)
	require.NoError(t, err)

	require.Equal(t, 2, repo.repairCalls, "repair check still runs before returning cached quota pool data")
	require.Equal(t, 1, repo.listCalls, "unchanged repair should keep the dashboard cache effective")
}

func accountWithQuotaPoolGroups(account Account, groups ...*Group) Account {
	account.Groups = groups
	account.GroupIDs = make([]int64, 0, len(groups))
	account.AccountGroups = make([]AccountGroup, 0, len(groups))
	for _, group := range groups {
		if group == nil {
			continue
		}
		account.GroupIDs = append(account.GroupIDs, group.ID)
		account.AccountGroups = append(account.AccountGroups, AccountGroup{
			AccountID: account.ID,
			GroupID:   group.ID,
			Group:     group,
		})
	}
	return account
}

func requireQuotaPoolGroup(t *testing.T, summaries []AccountQuotaGroupSummary, groupID int64, accountCount, schedulableCount int) {
	t.Helper()
	for _, summary := range summaries {
		if summary.GroupID == nil || *summary.GroupID != groupID {
			continue
		}
		require.Equal(t, accountCount, summary.AccountCount)
		require.Equal(t, schedulableCount, summary.SchedulableAccountCount)
		return
	}
	require.Failf(t, "group summary not found", "group_id=%d summaries=%#v", groupID, summaries)
}
