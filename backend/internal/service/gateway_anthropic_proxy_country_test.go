package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProxyCountryBlocksAnthropicScheduling(t *testing.T) {
	blocked := []string{"CN", "cn", " RU ", "BY", "IR", "KP", "CU", "SY"}
	for _, countryCode := range blocked {
		t.Run("blocked_"+countryCode, func(t *testing.T) {
			require.True(t, proxyCountryBlocksAnthropicScheduling(countryCode))
		})
	}

	allowed := []string{"US", "SG", "JP", "DE", ""}
	for _, countryCode := range allowed {
		t.Run("allowed_"+countryCode, func(t *testing.T) {
			require.False(t, proxyCountryBlocksAnthropicScheduling(countryCode))
		})
	}
}
