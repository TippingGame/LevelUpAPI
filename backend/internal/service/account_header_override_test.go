//go:build unit

package service

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrokHeaderOverridesApplyWithoutAllowingCredentialHeaders(t *testing.T) {
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			credKeyHeaderOverrideEnabled: true,
			credKeyHeaderOverrides: map[string]any{
				"User-Agent":    "relay-client/2.0",
				"X-Relay-Token": "relay-secret",
				"Authorization": "Bearer evil",
			},
		},
	}
	require.True(t, account.IsHeaderOverrideEligible())
	require.Equal(t, map[string]string{
		"user-agent":    "relay-client/2.0",
		"x-relay-token": "relay-secret",
	}, account.GetHeaderOverrides())

	headers := http.Header{}
	headers.Set("Authorization", "Bearer real")
	headers.Set("User-Agent", "original")
	account.ApplyHeaderOverrides(headers)
	require.Equal(t, "Bearer real", headers.Get("Authorization"))
	require.Equal(t, "relay-client/2.0", headers.Get("User-Agent"))
	require.Equal(t, "relay-secret", headers.Get("x-relay-token"))
}

func TestNormalizeHeaderOverrideCredentialsRejectsUnsafeEntries(t *testing.T) {
	for _, name := range []string{"Authorization", "Content-Type", "Cookie", "Host", "X-Grok-Conv-Id"} {
		err := NormalizeHeaderOverrideCredentials(map[string]any{
			credKeyHeaderOverrides: map[string]any{name: "bad"},
		})
		require.Error(t, err, "header %q should be blocked", name)
	}

	credentials := map[string]any{
		credKeyHeaderOverrides: map[string]any{
			" User-Agent ": " relay ",
			"X-Empty":      "",
			"":             "",
		},
	}
	require.NoError(t, NormalizeHeaderOverrideCredentials(credentials))
	require.Equal(t, map[string]any{"user-agent": "relay", "x-empty": ""}, credentials[credKeyHeaderOverrides])

	big := strings.Repeat("x", maxHeaderOverrideValueLength+1)
	require.Error(t, NormalizeHeaderOverrideCredentials(map[string]any{
		credKeyHeaderOverrides: map[string]any{"x-big": big},
	}))
}
