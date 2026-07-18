package admin

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/quotaview"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetUserPlatformQuotas returns the configured platform quotas for one user.
func (h *UserHandler) GetUserPlatformQuotas(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid user id")
		return
	}
	if h.userPlatformQuotaRepo == nil {
		response.Success(c, map[string]any{"platform_quotas": []any{}})
		return
	}
	if _, err := h.adminService.GetUser(c.Request.Context(), userID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	records, err := h.userPlatformQuotaRepo.ListByUser(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	now := time.Now().UTC()
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		out = append(out, quotaview.LazyZeroQuotaForResponse(record, now, true))
	}
	response.Success(c, map[string]any{"platform_quotas": out})
}

// UpdateUserPlatformQuotasRequest is the body for PUT /admin/users/:id/platform-quotas.
type UpdateUserPlatformQuotasRequest struct {
	Quotas []PlatformQuotaInput `json:"quotas" binding:"required"`
}

// PlatformQuotaInput is the limit input for one platform. A nil limit means unlimited.
type PlatformQuotaInput struct {
	Platform        string   `json:"platform" binding:"required"`
	DailyLimitUSD   *float64 `json:"daily_limit_usd"`
	WeeklyLimitUSD  *float64 `json:"weekly_limit_usd"`
	MonthlyLimitUSD *float64 `json:"monthly_limit_usd"`
}

