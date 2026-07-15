//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestGrokOAuthCustomBaseURLUsesOperatorPolicy(t *testing.T) {
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"base_url": "https://relay.example.test/xai/v1",
		},
	}

	target, err := buildGrokResponsesURL(account, &config.Config{})
	require.NoError(t, err)
	require.Equal(t, "https://relay.example.test/xai/v1/responses", target)

	restricted := &config.Config{}
	restricted.Security.URLAllowlist.Enabled = true
	restricted.Security.URLAllowlist.UpstreamHosts = []string{"other.example.test"}
	_, err = buildGrokResponsesURL(account, restricted)
	require.EqualError(t, err, "invalid base url: base URL rejected by URL security policy")
}

func TestGrokOAuthOfficialBaseURLBypassesOperatorAllowlist(t *testing.T) {
	account := &Account{Platform: PlatformGrok, Type: AccountTypeOAuth}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"other.example.test"}

	target, err := buildGrokResponsesURL(account, cfg)
	require.NoError(t, err)
	require.Equal(t, xai.DefaultCLIBaseURL+"/responses", target)
}

func TestGrokAPIKeyBaseURLUsesAllowlist(t *testing.T) {
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://relay.example.test/v1",
		},
	}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"relay.example.test"}

	target, err := buildGrokChatCompletionsURL(account, cfg)
	require.NoError(t, err)
	require.Equal(t, "https://relay.example.test/v1/chat/completions", target)
}

func TestGrokBillingURLFollowsForwardingBaseURL(t *testing.T) {
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"base_url": "https://relay.example.test/v1",
		},
	}
	weekly, err := buildGrokBillingURL(account, nil, true)
	require.NoError(t, err)
	require.Equal(t, "https://relay.example.test/v1/billing?format=credits", weekly)
}
