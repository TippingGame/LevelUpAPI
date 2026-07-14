//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type grokOAuthClientStub struct {
	refreshResponse *xai.TokenResponse
	ssoResponse     *xai.TokenResponse
	ssoErr          error
	ssoConvert      func(string) (*xai.TokenResponse, error)
	exchangeCalls   int
}

func TestGrokOAuthServiceBuildAccountCredentialsUsesOfficialAPI(t *testing.T) {
	svc := NewGrokOAuthService(nil, &grokOAuthClientStub{})
	defer svc.Stop()

	credentials := svc.BuildAccountCredentials(&GrokTokenInfo{
		AccessToken: "access-token",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	})

	require.Equal(t, xai.DefaultBaseURL, credentials["base_url"])
}

func (s *grokOAuthClientStub) ExchangeCode(context.Context, string, string, string, string, string) (*xai.TokenResponse, error) {
	s.exchangeCalls++
	return &xai.TokenResponse{}, nil
}

func (s *grokOAuthClientStub) RefreshToken(context.Context, string, string, string) (*xai.TokenResponse, error) {
	return s.refreshResponse, nil
}

func (s *grokOAuthClientStub) ConvertSSOToBuild(_ context.Context, token string, _ string) (*xai.TokenResponse, error) {
	if s.ssoConvert != nil {
		return s.ssoConvert(token)
	}
	return s.ssoResponse, s.ssoErr
}

func TestGrokOAuthServiceConvertSSOBatchKeepsOrderAndPartialFailures(t *testing.T) {
	svc := NewGrokOAuthService(nil, &grokOAuthClientStub{
		ssoConvert: func(token string) (*xai.TokenResponse, error) {
			if token == "bad-token" {
				return nil, errors.New("upstream rejected token")
			}
			return &xai.TokenResponse{
				AccessToken:  "access-" + token,
				RefreshToken: "refresh-" + token,
				ExpiresIn:    3600,
			}, nil
		},
	})
	defer svc.Stop()

	results := svc.ConvertSSOBatch(context.Background(), []string{
		"sso=first-token\nbad-token",
		"first-token,third-token",
	}, nil)

	require.Len(t, results, 3)
	require.Equal(t, 1, results[0].Index)
	require.Equal(t, "access-first-token", results[0].TokenInfo.AccessToken)
	require.Equal(t, 2, results[1].Index)
	require.Error(t, results[1].Err)
	require.Nil(t, results[1].TokenInfo)
	require.Equal(t, 3, results[2].Index)
	require.Equal(t, "access-third-token", results[2].TokenInfo.AccessToken)
}

func TestGrokOAuthServiceRejectsEmptyTokenResponses(t *testing.T) {
	t.Run("SSO conversion", func(t *testing.T) {
		svc := NewGrokOAuthService(nil, &grokOAuthClientStub{})
		defer svc.Stop()

		results := svc.ConvertSSOBatch(context.Background(), []string{"sso-token"}, nil)
		require.Len(t, results, 1)
		require.Nil(t, results[0].TokenInfo)
		require.ErrorContains(t, results[0].Err, "GROK_SSO_EMPTY_TOKEN_RESPONSE")
	})

	t.Run("refresh token", func(t *testing.T) {
		svc := NewGrokOAuthService(nil, &grokOAuthClientStub{})
		defer svc.Stop()

		info, err := svc.RefreshToken(context.Background(), "refresh-token", "", "client-id")
		require.Nil(t, info)
		require.ErrorContains(t, err, "GROK_OAUTH_EMPTY_TOKEN_RESPONSE")
	})
}

func TestGrokOAuthServiceRefreshTokenPreservesOriginalRefreshTokenWhenNotRotated(t *testing.T) {
	svc := NewGrokOAuthService(nil, &grokOAuthClientStub{
		refreshResponse: &xai.TokenResponse{
			AccessToken: "new-access-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		},
	})
	defer svc.Stop()

	info, err := svc.RefreshToken(context.Background(), "original-refresh-token", "", "client-id")
	require.NoError(t, err)
	require.Equal(t, "new-access-token", info.AccessToken)
	require.Equal(t, "original-refresh-token", info.RefreshToken)
	require.Equal(t, "client-id", info.ClientID)
}

func TestGrokOAuthServiceExchangeCodeRequiresStateForCallbackURLAndConsumesSession(t *testing.T) {
	client := &grokOAuthClientStub{}
	svc := NewGrokOAuthService(nil, client)
	defer svc.Stop()

	auth, err := svc.GenerateAuthURL(context.Background(), nil, "")
	require.NoError(t, err)

	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "http://127.0.0.1:56121/callback?code=code-without-state",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "GROK_OAUTH_STATE_REQUIRED")
	require.Zero(t, client.exchangeCalls)

	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "code-with-state",
		State:     auth.State,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "GROK_OAUTH_SESSION_NOT_FOUND")
	require.Zero(t, client.exchangeCalls)
}
