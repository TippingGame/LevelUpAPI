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
		SetSubscriptionType(service.SubscriptionTypeSubscription).
		SetRequiredAccountLevel(service.AccountLevelPlus).
		SetRateMultiplier(1).
		SetOwnerUserID(user.ID).
		SetPeakRateEnabled(true).
		SetPeakStart("09:00").
		SetPeakEnd("18:00").
		SetPeakRateMultiplier(1.75).
		SetAllowImageGeneration(true).
		SetAllowBatchImageGeneration(true).
		SetImageRateIndependent(true).
		SetImageRateMultiplier(1.25).
		SetBatchImageDiscountMultiplier(0.55).
		SetBatchImageHoldMultiplier(0.65).
		SetRequireOauthOnly(true).
		SetRequirePrivacySet(true).
		SetModelsListConfig(service.GroupModelsListConfig{
			Enabled: true,
			Models:  []string{"gpt-5.4", "gpt-image-2"},
		}).
		Save(ctx)
	require.NoError(t, err)

	routeGroup, err := client.Group.Create().
		SetName("g-auth-media-route").
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRequiredAccountLevel(service.AccountLevelPro).
		SetRateMultiplier(1).
		SetOwnerUserID(user.ID).
		SetIsExclusive(true).
		SetPeakRateEnabled(true).
		SetPeakStart("10:00").
		SetPeakEnd("20:00").
		SetPeakRateMultiplier(2).
		SetAllowImageGeneration(true).
		SetAllowBatchImageGeneration(true).
		SetImageRateIndependent(true).
		SetImageRateMultiplier(1.5).
		SetBatchImageDiscountMultiplier(0.45).
		SetBatchImageHoldMultiplier(0.75).
		SetRequireOauthOnly(true).
		SetRequirePrivacySet(true).
		SetModelsListConfig(service.GroupModelsListConfig{
			Enabled: true,
			Models:  []string{"gpt-image-2"},
		}).
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
	require.True(t, got.Group.AllowBatchImageGeneration)
	require.True(t, got.Group.ImageRateIndependent)
	require.InDelta(t, 1.25, got.Group.ImageRateMultiplier, 1e-12)
	require.InDelta(t, 0.55, got.Group.BatchImageDiscountMultiplier, 1e-12)
	require.InDelta(t, 0.65, got.Group.BatchImageHoldMultiplier, 1e-12)
	require.Equal(t, service.AccountLevelPlus, got.Group.RequiredAccountLevel)
	require.True(t, got.Group.RequireOAuthOnly)
	require.True(t, got.Group.RequirePrivacySet)
	require.NotNil(t, got.Group.OwnerUserID)
	require.Equal(t, user.ID, *got.Group.OwnerUserID)
	require.True(t, got.Group.PeakRateEnabled)
	require.Equal(t, "09:00", got.Group.PeakStart)
	require.Equal(t, "18:00", got.Group.PeakEnd)
	require.InDelta(t, 1.75, got.Group.PeakRateMultiplier, 1e-12)
	require.Equal(t, []string{"gpt-5.4", "gpt-image-2"}, got.Group.ModelsListConfig.Models)
	require.Len(t, got.GroupRoutes, 1)
	require.NotNil(t, got.GroupRoutes[0].Group)
	require.True(t, got.GroupRoutes[0].Group.AllowImageGeneration)
	require.True(t, got.GroupRoutes[0].Group.AllowBatchImageGeneration)
	require.True(t, got.GroupRoutes[0].Group.ImageRateIndependent)
	require.InDelta(t, 1.5, got.GroupRoutes[0].Group.ImageRateMultiplier, 1e-12)
	require.InDelta(t, 0.45, got.GroupRoutes[0].Group.BatchImageDiscountMultiplier, 1e-12)
	require.InDelta(t, 0.75, got.GroupRoutes[0].Group.BatchImageHoldMultiplier, 1e-12)
	require.Equal(t, service.AccountLevelPro, got.GroupRoutes[0].Group.RequiredAccountLevel)
	require.True(t, got.GroupRoutes[0].Group.RequireOAuthOnly)
	require.True(t, got.GroupRoutes[0].Group.RequirePrivacySet)
	require.True(t, got.GroupRoutes[0].Group.IsExclusive)
	require.NotNil(t, got.GroupRoutes[0].Group.OwnerUserID)
	require.Equal(t, user.ID, *got.GroupRoutes[0].Group.OwnerUserID)
	require.True(t, got.GroupRoutes[0].Group.PeakRateEnabled)
	require.Equal(t, "10:00", got.GroupRoutes[0].Group.PeakStart)
	require.Equal(t, "20:00", got.GroupRoutes[0].Group.PeakEnd)
	require.InDelta(t, 2.0, got.GroupRoutes[0].Group.PeakRateMultiplier, 1e-12)
	require.Equal(t, []string{"gpt-image-2"}, got.GroupRoutes[0].Group.ModelsListConfig.Models)
	require.False(t, service.IsAPIKeyGroupRouteSelectable(got, got.GroupRoutes[0]))
}
