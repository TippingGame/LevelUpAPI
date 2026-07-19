package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepositoryGetByKeyForAuthLoadsMediaControlsForPrimaryAndRouteGroups(t *testing.T) {
	repository, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "getbykey-auth-media@test.com")

	primaryGroup, err := client.Group.Create().
		SetName("g-auth-media-primary").
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		SetAllowImageGeneration(true).
		SetImageRateIndependent(true).
		SetImageRateMultiplier(1.25).
		Save(ctx)
	require.NoError(t, err)

	routeGroup, err := client.Group.Create().
		SetName("g-auth-media-route").
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		SetAllowImageGeneration(true).
		SetImageRateIndependent(true).
		SetImageRateMultiplier(1.5).
		Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{
		UserID:  user.ID,
		Key:     "sk-getbykey-auth-media",
		Name:    "Media Auth Key",
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
	require.True(t, got.Group.AllowImageGeneration)
	require.True(t, got.Group.ImageRateIndependent)
	require.InDelta(t, 1.25, got.Group.ImageRateMultiplier, 1e-12)
	require.Len(t, got.GroupRoutes, 1)
	require.NotNil(t, got.GroupRoutes[0].Group)
	require.True(t, got.GroupRoutes[0].Group.AllowImageGeneration)
	require.True(t, got.GroupRoutes[0].Group.ImageRateIndependent)
	require.InDelta(t, 1.5, got.GroupRoutes[0].Group.ImageRateMultiplier, 1e-12)
}
