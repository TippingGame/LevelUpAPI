package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type gatewayAnthropicConcurrencyCache struct {
	lastAcquireMax map[int64]int
}

func (c *gatewayAnthropicConcurrencyCache) AcquireAccountSlot(_ context.Context, accountID int64, maxConcurrency int, _ string) (bool, error) {
	if c.lastAcquireMax == nil {
		c.lastAcquireMax = map[int64]int{}
	}
	c.lastAcquireMax[accountID] = maxConcurrency
	return true, nil
}

func (c *gatewayAnthropicConcurrencyCache) ReleaseAccountSlot(context.Context, int64, string) error {
	return nil
}

func (c *gatewayAnthropicConcurrencyCache) GetAccountConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}

func (c *gatewayAnthropicConcurrencyCache) GetAccountConcurrencyBatch(_ context.Context, accountIDs []int64) (map[int64]int, error) {
	counts := make(map[int64]int, len(accountIDs))
	for _, accountID := range accountIDs {
		counts[accountID] = 0
	}
	return counts, nil
}

func (c *gatewayAnthropicConcurrencyCache) IncrementAccountWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}

func (c *gatewayAnthropicConcurrencyCache) DecrementAccountWaitCount(context.Context, int64) error {
	return nil
}

func (c *gatewayAnthropicConcurrencyCache) GetAccountWaitingCount(context.Context, int64) (int, error) {
	return 0, nil
}

func (c *gatewayAnthropicConcurrencyCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}

func (c *gatewayAnthropicConcurrencyCache) ReleaseUserSlot(context.Context, int64, string) error {
	return nil
}

func (c *gatewayAnthropicConcurrencyCache) GetUserConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}

func (c *gatewayAnthropicConcurrencyCache) IncrementWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}

func (c *gatewayAnthropicConcurrencyCache) DecrementWaitCount(context.Context, int64) error {
	return nil
}

func (c *gatewayAnthropicConcurrencyCache) GetAccountsLoadBatch(context.Context, []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	return nil, nil
}

func (c *gatewayAnthropicConcurrencyCache) GetUsersLoadBatch(context.Context, []UserWithConcurrency) (map[int64]*UserLoadInfo, error) {
	return nil, nil
}

func (c *gatewayAnthropicConcurrencyCache) CleanupExpiredAccountSlots(context.Context, int64) error {
	return nil
}

func (c *gatewayAnthropicConcurrencyCache) CleanupStaleProcessSlots(context.Context, string) error {
	return nil
}

func TestGatewayTryAcquireByLegacyOrderCapsAnthropicOAuthConcurrency(t *testing.T) {
	cache := &gatewayAnthropicConcurrencyCache{}
	svc := &GatewayService{
		concurrencyService: NewConcurrencyService(cache),
	}
	account := &Account{
		ID:          101,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 5,
		Extra: map[string]any{
			"max_sessions": 1,
		},
	}

	selection, ok, err := svc.tryAcquireByLegacyOrder(context.Background(), []*Account{account}, nil, "session", false)

	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, selection.Acquired)
	require.Equal(t, 1, cache.lastAcquireMax[account.ID])
}
