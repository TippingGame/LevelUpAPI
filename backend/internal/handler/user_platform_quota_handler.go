package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/quotaview"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

// GetMyPlatformQuotas returns the authenticated user's platform quota state.
func (h *UserHandler) GetMyPlatformQuotas(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h.userPlatformQuotaRepo == nil {
		response.Success(c, map[string]any{"platform_quotas": []any{}})
		return
	}

	records, err := h.userPlatformQuotaRepo.ListByUser(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	now := time.Now().UTC()
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		out = append(out, quotaview.LazyZeroQuotaForResponse(record, now, false))
	}
	response.Success(c, map[string]any{"platform_quotas": out})
}
