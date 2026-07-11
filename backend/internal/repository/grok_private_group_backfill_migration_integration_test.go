//go:build integration

package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMigration211BackfillsExistingUserGrokPrivateSubscription(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	userID := insertMigration203User(t, tx, suffix)
	templateGroupID := insertMigration203Group(
		t,
		tx,
		fmt.Sprintf("migration211-template-%d", suffix),
		service.PlatformOpenAI,
		service.GroupScopeUserPrivate,
		service.SubscriptionTypeSubscription,
		"",
		&userID,
	)
	_, err := tx.ExecContext(ctx, `
UPDATE groups
SET rate_multiplier = 1.25,
    daily_limit_usd = 3.5,
    weekly_limit_usd = 17.5,
    monthly_limit_usd = 50,
    rpm_limit = 7
WHERE id = $1
`, templateGroupID)
	require.NoError(t, err)

	migration := migration211SQL(t)
	_, err = tx.ExecContext(ctx, migration)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, migration)
	require.NoError(t, err, "migration must remain idempotent")

	var groupID int64
	var rate, daily, weekly, monthly float64
	var rpm, validityDays int
	err = tx.QueryRowContext(ctx, `
SELECT id, rate_multiplier, daily_limit_usd, weekly_limit_usd, monthly_limit_usd,
       rpm_limit, default_validity_days
FROM groups
WHERE owner_user_id = $1
  AND platform = 'grok'
  AND scope = 'user_private'
  AND deleted_at IS NULL
`, userID).Scan(&groupID, &rate, &daily, &weekly, &monthly, &rpm, &validityDays)
	require.NoError(t, err)
	require.Equal(t, 1.25, rate)
	require.Equal(t, 3.5, daily)
	require.Equal(t, 17.5, weekly)
	require.Equal(t, 50.0, monthly)
	require.Equal(t, 7, rpm)
	require.Equal(t, 365, validityDays)

	var subscriptionCount, allowedGroupCount int
	var expiresAt time.Time
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COUNT(*), MAX(expires_at)
FROM user_subscriptions
WHERE user_id = $1 AND group_id = $2 AND deleted_at IS NULL
`, userID, groupID).Scan(&subscriptionCount, &expiresAt))
	require.Equal(t, 1, subscriptionCount)
	require.True(t, expiresAt.After(time.Now().Add(364*24*time.Hour)))
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM user_allowed_groups WHERE user_id = $1 AND group_id = $2
`, userID, groupID).Scan(&allowedGroupCount))
	require.Equal(t, 1, allowedGroupCount)
}

func migration211SQL(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "migrations", "211_backfill_grok_user_private_groups.sql"))
	require.NoError(t, err)
	return string(content)
}
