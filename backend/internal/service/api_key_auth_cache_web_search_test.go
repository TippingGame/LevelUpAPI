package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotRoundTripsWebSearchPriceForPrimaryAndRouteGroups(t *testing.T) {
	primaryGroupID := int64(10)
	routeGroupID := int64(20)
	primaryPrice := 0.0
	routePrice := 0.027
	apiKey := &APIKey{
		ID:      1,
		UserID:  2,
		GroupID: &primaryGroupID,
		Key:     "sk-web-search-snapshot",
		Status:  StatusActive,
		User: &User{
			ID:     2,
			Status: StatusActive,
			Role:   RoleUser,
		},
		Group: &Group{
			ID:                    primaryGroupID,
			Name:                  "primary",
			Platform:              PlatformOpenAI,
			Status:                StatusActive,
			SubscriptionType:      SubscriptionTypeStandard,
			RateMultiplier:        1,
			WebSearchPricePerCall: &primaryPrice,
		},
		GroupRoutes: []APIKeyGroupRoute{
			{
				ID:      3,
				GroupID: routeGroupID,
				Enabled: true,
				Group: &Group{
					ID:                    routeGroupID,
					Name:                  "route",
					Platform:              PlatformOpenAI,
					Status:                StatusActive,
					SubscriptionType:      SubscriptionTypeStandard,
					RateMultiplier:        1,
					WebSearchPricePerCall: &routePrice,
				},
			},
		},
	}

	service := &APIKeyService{}
	snapshot := service.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.Equal(t, apiKeyAuthSnapshotVersion, snapshot.Version)
	require.NotNil(t, snapshot.Group)
	require.NotNil(t, snapshot.Group.WebSearchPricePerCall)
	require.Zero(t, *snapshot.Group.WebSearchPricePerCall)
	require.Len(t, snapshot.GroupRoutes, 1)
	require.NotNil(t, snapshot.GroupRoutes[0].Group)
	require.NotNil(t, snapshot.GroupRoutes[0].Group.WebSearchPricePerCall)
	require.InDelta(t, routePrice, *snapshot.GroupRoutes[0].Group.WebSearchPricePerCall, 1e-12)

	roundTrip := service.snapshotToAPIKey(apiKey.Key, snapshot)
	require.NotNil(t, roundTrip)
	require.NotNil(t, roundTrip.Group)
	require.NotNil(t, roundTrip.Group.WebSearchPricePerCall)
	require.Zero(t, *roundTrip.Group.WebSearchPricePerCall)
	require.Len(t, roundTrip.GroupRoutes, 1)
	require.NotNil(t, roundTrip.GroupRoutes[0].Group)
	require.NotNil(t, roundTrip.GroupRoutes[0].Group.WebSearchPricePerCall)
	require.InDelta(t, routePrice, *roundTrip.GroupRoutes[0].Group.WebSearchPricePerCall, 1e-12)
}
