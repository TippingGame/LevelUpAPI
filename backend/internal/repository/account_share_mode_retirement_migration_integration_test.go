//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMigration209RejectsActiveMembershipWithoutMutatingAccounts(t *testing.T) {
	tx := testTx(t)
	prepareMigration209RuntimeTables(t, tx)

	suffix := time.Now().UnixNano()
	ownerID := insertMigration203User(t, tx, suffix)
	privateGroupID := insertMigration203Group(t, tx, fmt.Sprintf("migration209-private-%d", suffix), service.PlatformOpenAI, service.GroupScopeUserPrivate, service.SubscriptionTypeStandard, "", &ownerID)
	accountID := insertMigration203PublicAccount(t, tx, ownerID, fmt.Sprintf("migration209-active-%d", suffix), service.AccountTypeOAuth, service.AccountLevelPlus, `{}`)
	insertMigration203AccountGroup(t, tx, accountID, privateGroupID, 7)
	_, err := tx.ExecContext(context.Background(), `
INSERT INTO account_share_listings (account_id, deleted_at) VALUES ($1, NULL);
INSERT INTO account_share_memberships (status, deleted_at) VALUES ('active', NULL)
`, accountID)
	require.NoError(t, err)

	_, err = tx.ExecContext(context.Background(), "SAVEPOINT migration209_guard")
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), migration209SQL(t))
	require.ErrorContains(t, err, "active memberships still exist")
	_, rollbackErr := tx.ExecContext(context.Background(), "ROLLBACK TO SAVEPOINT migration209_guard")
	require.NoError(t, rollbackErr)

	var shareMode string
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT share_mode FROM accounts WHERE id = $1", accountID).Scan(&shareMode))
	require.Equal(t, service.AccountShareModePublic, shareMode)
}

func TestMigration209ConvertsListingsDisablesKeysAndPreservesArchive(t *testing.T) {
	tx := testTx(t)
	prepareMigration209RuntimeTables(t, tx)

	suffix := time.Now().UnixNano()
	ownerID := insertMigration203User(t, tx, suffix)
	privateGroupID := insertMigration203Group(t, tx, fmt.Sprintf("migration209-private-%d", suffix), service.PlatformOpenAI, service.GroupScopeUserPrivate, service.SubscriptionTypeStandard, "", &ownerID)
	modeGroupID := insertMigration203Group(t, tx, fmt.Sprintf("migration209-mode-%d", suffix), service.PlatformOpenAI, service.GroupScopePublic, service.SubscriptionTypeStandard, "", nil)
	accountID := insertMigration203PublicAccount(t, tx, ownerID, fmt.Sprintf("migration209-account-%d", suffix), service.AccountTypeOAuth, service.AccountLevelPlus, `{}`)
	insertMigration203AccountGroup(t, tx, accountID, modeGroupID, 9)

	var apiKeyID int64
	err := tx.QueryRowContext(context.Background(), `
INSERT INTO api_keys (user_id, key, name, group_id, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, 'active', NOW(), NOW())
RETURNING id
`, ownerID, fmt.Sprintf("migration209-key-%d", suffix), fmt.Sprintf("migration209-key-%d", suffix), modeGroupID).Scan(&apiKeyID)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `
INSERT INTO api_key_group_routes (api_key_id, group_id, priority, weight, enabled, cooldown_seconds, created_at, updated_at)
VALUES ($1, $2, 100, 1, TRUE, 30, NOW(), NOW());
INSERT INTO account_share_mode_groups (group_id) VALUES ($2);
INSERT INTO account_share_listings (account_id, deleted_at) VALUES ($3, NULL);
INSERT INTO account_share_mode_settlement_entries (
    usage_log_id, account_id, owner_user_id, api_key_id, total_charge,
    rate_multiplier_snapshot, created_at
) VALUES (987654321, $3, $4, $1, 12.3456789012, 1.2500, NOW())
`, apiKeyID, modeGroupID, accountID, ownerID)
	require.NoError(t, err)

	ledgerBefore := migration209TableCount(t, tx, "user_balance_ledger")
	ordinarySettlementBefore := migration209TableCount(t, tx, "account_share_settlement_entries")
	_, err = tx.ExecContext(context.Background(), migration209SQL(t))
	require.NoError(t, err)

	var shareMode, shareStatus string
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT share_mode, share_status FROM accounts WHERE id = $1", accountID).Scan(&shareMode, &shareStatus))
	require.Equal(t, service.AccountShareModePrivate, shareMode)
	require.Equal(t, service.AccountShareStatusApproved, shareStatus)
	require.True(t, migration203AccountHasGroup(t, tx, accountID, privateGroupID))
	require.False(t, migration203AccountHasGroup(t, tx, accountID, modeGroupID))

	var keyDeletedAt, groupDeletedAt sql.NullTime
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT deleted_at FROM api_keys WHERE id = $1", apiKeyID).Scan(&keyDeletedAt))
	require.True(t, keyDeletedAt.Valid)
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT deleted_at FROM groups WHERE id = $1", modeGroupID).Scan(&groupDeletedAt))
	require.True(t, groupDeletedAt.Valid)
	require.Equal(t, 0, migration209TableCountWhere(t, tx, "api_key_group_routes", "api_key_id = $1", apiKeyID))

	var archivedCharge, archivedRatio string
	require.NoError(t, tx.QueryRowContext(context.Background(), `
SELECT total_charge::text, rate_multiplier_snapshot::text
FROM account_share_mode_settlement_archive
WHERE usage_log_id = 987654321
`).Scan(&archivedCharge, &archivedRatio))
	require.Equal(t, "12.3456789012", archivedCharge)
	require.Equal(t, "1.2500", archivedRatio)
	require.Equal(t, ledgerBefore, migration209TableCount(t, tx, "user_balance_ledger"))
	require.Equal(t, ordinarySettlementBefore, migration209TableCount(t, tx, "account_share_settlement_entries"))
	require.False(t, migration209TableExists(t, tx, "account_share_memberships"))
	require.False(t, migration209TableExists(t, tx, "account_share_listings"))
	require.False(t, migration209TableExists(t, tx, "account_share_mode_policies"))
	require.False(t, migration209TableExists(t, tx, "account_share_mode_groups"))
}

