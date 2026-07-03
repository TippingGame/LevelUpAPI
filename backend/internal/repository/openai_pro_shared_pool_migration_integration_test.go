//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMigration203RebindsApprovedObservedProOAuthAccounts(t *testing.T) {
	tx := testTx(t)
	suffix := time.Now().UnixNano()

	ownerID := insertMigration203User(t, tx, suffix)
	privateGroupID := insertMigration203Group(t, tx, fmt.Sprintf("migration203-private-%d", suffix), service.PlatformOpenAI, service.GroupScopeUserPrivate, service.SubscriptionTypeStandard, "", &ownerID)
	plusGroupID := insertMigration203Group(t, tx, fmt.Sprintf("migration203-plus-%d", suffix), service.PlatformOpenAI, service.GroupScopePublic, service.SubscriptionTypeStandard, service.AccountLevelPlus, nil)
	proGroupID := ensureMigration203ProPool(t, tx, suffix)

	observedProID := insertMigration203PublicAccount(t, tx, ownerID, fmt.Sprintf("migration203-observed-pro-%d", suffix), service.AccountTypeOAuth, service.AccountLevelPlus, `{"plan_type":"chatgpt_pro"}`)
	stillPlusID := insertMigration203PublicAccount(t, tx, ownerID, fmt.Sprintf("migration203-still-plus-%d", suffix), service.AccountTypeOAuth, service.AccountLevelPlus, `{"plan_type":"plus"}`)
	apiKeyID := insertMigration203PublicAccount(t, tx, ownerID, fmt.Sprintf("migration203-api-key-%d", suffix), service.AccountTypeAPIKey, service.AccountLevelPlus, `{"plan_type":"chatgpt_pro"}`)

	insertMigration203AccountGroup(t, tx, observedProID, privateGroupID, 7)
	insertMigration203AccountGroup(t, tx, observedProID, plusGroupID, 9)
	insertMigration203AccountGroup(t, tx, stillPlusID, privateGroupID, 7)
	insertMigration203AccountGroup(t, tx, stillPlusID, plusGroupID, 9)
	insertMigration203AccountGroup(t, tx, apiKeyID, privateGroupID, 7)
	insertMigration203AccountGroup(t, tx, apiKeyID, plusGroupID, 9)

	runMigration203(t, tx)
	runMigration203(t, tx)

	require.True(t, migration203AccountHasGroup(t, tx, observedProID, privateGroupID), "observed Pro account should keep owner private group")
	require.True(t, migration203AccountHasGroup(t, tx, observedProID, proGroupID), "observed Pro account should be in Pro shared pool")
	require.False(t, migration203AccountHasGroup(t, tx, observedProID, plusGroupID), "observed Pro account should leave stale Plus shared pool")
	require.Equal(t, 1, migration203PublicOpenAIStandardGroupCount(t, tx, observedProID), "observed Pro account should have one public standard pool binding")

	require.True(t, migration203AccountHasGroup(t, tx, stillPlusID, plusGroupID), "Plus OAuth account should stay in Plus pool")
	require.False(t, migration203AccountHasGroup(t, tx, stillPlusID, proGroupID), "Plus OAuth account should not be promoted")

	require.True(t, migration203AccountHasGroup(t, tx, apiKeyID, plusGroupID), "OpenAI API key binding should not be rewritten by the OAuth repair")
	require.False(t, migration203AccountHasGroup(t, tx, apiKeyID, proGroupID), "OpenAI API key should not be moved to Pro pool")
}

func runMigration203(t *testing.T, tx *sql.Tx) {
	t.Helper()
	sqlBytes, err := os.ReadFile(filepath.Join("..", "..", "migrations", "203_rebind_openai_pro_shared_pool.sql"))
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), string(sqlBytes))
	require.NoError(t, err)
}

func ensureMigration203ProPool(t *testing.T, tx *sql.Tx, suffix int64) int64 {
	t.Helper()
	ctx := context.Background()
	proGroupID, err := selectMigration203ProPool(ctx, tx)
	if err == nil {
		return proGroupID
	}
	require.True(t, errors.Is(err, sql.ErrNoRows), "select Pro pool: %v", err)
	insertMigration203Group(t, tx, fmt.Sprintf("migration203-pro-%d", suffix), service.PlatformOpenAI, service.GroupScopePublic, service.SubscriptionTypeStandard, service.AccountLevelPro, nil)
	proGroupID, err = selectMigration203ProPool(ctx, tx)
	require.NoError(t, err)
	return proGroupID
}

