package service

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

const grokDefaultAccessTokenTTL = 6 * time.Hour
const grokSSOImportConcurrency = 3

type GrokOAuthService struct {
	sessionStore *xai.SessionStore
	proxyRepo    ProxyRepository
	oauthClient  GrokOAuthClient
}

func NewGrokOAuthService(proxyRepo ProxyRepository, oauthClient GrokOAuthClient) *GrokOAuthService {
	return &GrokOAuthService{
		sessionStore: xai.NewSessionStore(),
		proxyRepo:    proxyRepo,
		oauthClient:  oauthClient,
	}
}

type GrokAuthURLResult struct {
	AuthURL   string `json:"auth_url"`
	SessionID string `json:"session_id"`
	State     string `json:"state"`
}

func (s *GrokOAuthService) GenerateAuthURL(ctx context.Context, proxyID *int64, redirectURI string) (*GrokAuthURLResult, error) {
	state, err := xai.GenerateState()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "GROK_OAUTH_STATE_FAILED", "failed to generate state: %v", err)
	}
	nonce, err := xai.GenerateNonce()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "GROK_OAUTH_NONCE_FAILED", "failed to generate nonce: %v", err)
	}
	codeVerifier, err := xai.GenerateCodeVerifier()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "GROK_OAUTH_VERIFIER_FAILED", "failed to generate code verifier: %v", err)
	}
	sessionID, err := xai.GenerateSessionID()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "GROK_OAUTH_SESSION_FAILED", "failed to generate session ID: %v", err)
	}

	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	redirectURI = xai.EffectiveRedirectURI(redirectURI)
	codeChallenge := xai.GenerateCodeChallenge(codeVerifier)

	authURL, err := xai.BuildAuthorizationURL(state, codeChallenge, redirectURI, nonce)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadRequest, "GROK_OAUTH_INVALID_AUTHORIZE_URL", "%v", err)
	}

	s.sessionStore.Set(sessionID, &xai.OAuthSession{
		State:         state,
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
		ClientID:      xai.EffectiveClientID(),
		Scope:         xai.EffectiveScope(),
		ProxyURL:      proxyURL,
		RedirectURI:   redirectURI,
		CreatedAt:     time.Now(),
	})

	return &GrokAuthURLResult{
		AuthURL:   authURL,
		SessionID: sessionID,
		State:     state,
	}, nil
}

type GrokExchangeCodeInput struct {
	SessionID   string
	Code        string
	State       string
	RedirectURI string
	ProxyID     *int64
}

type GrokTokenInfo struct {
	AccessToken       string `json:"access_token"`
	RefreshToken      string `json:"refresh_token,omitempty"`
	IDToken           string `json:"id_token,omitempty"`
	TokenType         string `json:"token_type,omitempty"`
	ExpiresIn         int64  `json:"expires_in"`
	ExpiresAt         int64  `json:"expires_at"`
	ClientID          string `json:"client_id,omitempty"`
	Scope             string `json:"scope,omitempty"`
	Email             string `json:"email,omitempty"`
	Subject           string `json:"sub,omitempty"`
	TeamID            string `json:"team_id,omitempty"`
	SubscriptionTier  string `json:"subscription_tier,omitempty"`
	EntitlementStatus string `json:"entitlement_status,omitempty"`
}

