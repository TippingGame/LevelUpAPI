-- Ensure a canonical OpenAI Pro public shared pool exists even when earlier
-- repair migrations encountered non-normalized group metadata, then rebind
-- approved OAuth accounts whose observed plan is Pro.

UPDATE groups
SET platform = 'openai',
    scope = 'public',
    owner_user_id = NULL,
    subscription_type = 'standard',
    required_account_level = 'pro',
    is_exclusive = FALSE,
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND status = 'active'
  AND platform = 'openai'
  AND (owner_user_id IS NULL OR scope = 'public')
  AND (
      lower(btrim(COALESCE(required_account_level, ''))) = 'pro'
      OR btrim(name) = 'PRO共享号池'
  )
  AND (
      scope <> 'public'
      OR owner_user_id IS NOT NULL
      OR COALESCE(subscription_type, '') <> 'standard'
      OR required_account_level <> 'pro'
      OR is_exclusive = TRUE
  );

WITH existing_pro_pool AS (
    SELECT id
    FROM groups
    WHERE deleted_at IS NULL
      AND platform = 'openai'
      AND status = 'active'
      AND owner_user_id IS NULL
      AND scope = 'public'
      AND is_exclusive = FALSE
      AND COALESCE(subscription_type, '') IN ('', 'standard')
      AND lower(btrim(COALESCE(required_account_level, ''))) = 'pro'
    LIMIT 1
),
source_pool AS (
    SELECT
        description,
        rate_multiplier,
        default_validity_days,
        allow_image_generation,
        image_rate_independent,
        image_rate_multiplier,
        claude_code_only,
        COALESCE(model_routing, '{}'::jsonb) AS model_routing,
        model_routing_enabled,
        mcp_xml_inject,
        COALESCE(supported_model_scopes, '[]'::jsonb) AS supported_model_scopes,
        sort_order,
        allow_messages_dispatch,
        require_oauth_only,
        require_privacy_set,
        default_mapped_model,
        COALESCE(messages_dispatch_model_config, '{}'::jsonb) AS messages_dispatch_model_config,
        rpm_limit
    FROM groups
    WHERE deleted_at IS NULL
      AND platform = 'openai'
      AND status = 'active'
      AND owner_user_id IS NULL
      AND scope = 'public'
      AND is_exclusive = FALSE
      AND COALESCE(subscription_type, '') IN ('', 'standard')
      AND lower(btrim(COALESCE(required_account_level, ''))) IN ('plus', 'free', '')
    ORDER BY CASE lower(btrim(COALESCE(required_account_level, '')))
        WHEN 'plus' THEN 0
        WHEN 'free' THEN 1
        ELSE 2
    END, sort_order, id
    LIMIT 1
),
template_pool AS (
    SELECT * FROM source_pool
    UNION ALL
    SELECT
        'OpenAI Pro public shared pool'::text,
        1.0::numeric,
        30::integer,
        FALSE,
        FALSE,
        1.0::numeric,
        FALSE,
        '{}'::jsonb,
        FALSE,
        TRUE,
        '[]'::jsonb,
        0::integer,
        FALSE,
        FALSE,
        FALSE,
        ''::text,
        '{}'::jsonb,
        0::integer
    WHERE NOT EXISTS (SELECT 1 FROM source_pool)
    LIMIT 1
),
candidate_name AS (
    SELECT CASE
        WHEN NOT EXISTS (SELECT 1 FROM groups WHERE deleted_at IS NULL AND name = 'PRO共享号池')
            THEN 'PRO共享号池'
        WHEN NOT EXISTS (SELECT 1 FROM groups WHERE deleted_at IS NULL AND name = 'OpenAI PRO共享号池')
            THEN 'OpenAI PRO共享号池'
        WHEN NOT EXISTS (SELECT 1 FROM groups WHERE deleted_at IS NULL AND name = 'OpenAI PRO共享号池(公共)')
            THEN 'OpenAI PRO共享号池(公共)'
        ELSE 'OpenAI PRO共享号池-' || ((SELECT COALESCE(MAX(id), 0) FROM groups) + 1)::text
    END AS name
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
    candidate_name.name,
    COALESCE(NULLIF(template_pool.description, ''), 'OpenAI Pro public shared pool'),
    template_pool.rate_multiplier,
    FALSE,
    'active',
    NULL,
    'public',
    'openai',
    'pro',
    'standard',
    template_pool.default_validity_days,
    template_pool.allow_image_generation,
    template_pool.image_rate_independent,
    template_pool.image_rate_multiplier,
    template_pool.claude_code_only,
    template_pool.model_routing,
    template_pool.model_routing_enabled,
    template_pool.mcp_xml_inject,
    template_pool.supported_model_scopes,
    template_pool.sort_order + 1,
    template_pool.allow_messages_dispatch,
    template_pool.require_oauth_only,
    template_pool.require_privacy_set,
    template_pool.default_mapped_model,
    template_pool.messages_dispatch_model_config,
    template_pool.rpm_limit,
    NOW(),
    NOW()
FROM candidate_name
CROSS JOIN template_pool
WHERE NOT EXISTS (SELECT 1 FROM existing_pro_pool);

WITH pro_pool AS (
    SELECT id
    FROM groups
    WHERE deleted_at IS NULL
      AND platform = 'openai'
      AND status = 'active'
      AND owner_user_id IS NULL
      AND scope = 'public'
      AND is_exclusive = FALSE
      AND COALESCE(subscription_type, '') IN ('', 'standard')
      AND lower(btrim(COALESCE(required_account_level, ''))) = 'pro'
    ORDER BY
        CASE
            WHEN name = 'PRO共享号池' THEN 0
            WHEN name = 'OpenAI PRO共享号池' THEN 1
            WHEN name = 'OpenAI PRO共享号池(公共)' THEN 2
            ELSE 3
        END,
        sort_order,
        id
    LIMIT 1
),
effective_pro_accounts AS (
    SELECT a.id
    FROM accounts a
    CROSS JOIN LATERAL (
        SELECT regexp_replace(lower(COALESCE(
            NULLIF(a.credentials->>'plan_type', ''),
            NULLIF(a.credentials->>'chatgpt_plan_type', ''),
            NULLIF(a.credentials->>'subscription_plan', ''),
            NULLIF(a.extra->>'plan_type', ''),
            NULLIF(a.extra->>'chatgpt_plan_type', ''),
            NULLIF(a.extra->>'subscription_plan', ''),
            ''
        )), '[[:space:]_-]+', '', 'g') AS token
    ) plan
    WHERE a.deleted_at IS NULL
      AND a.platform = 'openai'
      AND a.type = 'oauth'
      AND a.owner_user_id IS NOT NULL
      AND lower(btrim(COALESCE(a.share_mode, ''))) = 'public'
      AND lower(btrim(COALESCE(a.share_status, ''))) = 'approved'
      AND NOT (
          lower(btrim(COALESCE(a.account_level, ''))) = 'team'
          OR plan.token IN ('team', 'chatgptteam')
          OR plan.token LIKE 'team%'
      )
      AND (
          lower(btrim(COALESCE(a.account_level, ''))) = 'pro'
          OR plan.token = 'pro'
          OR plan.token = 'chatgptpro'
          OR plan.token LIKE 'pro%'
          OR plan.token LIKE 'chatgptpro%'
      )
),
stale_public_bindings AS (
    DELETE FROM account_groups ag
    USING groups g, effective_pro_accounts a, pro_pool p
    WHERE ag.account_id = a.id
      AND ag.group_id = g.id
      AND g.deleted_at IS NULL
      AND g.platform = 'openai'
      AND g.owner_user_id IS NULL
      AND g.scope = 'public'
      AND COALESCE(g.subscription_type, '') IN ('', 'standard')
      AND g.id <> p.id
    RETURNING ag.account_id, ag.priority
),
preferred_priorities AS (
    SELECT
        a.id AS account_id,
        COALESCE(MIN(s.priority), 50) AS priority
    FROM effective_pro_accounts a
    LEFT JOIN stale_public_bindings s ON s.account_id = a.id
    GROUP BY a.id
)
INSERT INTO account_groups (account_id, group_id, priority, created_at)
SELECT pp.account_id, p.id, pp.priority, NOW()
FROM preferred_priorities pp
CROSS JOIN pro_pool p
ON CONFLICT (account_id, group_id) DO NOTHING;
