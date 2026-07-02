package admin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseErrorPassthroughStatusCodes_Ranges(t *testing.T) {
	codes, provided, err := parseErrorPassthroughStatusCodes(json.RawMessage(`["500-503", 429]`))

	require.NoError(t, err)
	require.True(t, provided)
	require.Equal(t, []int{429, 500, 501, 502, 503}, codes)
}

func TestParseErrorPassthroughStatusCodes_StringList(t *testing.T) {
	codes, provided, err := parseErrorPassthroughStatusCodes(json.RawMessage(`"401,403-404"`))

	require.NoError(t, err)
	require.True(t, provided)
	require.Equal(t, []int{401, 403, 404}, codes)
}

func TestParseErrorPassthroughStatusCodes_Omitted(t *testing.T) {
	codes, provided, err := parseErrorPassthroughStatusCodes(nil)

	require.NoError(t, err)
	require.False(t, provided)
	require.Nil(t, codes)
}

func TestParseErrorPassthroughStatusCodes_RejectsInvalid(t *testing.T) {
	_, provided, err := parseErrorPassthroughStatusCodes(json.RawMessage(`"401,bad"`))

	require.True(t, provided)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad")
}
