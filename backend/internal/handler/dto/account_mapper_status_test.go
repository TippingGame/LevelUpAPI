package dto

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountFromServiceShallow_HidesIgnoredPoolModeLocalState(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	future := now.Add(time.Hour)
	rateLimitedAt := now.Add(-time.Minute)
	out := AccountFromServiceShallow(&service.Account{
		ID:                      1,
		Platform:                service.PlatformOpenAI,
		Type:                    service.AccountTypeAPIKey,
		Status:                  service.StatusActive,
		Schedulable:             true,
		Credentials:             map[string]any{"pool_mode": true},
		RateLimitedAt:           &rateLimitedAt,
		RateLimitResetAt:        &future,
		OverloadUntil:           &future,
		TempUnschedulableUntil:  &future,
		TempUnschedulableReason: "pool upstream transient",
	})

	require.NotNil(t, out)
	require.Nil(t, out.RateLimitedAt)
	require.Nil(t, out.RateLimitResetAt)
	require.Nil(t, out.OverloadUntil)
	require.Nil(t, out.TempUnschedulableUntil)
	require.Empty(t, out.TempUnschedulableReason)
}

func TestAccountFromServiceShallow_KeepsEffectiveOAuthLocalState(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	future := now.Add(time.Hour)
	rateLimitedAt := now.Add(-time.Minute)
	out := AccountFromServiceShallow(&service.Account{
		ID:                      2,
		Platform:                service.PlatformOpenAI,
		Type:                    service.AccountTypeOAuth,
		Status:                  service.StatusActive,
		Schedulable:             true,
		RateLimitedAt:           &rateLimitedAt,
		RateLimitResetAt:        &future,
		OverloadUntil:           &future,
		TempUnschedulableUntil:  &future,
		TempUnschedulableReason: "oauth cooling down",
	})

	require.NotNil(t, out)
	require.NotNil(t, out.RateLimitedAt)
	require.NotNil(t, out.RateLimitResetAt)
	require.NotNil(t, out.OverloadUntil)
	require.NotNil(t, out.TempUnschedulableUntil)
	require.Equal(t, "oauth cooling down", out.TempUnschedulableReason)
}

func TestAccountFromServiceShallow_HidesIgnoredOpenAIOAuthRelayPoolTempState(t *testing.T) {
	t.Parallel()

	future := time.Now().UTC().Add(time.Hour)
	out := AccountFromServiceShallow(&service.Account{
		ID:                      3,
		Platform:                service.PlatformOpenAI,
		Type:                    service.AccountTypeOAuth,
		Status:                  service.StatusActive,
		Schedulable:             true,
		TempUnschedulableUntil:  &future,
		TempUnschedulableReason: `{"matched_keyword":"upstream_relay_pool_unavailable","error_message":"No available accounts"}`,
	})

	require.NotNil(t, out)
	require.Nil(t, out.TempUnschedulableUntil)
	require.Empty(t, out.TempUnschedulableReason)
}

func TestAccountFromServiceShallow_UsesEffectiveOpenAILevelForOAuthDisplay(t *testing.T) {
	t.Parallel()

	out := AccountFromServiceShallow(&service.Account{
		ID:           4,
		Platform:     service.PlatformOpenAI,
		Type:         service.AccountTypeOAuth,
		AccountLevel: service.AccountLevelPlus,
		Credentials:  map[string]any{"plan_type": "chatgpt_pro"},
		Status:       service.StatusActive,
		Schedulable:  true,
	})

	require.NotNil(t, out)
	require.Equal(t, service.AccountLevelPro, out.AccountLevel)
}

func TestAccountFromServiceShallow_KeepsOpenAIAPIKeyStoredLevel(t *testing.T) {
	t.Parallel()

	out := AccountFromServiceShallow(&service.Account{
		ID:           5,
		Platform:     service.PlatformOpenAI,
		Type:         service.AccountTypeAPIKey,
		AccountLevel: service.AccountLevelPlus,
		Credentials:  map[string]any{"plan_type": "chatgpt_pro"},
		Status:       service.StatusActive,
		Schedulable:  true,
	})

	require.NotNil(t, out)
	require.Equal(t, service.AccountLevelPlus, out.AccountLevel)
}
