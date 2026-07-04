package claude

import "testing"

func TestDefaultModels_IncludesSonnet5(t *testing.T) {
	t.Parallel()

	for _, model := range DefaultModels {
		if model.ID == "claude-sonnet-5" {
			if model.DisplayName != "Claude Sonnet 5" {
				t.Fatalf("DisplayName = %q, want Claude Sonnet 5", model.DisplayName)
			}
			return
		}
	}

	t.Fatal("expected claude-sonnet-5 in DefaultModels")
}