func (s *GrokOAuthService) ExchangeCode(ctx context.Context, input *GrokExchangeCodeInput) (*GrokTokenInfo, error) {
	if input == nil {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_INVALID_INPUT", "input is required")
	}
	session, ok := s.sessionStore.Get(input.SessionID)
	if !ok {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_SESSION_NOT_FOUND", "session not found or expired")
	}
	defer s.sessionStore.Delete(input.SessionID)

	parsed := xai.ParseAuthorizationInput(input.Code)
	code := strings.TrimSpace(parsed.Code)
	if code == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_CODE_REQUIRED", "authorization code is required")
	}
	state := strings.TrimSpace(input.State)
	if state == "" {
		state = strings.TrimSpace(parsed.State)
	}
	if parsed.RequiresState && state == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_STATE_REQUIRED", "oauth state is required for callback URLs")
	}
	if state != "" && subtle.ConstantTimeCompare([]byte(state), []byte(session.State)) != 1 {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_INVALID_STATE", "invalid oauth state")
	}

	proxyURL := session.ProxyURL
	if input.ProxyID != nil {
		var err error
		proxyURL, err = s.proxyURL(ctx, input.ProxyID)
		if err != nil {
			return nil, err
		}
	}
	redirectURI := session.RedirectURI
	if strings.TrimSpace(input.RedirectURI) != "" {
		redirectURI = input.RedirectURI
	}

	tokenResp, err := s.oauthClient.ExchangeCode(ctx, code, session.CodeVerifier, redirectURI, proxyURL, session.ClientID)
	if err != nil {
		return nil, err
	}
	if err := validateGrokTokenResponse(tokenResp, "GROK_OAUTH_EMPTY_TOKEN_RESPONSE"); err != nil {
		return nil, err
	}
	return s.tokenInfoFromResponse(tokenResp, session.ClientID, nil), nil
}

func (s *GrokOAuthService) RefreshToken(ctx context.Context, refreshToken, proxyURL, clientID string) (*GrokTokenInfo, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_NO_REFRESH_TOKEN", "refresh_token is required")
	}
	tokenResp, err := s.oauthClient.RefreshToken(ctx, refreshToken, proxyURL, clientID)
	if err != nil {
		return nil, err
	}
	if err := validateGrokTokenResponse(tokenResp, "GROK_OAUTH_EMPTY_TOKEN_RESPONSE"); err != nil {
		return nil, err
	}
	tokenInfo := s.tokenInfoFromResponse(tokenResp, clientID, nil)
	if tokenInfo.RefreshToken == "" {
		tokenInfo.RefreshToken = refreshToken
	}
	return tokenInfo, nil
}

func (s *GrokOAuthService) ValidateRefreshToken(ctx context.Context, refreshToken string, proxyID *int64) (*GrokTokenInfo, error) {
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	return s.RefreshToken(ctx, refreshToken, proxyURL, xai.EffectiveClientID())
}

func (s *GrokOAuthService) ConvertFromSSO(ctx context.Context, ssoToken string, proxyID *int64) (*GrokTokenInfo, error) {
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	return s.convertSSOToken(ctx, ssoToken, proxyURL)
}

// GrokSSOConversionResult deliberately omits the source SSO token so callers
// cannot accidentally serialize or log it with per-item results.
type GrokSSOConversionResult struct {
	Index     int
	TokenInfo *GrokTokenInfo
	Err       error
}

// NormalizeGrokSSOTokens accepts textarea-style input and Cookie headers,
// removes empty/duplicate values, and never returns the original wrapper text.
func NormalizeGrokSSOTokens(contents []string) []string {
	seen := make(map[string]struct{})
	tokens := make([]string, 0, len(contents))
	for _, content := range contents {
		for _, value := range strings.FieldsFunc(content, func(r rune) bool {
			return r == '\n' || r == '\r' || r == ','
		}) {
			token := xai.NormalizeSSOToken(value)
			if token == "" {
				continue
			}
			if _, exists := seen[token]; exists {
				continue
			}
			seen[token] = struct{}{}
			tokens = append(tokens, token)
		}
	}
	return tokens
}

// ConvertSSOBatch converts at most three SSO values concurrently. Result order
// always follows the normalized input order, which keeps partial failures easy
// to map back to textarea rows without exposing the secret values.
func (s *GrokOAuthService) ConvertSSOBatch(ctx context.Context, contents []string, proxyID *int64) []GrokSSOConversionResult {
	tokens := NormalizeGrokSSOTokens(contents)
	results := make([]GrokSSOConversionResult, len(tokens))
	if len(tokens) == 0 {
		return results
	}
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		for index := range results {
			results[index] = GrokSSOConversionResult{Index: index + 1, Err: err}
		}
		return results
	}

	type job struct {
		index int
		token string
	}
	jobs := make(chan job, len(tokens))
	for index, token := range tokens {
		jobs <- job{index: index, token: token}
	}
	close(jobs)

	workerCount := grokSSOImportConcurrency
	if len(tokens) < workerCount {
		workerCount = len(tokens)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				results[item.index] = s.safeConvertSSOToken(ctx, item.token, proxyURL, item.index+1)
			}
		}()
	}
	wg.Wait()
	return results
}

