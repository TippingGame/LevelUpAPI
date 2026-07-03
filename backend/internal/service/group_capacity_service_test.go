package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type groupCapacityAccountRepoStub struct {
	AccountRepository
	accounts []Account
}

func (s *groupCapacityAccountRepoStub) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	out := make([]Account, len(s.accounts))
	copy(out, s.accounts)
	return out, nil
}

func TestGroupCapacityIncludesCodexProtectedOpenAIAccount(t *testing.T) {
	resetAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	svc := &GroupCapacityService{
		accountRepo: &groupCapacityAccountRepoStub{
			accounts: []Account{
				{
					ID:          1,
					Platform:    PlatformOpenAI,
					Type:        AccountTypeOAuth,
					Status:      StatusActive,
					Schedulable: true,
					Concurrency: 3,
					Extra: map[string]any{
						"codex_5h_limit_percent": 80.0,
						"codex_5h_used_percent":  81.0,
						"codex_5h_reset_at":      resetAt,
					},
				},
			},
		},
	}

	capacity, err := svc.getGroupCapacity(context.Background(), 10)

	require.NoError(t, err)
	require.Equal(t, 3, capacity.ConcurrencyMax)
}
