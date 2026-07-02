package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func resetAPIKeyGroupRouteBreakerForTest(t *testing.T) {
	t.Helper()
	orig := apiKeyGroupRouteBreaker
	apiKeyGroupRouteBreaker = newAPIKeyGroupRouteCircuitBreaker()
	t.Cleanup(func() { apiKeyGroupRouteBreaker = orig })
}

func testAPIKeyGroupRouteCandidate(groupID int64) apiKeyGroupRouteCandidate {
	return apiKeyGroupRouteCandidate{
		APIKey: &service.APIKey{ID: 10, GroupID: &groupID, Group: &service.Group{ID: groupID, Hydrated: true}},
		Route: service.APIKeyGroupRoute{
			GroupID:         groupID,
			Priority:        int(groupID),
			Weight:          1,
			Enabled:         true,
			CooldownSeconds: 30,
			Group:           &service.Group{ID: groupID, Hydrated: true},
		},
	}
}

func TestAPIKeyGroupRouteCursor_SwitchLoopsThroughRoutesUntilAttemptLimit(t *testing.T) {
	resetAPIKeyGroupRouteBreakerForTest(t)
	cursor := newAPIKeyGroupRouteCursorFromCandidates([]apiKeyGroupRouteCandidate{
		testAPIKeyGroupRouteCandidate(1),
		testAPIKeyGroupRouteCandidate(2),
	}, true)

	require.True(t, cursor.switchToNext(10, "first_failure", nil))
	current, ok := cursor.current()
	require.True(t, ok)
	require.Equal(t, int64(2), current.Route.GroupID)

	require.True(t, cursor.switchToNext(10, "failure_2", nil))
	current, ok = cursor.current()
	require.True(t, ok)
	require.Equal(t, int64(1), current.Route.GroupID)

	require.True(t, cursor.switchToNext(10, "failure_3", nil))
	current, ok = cursor.current()
	require.True(t, ok)
	require.Equal(t, int64(2), current.Route.GroupID)

	require.False(t, cursor.switchToNext(10, "failure_4", nil))
	require.Equal(t, maxAPIKeyGroupRouteCyclesPerRequest*2, cursor.attempts)
}

func TestBuildAPIKeyGroupRouteCandidates_SkipsCoolingDownRoutesForNewRequest(t *testing.T) {
	resetAPIKeyGroupRouteBreakerForTest(t)
	group1 := &service.Group{ID: 1, Status: service.StatusActive, Platform: service.PlatformAnthropic, Hydrated: true}
	group2 := &service.Group{ID: 2, Status: service.StatusActive, Platform: service.PlatformAnthropic, Hydrated: true}
	apiKey := &service.APIKey{
		ID:   10,
		User: &service.User{ID: 7, Status: service.StatusActive},
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: group1},
			{GroupID: 2, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: group2},
		},
	}
	apiKeyGroupRouteBreaker.recordFailure(apiKey.ID, 1, 30)

	candidates, available := buildAPIKeyGroupRouteCandidates(apiKey)

	require.True(t, available)
	require.Len(t, candidates, 1)
	require.Equal(t, int64(2), candidates[0].Route.GroupID)
}

func TestBuildAPIKeyGroupRouteCandidates_FallsBackWhenAllRoutesCoolingDown(t *testing.T) {
	resetAPIKeyGroupRouteBreakerForTest(t)
	group1 := &service.Group{ID: 1, Status: service.StatusActive, Platform: service.PlatformAnthropic, Hydrated: true}
	group2 := &service.Group{ID: 2, Status: service.StatusActive, Platform: service.PlatformAnthropic, Hydrated: true}
	apiKey := &service.APIKey{
		ID:   10,
		User: &service.User{ID: 7, Status: service.StatusActive},
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: group1},
			{GroupID: 2, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: group2},
		},
	}
	apiKeyGroupRouteBreaker.recordFailure(apiKey.ID, 1, 30)
	apiKeyGroupRouteBreaker.recordFailure(apiKey.ID, 2, 30)

	candidates, available := buildAPIKeyGroupRouteCandidates(apiKey)

	require.True(t, available)
	require.Len(t, candidates, 2)
	require.Equal(t, int64(1), candidates[0].Route.GroupID)
	require.Equal(t, int64(2), candidates[1].Route.GroupID)
}

func TestBuildAPIKeyGroupRouteCandidates_SkipsUnavailableRoutes(t *testing.T) {
	resetAPIKeyGroupRouteBreakerForTest(t)
	group1 := &service.Group{ID: 1, Status: service.StatusDisabled, Platform: service.PlatformAnthropic, Hydrated: true}
	group2 := &service.Group{ID: 2, Status: service.StatusActive, Platform: service.PlatformAnthropic, Hydrated: true}
	apiKey := &service.APIKey{
		ID:      10,
		User:    &service.User{ID: 7, Status: service.StatusActive},
		Group:   group1,
		GroupID: &group1.ID,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 1, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: group1},
			{GroupID: 2, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: group2},
		},
	}

	candidates, available := buildAPIKeyGroupRouteCandidates(apiKey)

	require.True(t, available)
	require.Len(t, candidates, 1)
	require.Equal(t, int64(2), candidates[0].Route.GroupID)
	require.Equal(t, group2, candidates[0].APIKey.Group)
}
