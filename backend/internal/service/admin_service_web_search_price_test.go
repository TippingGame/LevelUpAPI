//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminServiceCreateGroupNormalizesWebSearchPrice(t *testing.T) {
	tests := []struct {
		name      string
		price     *float64
		wantNil   bool
		wantPrice float64
	}{
		{name: "unset uses default", wantNil: true},
		{name: "negative uses default", price: webSearchFloat64Ptr(-1), wantNil: true},
		{name: "zero stays free", price: webSearchFloat64Ptr(0), wantPrice: 0},
		{name: "override preserved", price: webSearchFloat64Ptr(0.023), wantPrice: 0.023},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &groupRepoStubForAdmin{}
			service := &adminServiceImpl{groupRepo: repository}
			_, err := service.CreateGroup(context.Background(), &CreateGroupInput{
				Name:                  "web-search-price",
				Platform:              PlatformOpenAI,
				RateMultiplier:        1,
				WebSearchPricePerCall: tt.price,
			})
			require.NoError(t, err)
			require.NotNil(t, repository.created)
			if tt.wantNil {
				require.Nil(t, repository.created.WebSearchPricePerCall)
				return
			}
			require.NotNil(t, repository.created.WebSearchPricePerCall)
			require.InDelta(t, tt.wantPrice, *repository.created.WebSearchPricePerCall, 1e-12)
		})
	}
}

func TestAdminServiceUpdateGroupCanClearWebSearchPrice(t *testing.T) {
	existingPrice := 0.02
	clearPrice := -1.0
	repository := &groupRepoStubForAdmin{getByID: &Group{
		ID:                    1,
		Name:                  "web-search-price",
		Platform:              PlatformOpenAI,
		Status:                StatusActive,
		RateMultiplier:        1,
		WebSearchPricePerCall: &existingPrice,
	}}
	service := &adminServiceImpl{groupRepo: repository}

	updated, err := service.UpdateGroup(context.Background(), 1, &UpdateGroupInput{
		WebSearchPricePerCall: &clearPrice,
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Nil(t, updated.WebSearchPricePerCall)
	require.NotNil(t, repository.updated)
	require.Nil(t, repository.updated.WebSearchPricePerCall)
}
