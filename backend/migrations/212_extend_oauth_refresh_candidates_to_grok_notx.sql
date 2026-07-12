-- Include Grok OAuth accounts in the partial index used by the proactive
-- token-refresh candidate query. Migration 191 predates Grok support.
DROP INDEX CONCURRENTLY IF EXISTS idx_accounts_oauth_refresh_candidates;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_oauth_refresh_candidates
    ON accounts (priority, id)
    WHERE deleted_at IS NULL
      AND status = 'active'
      AND type = 'oauth'
      AND platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok')
      AND credentials ? 'refresh_token';
