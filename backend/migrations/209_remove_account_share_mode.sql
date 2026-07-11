-- Retire the multi-seat account-share-mode plaza while preserving immutable
-- financial audit data. Run this migration with the old application stopped.

DO $$
BEGIN
    IF to_regclass('public.account_share_memberships') IS NOT NULL
       AND EXISTS (
           SELECT 1
           FROM account_share_memberships
           WHERE status = 'active' AND deleted_at IS NULL
       ) THEN
        RAISE EXCEPTION 'account share mode retirement aborted: active memberships still exist';
    END IF;
END $$;

-- Every former listing account must have an ordinary private OpenAI group.
DO $$
BEGIN
    IF to_regclass('public.account_share_listings') IS NOT NULL
       AND EXISTS (
           SELECT 1
           FROM account_share_listings l
           JOIN accounts a ON a.id = l.account_id AND a.deleted_at IS NULL
           WHERE l.deleted_at IS NULL
             AND NOT EXISTS (
                 SELECT 1
                 FROM groups g
                 WHERE g.owner_user_id = a.owner_user_id
                   AND g.platform = 'openai'
                   AND g.scope = 'user_private'
                   AND g.status = 'active'
                   AND g.deleted_at IS NULL
             )
       ) THEN
        RAISE EXCEPTION 'account share mode retirement aborted: listing owner private OpenAI group is missing';
    END IF;
END $$;

-- Convert former listing accounts into normal private owned accounts and bind
-- them only to their owner's private OpenAI group.
UPDATE accounts a
SET share_mode = 'private',
    share_status = 'approved',
    updated_at = NOW()
FROM account_share_listings l
WHERE l.account_id = a.id
  AND l.deleted_at IS NULL
  AND a.deleted_at IS NULL;

DELETE FROM account_groups ag
USING account_share_listings l
WHERE ag.account_id = l.account_id
  AND l.deleted_at IS NULL;

INSERT INTO account_groups (account_id, group_id, priority, created_at)
SELECT l.account_id, g.id, 1, NOW()
FROM account_share_listings l
JOIN accounts a ON a.id = l.account_id AND a.deleted_at IS NULL
JOIN groups g
  ON g.owner_user_id = a.owner_user_id
 AND g.platform = 'openai'
 AND g.scope = 'user_private'
 AND g.status = 'active'
 AND g.deleted_at IS NULL
WHERE l.deleted_at IS NULL
ON CONFLICT (account_id, group_id) DO NOTHING;

-- Disable account-mode-only API keys and remove secondary routes to the mode
-- group. Keeping the rows soft-deleted preserves historical foreign keys.
UPDATE api_keys k
SET deleted_at = COALESCE(k.deleted_at, NOW()),
    updated_at = NOW()
WHERE k.group_id IN (SELECT group_id FROM account_share_mode_groups);

DELETE FROM api_key_group_routes r
USING account_share_mode_groups mg
WHERE r.group_id = mg.group_id;

UPDATE groups g
SET status = 'disabled',
    deleted_at = COALESCE(g.deleted_at, NOW()),
    updated_at = NOW()
WHERE g.id IN (SELECT group_id FROM account_share_mode_groups);

-- Keep settlement rows as a detached, read-only audit archive.
ALTER TABLE account_share_mode_settlement_entries
    RENAME TO account_share_mode_settlement_archive;

ALTER TABLE account_share_mode_settlement_archive
    DROP CONSTRAINT IF EXISTS account_share_mode_settlement_entries_usage_log_id_fkey,
    DROP CONSTRAINT IF EXISTS account_share_mode_settlement_entries_membership_id_fkey,
    DROP CONSTRAINT IF EXISTS account_share_mode_settlement_entries_listing_id_fkey,
    DROP CONSTRAINT IF EXISTS account_share_mode_settlement_entries_account_id_fkey,
    DROP CONSTRAINT IF EXISTS account_share_mode_settlement_entries_owner_user_id_fkey,
    DROP CONSTRAINT IF EXISTS account_share_mode_settlement_entries_consumer_user_id_fkey,
    DROP CONSTRAINT IF EXISTS account_share_mode_settlement_entries_api_key_id_fkey;

COMMENT ON TABLE account_share_mode_settlement_archive IS
    'Immutable audit archive for the retired multi-seat account-share-mode feature.';

-- Listing/membership-specific moderation fields are no longer part of the live
-- moderation contract. Generic account/owner/consumer audit fields remain.
DROP INDEX IF EXISTS idx_content_moderation_logs_account_share_listing_created_at;
ALTER TABLE content_moderation_logs
    DROP COLUMN IF EXISTS account_share_listing_id,
    DROP COLUMN IF EXISTS membership_id;

DROP TABLE IF EXISTS account_share_memberships;
DROP TABLE IF EXISTS account_share_listings;
DROP TABLE IF EXISTS account_share_mode_policies;
DROP TABLE IF EXISTS account_share_mode_groups;
