//go:build unit

package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildSchedulerMetadataAccount_KeepsOpenAIWSFlags(t *testing.T) {
	account := service.Account{
		ID:       42,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Extra: map[string]any{
			"openai_oauth_responses_websockets_v2_enabled": true,
			"openai_oauth_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
			"openai_ws_force_http":                         true,
			"openai_responses_mode":                        "force_chat_completions",
			"openai_responses_supported":                   false,
			"mixed_scheduling":                             true,
			"unused_large_field":                           "drop-me",
		},
	}

	got := buildSchedulerMetadataAccount(account)

	require.Equal(t, true, got.Extra["openai_oauth_responses_websockets_v2_enabled"])
	require.Equal(t, service.OpenAIWSIngressModePassthrough, got.Extra["openai_oauth_responses_websockets_v2_mode"])
	require.Equal(t, true, got.Extra["openai_ws_force_http"])
	require.Equal(t, "force_chat_completions", got.Extra["openai_responses_mode"])
	require.Equal(t, false, got.Extra["openai_responses_supported"])
	require.Equal(t, true, got.Extra["mixed_scheduling"])
	require.Nil(t, got.Extra["unused_large_field"])
}

func TestBuildSchedulerMetadataAccount_KeepsSchedulingProtectionExtra(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	resetAt := now.Add(30 * time.Minute).Format(time.RFC3339)
	account := service.Account{
		ID:          42,
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"privacy_mode":               service.PrivacyModeTrainingOff,
			"model_rate_limits":          map[string]any{"gpt-4o": map[string]any{"rate_limit_reset_at": resetAt}},
			"quota_limit":                100.0,
			"quota_used":                 100.0,
			"quota_daily_limit":          10.0,
			"quota_daily_used":           9.0,
			"quota_daily_start":          now.Add(-1 * time.Hour).Format(time.RFC3339),
			"quota_weekly_limit":         50.0,
			"quota_weekly_used":          20.0,
			"quota_weekly_start":         now.Add(-24 * time.Hour).Format(time.RFC3339),
			"base_rpm":                   5,
			"rpm_strategy":               "sticky_exempt",
			"rpm_sticky_buffer":          3,
			"user_msg_queue_mode":        "throttle",
			"anthropic_passthrough":      true,
			"openai_passthrough":         true,
			"openai_compact_mode":        service.OpenAICompactModeForceOn,
			"openai_compact_supported":   false,
			"enable_tls_fingerprint":     true,
			"tls_fingerprint_profile_id": 11,
			"session_id_masking_enabled": true,
			"custom_base_url_enabled":    true,
			"custom_base_url":            "https://relay.example.com",
			"cache_ttl_override_enabled": true,
			"cache_ttl_override_target":  "1h",
			"unused_large_field":         "drop-me",
		},
	}

	got := buildSchedulerMetadataAccount(account)

	require.True(t, got.IsPrivacySet())
	require.True(t, got.IsQuotaExceededAt(now))
	require.Greater(t, got.GetModelRateLimitRemainingTime("gpt-4o"), time.Duration(0))
	require.Equal(t, 5, got.GetBaseRPM())
	require.Equal(t, service.WindowCostStickyOnly, got.CheckRPMSchedulability(5))
	require.Equal(t, "throttle", got.GetUserMsgQueueMode())
	require.Equal(t, true, got.Extra["anthropic_passthrough"])
	require.True(t, got.IsOpenAIPassthroughEnabled())
	require.Equal(t, service.OpenAICompactModeForceOn, got.GetOpenAICompactMode())
	require.Equal(t, false, got.Extra["openai_compact_supported"])
	require.Equal(t, true, got.Extra["enable_tls_fingerprint"])
	require.Equal(t, 11, got.Extra["tls_fingerprint_profile_id"])
	require.Equal(t, true, got.Extra["session_id_masking_enabled"])
	require.Equal(t, true, got.Extra["custom_base_url_enabled"])
	require.Equal(t, "https://relay.example.com", got.Extra["custom_base_url"])
	require.Equal(t, true, got.Extra["cache_ttl_override_enabled"])
	require.Equal(t, "1h", got.Extra["cache_ttl_override_target"])
	require.Nil(t, got.Extra["unused_large_field"])
}

func TestBuildSchedulerMetadataAccount_KeepsSlimGroupMembership(t *testing.T) {
	account := service.Account{
		ID:       42,
		Platform: service.PlatformAnthropic,
		GroupIDs: []int64{7, 9, 7, 0},
		AccountGroups: []service.AccountGroup{
			{
				AccountID: 42,
				GroupID:   7,
				Priority:  2,
				Account:   &service.Account{ID: 42, Name: "drop-from-metadata"},
				Group:     &service.Group{ID: 7, Name: "drop-from-metadata"},
			},
			{
				AccountID: 42,
				GroupID:   11,
				Priority:  3,
				Group:     &service.Group{ID: 11, Name: "drop-from-metadata"},
			},
			{
				AccountID: 42,
				GroupID:   0,
				Priority:  4,
			},
		},
	}

	got := buildSchedulerMetadataAccount(account)

	require.Equal(t, []int64{7, 9, 11}, got.GroupIDs)
	require.Len(t, got.AccountGroups, 2)
	require.Equal(t, int64(42), got.AccountGroups[0].AccountID)
	require.Equal(t, int64(7), got.AccountGroups[0].GroupID)
	require.Equal(t, 2, got.AccountGroups[0].Priority)
	require.Nil(t, got.AccountGroups[0].Account)
	require.Nil(t, got.AccountGroups[0].Group)
	require.Equal(t, int64(11), got.AccountGroups[1].GroupID)
	require.Nil(t, got.Groups)
}

func TestBuildSchedulerMetadataAccount_KeepsProxySchedulingState(t *testing.T) {
	ownerID := int64(101)
	proxyID := int64(7)
	account := service.Account{
		ID:           42,
		Platform:     service.PlatformAnthropic,
		AccountLevel: service.AccountLevelUnknown,
		Type:         service.AccountTypeOAuth,
		OwnerUserID:  &ownerID,
		ProxyID:      &proxyID,
		Proxy: &service.Proxy{
			ID:       proxyID,
			Status:   service.StatusActive,
			Host:     "127.0.0.1",
			Port:     1080,
			Username: "secret-user",
			Password: "secret-pass",
		},
		Status:      service.StatusActive,
		Schedulable: true,
	}

	got := buildSchedulerMetadataAccount(account)

	require.Equal(t, account.AccountLevel, got.AccountLevel)
	require.NotNil(t, got.OwnerUserID)
	require.Equal(t, ownerID, *got.OwnerUserID)
	require.NotNil(t, got.ProxyID)
	require.Equal(t, proxyID, *got.ProxyID)
	require.NotNil(t, got.Proxy)
	require.Equal(t, proxyID, got.Proxy.ID)
	require.Equal(t, service.StatusActive, got.Proxy.Status)
	require.Empty(t, got.Proxy.Host)
	require.Empty(t, got.Proxy.Username)
	require.Empty(t, got.Proxy.Password)
	require.True(t, got.IsSchedulable())
}
