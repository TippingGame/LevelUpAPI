package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestAccountEffectivePriorityForUser(t *testing.T) {
	ownerID := int64(42)
	privatePriority := 3
	account := &Account{
		Priority:        50,
		OwnerUserID:     &ownerID,
		PrivatePriority: &privatePriority,
	}

	require.Equal(t, 3, account.EffectivePriorityForUser(ownerID))
	require.Equal(t, 50, account.EffectivePriorityForUser(99))
	require.Equal(t, 50, account.EffectivePriorityForUser(0))
}

func TestAccountEffectivePriorityForPublicShareUsesPrivatePriority(t *testing.T) {
	ownerID := int64(42)
	privatePriority := 3
	account := &Account{
		Priority:        50,
		OwnerUserID:     &ownerID,
		PrivatePriority: &privatePriority,
		ShareMode:       AccountShareModePublic,
		ShareStatus:     AccountShareStatusApproved,
	}

	require.Equal(t, 3, account.EffectivePriorityForUser(ownerID))
	require.Equal(t, 3, account.EffectivePriorityForUser(99))
	require.Equal(t, 3, account.EffectivePriorityForUser(0))
}

func TestAccountEffectivePriorityForPrivateOwnedAccountKeepsGlobalPriorityForOthers(t *testing.T) {
	ownerID := int64(42)
	privatePriority := 3
	account := &Account{
		Priority:        50,
		OwnerUserID:     &ownerID,
		PrivatePriority: &privatePriority,
		ShareMode:       AccountShareModePrivate,
		ShareStatus:     AccountShareStatusApproved,
	}

	require.Equal(t, 3, account.EffectivePriorityForUser(ownerID))
	require.Equal(t, 50, account.EffectivePriorityForUser(99))
	require.Equal(t, 50, account.EffectivePriorityForUser(0))
}

func TestAccountEffectivePriorityForRequest(t *testing.T) {
	ownerID := int64(42)
	privatePriority := 3
	account := &Account{
		Priority:        50,
		OwnerUserID:     &ownerID,
		PrivatePriority: &privatePriority,
	}

	ctx := context.WithValue(context.Background(), ctxkey.AuthenticatedUserID, ownerID)

	require.Equal(t, 3, account.EffectivePriorityForRequest(ctx))
	require.Equal(t, 50, account.EffectivePriorityForRequest(context.Background()))
}
