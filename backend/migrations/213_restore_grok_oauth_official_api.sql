-- LevelUpAPI 1.1.219 temporarily routed Grok OAuth traffic through the
-- subscription CLI proxy. Restore sub2api's supported HTTP forwarding endpoint
-- for both newly refreshed credentials and accounts persisted by that release.
UPDATE accounts
SET credentials = jsonb_set(
        COALESCE(credentials, '{}'::jsonb),
        '{base_url}',
        to_jsonb('https://api.x.ai/v1'::text),
        TRUE
    ),
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND platform = 'grok'
  AND type = 'oauth'
  AND lower(rtrim(btrim(COALESCE(credentials->>'base_url', '')), '/')) IN (
      'https://cli-chat-proxy.grok.com/v1',
      'https://cli-chat-proxy.grok.com:443/v1'
  );
