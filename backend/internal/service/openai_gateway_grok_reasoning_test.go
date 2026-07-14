package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPatchGrokResponsesBodyStripsNullReasoningContent(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"model":"grok-latest",
		"input":[
			{"type":"reasoning","summary":[{"type":"summary_text","text":"first"}],"content":null},
			{"type":"message","role":"user","content":"hi"},
			{"type":"reasoning","summary":[{"type":"summary_text","text":"second"}],"content":null}
		]
	}`)

	patched, err := patchGrokResponsesBody(body, "grok-4.5")
	require.NoError(t, err)
	require.True(t, json.Valid(patched))

	items := gjson.GetBytes(patched, "input").Array()
	require.Len(t, items, 3)
	require.False(t, items[0].Get("content").Exists())
	require.False(t, items[2].Get("content").Exists())
	require.True(t, items[0].Get("summary").Exists())
}

func TestPatchGrokResponsesBodyKeepsNonNullReasoningContent(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"grok-latest","input":[{"type":"reasoning","content":"keep me"}]}`)
	patched, err := patchGrokResponsesBody(body, "grok-4.5")
	require.NoError(t, err)
	require.Equal(t, "keep me", gjson.GetBytes(patched, "input.0.content").String())
}
