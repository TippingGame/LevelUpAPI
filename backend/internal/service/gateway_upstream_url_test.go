//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGatewayService_AnthropicCustomRelayUsesDatabaseAllowlistAdditions(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"other.example.test"}
	settings := NewSettingService(&settingValueRepoStub{values: map[string]string{
		SettingKeyUpstreamURLAllowlistExtraHosts: `["relay.example.test"]`,
	}}, cfg)
	proxyID := int64(7)
	account := &Account{
		ID:       45,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"custom_base_url_enabled": true,
			"custom_base_url":         "https://relay.example.test",
		},
		ProxyID: &proxyID,
		Proxy: &Proxy{
			ID:       proxyID,
			Protocol: "http",
			Host:     "proxy.example.test",
			Port:     8080,
		},
	}
	svc := &GatewayService{cfg: cfg, settingService: settings}

	req, _, err := svc.buildUpstreamRequest(
		context.Background(),
		nil,
		account,
		[]byte(`{"model":"claude-opus-4-8","messages":[]}`),
		"oauth-token",
		"oauth",
		"claude-opus-4-8",
		false,
		false,
	)

	require.NoError(t, err)
	require.Equal(t, "https://relay.example.test/v1/messages", req.URL.Scheme+"://"+req.URL.Host+req.URL.Path)
	require.Equal(t, "true", req.URL.Query().Get("beta"))
	require.Equal(t, "http://proxy.example.test:8080", req.URL.Query().Get("proxy"))
}

func TestGatewayService_ValidateUpstreamBaseURLRequiresConfig(t *testing.T) {
	_, err := (&GatewayService{}).validateUpstreamBaseURL("https://relay.example.test")
	require.EqualError(t, err, "invalid base_url: config is not available")
}
