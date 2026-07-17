package xai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsIncludeGrokMediaCatalog(t *testing.T) {
	ids := make(map[string]struct{})
	for _, model := range DefaultModels() {
		ids[model.ID] = struct{}{}
	}

	for _, modelID := range []string{
		"grok-imagine",
		"grok-imagine-image",
		"grok-imagine-image-quality",
		"grok-imagine-edit",
		"grok-imagine-video",
		"grok-imagine-video-1.5",
	} {
		require.Contains(t, ids, modelID)
		require.Equal(t, modelID, DefaultModelMapping()[modelID])
	}
}
