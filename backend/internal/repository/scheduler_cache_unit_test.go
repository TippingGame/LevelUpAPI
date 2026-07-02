//go:build unit

package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildSchedulerMetadataAccount_KeepsOpenAIWSFlags(t *testing.T) {
	account := service.Account{
		ID:       42,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Extra: map[string]any{
			"openai_oauth_responses_websockets_v2_enabled": true,
			"openai_oauth_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
			"openai_ws_force_http":                         true,
			"openai_responses_mode":                        "force_chat_completions",
			"openai_responses_supported":                   false,
			"mixed_scheduling":                             true,
			"unused_large_field":                           "drop-me",
		},
	}

	got := buildSchedulerMetadataAccount(account)

	require.Equal(t, true, got.Extra["openai_oauth_responses_websockets_v2_enabled"])
	require.Equal(t, service.OpenAIWSIngressModePassthrough, got.Extra["openai_oauth_responses_websockets_v2_mode"])
	require.Equal(t, true, got.Extra["openai_ws_force_http"])
	require.Equal(t, "force_chat_completions", got.Extra["openai_responses_mode"])
	require.Equal(t, false, got.Extra["openai_responses_supported"])
	require.Equal(t, true, got.Extra["mixed_scheduling"])
	require.Nil(t, got.Extra["unused_large_field"])
}

func TestBuildSchedulerMetadataAccount_KeepsSlimGroupMembership(t *testing.T) {
	account := service.Account{
		ID:       42,
		Platform: service.PlatformAnthropic,
		GroupIDs: []int64{7, 9, 7, 0},
		AccountGroups: []service.AccountGroup{
			{
				AccountID: 42,
				GroupID:   7,
				Priority:  2,
				Account:   &service.Account{ID: 42, Name: "drop-from-metadata"},
				Group:     &service.Group{ID: 7, Name: "drop-from-metadata"},
			},
			{
				AccountID: 42,
				GroupID:   11,
				Priority:  3,
				Group:     &service.Group{ID: 11, Name: "drop-from-metadata"},
			},
			{
				AccountID: 42,
				GroupID:   0,
				Priority:  4,
			},
		},
	}

	got := buildSchedulerMetadataAccount(account)

	require.Equal(t, []int64{7, 9, 11}, got.GroupIDs)
	require.Len(t, got.AccountGroups, 2)
	require.Equal(t, int64(42), got.AccountGroups[0].AccountID)
	require.Equal(t, int64(7), got.AccountGroups[0].GroupID)
	require.Equal(t, 2, got.AccountGroups[0].Priority)
	require.Nil(t, got.AccountGroups[0].Account)
	require.Nil(t, got.AccountGroups[0].Group)
	require.Equal(t, int64(11), got.AccountGroups[1].GroupID)
	require.Nil(t, got.Groups)
}

func TestBuildSchedulerMetadataAccount_KeepsProxySchedulingState(t *testing.T) {
	ownerID := int64(101)
	proxyID := int64(7)
	account := service.Account{
		ID:           42,
		Platform:     service.PlatformAnthropic,
		AccountLevel: service.AccountLevelUnknown,
		Type:         service.AccountTypeOAuth,
		OwnerUserID:  &ownerID,
		ProxyID:      &proxyID,
		Proxy: &service.Proxy{
			ID:       proxyID,
			Status:   service.StatusActive,
			Host:     "127.0.0.1",
			Port:     1080,
			Username: "secret-user",
			Password: "secret-pass",
		},
		Status:      service.StatusActive,
		Schedulable: true,
	}

	got := buildSchedulerMetadataAccount(account)

	require.Equal(t, account.AccountLevel, got.AccountLevel)
	require.NotNil(t, got.OwnerUserID)
	require.Equal(t, ownerID, *got.OwnerUserID)
	require.NotNil(t, got.ProxyID)
	require.Equal(t, proxyID, *got.ProxyID)
	require.NotNil(t, got.Proxy)
	require.Equal(t, proxyID, got.Proxy.ID)
	require.Equal(t, service.StatusActive, got.Proxy.Status)
	require.Empty(t, got.Proxy.Host)
	require.Empty(t, got.Proxy.Username)
	require.Empty(t, got.Proxy.Password)
	require.True(t, got.IsSchedulable())
}