func TestMigration209RejectsListingWithoutOwnerPrivateGroup(t *testing.T) {
	tx := testTx(t)
	prepareMigration209RuntimeTables(t, tx)

	suffix := time.Now().UnixNano()
	ownerID := insertMigration203User(t, tx, suffix)
	accountID := insertMigration203PublicAccount(t, tx, ownerID, fmt.Sprintf("migration209-no-group-%d", suffix), service.AccountTypeOAuth, service.AccountLevelPlus, `{}`)
	_, err := tx.ExecContext(context.Background(), "INSERT INTO account_share_listings (account_id, deleted_at) VALUES ($1, NULL)", accountID)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), migration209SQL(t))
	require.ErrorContains(t, err, "private OpenAI group is missing")
}

func prepareMigration209RuntimeTables(t *testing.T, tx *sql.Tx) {
	t.Helper()
	_, err := tx.ExecContext(context.Background(), `
DROP TABLE IF EXISTS account_share_mode_settlement_archive;
DROP TABLE IF EXISTS account_share_mode_settlement_entries;
DROP TABLE IF EXISTS account_share_memberships;
DROP TABLE IF EXISTS account_share_listings;
DROP TABLE IF EXISTS account_share_mode_policies;
DROP TABLE IF EXISTS account_share_mode_groups;

CREATE TABLE account_share_mode_groups (group_id BIGINT NOT NULL);
CREATE TABLE account_share_mode_policies (id BIGINT);
CREATE TABLE account_share_listings (account_id BIGINT NOT NULL, deleted_at TIMESTAMPTZ);
CREATE TABLE account_share_memberships (status VARCHAR(20) NOT NULL, deleted_at TIMESTAMPTZ);
CREATE TABLE account_share_mode_settlement_entries (
    usage_log_id BIGINT,
    account_id BIGINT,
    owner_user_id BIGINT,
    api_key_id BIGINT,
    total_charge NUMERIC(20,10) NOT NULL DEFAULT 0,
    rate_multiplier_snapshot NUMERIC(10,4) NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS account_share_listing_id BIGINT;
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS membership_id BIGINT
`)
	require.NoError(t, err)
}

func migration209SQL(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "migrations", "209_remove_account_share_mode.sql"))
	require.NoError(t, err)
	return string(content)
}

func migration209TableExists(t *testing.T, tx *sql.Tx, table string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT to_regclass('public.' || $1) IS NOT NULL", table).Scan(&exists))
	return exists
}

func migration209TableCount(t *testing.T, tx *sql.Tx, table string) int {
	t.Helper()
	return migration209TableCountWhere(t, tx, table, "TRUE")
}

func migration209TableCountWhere(t *testing.T, tx *sql.Tx, table, predicate string, args ...any) int {
	t.Helper()
	var count int
	err := tx.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table+" WHERE "+predicate, args...).Scan(&count)
	require.NoError(t, err)
	return count
}
