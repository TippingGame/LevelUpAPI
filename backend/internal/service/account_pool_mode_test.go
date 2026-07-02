//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetPoolModeRetryCount(t *testing.T) {
	tests := []struct {
		name     string
		account  *Account
		expected int
	}{
		{
			name: "default_when_not_pool_mode",
			account: &Account{
				Type:        AccountTypeAPIKey,
				Platform:    PlatformOpenAI,
				Credentials: map[string]any{},
			},
			expected: defaultPoolModeRetryCount,
		},
		{
			name: "default_when_missing_retry_count",
			account: &Account{
				Type:     AccountTypeAPIKey,
				Platform: PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode": true,
				},
			},
			expected: defaultPoolModeRetryCount,
		},
		{
			name: "supports_float64_from_json_credentials",
			account: &Account{
				Type:     AccountTypeAPIKey,
				Platform: PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":             true,
					"pool_mode_retry_count": float64(5),
				},
			},
			expected: 5,
		},
		{
			name: "supports_json_number",
			account: &Account{
				Type:     AccountTypeAPIKey,
				Platform: PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":             true,
					"pool_mode_retry_count": json.Number("4"),
				},
			},
			expected: 4,
		},
		{
			name: "supports_string_value",
			account: &Account{
				Type:     AccountTypeAPIKey,
				Platform: PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":             true,
					"pool_mode_retry_count": "2",
				},
			},
			expected: 2,
		},
		{
			name: "negative_value_is_clamped_to_zero",
			account: &Account{
				Type:     AccountTypeAPIKey,
				Platform: PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":             true,
					"pool_mode_retry_count": -1,
				},
			},
			expected: 0,
		},
		{
			name: "oversized_value_is_clamped_to_max",
			account: &Account{
				Type:     AccountTypeAPIKey,
				Platform: PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":             true,
					"pool_mode_retry_count": 99,
				},
			},
			expected: maxPoolModeRetryCount,
		},
		{
			name: "invalid_value_falls_back_to_default",
			account: &Account{
				Type:     AccountTypeAPIKey,
				Platform: PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":             true,
					"pool_mode_retry_count": "oops",
				},
			},
			expected: defaultPoolModeRetryCount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.account.GetPoolModeRetryCount())
		})
	}
}

func TestGetPoolModeRetryStatusCodes(t *testing.T) {
	tests := []struct {
		name     string
		account  *Account
		expected []int
	}{
		{
			name:     "nil_account_returns_nil",
			account:  nil,
			expected: nil,
		},
		{
			name: "missing_key_returns_nil",
			account: &Account{
				Type:        AccountTypeAPIKey,
				Platform:    PlatformOpenAI,
				Credentials: map[string]any{"pool_mode": true},
			},
			expected: nil,
		},
		{
			name: "empty_slice_is_preserved",
			account: &Account{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{},
				},
			},
			expected: []int{},
		},
		{
			name: "normalizes_mixed_values_and_ranges",
			account: &Account{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{
						float64(503),
						"502",
						"500-501",
						json.Number("529"),
						int64(503),
						99,
						600,
						"bad",
					},
				},
			},
			expected: []int{500, 501, 502, 503, 529},
		},
		{
			name: "string_list_is_parsed",
			account: &Account{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": "502,503",
				},
			},
			expected: []int{502, 503},
		},
		{
			name: "string_range_is_expanded",
			account: &Account{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": "500-503",
				},
			},
			expected: []int{500, 501, 502, 503},
		},
		{
			name: "invalid_string_returns_nil",
			account: &Account{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": "bad",
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.account.GetPoolModeRetryStatusCodes())
		})
	}
}

func TestIsPoolModeRetryableStatus_Account(t *testing.T) {
	tests := []struct {
		name       string
		account    *Account
		statusCode int
		expected   bool
	}{
		{
			name:       "nil_account_uses_default_429",
			account:    nil,
			statusCode: 429,
			expected:   true,
		},
		{
			name: "unconfigured_uses_default_403",
			account: &Account{
				Credentials: map[string]any{"pool_mode": true},
			},
			statusCode: 403,
			expected:   true,
		},
		{
			name: "unconfigured_rejects_502",
			account: &Account{
				Credentials: map[string]any{"pool_mode": true},
			},
			statusCode: 502,
			expected:   false,
		},
		{
			name: "configured_list_overrides_default",
			account: &Account{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{float64(502), float64(503)},
				},
			},
			statusCode: 401,
			expected:   false,
		},
		{
			name: "configured_list_adds_502",
			account: &Account{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{float64(502), float64(503)},
				},
			},
			statusCode: 502,
			expected:   true,
		},
		{
			name: "configured_range_adds_503",
			account: &Account{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": "500-503",
				},
			},
			statusCode: 503,
			expected:   true,
		},
		{
			name: "configured_range_overrides_default",
			account: &Account{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": "500-503",
				},
			},
			statusCode: 429,
			expected:   false,
		},
		{
			name: "empty_list_disables_default_codes",
			account: &Account{
				Credentials: map[string]any{
					"pool_mode_retry_status_codes": []any{},
				},
			},
			statusCode: 429,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.account.IsPoolModeRetryableStatus(tt.statusCode))
		})
	}
}
