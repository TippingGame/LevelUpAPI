INSERT INTO groups (
    name, description, rate_multiplier, is_exclusive, status, scope,
    platform, subscription_type, allow_messages_dispatch, require_oauth_only,
    created_at, updated_at
)
SELECT
    'grok-default', 'Default Grok OAuth text model pool', 1.0, FALSE, 'active', 'public',
    'grok', 'standard', TRUE, TRUE, NOW(), NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM groups WHERE name = 'grok-default' AND deleted_at IS NULL
);
