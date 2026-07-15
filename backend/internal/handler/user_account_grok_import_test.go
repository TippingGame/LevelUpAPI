//go:build unit

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type ownedGrokImportAccountRepoStub struct {
	service.AccountRepository
	created       *service.Account
	boundGroupIDs []int64
}

func (s *ownedGrokImportAccountRepoStub) Create(_ context.Context, account *service.Account) error {
	account.ID = 901
	s.created = account
	return nil
}

func (s *ownedGrokImportAccountRepoStub) BindGroups(_ context.Context, _ int64, groupIDs []int64) error {
	s.boundGroupIDs = append([]int64(nil), groupIDs...)
	return nil
}

type ownedGrokImportPrivateGroupStub struct {
	group *service.Group
}

func (s ownedGrokImportPrivateGroupStub) ProvisionUserPrivateGroups(context.Context, int64) error {
	return nil
}

func (s ownedGrokImportPrivateGroupStub) GetActiveUserPrivateGroup(context.Context, int64, string) (*service.Group, error) {
	return s.group, nil
}

type ownedGrokImportProxyRepoStub struct {
	service.ProxyRepository
	proxy *service.Proxy
}

func (s ownedGrokImportProxyRepoStub) GetByID(context.Context, int64) (*service.Proxy, error) {
	return s.proxy, nil
}

type ownedGrokImportOAuthClientStub struct {
	service.GrokOAuthClient
	refreshToken string
	proxyURL     string
	clientID     string
}

func (s *ownedGrokImportOAuthClientStub) RefreshToken(_ context.Context, refreshToken, proxyURL, clientID string) (*xai.TokenResponse, error) {
	s.refreshToken = refreshToken
	s.proxyURL = proxyURL
	s.clientID = clientID
	return &xai.TokenResponse{
		AccessToken:  "grok-access-token",
		RefreshToken: "grok-rotated-refresh-token",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
	}, nil
}

func newOwnedGrokImportTestHandler(t *testing.T) (*UserAccountHandler, *ownedGrokImportAccountRepoStub, *ownedGrokImportOAuthClientStub, int64) {
	t.Helper()
	proxyID := int64(42)
	accountRepo := &ownedGrokImportAccountRepoStub{}
	accountService := service.NewAccountService(accountRepo, nil, nil, nil)
	accountService.SetUserPrivateGroupProvisioner(ownedGrokImportPrivateGroupStub{group: &service.Group{ID: 77, Platform: service.PlatformGrok}})
	oauthClient := &ownedGrokImportOAuthClientStub{}
	oauthService := service.NewGrokOAuthService(ownedGrokImportProxyRepoStub{proxy: &service.Proxy{
		ID:       proxyID,
		Protocol: "http",
		Host:     "proxy.test",
		Port:     8080,
		Status:   service.StatusActive,
	}}, oauthClient)
	t.Cleanup(oauthService.Stop)
	return &UserAccountHandler{
		accountService:   accountService,
		grokOAuthService: oauthService,
	}, accountRepo, oauthClient, proxyID
}

func TestNormalizeGrokRefreshTokenImportDeduplicatesTextareaInput(t *testing.T) {
	got := normalizeGrokRefreshTokenImport([]string{
		"refresh-one\nrefresh-two",
		"refresh-one, refresh-three\r\n",
	})
	require.Equal(t, []string{"refresh-one", "refresh-two", "refresh-three"}, got)
}

