package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupRepositoryPersistsExtendedPolicyFields(t *testing.T) {
	apiKeyRepo, client := newAPIKeyRepoSQLite(t)
	repo := &groupRepository{client: client, sql: apiKeyRepo.sql}
	ctx := context.Background()

	group := &service.Group{
		Name:                         "extended-policy",
		Platform:                     service.PlatformGemini,
		RateMultiplier:               1,
		Status:                       service.StatusActive,
		Scope:                        service.GroupScopePublic,
		SubscriptionType:             service.SubscriptionTypeSubscription,
		PeakRateEnabled:              true,
		PeakStart:                    "09:00",
		PeakEnd:                      "18:00",
		PeakRateMultiplier:           1.75,
		AllowImageGeneration:         true,
		AllowBatchImageGeneration:    true,
		ImageRateIndependent:         true,
		ImageRateMultiplier:          1.25,
		BatchImageDiscountMultiplier: 0.55,
		BatchImageHoldMultiplier:     0.7,
		ModelsListConfig: service.GroupModelsListConfig{
			Enabled: true,
			Models:  []string{"gemini-2.5-flash-image", "gemini-3-pro-image-preview"},
		},
		DuplicateOperationID: "extended-policy-operation",
	}

	require.NoError(t, repo.Create(ctx, group))

	created, err := repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	assertExtendedGroupPolicyFields(t, created, group)

	recovered, err := repo.FindByDuplicateOperationID(ctx, group.DuplicateOperationID)
	require.NoError(t, err)
	require.NotNil(t, recovered)
	require.Equal(t, group.ID, recovered.ID)
	require.Equal(t, group.DuplicateOperationID, recovered.DuplicateOperationID)

	group.PeakStart = "10:00"
	group.PeakEnd = "20:00"
	group.PeakRateMultiplier = 2
	group.BatchImageDiscountMultiplier = 0.6
	group.BatchImageHoldMultiplier = 0.8
	group.ModelsListConfig = service.GroupModelsListConfig{
		Enabled: true,
		Models:  []string{"gemini-3-pro-image-preview"},
	}
	require.NoError(t, repo.Update(ctx, group))

	updated, err := repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	assertExtendedGroupPolicyFields(t, updated, group)
	require.Equal(t, group.DuplicateOperationID, updated.DuplicateOperationID)
}

func assertExtendedGroupPolicyFields(t *testing.T, actual, expected *service.Group) {
	t.Helper()
	require.NotNil(t, actual)
	require.Equal(t, expected.PeakRateEnabled, actual.PeakRateEnabled)
	require.Equal(t, expected.PeakStart, actual.PeakStart)
	require.Equal(t, expected.PeakEnd, actual.PeakEnd)
	require.InDelta(t, expected.PeakRateMultiplier, actual.PeakRateMultiplier, 1e-12)
	require.Equal(t, expected.AllowImageGeneration, actual.AllowImageGeneration)
	require.Equal(t, expected.AllowBatchImageGeneration, actual.AllowBatchImageGeneration)
	require.Equal(t, expected.ImageRateIndependent, actual.ImageRateIndependent)
	require.InDelta(t, expected.ImageRateMultiplier, actual.ImageRateMultiplier, 1e-12)
	require.InDelta(t, expected.BatchImageDiscountMultiplier, actual.BatchImageDiscountMultiplier, 1e-12)
	require.InDelta(t, expected.BatchImageHoldMultiplier, actual.BatchImageHoldMultiplier, 1e-12)
	require.Equal(t, expected.ModelsListConfig, actual.ModelsListConfig)
}