func (s *GrokOAuthService) safeConvertSSOToken(ctx context.Context, token, proxyURL string, index int) (result GrokSSOConversionResult) {
	result.Index = index
	defer func() {
		if recover() != nil {
			result.TokenInfo = nil
			result.Err = infraerrors.New(http.StatusInternalServerError, "GROK_SSO_WORKER_FAILED", "Grok SSO conversion worker failed")
		}
	}()
	result.TokenInfo, result.Err = s.convertSSOToken(ctx, token, proxyURL)
	return result
}

func (s *GrokOAuthService) convertSSOToken(ctx context.Context, ssoToken, proxyURL string) (*GrokTokenInfo, error) {
	tokenResp, err := s.oauthClient.ConvertSSOToBuild(ctx, ssoToken, proxyURL)
	if err != nil {
		return nil, err
	}
	if err := validateGrokTokenResponse(tokenResp, "GROK_SSO_EMPTY_TOKEN_RESPONSE"); err != nil {
		return nil, err
	}
	return s.tokenInfoFromResponse(tokenResp, xai.DefaultClientID, nil), nil
}

func validateGrokTokenResponse(tokenResp *xai.TokenResponse, reason string) error {
	if tokenResp != nil && strings.TrimSpace(tokenResp.AccessToken) != "" {
		return nil
	}
	return infraerrors.New(http.StatusBadGateway, reason, "xAI OAuth returned no access token")
}

func (s *GrokOAuthService) RefreshAccountToken(ctx context.Context, account *Account) (*GrokTokenInfo, error) {
	if account == nil || account.Platform != PlatformGrok {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_INVALID_ACCOUNT", "account is not a Grok account")
	}
	if account.Type != AccountTypeOAuth {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_INVALID_ACCOUNT_TYPE", "account is not an OAuth account")
	}

	proxyURL, err := s.proxyURL(ctx, account.ProxyID)
	if err != nil {
		return nil, err
	}
	refreshToken := account.GetCredential("refresh_token")
	if strings.TrimSpace(refreshToken) == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_NO_REFRESH_TOKEN", "no refresh token available")
	}

	clientID := account.GetCredential("client_id")
	tokenInfo, err := s.RefreshToken(ctx, refreshToken, proxyURL, clientID)
	if err != nil {
		return nil, err
	}
	tokenInfo.SubscriptionTier = account.GetCredential("subscription_tier")
	tokenInfo.EntitlementStatus = account.GetCredential("entitlement_status")
	return tokenInfo, nil
}

func (s *GrokOAuthService) BuildAccountCredentials(tokenInfo *GrokTokenInfo) map[string]any {
	if tokenInfo == nil {
		return nil
	}
	expiresAt := time.Unix(tokenInfo.ExpiresAt, 0).UTC().Format(time.RFC3339)
	creds := map[string]any{
		"access_token": tokenInfo.AccessToken,
		"expires_at":   expiresAt,
	}
	if tokenInfo.RefreshToken != "" {
		creds["refresh_token"] = tokenInfo.RefreshToken
	}
	if tokenInfo.TokenType != "" {
		creds["token_type"] = tokenInfo.TokenType
	}
	if tokenInfo.IDToken != "" {
		creds["id_token"] = tokenInfo.IDToken
	}
	if tokenInfo.ClientID != "" {
		creds["client_id"] = tokenInfo.ClientID
	}
	if tokenInfo.Scope != "" {
		creds["scope"] = tokenInfo.Scope
	}
	if tokenInfo.Email != "" {
		creds["email"] = tokenInfo.Email
	}
	if tokenInfo.Subject != "" {
		creds["sub"] = tokenInfo.Subject
	}
	if tokenInfo.TeamID != "" {
		creds["team_id"] = tokenInfo.TeamID
	}
	if tokenInfo.SubscriptionTier != "" {
		creds["subscription_tier"] = tokenInfo.SubscriptionTier
	}
	if tokenInfo.EntitlementStatus != "" {
		creds["entitlement_status"] = tokenInfo.EntitlementStatus
	}
	// Subscription OAuth traffic uses the official CLI gateway by default.
	// This is only a forwarding hint; authorization and refresh still use the
	// official auth.x.ai endpoints and never this stored value.
	creds["base_url"] = xai.DefaultCLIBaseURL
	return creds
}

