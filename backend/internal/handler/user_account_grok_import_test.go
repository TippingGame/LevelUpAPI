//go:build unit

package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeGrokRefreshTokenImportDeduplicatesTextareaInput(t *testing.T) {
	got := normalizeGrokRefreshTokenImport([]string{
		"refresh-one\nrefresh-two",
		"refresh-one, refresh-three\r\n",
	})
	require.Equal(t, []string{"refresh-one", "refresh-two", "refresh-three"}, got)
}
