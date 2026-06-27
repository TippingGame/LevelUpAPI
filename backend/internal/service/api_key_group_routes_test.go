package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type apiKeyGroupRouteGroupRepoStub struct {
	groups map[int64]*Group
}

func (s *apiKeyGroupRouteGroupRepoStub) Create(context.Context, *Group) error {
	panic("unexpected Create call")
}
func (s *apiKeyGroupRouteGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	if group, ok := s.groups[id]; ok {
		clone := *group
		return &clone, nil
	}
	return nil, ErrGroupNotFound
}
func (s *apiKeyGroupRouteGroupRepoStub) GetByIDLite(ctx context.Context, id int64) (*Group, error) {
	return s.GetByID(ctx, id)
}
func (s *apiKeyGroupRouteGroupRepoStub) Update(context.Context, *Group) error {
	panic("unexpected Update call")
}
func (s *apiKeyGroupRouteGroupRepoStub) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}
func (s *apiKeyGroupRouteGroupRepoStub) DeleteCascade(context.Context, int64) ([]int64, error) {
	panic("unexpected DeleteCascade call")
}
func (s *apiKeyGroupRouteGroupRepoStub) List(context.Context, pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (s *apiKeyGroupRouteGroupRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}
func (s *apiKeyGroupRouteGroupRepoStub) ListActive(context.Context) ([]Group, error) {
	panic("unexpected ListActive call")
}
func (s *apiKeyGroupRouteGroupRepoStub) ListActiveByPlatform(context.Context, string) ([]Group, error) {
	panic("unexpected ListActiveByPlatform call")
}
func (s *apiKeyGroupRouteGroupRepoStub) ExistsByName(context.Context, string) (bool, error) {
	panic("unexpected ExistsByName call")
}
func (s *apiKeyGroupRouteGroupRepoStub) GetAccountCount(context.Context, int64) (int64, int64, error) {
	panic("unexpected GetAccountCount call")
}
func (s *apiKeyGroupRouteGroupRepoStub) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected DeleteAccountGroupsByGroupID call")
}
func (s *apiKeyGroupRouteGroupRepoStub) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	panic("unexpected GetAccountIDsByGroupIDs call")
}
func (s *apiKeyGroupRouteGroupRepoStub) BindAccountsToGroup(context.Context, int64, []int64) error {
	panic("unexpected BindAccountsToGroup call")
}
func (s *apiKeyGroupRouteGroupRepoStub) UpdateSortOrders(context.Context, []GroupSortOrderUpdate) error {
	panic("unexpected UpdateSortOrders call")
}

func TestAPIKeyServiceValidateAPIKeyGroupRoutesRequiresSamePlatform(t *testing.T) {
	user := &User{ID: 7}
	svc := &APIKeyService{groupRepo: &apiKeyGroupRouteGroupRepoStub{groups: map[int64]*Group{
		1: &Group{ID: 1, Platform: PlatformAnthropic, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard},
		2: &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard},
	}}}

	err := svc.validateAPIKeyGroupRoutes(context.Background(), user, []APIKeyGroupRoute{
		{GroupID: 1, Enabled: true},
		{GroupID: 2, Enabled: true},
	})

	require.ErrorIs(t, err, ErrAPIKeyGroupRouteInvalid)
}

func TestNormalizeAPIKeyGroupRoutePriorityPreservesInputOrder(t *testing.T) {
	routes := []APIKeyGroupRoute{
		{GroupID: 1, Priority: 10, Weight: 9, Enabled: true, Group: &Group{ID: 1, RateMultiplier: 1.2}},
		{GroupID: 2, Priority: 20, Weight: 1, Enabled: true, Group: &Group{ID: 2, RateMultiplier: 0.8}},
		{GroupID: 3, Priority: 30, Weight: 1, Enabled: true, Group: &Group{ID: 3, RateMultiplier: 2.0}},
	}

	normalizeAPIKeyGroupRoutePriority(routes)

	require.Equal(t, []int64{1, 2, 3}, []int64{routes[0].GroupID, routes[1].GroupID, routes[2].GroupID})
	require.Equal(t, []int{100, 200, 300}, []int{routes[0].Priority, routes[1].Priority, routes[2].Priority})
	require.Equal(t, []int{1, 1, 1}, []int{routes[0].Weight, routes[1].Weight, routes[2].Weight})
}
