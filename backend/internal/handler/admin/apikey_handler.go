package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AdminAPIKeyHandler handles admin API key management
type AdminAPIKeyHandler struct {
	adminService service.AdminService
}

// NewAdminAPIKeyHandler creates a new admin API key handler
func NewAdminAPIKeyHandler(adminService service.AdminService) *AdminAPIKeyHandler {
	return &AdminAPIKeyHandler{
		adminService: adminService,
	}
}

// AdminUpdateAPIKeyGroupRequest represents the request to update an API key.
type AdminUpdateAPIKeyGroupRequest struct {
	GroupID             *int64                          `json:"group_id"`               // nil=不修改, 0=解绑, >0=绑定到目标分组
	GroupRoutes         *[]AdminAPIKeyGroupRouteRequest `json:"group_routes"`           // nil=不修改, []=解绑, 非空=多分组路由
	ResetRateLimitUsage *bool                           `json:"reset_rate_limit_usage"` // true=重置 5h/1d/7d 限速用量
}

type AdminAPIKeyGroupRouteRequest struct {
	GroupID         int64 `json:"group_id"`
	Priority        int   `json:"priority"`
	Weight          int   `json:"weight"`
	Enabled         *bool `json:"enabled"`
	CooldownSeconds int   `json:"cooldown_seconds"`
}

func adminAPIKeyGroupRouteRequestsToService(routes []AdminAPIKeyGroupRouteRequest) []service.APIKeyGroupRoute {
	if len(routes) == 0 {
		return nil
	}
	out := make([]service.APIKeyGroupRoute, 0, len(routes))
	for _, route := range routes {
		enabled := true
		if route.Enabled != nil {
			enabled = *route.Enabled
		}
		out = append(out, service.APIKeyGroupRoute{
			GroupID:         route.GroupID,
			Priority:        route.Priority,
			Weight:          route.Weight,
			Enabled:         enabled,
			CooldownSeconds: route.CooldownSeconds,
		})
	}
	return out
}

// UpdateGroup handles updating an API key's admin-managed fields.
// PUT /api/v1/admin/api-keys/:id
func (h *AdminAPIKeyHandler) UpdateGroup(c *gin.Context) {
	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid API key ID")
		return
	}

	var req AdminUpdateAPIKeyGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	var resetKey *service.APIKey
	if req.ResetRateLimitUsage != nil && *req.ResetRateLimitUsage {
		resetKey, err = h.adminService.AdminResetAPIKeyRateLimitUsage(c.Request.Context(), keyID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	var result *service.AdminUpdateAPIKeyGroupIDResult
	if req.GroupRoutes != nil {
		routes := adminAPIKeyGroupRouteRequestsToService(*req.GroupRoutes)
		result, err = h.adminService.AdminUpdateAPIKeyGroupRoutes(c.Request.Context(), keyID, req.GroupID, routes)
	} else {
		result, err = h.adminService.AdminUpdateAPIKeyGroupID(c.Request.Context(), keyID, req.GroupID)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if resetKey != nil && req.GroupID == nil && req.GroupRoutes == nil {
		result.APIKey = resetKey
	}

	resp := struct {
		APIKey                 *dto.APIKey `json:"api_key"`
		AutoGrantedGroupAccess bool        `json:"auto_granted_group_access"`
		GrantedGroupID         *int64      `json:"granted_group_id,omitempty"`
		GrantedGroupName       string      `json:"granted_group_name,omitempty"`
	}{
		APIKey:                 dto.APIKeyFromService(result.APIKey),
		AutoGrantedGroupAccess: result.AutoGrantedGroupAccess,
		GrantedGroupID:         result.GrantedGroupID,
		GrantedGroupName:       result.GrantedGroupName,
	}
	response.Success(c, resp)
}
