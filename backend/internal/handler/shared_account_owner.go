package handler

import (
	"context"
	"log/slog"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func sharedAccountOwnerStatusForUser(ctx context.Context, attrService *service.UserAttributeService, user *service.User) service.SharedAccountOwnerStatus {
	status := service.SharedAccountOwnerStatusFromUser(user)
	if attrService == nil || user == nil || user.ID <= 0 {
		return status
	}
	resolved, err := attrService.ResolveSharedAccountOwnerStatus(ctx, user)
	if err != nil {
		slog.Warn("failed to resolve shared account owner status", "user_id", user.ID, "error", err)
		return status
	}
	return resolved
}

func applySharedAccountOwnerStatus(out *dto.User, status service.SharedAccountOwnerStatus) {
	if out == nil {
		return
	}
	out.CanManageUserAccounts = status.Enabled
	out.SharedAccountOwnerStatus = dto.SharedAccountOwnerStatusFromService(status)
}
