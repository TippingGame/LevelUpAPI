package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBillingServiceCalculateWebSearchCost(t *testing.T) {
	tests := []struct {
		name           string
		callCount      int
		groupPrice     *float64
		multiplier     float64
		wantTotalCost  float64
		wantActualCost float64
	}{
		{name: "default price", callCount: 2, multiplier: 1, wantTotalCost: 0.02, wantActualCost: 0.02},
		{name: "group override", callCount: 3, groupPrice: webSearchFloat64Ptr(0.025), multiplier: 1.2, wantTotalCost: 0.075, wantActualCost: 0.09},
		{name: "free override", callCount: 1, groupPrice: webSearchFloat64Ptr(0), multiplier: 9, wantTotalCost: 0, wantActualCost: 0},
		{name: "negative price falls back", callCount: 1, groupPrice: webSearchFloat64Ptr(-1), multiplier: 2, wantTotalCost: 0.01, wantActualCost: 0.02},
		{name: "negative multiplier clamps to zero", callCount: 1, multiplier: -1, wantTotalCost: 0.01, wantActualCost: 0},
		{name: "no calls", callCount: 0, multiplier: 1, wantTotalCost: 0, wantActualCost: 0},
	}

	service := &BillingService{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := service.CalculateWebSearchCost(tt.callCount, tt.groupPrice, tt.multiplier)
			require.NotNil(t, cost)
			require.InDelta(t, tt.wantTotalCost, cost.TotalCost, 1e-12)
			require.InDelta(t, tt.wantActualCost, cost.ActualCost, 1e-12)
			if tt.callCount > 0 {
				require.Equal(t, string(BillingModePerRequest), cost.BillingMode)
			}
		})
	}
}

func TestCalculateOpenAIRecordUsageCostPrioritizesWebSearchPerCallBilling(t *testing.T) {
	price := 0.025
	service := &OpenAIGatewayService{billingService: &BillingService{}}
	cost, err := service.calculateOpenAIRecordUsageCost(
		context.Background(),
		&OpenAIForwardResult{WebSearchCalls: 2, ImageCount: 3},
		&APIKey{Group: &Group{WebSearchPricePerCall: &price}},
		"unpriced-search-model",
		1.5,
		1.5,
		1.5,
		1.5,
		UsageTokens{InputTokens: 999, OutputTokens: 999},
		"",
	)

	require.NoError(t, err)
	require.NotNil(t, cost)
	require.Equal(t, string(BillingModePerRequest), cost.BillingMode)
	require.InDelta(t, 0.05, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.075, cost.ActualCost, 1e-12)
}

func TestOpenAIGatewayServiceRecordUsageBillsWebSearchPerCall(t *testing.T) {
	groupID := int64(42)
	groupPrice := 0.025
	usageRepository := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepository := &openAIRecordUsageUserRepoStub{}
	service := newOpenAIRecordUsageServiceForTest(usageRepository, userRepository, &openAIRecordUsageSubRepoStub{}, nil)

	err := service.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:      "alpha-search-billing",
			Model:          "gpt-5.4",
			WebSearchCalls: 1,
			Duration:       time.Second,
		},
		APIKey: &APIKey{
			ID:      1001,
			GroupID: &groupID,
			Group: &Group{
				ID:                    groupID,
				Platform:              PlatformOpenAI,
				RateMultiplier:        2,
				WebSearchPricePerCall: &groupPrice,
			},
		},
		User:    &User{ID: 2001},
		Account: &Account{ID: 3001},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepository.lastLog)
	require.NotNil(t, usageRepository.lastLog.BillingMode)
	require.Equal(t, string(BillingModePerRequest), *usageRepository.lastLog.BillingMode)
	require.InDelta(t, 0.025, usageRepository.lastLog.TotalCost, 1e-12)
	require.InDelta(t, 0.05, usageRepository.lastLog.ActualCost, 1e-12)
	require.InDelta(t, 0.05, userRepository.lastAmount, 1e-12)
}

func webSearchFloat64Ptr(value float64) *float64 {
	return &value
}
