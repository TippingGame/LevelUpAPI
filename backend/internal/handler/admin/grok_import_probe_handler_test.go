//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type grokImportAdminService struct {
	*stubAdminService
	mu     sync.Mutex
	nextID int64
}

func newGrokImportAdminService() *grokImportAdminService {
	return &grokImportAdminService{
		stubAdminService: newStubAdminService(),
		nextID:           500,
	}
}

func (s *grokImportAdminService) CreateAccount(_ context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.mu.Unlock()
	return &service.Account{
		ID:          id,
		Name:        input.Name,
		Platform:    input.Platform,
		Type:        input.Type,
		Credentials: input.Credentials,
		Extra:       input.Extra,
		ProxyID:     input.ProxyID,
		Concurrency: input.Concurrency,
		Status:      service.StatusActive,
		Schedulable: true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, nil
}

func TestAccountCreateWithoutAutomaticGrokProbeServiceStillSucceeds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAccountHandler(
		newGrokImportAdminService(),
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)

	router := gin.New()
	router.POST("/api/v1/admin/accounts", handler.Create)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/accounts",
		strings.NewReader(`{"name":"grok-rt","platform":"grok","type":"oauth","credentials":{"refresh_token":"secret"}}`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
}
