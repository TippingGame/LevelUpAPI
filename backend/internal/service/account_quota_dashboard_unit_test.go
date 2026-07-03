package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountQuotaDashboardGroupUsesEffectiveOpenAIProLevel(t *testing.T) {
	now := time.Date(2026, 7, 4, 6, 0, 0, 0, time.UTC)
	proGroupID := int64(1001)
	account := Account{
		ID:           2001,
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		AccountLevel: AccountLevelPlus,
		Credentials:  map[string]any{"plan_type": "chatgpt_pro"},
		Status:       StatusActive,
		Schedulable:  true,
		Groups: []*Group{{
			ID:                   proGroupID,
			Name:                 "PRO shared pool",
			Platform:             PlatformOpenAI,
			Status:               StatusActive,
			Scope:                GroupScopePublic,
			SubscriptionType:     SubscriptionTypeStandard,
			RequiredAccountLevel: AccountLevelPro,
		}},
	}

	builder := newAccountQuotaGroupDashboardBuilder(now)
	builder.addAccountWithGroupFilter(account, isPlatformSharedQuotaGroup)
	summaries := builder.finalize()

	require.Len(t, summaries, 1)
	require.Equal(t, proGroupID, *summaries[0].GroupID)
	require.Equal(t, 1, summaries[0].AccountCount)
	require.Equal(t, 1, summaries[0].SchedulableAccountCount)
}

func TestAccountQuotaDashboardIgnoresOpenAIOAuthRelayPoolTempState(t *testing.T) {
	now := time.Date(2026, 7, 4, 6, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	account := Account{
		ID:                      2002,
		Platform:                PlatformOpenAI,
		Type:                    AccountTypeOAuth,
		AccountLevel:            AccountLevelPro,
		Status:                  StatusActive,
		Schedulable:             true,
		TempUnschedulableUntil:  &future,
		TempUnschedulableReason: `{"matched_keyword":"upstream_relay_pool_unavailable","error_message":"No available accounts"}`,
	}

	builder := newAccountQuotaDashboardBuilder(now)
	builder.addAccount(account)
	dashboard := builder.finalize()

	require.Equal(t, 1, dashboard.Totals.AccountCount)
	require.Equal(t, 1, dashboard.Totals.SchedulableAccountCount)
	require.Equal(t, 0, dashboard.Totals.RateLimitedAccountCount)
}
