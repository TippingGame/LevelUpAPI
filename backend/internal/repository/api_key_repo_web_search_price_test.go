package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepositoryGetByKeyForAuthLoadsWebSearchPriceForPrimaryAndRouteGroups(t *testing.T) {
	repository, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "getbykey-auth-web-search@test.com")

	primaryPrice := 0.0
	primaryGroup, err := client.Group.Create().
		SetName("g-auth-web-search-primary").
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		SetWebSearchPricePerCall(primaryPrice).
		Save(ctx)
	require.NoError(t, err)

	routePrice := 0.027
	routeGroup, err := client.Group.Create().
		SetName("g-auth-web-search-route").
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		SetWebSearchPricePerCall(routePrice).
		Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{
		UserID:  user.ID,
		Key:     "sk-getbykey-auth-web-search",
		Name:    "Web Search Auth Key",
		GroupID: &primaryGroup.ID,
		Status:  service.StatusActive,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: routeGroup.ID, Priority: 1, Weight: 100, Enabled: true},
		},
	}
	require.NoError(t, repository.Create(ctx, key))

	got, err := repository.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.NotNil(t, got.Group)
	require.NotNil(t, got.Group.WebSearchPricePerCall)
	require.Zero(t, *got.Group.WebSearchPricePerCall)
	require.Len(t, got.GroupRoutes, 1)
	require.NotNil(t, got.GroupRoutes[0].Group)
	require.NotNil(t, got.GroupRoutes[0].Group.WebSearchPricePerCall)
	require.InDelta(t, routePrice, *got.GroupRoutes[0].Group.WebSearchPricePerCall, 1e-12)
}
