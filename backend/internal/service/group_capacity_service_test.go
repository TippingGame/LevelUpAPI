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
	rows          []GroupAccountCapacityRow
	requested     []int64
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

func (s *groupCapacityAccountRepoStub) ListSchedulableCapacityByGroupIDs(_ context.Context, groupIDs []int64) ([]GroupAccountCapacityRow, error) {
	s.requested = append([]int64(nil), groupIDs...)
	return append([]GroupAccountCapacityRow(nil), s.rows...), nil
}

type groupCapacityBatchGroupRepoStub struct {
	GroupRepository
	groupIDs  []int64
	listCalls int
}

func (s *groupCapacityBatchGroupRepoStub) ListActiveIDs(context.Context) ([]int64, error) {
	s.listCalls++
	return append([]int64(nil), s.groupIDs...), nil
}

type groupCapacityConcurrencyCacheStub struct {
	ConcurrencyCache
	counts    map[int64]int
	requested []int64
}

func (s *groupCapacityConcurrencyCacheStub) GetAccountConcurrencyBatch(_ context.Context, accountIDs []int64) (map[int64]int, error) {
	s.requested = append([]int64(nil), accountIDs...)
	out := make(map[int64]int, len(accountIDs))
	for _, id := range accountIDs {
		out[id] = s.counts[id]
	}
	return out, nil
}

type groupCapacitySessionCacheStub struct {
	SessionLimitCache
	counts       map[int64]int
	requested    []int64
	idleTimeouts map[int64]time.Duration
}

func (s *groupCapacitySessionCacheStub) GetActiveSessionCountBatch(_ context.Context, accountIDs []int64, idleTimeouts map[int64]time.Duration) (map[int64]int, error) {
	s.requested = append([]int64(nil), accountIDs...)
	s.idleTimeouts = make(map[int64]time.Duration, len(idleTimeouts))
	for id, timeout := range idleTimeouts {
		s.idleTimeouts[id] = timeout
	}
	out := make(map[int64]int, len(accountIDs))
	for _, id := range accountIDs {
		out[id] = s.counts[id]
	}
	return out, nil
}

type groupCapacityRPMCacheStub struct {
	RPMCache
	counts    map[int64]int
	requested []int64
}

func (s *groupCapacityRPMCacheStub) GetRPMBatch(_ context.Context, accountIDs []int64) (map[int64]int, error) {
	s.requested = append([]int64(nil), accountIDs...)
	out := make(map[int64]int, len(accountIDs))
	for _, id := range accountIDs {
		out[id] = s.counts[id]
	}
	return out, nil
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

func TestGetAllGroupCapacityBatchAggregatesRuntimeAndLimits(t *testing.T) {
	accountRepo := &groupCapacityAccountRepoStub{
		rows: []GroupAccountCapacityRow{
			{
				GroupID:     10,
				AccountID:   1,
				Concurrency: 2,
				Extra: map[string]any{
					"max_sessions":                 3,
					"session_idle_timeout_minutes": 7,
					"base_rpm":                     11,
				},
			},
			{
				GroupID:     20,
				AccountID:   1,
				Concurrency: 2,
				Extra: map[string]any{
					"max_sessions":                 3,
					"session_idle_timeout_minutes": 7,
					"base_rpm":                     11,
				},
			},
			{
				GroupID:     20,
				AccountID:   2,
				Concurrency: 4,
				Extra: map[string]any{
					"max_sessions":                 1,
					"session_idle_timeout_minutes": 9,
					"base_rpm":                     13,
				},
			},
		},
	}
	groupRepo := &groupCapacityBatchGroupRepoStub{groupIDs: []int64{10, 20}}
	concurrencyCache := &groupCapacityConcurrencyCacheStub{counts: map[int64]int{1: 1, 2: 2}}
	sessionCache := &groupCapacitySessionCacheStub{counts: map[int64]int{1: 2, 2: 1}}
	rpmCache := &groupCapacityRPMCacheStub{counts: map[int64]int{1: 5, 2: 7}}
	svc := NewGroupCapacityService(
		accountRepo,
		groupRepo,
		NewConcurrencyService(concurrencyCache),
		sessionCache,
		rpmCache,
	)

	results, err := svc.GetAllGroupCapacity(context.Background())
	require.NoError(t, err)

	require.Equal(t, 1, groupRepo.listCalls)
	require.Equal(t, []int64{10, 20}, accountRepo.requested)
	require.Equal(t, []int64{1, 2}, concurrencyCache.requested)
	require.ElementsMatch(t, []int64{1, 2}, sessionCache.requested)
	require.ElementsMatch(t, []int64{1, 2}, rpmCache.requested)
	require.Equal(t, 7*time.Minute, sessionCache.idleTimeouts[1])
	require.Equal(t, 9*time.Minute, sessionCache.idleTimeouts[2])

	require.Equal(t, []GroupCapacitySummary{
		{
			GroupID:         10,
			ConcurrencyUsed: 1,
			ConcurrencyMax:  2,
			SessionsUsed:    2,
			SessionsMax:     3,
			RPMUsed:         5,
			RPMMax:          11,
		},
		{
			GroupID:         20,
			ConcurrencyUsed: 3,
			ConcurrencyMax:  6,
			SessionsUsed:    3,
			SessionsMax:     4,
			RPMUsed:         12,
			RPMMax:          24,
		},
	}, results)
}

func TestGetAllGroupCapacityBatchKeepsEmptyGroupRows(t *testing.T) {
	accountRepo := &groupCapacityAccountRepoStub{
		rows: []GroupAccountCapacityRow{
			{GroupID: 20, AccountID: 2, Concurrency: 4},
		},
	}
	groupRepo := &groupCapacityBatchGroupRepoStub{groupIDs: []int64{10, 20}}
	svc := NewGroupCapacityService(accountRepo, groupRepo, nil, nil, nil)

	results, err := svc.GetAllGroupCapacity(context.Background())
	require.NoError(t, err)

	require.Equal(t, []GroupCapacitySummary{
		{GroupID: 10},
		{GroupID: 20, ConcurrencyMax: 4},
	}, results)
}
