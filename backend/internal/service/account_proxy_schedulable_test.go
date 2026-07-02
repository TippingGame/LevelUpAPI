package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountHasRequiredProxyForScheduling(t *testing.T) {
	ownerID := int64(101)
	proxyID := int64(7)

	tests := []struct {
		name string
		acc  *Account
		want bool
	}{
		{
			name: "admin anthropic account without proxy is allowed",
			acc: &Account{
				Platform: PlatformAnthropic,
				Type:     AccountTypeOAuth,
			},
			want: true,
		},
		{
			name: "owned anthropic account without proxy is blocked",
			acc: &Account{
				Platform:    PlatformAnthropic,
				Type:        AccountTypeOAuth,
				OwnerUserID: &ownerID,
			},
			want: false,
		},
		{
			name: "owned anthropic account with missing proxy object is blocked",
			acc: &Account{
				Platform:    PlatformAnthropic,
				Type:        AccountTypeOAuth,
				OwnerUserID: &ownerID,
				ProxyID:     &proxyID,
			},
			want: false,
		},
		{
			name: "owned anthropic account with disabled proxy is blocked",
			acc: &Account{
				Platform:    PlatformAnthropic,
				Type:        AccountTypeOAuth,
				OwnerUserID: &ownerID,
				ProxyID:     &proxyID,
				Proxy:       &Proxy{ID: proxyID, Status: StatusDisabled},
			},
			want: false,
		},
		{
			name: "owned anthropic account with active proxy is allowed",
			acc: &Account{
				Platform:    PlatformAnthropic,
				Type:        AccountTypeOAuth,
				OwnerUserID: &ownerID,
				ProxyID:     &proxyID,
				Proxy:       &Proxy{ID: proxyID, Status: StatusActive},
			},
			want: true,
		},
		{
			name: "owned openai pro account follows the same proxy protection",
			acc: &Account{
				Platform:     PlatformOpenAI,
				AccountLevel: AccountLevelPro,
				Type:         AccountTypeOAuth,
				OwnerUserID:  &ownerID,
				ProxyID:      &proxyID,
				Proxy:        &Proxy{ID: proxyID, Status: StatusActive},
			},
			want: true,
		},
		{
			name: "owned openai plus account does not require proxy",
			acc: &Account{
				Platform:     PlatformOpenAI,
				AccountLevel: AccountLevelPlus,
				Type:         AccountTypeOAuth,
				OwnerUserID:  &ownerID,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.acc.HasRequiredProxyForScheduling())
		})
	}
}

func TestAccountIsSchedulableBlocksOwnedAnthropicWhenProxyInactive(t *testing.T) {
	ownerID := int64(101)
	proxyID := int64(7)
	account := &Account{
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		OwnerUserID: &ownerID,
		ProxyID:     &proxyID,
		Proxy:       &Proxy{ID: proxyID, Status: StatusDisabled},
		Status:      StatusActive,
		Schedulable: true,
	}

	require.False(t, account.IsSchedulable())
}