func (s *GrokOAuthService) BuildAccountExtra(tokenInfo *GrokTokenInfo) map[string]any {
	extra := map[string]any{}
	if tokenInfo == nil {
		return extra
	}
	if tokenInfo.Email != "" {
		extra["email"] = tokenInfo.Email
	}
	if tokenInfo.Subject != "" {
		extra["sub"] = tokenInfo.Subject
	}
	if tokenInfo.TeamID != "" {
		extra["team_id"] = tokenInfo.TeamID
	}
	if tokenInfo.SubscriptionTier != "" {
		extra["subscription_tier"] = tokenInfo.SubscriptionTier
	}
	if tokenInfo.EntitlementStatus != "" {
		extra["entitlement_status"] = tokenInfo.EntitlementStatus
	}
	return extra
}

func (s *GrokOAuthService) Stop() {
	s.sessionStore.Stop()
}

func (s *GrokOAuthService) tokenInfoFromResponse(tokenResp *xai.TokenResponse, clientID string, existing map[string]any) *GrokTokenInfo {
	if tokenResp == nil {
		return nil
	}
	now := time.Now()
	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = int64(grokDefaultAccessTokenTTL.Seconds())
	}
	info := &GrokTokenInfo{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    expiresIn,
		ExpiresAt:    now.Add(time.Duration(expiresIn) * time.Second).Unix(),
		ClientID:     strings.TrimSpace(clientID),
		Scope:        tokenResp.Scope,
	}
	if info.ClientID == "" {
		info.ClientID = xai.EffectiveClientID()
	}
	if info.TokenType == "" {
		info.TokenType = "Bearer"
	}
	applyGrokTokenClaims(info, tokenResp.IDToken)
	applyGrokTokenClaims(info, tokenResp.AccessToken)
	if existing != nil {
		if info.Email == "" {
			if email, _ := existing["email"].(string); email != "" {
				info.Email = email
			}
		}
		if info.Subject == "" {
			if subject, _ := existing["sub"].(string); subject != "" {
				info.Subject = subject
			}
		}
		if info.TeamID == "" {
			if teamID, _ := existing["team_id"].(string); teamID != "" {
				info.TeamID = teamID
			}
		}
	}
	return info
}

func (s *GrokOAuthService) proxyURL(ctx context.Context, proxyID *int64) (string, error) {
	if proxyID == nil {
		return "", nil
	}
	if s.proxyRepo == nil {
		return "", infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_PROXY_NOT_AVAILABLE", "proxy repository is not available")
	}
	proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
	if err != nil {
		return "", infraerrors.Newf(http.StatusBadRequest, "GROK_OAUTH_PROXY_NOT_FOUND", "proxy not found: %v", err)
	}
	if proxy == nil {
		return "", nil
	}
	return proxy.URL(), nil
}

func applyGrokTokenClaims(info *GrokTokenInfo, token string) {
	if info == nil || strings.TrimSpace(token) == "" {
		return
	}
	claims := xai.DecodeJWTClaims(token)
	if claims == nil {
		return
	}
	if info.Email == "" {
		info.Email = xai.JWTClaimString(claims, "email")
	}
	if info.Subject == "" {
		info.Subject = xai.JWTClaimString(claims, "sub")
	}
	if info.TeamID == "" {
		info.TeamID = xai.JWTClaimString(claims, "team_id")
	}
}
