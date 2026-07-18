package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// CreateShadowRequest is the request body for CreateShadow.
type CreateShadowRequest struct {
	Name        string  `json:"name"`
	Priority    int     `json:"priority"`
	Concurrency int     `json:"concurrency"`
	GroupIDs    []int64 `json:"group_ids"`
}

// CreateShadow creates a spark-dimension shadow account for an OpenAI OAuth parent account.
func (h *OpenAIOAuthHandler) CreateShadow(c *gin.Context) {
	parentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	var req CreateShadowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	shadow, err := h.adminService.CreateShadow(c.Request.Context(), parentID, service.ShadowOptions{
		Name:        req.Name,
		Priority:    req.Priority,
		Concurrency: req.Concurrency,
		GroupIDs:    req.GroupIDs,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromServiceShallow(shadow))
}
