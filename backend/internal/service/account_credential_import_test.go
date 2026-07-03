package service

import "testing"

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
