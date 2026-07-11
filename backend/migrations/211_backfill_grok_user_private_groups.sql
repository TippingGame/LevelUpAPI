-- Backfill the Grok private subscription pool for users created before Grok support.
-- New users are provisioned by userPrivateGroupService; this migration repairs existing users.

WITH users_missing_grok AS (
    SELECT u.id AS user_id
    FROM users u
    WHERE u.deleted_at IS NULL
      AND u.role = 'user'
      AND NOT EXISTS (
          SELECT 1
          FROM groups existing
          WHERE existing.owner_user_id = u.id
            AND existing.scope = 'user_private'
            AND existing.platform = 'grok'
            AND existing.deleted_at IS NULL
      )
), private_group_templates AS (
    SELECT missing.user_id,
           template.rate_multiplier,
           template.daily_limit_usd,
           template.weekly_limit_usd,
           template.monthly_limit_usd,
           template.rpm_limit
    FROM users_missing_grok missing
    LEFT JOIN LATERAL (
        SELECT g.rate_multiplier,
               g.daily_limit_usd,
               g.weekly_limit_usd,
               g.monthly_limit_usd,
               g.rpm_limit
        FROM groups g
        WHERE g.owner_user_id = missing.user_id
          AND g.scope = 'user_private'
          AND g.deleted_at IS NULL
        ORDER BY CASE WHEN g.platform = 'openai' THEN 0 ELSE 1 END, g.id
        LIMIT 1
    ) template ON TRUE
)
INSERT INTO groups (
    name, description, rate_multiplier, is_exclusive, status,
    owner_user_id, scope, platform, subscription_type,
    daily_limit_usd, weekly_limit_usd, monthly_limit_usd,
    default_validity_days, allow_messages_dispatch, rpm_limit,
    supported_model_scopes, created_at, updated_at
)
SELECT
    'private-u' || template.user_id || '-grok',
    'Private subscription group for user ' || template.user_id || ' on grok.',
    COALESCE(template.rate_multiplier, 1),
    TRUE,
    'active',
    template.user_id,
    'user_private',
    'grok',
    'subscription',
    template.daily_limit_usd,
    template.weekly_limit_usd,
    template.monthly_limit_usd,
    365,
    FALSE,
    COALESCE(template.rpm_limit, 0),
    '[]'::jsonb,
    NOW(),
    NOW()
FROM private_group_templates template
ON CONFLICT DO NOTHING;

INSERT INTO user_subscriptions (
    user_id, group_id, starts_at, expires_at, status,
    assigned_at, notes, created_at, updated_at
)
SELECT
    g.owner_user_id,
    g.id,
    NOW(),
    NOW() + INTERVAL '365 days',
    'active',
    NOW(),
    'auto assigned by Grok private group backfill',
    NOW(),
    NOW()
FROM groups g
JOIN users u ON u.id = g.owner_user_id
WHERE g.deleted_at IS NULL
  AND g.scope = 'user_private'
  AND g.platform = 'grok'
  AND g.status = 'active'
  AND g.subscription_type = 'subscription'
  AND u.deleted_at IS NULL
  AND u.role = 'user'
  AND NOT EXISTS (
      SELECT 1
      FROM user_subscriptions existing
      WHERE existing.user_id = g.owner_user_id
        AND existing.group_id = g.id
        AND existing.deleted_at IS NULL
  )
ON CONFLICT DO NOTHING;

INSERT INTO user_allowed_groups (user_id, group_id, created_at)
SELECT g.owner_user_id, g.id, NOW()
FROM groups g
JOIN users u ON u.id = g.owner_user_id
WHERE g.deleted_at IS NULL
  AND g.scope = 'user_private'
  AND g.platform = 'grok'
  AND g.status = 'active'
  AND g.subscription_type = 'subscription'
  AND u.deleted_at IS NULL
  AND u.role = 'user'
ON CONFLICT DO NOTHING;
