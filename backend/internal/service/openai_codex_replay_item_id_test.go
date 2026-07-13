//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterCodexInput_StripsInvalidReplayIDs(t *testing.T) {
	input := []any{
		map[string]any{"type": "message", "id": "item_message", "role": "user"},
		map[string]any{"type": "message", "id": "msg_valid", "role": "assistant"},
		map[string]any{"type": "function_call", "id": "item_call", "call_id": "fc_call", "name": "bash"},
		map[string]any{"type": "function_call", "id": "fc_valid", "call_id": "fc_valid", "name": "bash"},
		map[string]any{"type": "function_call_output", "id": "output_1", "call_id": "fc_call", "output": "done"},
	}

	filtered := filterCodexInput(input, true)
	require.Len(t, filtered, 5)

	_, hasID := filtered[0].(map[string]any)["id"]
	require.False(t, hasID)
	require.Equal(t, "msg_valid", filtered[1].(map[string]any)["id"])
	_, hasID = filtered[2].(map[string]any)["id"]
	require.False(t, hasID)
	require.Equal(t, "fc_call", filtered[2].(map[string]any)["call_id"])
	require.Equal(t, "fc_valid", filtered[3].(map[string]any)["id"])
	require.Equal(t, "output_1", filtered[4].(map[string]any)["id"])
}

func TestFilterCodexInput_InvalidReplayIDDoesNotMutateInput(t *testing.T) {
	original := map[string]any{"type": "message", "id": "item_message", "role": "user"}
	filtered := filterCodexInput([]any{original}, true)

	require.Equal(t, "item_message", original["id"])
	_, hasID := filtered[0].(map[string]any)["id"]
	require.False(t, hasID)
}
