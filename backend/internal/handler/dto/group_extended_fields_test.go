package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupFromServiceAdminPreservesExtendedPolicyFields(t *testing.T) {
	group := &service.Group{
		ID:                           7,
		Name:                         "extended-policy",
		AllowImageGeneration:         true,
		AllowBatchImageGeneration:    true,
		BatchImageDiscountMultiplier: 0.55,
		BatchImageHoldMultiplier:     0.7,
		PeakRateEnabled:              true,
		PeakStart:                    "09:00",
		PeakEnd:                      "18:00",
		PeakRateMultiplier:           1.75,
		ModelsListConfig: service.GroupModelsListConfig{
			Enabled: true,
			Models:  []string{"gpt-5.4", "gpt-image-2"},
		},
	}

	got := GroupFromServiceAdmin(group)

	require.NotNil(t, got)
	require.True(t, got.AllowImageGeneration)
	require.True(t, got.AllowBatchImageGeneration)
	require.InDelta(t, 0.55, got.BatchImageDiscountMultiplier, 1e-12)
	require.InDelta(t, 0.7, got.BatchImageHoldMultiplier, 1e-12)
	require.True(t, got.PeakRateEnabled)
	require.Equal(t, "09:00", got.PeakStart)
	require.Equal(t, "18:00", got.PeakEnd)
	require.InDelta(t, 1.75, got.PeakRateMultiplier, 1e-12)
	require.Equal(t, group.ModelsListConfig, got.ModelsListConfig)
}
