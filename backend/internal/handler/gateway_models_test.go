//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type gatewayModelsAccountRepoStub struct {
	service.AccountRepository

	byGroup map[int64][]service.Account
}

func (s *gatewayModelsAccountRepoStub) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]service.Account, error) {
	accounts, ok := s.byGroup[groupID]
	if !ok {
		return nil, nil
	}
	out := make([]service.Account, len(accounts))
	copy(out, accounts)
	return out, nil
}

type gatewayModelsResponseForTest struct {
	Object string                    `json:"object"`
	Data   []gatewayModelItemForTest `json:"data"`
}

type gatewayModelItemForTest struct {
	ID string `json:"id"`
}

func newGatewayModelsHandlerForTest(repo service.AccountRepository) *GatewayHandler {
	return &GatewayHandler{
		gatewayService: service.NewGatewayService(
			repo,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		),
	}
}

func TestGatewayModels_GeminiGroupFallsBackToGeminiModels(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(20)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{ID: 1, Platform: service.PlatformGemini},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformGemini},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "list", got.Object)
	require.Contains(t, modelIDsForTest(got.Data), "gemini-2.5-flash")
	require.NotContains(t, modelIDsForTest(got.Data), "claude-opus-4-6")
}

func TestGatewayModels_GeminiGroupFiltersMappedModelsByPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(21)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformAnthropic,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"claude-sonnet-4-6": "claude-sonnet-4-6",
							},
						},
					},
					{
						ID:       2,
						Platform: service.PlatformGemini,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"gemini-2.5-flash": "gemini-2.5-flash",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformGemini},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gemini-2.5-flash"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_GroupRoutesSkipUnavailableGroupsAndMergeMappedModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetAPIKeyGroupRouteBreakerForTest(t)

	disabledGroupID := int64(30)
	primaryGroupID := int64(31)
	fallbackGroupID := int64(32)
	disabledGroup := &service.Group{ID: disabledGroupID, Status: service.StatusDisabled, Platform: service.PlatformAnthropic}
	primaryGroup := &service.Group{ID: primaryGroupID, Status: service.StatusActive, Platform: service.PlatformAnthropic}
	fallbackGroup := &service.Group{ID: fallbackGroupID, Status: service.StatusActive, Platform: service.PlatformAnthropic}

	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				disabledGroupID: {
					{
						ID:       1,
						Platform: service.PlatformAnthropic,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"claude-disabled-only": "claude-disabled-only",
							},
						},
					},
				},
				primaryGroupID: {
					{
						ID:       2,
						Platform: service.PlatformAnthropic,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"claude-sonnet-route": "claude-sonnet-route",
								"claude-shared":       "claude-shared",
							},
						},
					},
				},
				fallbackGroupID: {
					{
						ID:       3,
						Platform: service.PlatformAnthropic,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"claude-opus-route": "claude-opus-route",
								"claude-shared":     "claude-shared",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      10,
		User:    &service.User{ID: 7, Status: service.StatusActive},
		GroupID: &disabledGroupID,
		Group:   disabledGroup,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: disabledGroupID, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: disabledGroup},
			{GroupID: primaryGroupID, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: primaryGroup},
			{GroupID: fallbackGroupID, Priority: 3, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: fallbackGroup},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"claude-opus-route", "claude-shared", "claude-sonnet-route"}, modelIDsForTest(got.Data))
	require.NotContains(t, modelIDsForTest(got.Data), "claude-disabled-only")
}

func TestGatewayModels_GroupRoutesFallbackUsesSelectableRoutePlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetAPIKeyGroupRouteBreakerForTest(t)

	disabledGroupID := int64(40)
	activeGroupID := int64(41)
	disabledGroup := &service.Group{ID: disabledGroupID, Status: service.StatusDisabled, Platform: service.PlatformAnthropic}
	activeGroup := &service.Group{ID: activeGroupID, Status: service.StatusActive, Platform: service.PlatformGemini}
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				activeGroupID: {
					{ID: 1, Platform: service.PlatformGemini},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      11,
		User:    &service.User{ID: 7, Status: service.StatusActive},
		GroupID: &disabledGroupID,
		Group:   disabledGroup,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: disabledGroupID, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: disabledGroup},
			{GroupID: activeGroupID, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: activeGroup},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Contains(t, modelIDsForTest(got.Data), "gemini-2.5-flash")
	require.NotContains(t, modelIDsForTest(got.Data), "claude-opus-4-6")
}

func modelIDsForTest(models []gatewayModelItemForTest) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}
