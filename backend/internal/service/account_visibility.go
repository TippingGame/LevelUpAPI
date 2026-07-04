package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

func AuthenticatedUserIDFromContext(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	userID, _ := ctx.Value(ctxkey.AuthenticatedUserID).(int64)
	return userID
}

func IsAccountVisibleToRequestUser(ctx context.Context, account *Account) bool {
	return account.IsVisibleToConsumer(AuthenticatedUserIDFromContext(ctx))
}

func (a *Account) EffectivePriorityForUser(userID int64) int {
	if a == nil {
		return 0
	}
	if userID > 0 && a.OwnerUserID != nil && *a.OwnerUserID == userID && a.PrivatePriority != nil && *a.PrivatePriority > 0 {
		return *a.PrivatePriority
	}
	if a.IsPublicShareApproved() && a.PrivatePriority != nil && *a.PrivatePriority > 0 {
		return *a.PrivatePriority
	}
	return a.Priority
}

func (a *Account) EffectivePriorityForRequest(ctx context.Context) int {
	return a.EffectivePriorityForUser(AuthenticatedUserIDFromContext(ctx))
}

func FilterAccountsVisibleToRequestUser(ctx context.Context, accounts []Account) []Account {
	if len(accounts) == 0 {
		return accounts
	}
	filtered := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if IsAccountVisibleToRequestUser(ctx, &account) {
			filtered = append(filtered, account)
		}
	}
	return filtered
}
