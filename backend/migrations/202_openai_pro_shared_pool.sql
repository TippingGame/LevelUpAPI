UPDATE groups
SET platform = 'openai',
    scope = 'public',
    owner_user_id = NULL,
    subscription_type = 'standard',
    required_account_level = 'pro',
    updated_at = NOW()
WHERE name = 'PRO共享号池'
  AND deleted_at IS NULL
  AND (owner_user_id IS NULL OR scope = 'public')
  AND (
      platform <> 'openai'
      OR scope <> 'public'
      OR owner_user_id IS NOT NULL
      OR COALESCE(subscription_type, '') <> 'standard'
      OR required_account_level <> 'pro'
  );

WITH source_pool AS (
    SELECT *
    FROM groups
    WHERE deleted_at IS NULL
      AND platform = 'openai'
      AND status = 'active'
      AND owner_user_id IS NULL
      AND scope = 'public'
      AND COALESCE(subscription_type, '') IN ('', 'standard')
      AND required_account_level IN ('plus', 'free')
    ORDER BY CASE required_account_level WHEN 'plus' THEN 0 WHEN 'free' THEN 1 ELSE 2 END, sort_order, id
    LIMIT 1
)
INSERT INTO groups (
    name,
    description,
    rate_multiplier,
    is_exclusive,
    status,
    owner_user_id,
    scope,
    platform,
    required_account_level,
    subscription_type,
    default_validity_days,
    allow_image_generation,
    image_rate_independent,
    image_rate_multiplier,
    claude_code_only,
    model_routing,
    model_routing_enabled,
    mcp_xml_inject,
    supported_model_scopes,
    sort_order,
    allow_messages_dispatch,
    require_oauth_only,
    require_privacy_set,
    default_mapped_model,
    messages_dispatch_model_config,
    rpm_limit,
    created_at,
    updated_at
)
SELECT
    'PRO共享号池',
    COALESCE(NULLIF(description, ''), 'OpenAI Pro public shared pool'),
    rate_multiplier,
    FALSE,
    status,
    NULL,
    'public',
    'openai',
    'pro',
    'standard',
    default_validity_days,
    allow_image_generation,
    image_rate_independent,
    image_rate_multiplier,
    claude_code_only,
    model_routing,
    model_routing_enabled,
    mcp_xml_inject,
    supported_model_scopes,
    sort_order + 1,
    allow_messages_dispatch,
    require_oauth_only,
    require_privacy_set,
    default_mapped_model,
    messages_dispatch_model_config,
    rpm_limit,
    NOW(),
    NOW()
FROM source_pool
WHERE NOT EXISTS (
    SELECT 1
    FROM groups
    WHERE deleted_at IS NULL
      AND (
          name = 'PRO共享号池'
          OR (platform = 'openai' AND owner_user_id IS NULL AND scope = 'public' AND required_account_level = 'pro')
      )
);
