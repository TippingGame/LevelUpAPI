package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotRoundTripsVideoPricingForPrimaryAndRouteGroups(t *testing.T) {
	primaryGroupID := int64(30)
	routeGroupID := int64(40)
	primaryPrice := 0.07
	routePrice := 0.25
	apiKey := &APIKey{
		ID:      1,
		UserID:  2,
		GroupID: &primaryGroupID,
		Key:     "sk-video-snapshot",
		Status:  StatusActive,
		User:    &User{ID: 2, Status: StatusActive, Role: RoleUser},
		Group: &Group{
			ID:                   primaryGroupID,
			Name:                 "primary",
			Platform:             PlatformGrok,
			Status:               StatusActive,
			SubscriptionType:     SubscriptionTypeStandard,
			RateMultiplier:       1,
			VideoRateIndependent: true,
			VideoRateMultiplier:  0.8,
			VideoPrice720P:       &primaryPrice,
		},
		GroupRoutes: []APIKeyGroupRoute{
			{
				ID:      3,
				GroupID: routeGroupID,
				Enabled: true,
				Group: &Group{
					ID:                   routeGroupID,
					Name:                 "route",
					Platform:             PlatformGrok,
					Status:               StatusActive,
					SubscriptionType:     SubscriptionTypeStandard,
					RateMultiplier:       1,
					VideoRateIndependent: true,
					VideoRateMultiplier:  1.2,
					VideoPrice1080P:      &routePrice,
				},
			},
		},
	}

	service := &APIKeyService{}
	snapshot := service.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.Equal(t, apiKeyAuthSnapshotVersion, snapshot.Version)
	require.NotNil(t, snapshot.Group)
	require.True(t, snapshot.Group.VideoRateIndependent)
	require.InDelta(t, 0.8, snapshot.Group.VideoRateMultiplier, 1e-12)
	require.InDelta(t, primaryPrice, *snapshot.Group.VideoPrice720P, 1e-12)
	require.Len(t, snapshot.GroupRoutes, 1)
	require.NotNil(t, snapshot.GroupRoutes[0].Group)
	require.InDelta(t, routePrice, *snapshot.GroupRoutes[0].Group.VideoPrice1080P, 1e-12)

	roundTrip := service.snapshotToAPIKey(apiKey.Key, snapshot)
	require.NotNil(t, roundTrip)
	require.NotNil(t, roundTrip.Group)
	require.True(t, roundTrip.Group.VideoRateIndependent)
	require.InDelta(t, 0.8, roundTrip.Group.VideoRateMultiplier, 1e-12)
	require.InDelta(t, primaryPrice, *roundTrip.Group.VideoPrice720P, 1e-12)
	require.Len(t, roundTrip.GroupRoutes, 1)
	require.NotNil(t, roundTrip.GroupRoutes[0].Group)
	require.InDelta(t, 1.2, roundTrip.GroupRoutes[0].Group.VideoRateMultiplier, 1e-12)
	require.InDelta(t, routePrice, *roundTrip.GroupRoutes[0].Group.VideoPrice1080P, 1e-12)
}
