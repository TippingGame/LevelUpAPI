package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyAnthropicOAuthPoolProtectionDefaults(t *testing.T) {
	credentials, extra := applyAnthropicOAuthPoolProtectionDefaults(
		PlatformAnthropic,
		AccountTypeOAuth,
		nil,
		nil,
	)

	account := &Account{
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Credentials: credentials,
		Extra:       extra,
		Concurrency: 3,
	}

	require.True(t, account.IsTempUnschedulableEnabled())
	rules := account.GetTempUnschedulableRules()
	require.Len(t, rules, 1)
	require.Equal(t, 529, rules[0].ErrorCode)
	require.Equal(t, anthropicOAuthDefaultCooldownMinutes, rules[0].DurationMinutes)
	require.Equal(t, anthropicOAuthDefaultMaxSessions, account.GetMaxSessions())
	require.Equal(t, anthropicOAuthDefaultSessionIdleTimeoutMinutes, account.GetSessionIdleTimeoutMinutes())
	require.Equal(t, anthropicOAuthDefaultBaseRPM, account.GetBaseRPM())
	require.Equal(t, anthropicOAuthDefaultRPMStrategy, account.GetRPMStrategy())
	require.Equal(t, "serialize", account.GetUserMsgQueueMode())
}

func TestApplyAnthropicOAuthPoolProtectionDefaultsPreservesOverrides(t *testing.T) {
	customRules := []any{
		map[string]any{
			"error_code":       500,
			"keywords":         []any{"custom"},
			"duration_minutes": 7,
		},
	}
	credentials, extra := applyAnthropicOAuthPoolProtectionDefaults(
		PlatformAnthropic,
		AccountTypeSetupToken,
		map[string]any{
			"temp_unschedulable_enabled": false,
			"temp_unschedulable_rules":   customRules,
		},
		map[string]any{
			"max_sessions":                 5,
			"session_idle_timeout_minutes": 15,
			"base_rpm":                     20,
			"rpm_strategy":                 "tiered",
			"user_msg_queue_mode":          "throttle",
		},
	)

	account := &Account{
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Credentials: credentials,
		Extra:       extra,
	}

	require.False(t, account.IsTempUnschedulableEnabled())
	require.Equal(t, customRules, credentials["temp_unschedulable_rules"])
	require.Equal(t, 5, account.GetMaxSessions())
	require.Equal(t, 15, account.GetSessionIdleTimeoutMinutes())
	require.Equal(t, 20, account.GetBaseRPM())
	require.Equal(t, "tiered", account.GetRPMStrategy())
	require.Equal(t, "throttle", account.GetUserMsgQueueMode())
}

func TestApplyAnthropicOAuthPoolProtectionDefaultsSkipsAPIKey(t *testing.T) {
	credentials := map[string]any{"api_key": "sk-test"}
	extra := map[string]any{"base_rpm": 9}

	gotCredentials, gotExtra := applyAnthropicOAuthPoolProtectionDefaults(
		PlatformAnthropic,
		AccountTypeAPIKey,
		credentials,
		extra,
	)

	require.Equal(t, credentials, gotCredentials)
	require.Equal(t, extra, gotExtra)
	require.Len(t, gotCredentials, 1)
	require.Len(t, gotExtra, 1)
}
