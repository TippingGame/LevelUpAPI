package handler

import (
	"context"

	"go.uber.org/zap"
)

func (h *GatewayHandler) clearStickySessionIfBoundTo(ctx context.Context, groupID *int64, sessionHash string, accountID int64, reqLog *zap.Logger, reason string) bool {
	if h == nil || h.gatewayService == nil || sessionHash == "" || accountID <= 0 {
		return false
	}
	cleared, err := h.gatewayService.ClearStickySessionIfBoundTo(ctx, groupID, sessionHash, accountID)
	if err != nil {
		reqLog.Warn("gateway.clear_sticky_session_failed", zap.Int64("account_id", accountID), zap.String("reason", reason), zap.Error(err))
		return false
	}
	if cleared {
		reqLog.Info("gateway.sticky_session_cleared", zap.Int64("account_id", accountID), zap.String("reason", reason))
	}
	return cleared
}

func (h *OpenAIGatewayHandler) clearStickySessionIfBoundTo(ctx context.Context, groupID *int64, sessionHash string, accountID int64, reqLog *zap.Logger, reason string) bool {
	if h == nil || h.gatewayService == nil || sessionHash == "" || accountID <= 0 {
		return false
	}
	cleared, err := h.gatewayService.ClearStickySessionIfBoundTo(ctx, groupID, sessionHash, accountID)
	if err != nil {
		reqLog.Warn("openai.clear_sticky_session_failed", zap.Int64("account_id", accountID), zap.String("reason", reason), zap.Error(err))
		return false
	}
	if cleared {
		reqLog.Info("openai.sticky_session_cleared", zap.Int64("account_id", accountID), zap.String("reason", reason))
	}
	return cleared
}
