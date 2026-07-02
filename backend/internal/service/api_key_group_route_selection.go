package service

import (
	"sort"
	"strings"
)

// IsAPIKeyGroupAvailable reports whether a group can be used by runtime routing.
func IsAPIKeyGroupAvailable(group *Group) bool {
	if group == nil || strings.EqualFold(group.Status, "deleted") {
		return false
	}
	return group.IsActive()
}

// CanAPIKeyUserUseGroup checks runtime user/group authorization using auth-cache data.
// Subscription groups are validated by subscription/billing checks later in the request.
func CanAPIKeyUserUseGroup(user *User, group *Group) bool {
	if user == nil || group == nil {
		return false
	}
	if group.IsSubscriptionType() {
		return true
	}
	return user.CanBindGroup(group.ID, group.IsExclusive)
}

func APIKeyGroupRouteLess(a, b APIKeyGroupRoute) bool {
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	if a.Weight != b.Weight {
		return a.Weight > b.Weight
	}
	return a.GroupID < b.GroupID
}

func SortAPIKeyGroupRoutes(routes []APIKeyGroupRoute) {
	sort.SliceStable(routes, func(i, j int) bool {
		return APIKeyGroupRouteLess(routes[i], routes[j])
	})
}

// IsAPIKeyGroupRouteSelectable reports whether a configured API key group route
// is a viable candidate before account selection.
func IsAPIKeyGroupRouteSelectable(apiKey *APIKey, route APIKeyGroupRoute) bool {
	if apiKey == nil || !route.Enabled || route.GroupID <= 0 || route.Group == nil {
		return false
	}
	if route.Group.ID != route.GroupID {
		return false
	}
	if !IsAPIKeyGroupAvailable(route.Group) {
		return false
	}
	return CanAPIKeyUserUseGroup(apiKey.User, route.Group)
}

// FirstSelectableAPIKeyGroupRoute returns the first usable route in the same
// ordering used by request routing. A legacy single group_id is treated as a
// one-route configuration.
func FirstSelectableAPIKeyGroupRoute(apiKey *APIKey) (APIKeyGroupRoute, bool) {
	if apiKey == nil {
		return APIKeyGroupRoute{}, false
	}
	routes := append([]APIKeyGroupRoute(nil), apiKey.GroupRoutes...)
	if len(routes) == 0 && apiKey.GroupID != nil && apiKey.Group != nil {
		routes = append(routes, APIKeyGroupRoute{
			GroupID:         *apiKey.GroupID,
			Priority:        100,
			Weight:          1,
			Enabled:         true,
			CooldownSeconds: 30,
			Group:           apiKey.Group,
		})
	}
	SortAPIKeyGroupRoutes(routes)
	for _, route := range routes {
		if IsAPIKeyGroupRouteSelectable(apiKey, route) {
			return route, true
		}
	}
	return APIKeyGroupRoute{}, false
}

// HasAPIKeyGroupAssignment reports whether the key has any configured group
// binding, including multi-group route bindings.
func HasAPIKeyGroupAssignment(apiKey *APIKey) bool {
	if apiKey == nil {
		return false
	}
	if apiKey.GroupID != nil && *apiKey.GroupID > 0 {
		return true
	}
	for _, route := range apiKey.GroupRoutes {
		if route.GroupID > 0 {
			return true
		}
	}
	return false
}
