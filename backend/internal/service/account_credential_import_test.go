package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAccountCredentialImportContents_CodexManagerKeepsStrongOpenAIIdentity(t *testing.T) {
	content := `{
		"tokens": {
			"access_token": "access-token",
			"refresh_token": "refresh-token"
		},
		"meta": {
			"issuer": "https://auth.openai.com/",
			"chatgptAccountId": "team-account",
			"chatgptUserId": "team-member-a",
			"organizationId": "team-org",
			"workspaceId": "workspace-1"
		}
	}`

	sources, errs := ParseAccountCredentialImportContents([]string{content})
	if len(errs) != 0 {
		t.Fatalf("ParseAccountCredentialImportContents errors = %#v", errs)
	}
	if len(sources) != 1 {
		t.Fatalf("ParseAccountCredentialImportContents sources = %d, want 1", len(sources))
	}

	credentials := sources[0].Credentials
	if got := credentials["chatgpt_account_id"]; got != "team-account" {
		t.Fatalf("chatgpt_account_id = %#v, want team-account", got)
	}
	if got := credentials["chatgpt_user_id"]; got != "team-member-a" {
		t.Fatalf("chatgpt_user_id = %#v, want team-member-a", got)
	}
	if got := credentials["organization_id"]; got != "team-org" {
		t.Fatalf("organization_id = %#v, want team-org", got)
	}
	if got := credentials["workspace_id"]; got != "workspace-1" {
		t.Fatalf("workspace_id = %#v, want workspace-1", got)
	}
}

func TestParseAccountCredentialImportContents_GrokRefreshTokenJSON(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "top level",
			content: `{"platform":"grok","refresh_token":"grok-refresh","client_id":"grok-client"}`,
		},
		{
			name:    "nested credentials",
			content: `{"platform":"xai","type":"oauth","credentials":{"refresh_token":"grok-refresh","client_id":"grok-client"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sources, errs := ParseAccountCredentialImportContents([]string{tt.content})

			require.Empty(t, errs)
			require.Len(t, sources, 1)
			require.Equal(t, AccountCredentialImportKindGrokRefreshToken, sources[0].Kind)
			require.Equal(t, PlatformGrok, sources[0].Platform)
			require.Equal(t, "grok-refresh", sources[0].Token)
			require.Equal(t, "grok-client", sources[0].ClientID)
		})
	}
}

func TestParseAccountCredentialImportContents_GrokOAuthAllowsOfficialExportBaseURL(t *testing.T) {
	content := `{
		"platform":"grok",
		"type":"oauth",
		"credentials":{
			"access_token":"grok-access",
			"refresh_token":"grok-refresh",
			"base_url":"https://api.x.ai/v1"
		}
	}`

	sources, errs := ParseAccountCredentialImportContents([]string{content})

	require.Empty(t, errs)
	require.Len(t, sources, 1)
	require.Equal(t, AccountCredentialImportKindOAuthCredentials, sources[0].Kind)
	require.Equal(t, PlatformGrok, sources[0].Platform)
	require.NotContains(t, sources[0].Credentials, "base_url")
}

func TestParseAccountCredentialImportContents_GrokOAuthRejectsImportedCustomBaseURL(t *testing.T) {
	content := `{"platform":"grok","type":"oauth","credentials":{"access_token":"grok-access","base_url":"https://third-party.example.com/v1"}}`

	sources, errs := ParseAccountCredentialImportContents([]string{content})

	require.Empty(t, sources)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0].Message, "disallowed credential field: base_url")
}
