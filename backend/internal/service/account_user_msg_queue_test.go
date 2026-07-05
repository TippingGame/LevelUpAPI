package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAccountGetUserMsgQueueModeDefaultsAnthropicOAuthToSerialize(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]any
	}{
		{name: "nil extra", extra: nil},
		{name: "empty extra", extra: map[string]any{}},
		{name: "invalid mode", extra: map[string]any{"user_msg_queue_mode": "invalid"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform: PlatformAnthropic,
				Type:     AccountTypeOAuth,
				Extra:    tt.extra,
			}

			require.Equal(t, config.UMQModeSerialize, account.GetUserMsgQueueMode())
		})
	}
}

func TestAccountGetUserMsgQueueModeAllowsExplicitOff(t *testing.T) {
	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"user_msg_queue_mode": config.UMQModeOff},
	}

	require.Empty(t, account.GetUserMsgQueueMode())
}

func TestAccountGetUserMsgQueueModePreservesExplicitAnthropicMode(t *testing.T) {
	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeSetupToken,
		Extra:    map[string]any{"user_msg_queue_mode": config.UMQModeThrottle},
	}

	require.Equal(t, config.UMQModeThrottle, account.GetUserMsgQueueMode())
}

func TestAccountGetUserMsgQueueModeLeavesNonAnthropicOAuthUnset(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		typ      string
	}{
		{name: "anthropic api key", platform: PlatformAnthropic, typ: AccountTypeAPIKey},
		{name: "openai oauth", platform: PlatformOpenAI, typ: AccountTypeOAuth},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform: tt.platform,
				Type:     tt.typ,
				Extra:    map[string]any{},
			}

			require.Empty(t, account.GetUserMsgQueueMode())
		})
	}
}
