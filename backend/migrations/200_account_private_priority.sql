-- Add owner-only account scheduling priority.
-- Existing owned accounts keep their current self-use behavior by copying the
-- prior priority into private_priority. The global priority can then be managed
-- independently for shared/account-pool scheduling.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS private_priority INT;

UPDATE accounts
SET private_priority = priority
WHERE owner_user_id IS NOT NULL
  AND private_priority IS NULL;

COMMENT ON COLUMN accounts.private_priority IS
    'Owner-only scheduling priority used when the account owner consumes this account; lower value means higher priority.';

CREATE INDEX IF NOT EXISTS idx_accounts_owner_private_priority
    ON accounts(owner_user_id, private_priority)
    WHERE deleted_at IS NULL;
