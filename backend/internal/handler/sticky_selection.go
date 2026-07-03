package handler

import (
	"context"

	"go.uber.org/zap"
)

type stickyReconcileAction string

const (
	stickyReconcileUnchanged stickyReconcileAction = "unchanged"
	stickyReconcileCleared   stickyReconcileAction = "cleared"
	stickyReconcileReplaced  stickyReconcileAction = "replaced"
	stickyReconcileMoved     stickyReconcileAction = "moved"
)

func reconcileStickyBoundAccount(previousBoundID, selectedAccountID, latestCachedID int64) (int64, stickyReconcileAction) {
	if previousBoundID <= 0 || selectedAccountID <= 0 || previousBoundID == selectedAccountID {
		return previousBoundID, stickyReconcileUnchanged
	}
	switch latestCachedID {
	case 0:
		return 0, stickyReconcileCleared
	case selectedAccountID:
		return selectedAccountID, stickyReconcileReplaced
	case previousBoundID:
		return previousBoundID, stickyReconcileUnchanged
	default:
		return latestCachedID, stickyReconcileMoved
	}
}

func stickySelectionHonored(sessionKey string, boundAccountID, selectedAccountID int64) bool {
	return sessionKey != "" && boundAccountID > 0 && boundAccountID == selectedAccountID
}

func (h *GatewayHandler) refreshStickyBoundAccountAfterSelection(
	ctx context.Context,
	reqLog *zap.Logger,
	groupID *int64,
	sessionKey string,
	previousBoundID int64,
	selectedAccountID int64,
) int64 {
	if sessionKey == "" || previousBoundID <= 0 || selectedAccountID <= 0 || previousBoundID == selectedAccountID {
		return previousBoundID
	}
	latestCachedID, err := h.gatewayService.GetCachedSessionAccountID(ctx, groupID, sessionKey)
	if err != nil {
		if reqLog != nil {
			reqLog.Warn("sticky.bound_account_refresh_failed",
				zap.String("session_key", sessionKey),
				zap.Int64("previous_bound_account_id", previousBoundID),
				zap.Int64("selected_account_id", selectedAccountID),
				zap.Error(err),
			)
		}
		return previousBoundID
	}
	nextBoundID, action := reconcileStickyBoundAccount(previousBoundID, selectedAccountID, latestCachedID)
	if action != stickyReconcileUnchanged && reqLog != nil {
		reqLog.Info("sticky.bound_account_reconciled",
			zap.String("session_key", sessionKey),
			zap.String("action", string(action)),
			zap.Int64("previous_bound_account_id", previousBoundID),
			zap.Int64("selected_account_id", selectedAccountID),
			zap.Int64("latest_cached_account_id", latestCachedID),
			zap.Int64("next_bound_account_id", nextBoundID),
		)
	}
	return nextBoundID
}
