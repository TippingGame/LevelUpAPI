package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAIPlusConcurrency_DefaultAndAdminConfiguredValue(t *testing.T) {
	got, err := NormalizeOpenAIPlusConcurrency(PlatformOpenAI, AccountLevelPlus, 0)
	require.NoError(t, err)
	require.Equal(t, OpenAIPlusDefaultConcurrency, got)

	got, err = NormalizeOpenAIPlusConcurrency(PlatformOpenAI, AccountLevelPlus, 5)
	require.NoError(t, err)
	require.Equal(t, 5, got)
}

func TestDefaultOAuthAccountConcurrencyForPlatform(t *testing.T) {
	require.Equal(t, OpenAIPlusDefaultConcurrency, DefaultOAuthAccountConcurrencyForPlatform(PlatformOpenAI))
	require.Equal(t, OAuthAccountDefaultConcurrency, DefaultOAuthAccountConcurrencyForPlatform(PlatformAnthropic))
	require.Equal(t, OAuthAccountDefaultConcurrency, DefaultOAuthAccountConcurrencyForPlatform(PlatformGemini))
}

func TestNormalizeOpenAIAccountLevel_FromPlanType(t *testing.T) {
	require.Equal(t, AccountLevelPlus, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelUnknown,
		map[string]any{"plan_type": "plus"},
		nil,
	))
	require.Equal(t, AccountLevelPro, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelUnknown,
		map[string]any{"plan_type": "chatgpt_pro"},
		nil,
	))
	require.Equal(t, AccountLevelPro, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelUnknown,
		map[string]any{"plan_type": "pro5x"},
		nil,
	))
	require.Equal(t, AccountLevelPro, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelPlus,
		map[string]any{
			"plan_type": "plus",
			"account": map[string]any{
				"plan_type": "chatgpt_pro",
			},
		},
		nil,
	))
	require.Equal(t, AccountLevelPro, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelPlus,
		map[string]any{
			"plan_type":          "plus",
			"chatgpt_account_id": "acct-pro",
			"accounts": map[string]any{
				"acct-pro": map[string]any{
					"entitlement": map[string]any{
						"subscription_plan": "chatgpt_pro",
					},
				},
			},
		},
		nil,
	))
	require.Equal(t, AccountLevelPro, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelPlus,
		map[string]any{
			"plan_type": "plus",
			"accounts": map[string]any{
				"acct-default-pro": map[string]any{
					"account": map[string]any{
						"is_default": true,
						"plan_type":  "chatgpt_pro",
					},
				},
				"acct-plus": map[string]any{
					"account": map[string]any{
						"plan_type": "plus",
					},
				},
			},
		},
		nil,
	))
	require.Equal(t, AccountLevelPlus, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelPlus,
		map[string]any{
			"accounts": map[string]any{
				"acct-default-plus": map[string]any{
					"account": map[string]any{
						"is_default": true,
						"plan_type":  "plus",
					},
				},
				"acct-non-default-pro": map[string]any{
					"account": map[string]any{
						"plan_type": "chatgpt_pro",
					},
				},
			},
		},
		nil,
	))
}

func TestNormalizeOpenAIAccountLevel_ManualLevelTakesPriority(t *testing.T) {
	require.Equal(t, AccountLevelPro, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelPro,
		map[string]any{"plan_type": "plus"},
		nil,
	))
	require.Equal(t, AccountLevelUnknown, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelUnknown,
		map[string]any{"account_level": "pro"},
		nil,
	))
	require.Equal(t, AccountLevelUnknown, NormalizeOpenAIAccountLevel(
		PlatformAnthropic,
		AccountLevelUnknown,
		map[string]any{"plan_type": "plus"},
		nil,
	))
}

func TestEffectiveOpenAISharedPoolAccountLevel_UsesHighestKnownLevel(t *testing.T) {
	require.Equal(t, AccountLevelPro, EffectiveOpenAISharedPoolAccountLevel(
		PlatformOpenAI,
		AccountLevelPlus,
		map[string]any{"plan_type": "chatgpt_pro"},
		nil,
	))
	require.Equal(t, AccountLevelPro, EffectiveOpenAISharedPoolAccountLevel(
		PlatformOpenAI,
		AccountLevelPro,
		map[string]any{"plan_type": "plus"},
		nil,
	))
	require.Equal(t, AccountLevelPlus, EffectiveOpenAISharedPoolAccountLevel(
		PlatformOpenAI,
		AccountLevelPlus,
		map[string]any{"plan_type": "free"},
		nil,
	))
	require.Equal(t, AccountLevelPro, EffectiveOpenAISharedPoolAccountLevel(
		PlatformOpenAI,
		AccountLevelUnknown,
		map[string]any{"plan_type": "pro5x"},
		nil,
	))
	require.Equal(t, AccountLevelPro, EffectiveOpenAISharedPoolAccountLevel(
		PlatformOpenAI,
		AccountLevelPlus,
		map[string]any{
			"plan_type": "plus",
			"account_info": map[string]any{
				"entitlement": map[string]any{
					"subscription_plan": "chatgpt_pro",
				},
			},
		},
		nil,
	))
}

func TestValidateAccountLoadFactor_Max(t *testing.T) {
	loadFactor := AccountMaxLoadFactor + 1
	require.Error(t, ValidateAccountLoadFactor(&loadFactor))

	loadFactor = AccountMaxLoadFactor
	require.NoError(t, ValidateAccountLoadFactor(&loadFactor))
}
