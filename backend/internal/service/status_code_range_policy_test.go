package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type statusRangeAccountRepoStub struct {
	AccountRepository

	tempCalls     int
	lastAccountID int64
	lastUntil     time.Time
	lastReason    string
}

func (r *statusRangeAccountRepoStub) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	r.lastAccountID = id
	r.lastUntil = until
	r.lastReason = reason
	return nil
}

func TestParseHTTPStatusCodeRangesValue_MixedInputs(t *testing.T) {
	ranges := parseHTTPStatusCodeRangesValue([]any{
		float64(401),
		"403-407",
		"500, 502-503",
		"599",
		"600",
		"bad",
	})

	require.True(t, httpStatusCodeRangesContain(ranges, 401))
	require.True(t, httpStatusCodeRangesContain(ranges, 405))
	require.True(t, httpStatusCodeRangesContain(ranges, 500))
	require.True(t, httpStatusCodeRangesContain(ranges, 503))
	require.True(t, httpStatusCodeRangesContain(ranges, 599))
	require.False(t, httpStatusCodeRangesContain(ranges, 402))
	require.False(t, httpStatusCodeRangesContain(ranges, 408))
	require.False(t, httpStatusCodeRangesContain(ranges, 501))
}

func TestParseHTTPStatusCodesValue_StrictMixedInputs(t *testing.T) {
	codes, err := ParseHTTPStatusCodesValue([]any{
		float64(401),
		"403-405",
		"500, 502-503",
	})

	require.NoError(t, err)
	require.Equal(t, []int{401, 403, 404, 405, 500, 502, 503}, codes)
}

func TestParseHTTPStatusCodesValue_StrictRejectsInvalidTokens(t *testing.T) {
	_, err := ParseHTTPStatusCodesValue("401, 600, bad")

	require.Error(t, err)
	require.Contains(t, err.Error(), "600")
	require.Contains(t, err.Error(), "bad")
}

func TestAccountShouldHandleErrorCode_CustomRanges(t *testing.T) {
	account := &Account{
		Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         "401,403-407,500-503",
		},
	}

	require.True(t, account.ShouldHandleErrorCode(401))
	require.True(t, account.ShouldHandleErrorCode(405))
	require.True(t, account.ShouldHandleErrorCode(503))
	require.False(t, account.ShouldHandleErrorCode(400))
	require.False(t, account.ShouldHandleErrorCode(408))
	require.False(t, account.ShouldHandleErrorCode(504))
}

func TestAccountShouldHandleErrorCode_CustomExactCodesStillWork(t *testing.T) {
	account := &Account{
		Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(429), float64(529)},
		},
	}

	require.True(t, account.ShouldHandleErrorCode(429))
	require.True(t, account.ShouldHandleErrorCode(529))
	require.False(t, account.ShouldHandleErrorCode(500))
}

func TestAccountShouldHandleErrorCode_EmptyCustomPolicyDoesNotMatchAll(t *testing.T) {
	cases := []struct {
		name string
		raw  any
	}{
		{name: "missing", raw: nil},
		{name: "empty list", raw: []any{}},
		{name: "empty string", raw: ""},
		{name: "invalid string", raw: "bad"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			credentials := map[string]any{"custom_error_codes_enabled": true}
			if tc.raw != nil {
				credentials["custom_error_codes"] = tc.raw
			}
			account := &Account{
				Type:        AccountTypeAPIKey,
				Credentials: credentials,
			}

			require.False(t, account.HasActiveCustomErrorCodePolicy())
			require.False(t, account.ShouldHandleErrorCode(429))
			require.False(t, account.ShouldHandleErrorCode(500))
		})
	}
}

func TestTempUnschedulableRule_StatusCodeRange(t *testing.T) {
	repo := &statusRangeAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{
		ID:       77,
		Type:     AccountTypeAPIKey,
		Platform: PlatformGemini,
		Credentials: map[string]any{
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules": []any{
				map[string]any{
					"error_code":       "500-503",
					"keywords":         []any{"overloaded"},
					"duration_minutes": float64(10),
				},
			},
		},
	}

	require.True(t, svc.HandleTempUnschedulable(context.Background(), account, 502, []byte("server overloaded")))
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, int64(77), repo.lastAccountID)
	require.Contains(t, repo.lastReason, `"status_code":502`)
	require.WithinDuration(t, time.Now().Add(10*time.Minute), repo.lastUntil, 2*time.Second)
}

func TestTempUnschedulableRule_StatusCodeRangeMiss(t *testing.T) {
	repo := &statusRangeAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{
		ID:       78,
		Type:     AccountTypeAPIKey,
		Platform: PlatformGemini,
		Credentials: map[string]any{
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules": []any{
				map[string]any{
					"error_code":       "500-503",
					"keywords":         []any{"overloaded"},
					"duration_minutes": float64(10),
				},
			},
		},
	}

	require.False(t, svc.HandleTempUnschedulable(context.Background(), account, 504, []byte("server overloaded")))
	require.Zero(t, repo.tempCalls)
	require.Nil(t, account.TempUnschedulableUntil)
}

func TestHandleTempUnschedulable_PoolModeDefaultSkipsLocalState(t *testing.T) {
	repo := &statusRangeAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{
		ID:       79,
		Type:     AccountTypeAPIKey,
		Platform: PlatformGemini,
		Credentials: map[string]any{
			"pool_mode":                  true,
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules": []any{
				map[string]any{
					"error_code":       "500-503",
					"keywords":         []any{"overloaded"},
					"duration_minutes": float64(10),
				},
			},
		},
	}

	require.False(t, svc.HandleTempUnschedulable(context.Background(), account, 502, []byte("server overloaded")))
	require.Zero(t, repo.tempCalls)
	require.Nil(t, account.TempUnschedulableUntil)
}

func TestHandleTempUnschedulable_PoolModeActiveCustomPolicyAllowsLocalState(t *testing.T) {
	repo := &statusRangeAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{
		ID:       80,
		Type:     AccountTypeAPIKey,
		Platform: PlatformGemini,
		Credentials: map[string]any{
			"pool_mode":                  true,
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(502)},
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules": []any{
				map[string]any{
					"error_code":       "500-503",
					"keywords":         []any{"overloaded"},
					"duration_minutes": float64(10),
				},
			},
		},
	}

	require.True(t, svc.HandleTempUnschedulable(context.Background(), account, 502, []byte("server overloaded")))
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, int64(80), repo.lastAccountID)
}