func selectMigration203ProPool(ctx context.Context, tx *sql.Tx) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM groups
WHERE deleted_at IS NULL
  AND platform = 'openai'
  AND status = 'active'
  AND owner_user_id IS NULL
  AND scope = 'public'
  AND COALESCE(subscription_type, '') IN ('', 'standard')
  AND required_account_level = 'pro'
ORDER BY CASE WHEN name = 'PRO共享号池' THEN 0 ELSE 1 END, sort_order, id
LIMIT 1
`).Scan(&id)
	return id, err
}

func insertMigration203User(t *testing.T, tx *sql.Tx, suffix int64) int64 {
	t.Helper()
	var id int64
	err := tx.QueryRowContext(context.Background(), `
INSERT INTO users (email, password_hash, role, status, balance, concurrency, username, created_at, updated_at)
VALUES ($1, 'hash', 'user', 'active', 0, 5, $2, NOW(), NOW())
RETURNING id
`, fmt.Sprintf("migration203-%d@example.com", suffix), fmt.Sprintf("migration203-%d", suffix)).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertMigration203Group(t *testing.T, tx *sql.Tx, name, platform, scope, subscriptionType, requiredLevel string, ownerUserID *int64) int64 {
	t.Helper()
	var id int64
	err := tx.QueryRowContext(context.Background(), `
INSERT INTO groups (
    name, platform, scope, subscription_type, required_account_level,
    owner_user_id, status, rate_multiplier, is_exclusive, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, 'active', 1.0, FALSE, NOW(), NOW())
RETURNING id
`, name, platform, scope, subscriptionType, requiredLevel, ownerUserID).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertMigration203PublicAccount(t *testing.T, tx *sql.Tx, ownerID int64, name, accountType, accountLevel, credentialsJSON string) int64 {
	t.Helper()
	var id int64
	err := tx.QueryRowContext(context.Background(), `
INSERT INTO accounts (
    name, platform, type, account_level, credentials, extra,
    owner_user_id, share_mode, share_status, status, schedulable,
    concurrency, priority, created_at, updated_at
)
VALUES ($1, 'openai', $2, $3, $4::jsonb, '{}'::jsonb, $5, 'public', 'approved', 'active', TRUE, 3, 50, NOW(), NOW())
RETURNING id
`, name, accountType, accountLevel, credentialsJSON, ownerID).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertMigration203AccountGroup(t *testing.T, tx *sql.Tx, accountID, groupID int64, priority int) {
	t.Helper()
	_, err := tx.ExecContext(context.Background(), `
INSERT INTO account_groups (account_id, group_id, priority, created_at)
VALUES ($1, $2, $3, NOW())
`, accountID, groupID, priority)
	require.NoError(t, err)
}

func migration203AccountHasGroup(t *testing.T, tx *sql.Tx, accountID, groupID int64) bool {
	t.Helper()
	var exists bool
	err := tx.QueryRowContext(context.Background(), `
SELECT EXISTS (
    SELECT 1
    FROM account_groups
    WHERE account_id = $1
      AND group_id = $2
)
`, accountID, groupID).Scan(&exists)
	require.NoError(t, err)
	return exists
}

func migration203PublicOpenAIStandardGroupCount(t *testing.T, tx *sql.Tx, accountID int64) int {
	t.Helper()
	var count int
	err := tx.QueryRowContext(context.Background(), `
SELECT COUNT(*)
FROM account_groups ag
JOIN groups g ON g.id = ag.group_id
WHERE ag.account_id = $1
  AND g.deleted_at IS NULL
  AND g.platform = 'openai'
  AND g.owner_user_id IS NULL
  AND g.scope = 'public'
  AND COALESCE(g.subscription_type, '') IN ('', 'standard')
`, accountID).Scan(&count)
	require.NoError(t, err)
	return count
}
