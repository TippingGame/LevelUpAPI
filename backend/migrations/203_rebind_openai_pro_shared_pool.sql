WITH pro_pool AS (
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
      AND a.share_mode = 'public'
      AND a.share_status = 'approved'
      AND NOT (
          a.account_level = 'team'
          OR plan.token IN ('team', 'chatgptteam')
          OR plan.token LIKE 'team%'
      )
      AND (
          a.account_level = 'pro'
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
