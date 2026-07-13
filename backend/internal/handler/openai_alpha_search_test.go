package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyHasConfiguredOpenAIGroupAcrossRoutes(t *testing.T) {
	openAIGroup := &service.Group{ID: 2, Platform: service.PlatformOpenAI}
	grokGroup := &service.Group{ID: 1, Platform: service.PlatformGrok}
	apiKey := &service.APIKey{
		Group: grokGroup,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Group: grokGroup},
			{GroupID: 2, Group: openAIGroup},
		},
	}

	require.True(t, apiKeyHasConfiguredOpenAIGroup(apiKey))
	require.False(t, apiKeyHasConfiguredOpenAIGroup(&service.APIKey{Group: grokGroup}))
	require.False(t, apiKeyHasConfiguredOpenAIGroup(&service.APIKey{
		Group: openAIGroup,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Group: grokGroup},
		},
	}))
}

func TestNewOpenAIAlphaSearchRouteCursorFiltersNonOpenAIGroups(t *testing.T) {
	resetAPIKeyGroupRouteBreakerForTest(t)
	openAIGroup := &service.Group{ID: 2, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true}
	grokGroup := &service.Group{ID: 1, Platform: service.PlatformGrok, Status: service.StatusActive, Hydrated: true}
	apiKey := &service.APIKey{
		ID:   10,
		User: &service.User{ID: 7, Status: service.StatusActive},
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Priority: 1, Weight: 1, Enabled: true, Group: grokGroup},
			{GroupID: 2, Priority: 2, Weight: 1, Enabled: true, Group: openAIGroup},
		},
	}

	cursor := newOpenAIAlphaSearchRouteCursor(apiKey)
	candidate, ok := cursor.current()
	require.True(t, ok)
	require.Equal(t, int64(2), candidate.Route.GroupID)
	require.Len(t, cursor.candidates, 1)
}