func TestCreateOwnedAccountFromGrokRefreshTokenJSONExchangesAndImports(t *testing.T) {
	handler, accountRepo, oauthClient, proxyID := newOwnedGrokImportTestHandler(t)
	sources, parseErrors := service.ParseAccountCredentialImportContents([]string{
		`{"name":"Shared Grok","platform":"grok","refresh_token":"grok-refresh-token","client_id":"untrusted-client"}`,
	})
	require.Empty(t, parseErrors)
	require.Len(t, sources, 1)
	require.Equal(t, service.AccountCredentialImportKindGrokRefreshToken, sources[0].Kind)

	account, err := handler.createOwnedAccountFromCredentialImportSource(context.Background(), 123, sources[0], importUserAccountCredentialsRequest{
		Platform:    service.PlatformGrok,
		ProxyID:     &proxyID,
		ShareMode:   service.AccountShareModePrivate,
		Concurrency: 8,
	}, 1)

	require.NoError(t, err)
	require.Same(t, accountRepo.created, account)
	require.Equal(t, "grok-refresh-token", oauthClient.refreshToken)
	require.Equal(t, "http://proxy.test:8080", oauthClient.proxyURL)
	require.Equal(t, xai.EffectiveClientID(), oauthClient.clientID)
	require.Equal(t, service.PlatformGrok, account.Platform)
	require.Equal(t, service.AccountTypeOAuth, account.Type)
	require.Equal(t, "Shared Grok", account.Name)
	require.Equal(t, int64(123), *account.OwnerUserID)
	require.Equal(t, proxyID, *account.ProxyID)
	require.Equal(t, 1, account.Concurrency)
	require.Equal(t, "grok-access-token", account.Credentials["access_token"])
	require.Equal(t, "grok-rotated-refresh-token", account.Credentials["refresh_token"])
	require.NotContains(t, account.Credentials, "base_url")
	require.NotEmpty(t, account.Credentials["model_mapping"])
	require.Equal(t, []int64{77}, accountRepo.boundGroupIDs)
}

func TestCreateOwnedAccountFromGrokOAuthJSONImportsOfficialCredentials(t *testing.T) {
	handler, accountRepo, _, proxyID := newOwnedGrokImportTestHandler(t)
	sources, parseErrors := service.ParseAccountCredentialImportContents([]string{
		`{"name":"Shared Grok OAuth","platform":"grok","type":"oauth","credentials":{"access_token":"grok-access-token","refresh_token":"grok-refresh-token","base_url":"https://api.x.ai/v1"}}`,
	})
	require.Empty(t, parseErrors)
	require.Len(t, sources, 1)
	require.Equal(t, service.AccountCredentialImportKindOAuthCredentials, sources[0].Kind)

	account, err := handler.createOwnedAccountFromCredentialImportSource(context.Background(), 125, sources[0], importUserAccountCredentialsRequest{
		Platform:    service.PlatformGrok,
		ProxyID:     &proxyID,
		ShareMode:   service.AccountShareModePrivate,
		Concurrency: 7,
	}, 1)

	require.NoError(t, err)
	require.Same(t, accountRepo.created, account)
	require.Equal(t, service.PlatformGrok, account.Platform)
	require.Equal(t, service.AccountTypeOAuth, account.Type)
	require.Equal(t, "Shared Grok OAuth", account.Name)
	require.Equal(t, int64(125), *account.OwnerUserID)
	require.Equal(t, proxyID, *account.ProxyID)
	require.Equal(t, 1, account.Concurrency)
	require.Equal(t, "grok-access-token", account.Credentials["access_token"])
	require.Equal(t, "grok-refresh-token", account.Credentials["refresh_token"])
	require.NotContains(t, account.Credentials, "base_url")
	require.NotEmpty(t, account.Credentials["model_mapping"])
}

func TestCreateOwnedGrokTokenInfoImportDoesNotPersistBaseURL(t *testing.T) {
	handler, accountRepo, _, proxyID := newOwnedGrokImportTestHandler(t)

	name, err := handler.createOwnedGrokAccountFromTokenInfo(context.Background(), 124, importUserAccountCredentialsRequest{
		Platform:    service.PlatformGrok,
		ProxyID:     &proxyID,
		ShareMode:   service.AccountShareModePrivate,
		Concurrency: 6,
	}, &service.GrokTokenInfo{
		AccessToken:  "grok-access-token",
		RefreshToken: "grok-refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}, 1, 1)

	require.NoError(t, err)
	require.Equal(t, "Grok OAuth Account", name)
	require.NotNil(t, accountRepo.created)
	require.Equal(t, 1, accountRepo.created.Concurrency)
	require.Equal(t, proxyID, *accountRepo.created.ProxyID)
	require.NotContains(t, accountRepo.created.Credentials, "base_url")
}
