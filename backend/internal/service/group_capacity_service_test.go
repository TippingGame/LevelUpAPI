package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type groupCapacityAccountRepoStub struct {
	AccountRepository
	accounts      []Account
	repairUserIDs []int64
}

func (s *groupCapacityAccountRepoStub) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	out := make([]Account, len(s.accounts))
	copy(out, s.accounts)
	return out, nil
}

func (s *groupCapacityAccountRepoStub) RepairQuotaPoolVisibleOpenAISharedPoolBindings(_ context.Context, userID int64) (bool, error) {
	s.repairUserIDs = append(s.repairUserIDs, userID)
	return true, nil
}

type groupCapacityVisibleGroupRepoStub struct {
	groupRepoNoop
	groups []Group
	userID int64
}

func (s *groupCapacityVisibleGroupRepoStub) ListActiveVisibleToUser(_ context.Context, userID int64, _ []int64) ([]Group, error) {
	s.userID = userID
	out := make([]Group, len(s.groups))
	copy(out, s.groups)
	return out, nil
}

func TestGroupCapacityIncludesCodexProtectedOpenAIAccount(t *testing.T) {
	resetAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	svc := &GroupCapacityService{
		accountRepo: &groupCapacityAccountRepoStub{
			accounts: []Account{
				{
					ID:          1,
					Platform:    PlatformOpenAI,
					Type:        AccountTypeOAuth,
					Status:      StatusActive,
					Schedulable: true,
					Concurrency: 3,
					Extra: map[string]any{
						"codex_5h_limit_percent": 80.0,
						"codex_5h_used_percent":  81.0,
						"codex_5h_reset_at":      resetAt,
					},
				},
			},
		},
	}

	capacity, err := svc.getGroupCapacity(context.Background(), 10)

	require.NoError(t, err)
	require.Equal(t, 3, capacity.ConcurrencyMax)
}

func TestFilterPublicBalanceGroupsNormalizesLegacyMetadata(t *testing.T) {
	ownerID := int64(42)
	groups := []Group{
		{ID: 1, Scope: " Public ", SubscriptionType: " STANDARD "},
		{ID: 2, Scope: GroupScopePublic, SubscriptionType: ""},
		{ID: 3, Scope: GroupScopePublic, SubscriptionType: SubscriptionTypeSubscription},
		{ID: 4, Scope: GroupScopeUserPrivate, OwnerUserID: &ownerID, SubscriptionType: SubscriptionTypeStandard},
		{ID: 5, Scope: GroupScopePublic, SubscriptionType: SubscriptionTypeStandard, IsExclusive: true},
	}

	filtered := filterPublicBalanceGroups(groups)

	require.Len(t, filtered, 2)
	require.Equal(t, int64(1), filtered[0].ID)
	require.Equal(t, int64(2), filtered[1].ID)
}

func TestGetUserVisiblePublicBalanceGroupsFiltersSharedPoolMetadata(t *testing.T) {
	ownerID := int64(99)
	groupRepo := &groupCapacityVisibleGroupRepoStub{
		groups: []Group{
			{ID: 1, Name: "OpenAI Pro", Scope: " Public ", SubscriptionType: " STANDARD "},
			{ID: 2, Name: "legacy standard", Scope: GroupScopePublic, SubscriptionType: ""},
			{ID: 3, Name: "subscription", Scope: GroupScopePublic, SubscriptionType: SubscriptionTypeSubscription},
			{ID: 4, Name: "private", Scope: GroupScopeUserPrivate, OwnerUserID: &ownerID, SubscriptionType: SubscriptionTypeStandard},
			{ID: 5, Name: "exclusive", Scope: GroupScopePublic, SubscriptionType: SubscriptionTypeStandard, IsExclusive: true},
		},
	}
	svc := &GroupCapacityService{groupRepo: groupRepo}

	groups, err := svc.GetUserVisiblePublicBalanceGroups(context.Background(), 42)

	require.NoError(t, err)
	require.Equal(t, int64(42), groupRepo.userID)
	require.Len(t, groups, 2)
	require.Equal(t, int64(1), groups[0].ID)
	require.Equal(t, int64(2), groups[1].ID)
}

func TestGetUserVisiblePublicBalanceGroupsRepairsVisibleOpenAISharedPools(t *testing.T) {
	accountRepo := &groupCapacityAccountRepoStub{}
	groupRepo := &groupCapacityVisibleGroupRepoStub{
		groups: []Group{
			{ID: 1, Name: "PRO共享号池", Scope: GroupScopePublic, SubscriptionType: SubscriptionTypeStandard},
		},
	}
	svc := &GroupCapacityService{
		accountRepo: accountRepo,
		groupRepo:   groupRepo,
	}

	groups, err := svc.GetUserVisiblePublicBalanceGroups(context.Background(), 42)

	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, []int64{42}, accountRepo.repairUserIDs)
	require.Equal(t, int64(42), groupRepo.userID)
}
