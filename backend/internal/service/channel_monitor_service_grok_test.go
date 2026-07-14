//go:build unit

package service

import (
	"strings"
	"testing"
)

func TestNormalizeMonitorPrimaryModelDefaultsOnlyGrok(t *testing.T) {
	if got := normalizeMonitorPrimaryModel(MonitorProviderGrok, ""); got != MonitorDefaultGrokModel {
		t.Fatalf("expected Grok default %q, got %q", MonitorDefaultGrokModel, got)
	}
	if got := normalizeMonitorPrimaryModel(MonitorProviderOpenAI, ""); got != "" {
		t.Fatalf("OpenAI empty model should remain required, got %q", got)
	}
}

func TestApplyMonitorUpdateSwitchToGrokUsesDefaultModel(t *testing.T) {
	grok := MonitorProviderGrok
	existing := &ChannelMonitor{
		Provider:        MonitorProviderOpenAI,
		PrimaryModel:    "gpt-5",
		IntervalSeconds: 60,
	}
	if err := applyMonitorUpdate(existing, ChannelMonitorUpdateParams{Provider: &grok}); err != nil {
		t.Fatalf("switch provider: %v", err)
	}
	if existing.PrimaryModel != MonitorDefaultGrokModel {
		t.Fatalf("expected %q, got %q", MonitorDefaultGrokModel, existing.PrimaryModel)
	}
}

func TestSanitizeErrorMessageRedactsXAIKeys(t *testing.T) {
	message := `upstream rejected xai-secret_key-123 and echoed https://api.x.ai?api_key=xai-query-secret`
	got := sanitizeErrorMessage(message)
	if strings.Contains(got, "xai-secret_key-123") || strings.Contains(got, "xai-query-secret") {
		t.Fatalf("xAI key leaked after sanitization: %q", got)
	}
	if !strings.Contains(got, "xai-***REDACTED***") {
		t.Fatalf("missing xAI redaction marker: %q", got)
	}
}
