package routes

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminSettingsAndBillingProbeRoutesAreRegistered(t *testing.T) {
	source, err := os.ReadFile("admin.go")
	require.NoError(t, err)
	body := string(source)

	for _, route := range []string{
		`accounts.GET("/upstream-billing-probe/settings"`,
		`accounts.PUT("/upstream-billing-probe/settings"`,
		`accounts.POST("/upstream-billing-probe/batch"`,
		`accounts.POST("/import/codex-session"`,
		`codes.POST("/batch-update"`,
		`adminSettings.GET("/email-templates"`,
		`adminSettings.POST("/email-template-preview"`,
		`adminSettings.GET("/email-templates/:event/:locale"`,
		`adminSettings.PUT("/email-templates/:event/:locale"`,
		`adminSettings.POST("/email-templates/:event/:locale/restore-official"`,
		`adminSettings.GET("/rate-limit-429-cooldown"`,
		`adminSettings.PUT("/rate-limit-429-cooldown"`,
	} {
		require.Contains(t, body, route)
	}

	require.Less(t,
		indexOf(t, body, `accounts.GET("/upstream-billing-probe/settings"`),
		indexOf(t, body, `accounts.GET("/:id"`),
		"static account routes must be registered before the parameterized /:id route",
	)
}

func indexOf(t *testing.T, source, value string) int {
	t.Helper()
	for i := 0; i+len(value) <= len(source); i++ {
		if source[i:i+len(value)] == value {
			return i
		}
	}
	t.Fatalf("%q not found", value)
	return -1
}
