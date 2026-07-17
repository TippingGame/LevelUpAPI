package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountGrokMediaGenerationEligibility(t *testing.T) {
	tests := []struct {
		name       string
		account    *Account
		want       bool
		wantReason string
	}{
		{
			name: "unprobed OAuth account remains eligible",
			account: &Account{
				Platform: PlatformGrok,
				Type:     AccountTypeOAuth,
			},
			want:       true,
			wantReason: "billing_unobserved",
		},
		{
			name: "overall billing 403 blocks OAuth media generation",
			account: &Account{
				Platform: PlatformGrok,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					grokBillingExtraKey: map[string]any{"status_code": http.StatusForbidden},
				},
			},
			want:       false,
			wantReason: "billing_forbidden",
		},
		{
			name: "either billing window 403 blocks OAuth media generation",
			account: &Account{
				Platform: PlatformGrok,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					grokBillingExtraKey: map[string]any{
						"status_code":         http.StatusOK,
						"weekly_status_code":  http.StatusOK,
						"monthly_status_code": http.StatusForbidden,
					},
				},
			},
			want:       false,
			wantReason: "billing_forbidden",
		},
		{
			name: "explicit true overrides OAuth billing 403",
			account: &Account{
				Platform: PlatformGrok,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					GrokMediaEligibleExtraKey: true,
					grokBillingExtraKey:       map[string]any{"status_code": http.StatusForbidden},
				},
			},
			want:       true,
			wantReason: "override_enabled",
		},
		{
			name: "API key is eligible without a billing probe",
			account: &Account{
				Platform: PlatformGrok,
				Type:     AccountTypeAPIKey,
			},
			want:       true,
			wantReason: "non_oauth",
		},
		{
			name: "explicit false blocks an API key account",
			account: &Account{
				Platform: PlatformGrok,
				Type:     AccountTypeAPIKey,
				Extra:    map[string]any{GrokMediaEligibleExtraKey: false},
			},
			want:       false,
			wantReason: "override_disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := tt.account.GrokMediaGenerationEligibility()
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantReason, reason)
			require.Equal(t, tt.want, tt.account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityGrokMediaGeneration))
		})
	}
}
