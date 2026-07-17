package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildSchedulerMetadataAccountPreservesPublicShareVisibility(t *testing.T) {
	ownerID := int64(42)
	consumerID := int64(99)
	proxyID := int64(7)
	account := service.Account{
		ID:           101,
		Platform:     service.PlatformOpenAI,
		Type:         service.AccountTypeOAuth,
		AccountLevel: service.AccountLevelPro,
		OwnerUserID:  &ownerID,
		ShareMode:    service.AccountShareModePublic,
		ShareStatus:  service.AccountShareStatusApproved,
		ProxyID:      &proxyID,
		Proxy:        &service.Proxy{ID: proxyID, Status: service.StatusActive},
		Status:       service.StatusActive,
		Schedulable:  true,
		Concurrency:  1,
		Priority:     10,
	}

	metadata := buildSchedulerMetadataAccount(account)
	ctx := context.WithValue(context.Background(), ctxkey.AuthenticatedUserID, consumerID)

	require.Equal(t, service.AccountShareModePublic, metadata.ShareMode)
	require.Equal(t, service.AccountShareStatusApproved, metadata.ShareStatus)
	require.True(t, metadata.IsVisibleToConsumer(consumerID))
	require.True(t, service.IsAccountVisibleToRequestUser(ctx, &metadata))
	require.True(t, metadata.IsSchedulable())
	require.NotNil(t, metadata.Proxy)
	require.Equal(t, service.StatusActive, metadata.Proxy.Status)
}

func TestBuildSchedulerMetadataAccountPreservesGrokMediaEligibility(t *testing.T) {
	account := service.Account{
		ID:       102,
		Platform: service.PlatformGrok,
		Type:     service.AccountTypeOAuth,
		Extra: map[string]any{
			service.GrokMediaEligibleExtraKey: false,
			"grok_billing_snapshot": map[string]any{
				"status_code":         403,
				"weekly_status_code":  200,
				"monthly_status_code": 403,
			},
			"unused_large_field": "drop-me",
		},
	}

	metadata := buildSchedulerMetadataAccount(account)

	require.Equal(t, false, metadata.Extra[service.GrokMediaEligibleExtraKey])
	require.Equal(t, account.Extra["grok_billing_snapshot"], metadata.Extra["grok_billing_snapshot"])
	require.NotContains(t, metadata.Extra, "unused_large_field")
}