// UpdateUserPlatformQuotas replaces all platform quotas for one user.
func (h *UserHandler) UpdateUserPlatformQuotas(c *gin.Context) {
	if h.userPlatformQuotaRepo == nil {
		response.Error(c, 503, "platform quota service not available")
		return
	}

	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid user ID")
		return
	}

	var req UpdateUserPlatformQuotasRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len(req.Quotas) > len(service.AllowedQuotaPlatforms) {
		response.BadRequest(c, fmt.Sprintf("quotas length must be <= %d", len(service.AllowedQuotaPlatforms)))
		return
	}

	seen := make(map[string]struct{}, len(req.Quotas))
	for _, quota := range req.Quotas {
		if !service.IsAllowedQuotaPlatform(quota.Platform) {
			response.BadRequest(c, "invalid platform: "+quota.Platform)
			return
		}
		if _, duplicate := seen[quota.Platform]; duplicate {
			response.BadRequest(c, "duplicate platform: "+quota.Platform)
			return
		}
		seen[quota.Platform] = struct{}{}

		for _, limit := range []struct {
			name  string
			value *float64
		}{
			{name: "daily_limit_usd", value: quota.DailyLimitUSD},
			{name: "weekly_limit_usd", value: quota.WeeklyLimitUSD},
			{name: "monthly_limit_usd", value: quota.MonthlyLimitUSD},
		} {
			if limit.value == nil {
				continue
			}
			if *limit.value < 0 {
				response.BadRequest(c, limit.name+" must be >= 0")
				return
			}
			if math.IsNaN(*limit.value) || math.IsInf(*limit.value, 0) {
				response.BadRequest(c, limit.name+" must be a finite number")
				return
			}
		}
	}

	records := make([]service.UserPlatformQuotaRecord, 0, len(req.Quotas))
	for _, quota := range req.Quotas {
		records = append(records, service.UserPlatformQuotaRecord{
			UserID:          userID,
			Platform:        quota.Platform,
			DailyLimitUSD:   quota.DailyLimitUSD,
			WeeklyLimitUSD:  quota.WeeklyLimitUSD,
			MonthlyLimitUSD: quota.MonthlyLimitUSD,
		})
	}

	ctx := c.Request.Context()
	if _, err := h.adminService.GetUser(ctx, userID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	beforeRecords, beforeErr := h.userPlatformQuotaRepo.ListByUser(ctx, userID)
	if beforeErr != nil {
		slog.Warn("quota audit before snapshot failed", "user_id", userID, "err", beforeErr)
	}
	if err := h.userPlatformQuotaRepo.UpsertForUser(ctx, userID, records); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	beforeByPlatform := make(map[string]service.UserPlatformQuotaRecord, len(beforeRecords))
	for _, record := range beforeRecords {
		beforeByPlatform[record.Platform] = record
	}
	afterPlatforms := make(map[string]struct{}, len(records))
	for _, record := range records {
		afterPlatforms[record.Platform] = struct{}{}
	}
	changes := make([]map[string]any, 0, len(records))
	for _, record := range records {
		entry := map[string]any{
			"platform":          record.Platform,
			"daily_limit_usd":   record.DailyLimitUSD,
			"weekly_limit_usd":  record.WeeklyLimitUSD,
			"monthly_limit_usd": record.MonthlyLimitUSD,
		}
		if previous, ok := beforeByPlatform[record.Platform]; ok {
			entry["before_daily_limit_usd"] = previous.DailyLimitUSD
			entry["before_weekly_limit_usd"] = previous.WeeklyLimitUSD
			entry["before_monthly_limit_usd"] = previous.MonthlyLimitUSD
		}
		changes = append(changes, entry)
	}
	for _, previous := range beforeRecords {
		if _, kept := afterPlatforms[previous.Platform]; kept {
			continue
		}
		changes = append(changes, map[string]any{
			"platform":                 previous.Platform,
			"removed":                  true,
			"before_daily_limit_usd":   previous.DailyLimitUSD,
			"before_weekly_limit_usd":  previous.WeeklyLimitUSD,
			"before_monthly_limit_usd": previous.MonthlyLimitUSD,
		})
	}
	slog.Info("admin.quota_updated",
		"actor_admin_id", getAdminIDFromContext(c),
		"target_user_id", userID,
		"platform_count", len(records),
		"before_snapshot_available", beforeErr == nil,
		"changes", changes,
	)

	if h.billingCache != nil {
		for _, platform := range service.AllowedQuotaPlatforms {
			if err := h.billingCache.DeleteUserPlatformQuotaCache(ctx, userID, platform); err != nil {
				slog.Error("quota cache invalidation failed after quota update", "user_id", userID, "platform", platform, "err", err)
			}
		}
	}

	latest, err := h.userPlatformQuotaRepo.ListByUser(ctx, userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	now := time.Now().UTC()
	out := make([]map[string]any, 0, len(latest))
	for _, record := range latest {
		out = append(out, quotaview.LazyZeroQuotaForResponse(record, now, true))
	}
	response.Success(c, map[string]any{"platform_quotas": out})
}

// ResetUserPlatformQuotaWindowRequest is the body for POST /admin/users/:id/platform-quotas/reset.
type ResetUserPlatformQuotaWindowRequest struct {
	Platform string `json:"platform" binding:"required"`
	Window   string `json:"window" binding:"required"`
}

var allowedWindowsForQuotaReset = map[string]struct{}{
	"daily":   {},
	"weekly":  {},
	"monthly": {},
}

// ResetUserPlatformQuotaWindow resets one usage window immediately.
func (h *UserHandler) ResetUserPlatformQuotaWindow(c *gin.Context) {
	if h.userPlatformQuotaRepo == nil {
		response.Error(c, 503, "platform quota service not available")
		return
	}

	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid user ID")
		return
	}

	var req ResetUserPlatformQuotaWindowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !service.IsAllowedQuotaPlatform(req.Platform) {
		response.BadRequest(c, "invalid platform: "+req.Platform)
		return
	}
	if _, ok := allowedWindowsForQuotaReset[req.Window]; !ok {
		response.BadRequest(c, "invalid window: "+req.Window)
		return
	}

	ctx := c.Request.Context()
	if _, err := h.adminService.GetUser(ctx, userID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	now := time.Now().UTC()
	if err := h.userPlatformQuotaRepo.ResetExpiredWindow(ctx, userID, req.Platform, req.Window, now); err != nil {
		if errors.Is(err, service.ErrUserPlatformQuotaNotFound) {
			response.NotFound(c, "user platform quota not found")
			return
		}
		response.ErrorFrom(c, err)
		return
	}

	slog.Info("admin.quota_window_reset",
		"actor_admin_id", getAdminIDFromContext(c),
		"target_user_id", userID,
		"platform", req.Platform,
		"window", req.Window,
	)
	if h.billingCache != nil {
		if err := h.billingCache.DeleteUserPlatformQuotaCache(ctx, userID, req.Platform); err != nil {
			slog.Error("quota cache invalidation failed after window reset", "user_id", userID, "platform", req.Platform, "err", err)
		}
	}

	latest, err := h.userPlatformQuotaRepo.ListByUser(ctx, userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]map[string]any, 0, len(latest))
	for _, record := range latest {
		out = append(out, quotaview.LazyZeroQuotaForResponse(record, now, true))
	}
	response.Success(c, map[string]any{"platform_quotas": out})
}
