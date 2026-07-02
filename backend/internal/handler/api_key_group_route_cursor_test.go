package handler

import (
	"net/http"
	"testing"
	"time"

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

func TestAPIKeyGroupRouteBreaker_RecordFailurePrunesExpiredStates(t *testing.T) {
	breaker := newAPIKeyGroupRouteCircuitBreaker()
	breaker.recordFailure(10, 1, 30)
	expiredKey := apiKeyGroupRouteBreakerKey(10, 1)
	breaker.mu.Lock()
	state := breaker.states[expiredKey]
	state.cooldownUntil = time.Now().Add(-time.Second)
	breaker.states[expiredKey] = state
	breaker.mu.Unlock()

	breaker.recordFailure(10, 2, 30)

	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	_, hasExpired := breaker.states[expiredKey]
	_, hasFresh := breaker.states[apiKeyGroupRouteBreakerKey(10, 2)]
	require.False(t, hasExpired)
	require.True(t, hasFresh)
}

func TestAPIKeyGroupRouteBreaker_RecordFailureCapsStateCount(t *testing.T) {
	breaker := newAPIKeyGroupRouteCircuitBreaker()

	for i := int64(1); i <= int64(maxAPIKeyGroupRouteBreakerStates+8); i++ {
		breaker.recordFailure(10, i, 30)
	}

	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	require.LessOrEqual(t, len(breaker.states), maxAPIKeyGroupRouteBreakerStates)
	require.Contains(t, breaker.states, apiKeyGroupRouteBreakerKey(10, int64(maxAPIKeyGroupRouteBreakerStates+8)))
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

func TestWeightedAPIKeyGroupRouteStartIndexUsesBestPriorityBucket(t *testing.T) {
	candidates := []apiKeyGroupRouteCandidate{
		{Route: service.APIKeyGroupRoute{GroupID: 1, Priority: 100, Weight: 1}},
		{Route: service.APIKeyGroupRoute{GroupID: 2, Priority: 100, Weight: 3}},
		{Route: service.APIKeyGroupRoute{GroupID: 3, Priority: 200, Weight: 1000}},
	}

	bucketLen, totalWeight := apiKeyGroupRouteBestPriorityBucket(candidates)
	require.Equal(t, 2, bucketLen)
	require.Equal(t, 4, totalWeight)
	require.Equal(t, 0, weightedAPIKeyGroupRouteStartIndex(candidates, 0))
	require.Equal(t, 1, weightedAPIKeyGroupRouteStartIndex(candidates, 1))
	require.Equal(t, 1, weightedAPIKeyGroupRouteStartIndex(candidates, 3))
	require.Equal(t, 1, weightedAPIKeyGroupRouteStartIndex(candidates, 5))
}

func TestRotateAPIKeyGroupRouteCandidatesByStartKeepsLowerPriorityFallbacks(t *testing.T) {
	candidates := []apiKeyGroupRouteCandidate{
		{Route: service.APIKeyGroupRoute{GroupID: 1, Priority: 100, Weight: 1}},
		{Route: service.APIKeyGroupRoute{GroupID: 2, Priority: 100, Weight: 1}},
		{Route: service.APIKeyGroupRoute{GroupID: 3, Priority: 200, Weight: 1}},
	}

	rotated := rotateAPIKeyGroupRouteCandidatesByStart(candidates, 2, 1)

	require.Equal(t, []int64{2, 1, 3}, []int64{
		rotated[0].Route.GroupID,
		rotated[1].Route.GroupID,
		rotated[2].Route.GroupID,
	})
}

func TestShouldSkipRouteOnSubscriptionResolveError(t *testing.T) {
	require.True(t, shouldSkipRouteOnSubscriptionResolveError(service.ErrSubscriptionNotFound))
	require.True(t, shouldSkipRouteOnSubscriptionResolveError(service.ErrSubscriptionInvalid))
	require.True(t, shouldSkipRouteOnSubscriptionResolveError(service.ErrSubscriptionExpired))
	require.True(t, shouldSkipRouteOnSubscriptionResolveError(service.ErrSubscriptionSuspended))
	require.True(t, shouldSkipRouteOnSubscriptionResolveError(service.ErrSubscriptionRepositoryUnavailable))
	require.False(t, shouldSkipRouteOnSubscriptionResolveError(service.ErrInsufficientBalance))
}

func TestRouteSubscriptionSkipDoesNotCooldownRoute(t *testing.T) {
	resetAPIKeyGroupRouteBreakerForTest(t)
	cursor := newAPIKeyGroupRouteCursorFromCandidates([]apiKeyGroupRouteCandidate{
		testAPIKeyGroupRouteCandidate(1),
		testAPIKeyGroupRouteCandidate(2),
	}, true)

	require.True(t, cursor.skipToNext("route_subscription_resolve_failed", nil))
	require.True(t, apiKeyGroupRouteBreaker.available(10, 1, time.Now()))
	current, ok := cursor.current()
	require.True(t, ok)
	require.Equal(t, int64(2), current.Route.GroupID)
}

func TestShouldSwitchAPIKeyGroupRoute_SkipsReplayUnsafeTimeouts(t *testing.T) {
	require.False(t, shouldSwitchAPIKeyGroupRoute(&service.UpstreamFailoverError{StatusCode: http.StatusGatewayTimeout}))
	require.False(t, shouldSwitchAPIKeyGroupRoute(&service.UpstreamFailoverError{StatusCode: 524}))
	require.False(t, shouldSwitchAPIKeyGroupRoute(&service.UpstreamFailoverError{StatusCode: http.StatusBadRequest}))
	require.False(t, shouldSwitchAPIKeyGroupRoute(&service.UpstreamFailoverError{StatusCode: http.StatusRequestTimeout}))

	require.True(t, shouldSwitchAPIKeyGroupRoute(&service.UpstreamFailoverError{StatusCode: http.StatusUnauthorized}))
	require.True(t, shouldSwitchAPIKeyGroupRoute(&service.UpstreamFailoverError{StatusCode: http.StatusPaymentRequired}))
	require.True(t, shouldSwitchAPIKeyGroupRoute(&service.UpstreamFailoverError{StatusCode: http.StatusForbidden}))
	require.True(t, shouldSwitchAPIKeyGroupRoute(&service.UpstreamFailoverError{StatusCode: http.StatusBadGateway}))
	require.True(t, shouldSwitchAPIKeyGroupRoute(&service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}))
	require.True(t, shouldSwitchAPIKeyGroupRoute(&service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}))
	require.True(t, shouldSwitchAPIKeyGroupRoute(&service.UpstreamFailoverError{StatusCode: 529}))
}
