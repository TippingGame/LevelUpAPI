-- Reconcile approved OpenAI OAuth public-share accounts with the shared pool
-- that matches their effective plan level. This covers accounts whose
-- credentials/extra plan_type changed after they were originally approved.

WITH candidate_accounts AS (
    SELECT
        a.id,
        CASE
            WHEN lower(btrim(COALESCE(a.account_level, ''))) = 'team'
                OR plan.token IN ('team', 'chatgptteam')
                OR plan.token LIKE 'team%' THEN 'team'
            WHEN lower(btrim(COALESCE(a.account_level, ''))) = 'pro'
                OR plan.token = 'pro'
                OR plan.token = 'chatgptpro'
                OR plan.token LIKE 'pro%'
                OR plan.token LIKE 'chatgptpro%' THEN 'pro'
            WHEN lower(btrim(COALESCE(a.account_level, ''))) = 'plus'
                OR plan.token = 'plus'
                OR plan.token = 'chatgptplus'
                OR plan.token LIKE 'plus%' THEN 'plus'
            WHEN lower(btrim(COALESCE(a.account_level, ''))) = 'free'
                OR plan.token IN ('free', 'chatgptfree') THEN 'free'
            ELSE 'free'
        END AS effective_level
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
),
candidate_target_groups AS (
    SELECT
        g.id,
        lower(btrim(COALESCE(g.required_account_level, ''))) AS required_level,
        g.name,
        g.sort_order
    FROM groups g
    WHERE g.deleted_at IS NULL
      AND g.platform = 'openai'
      AND g.status = 'active'
      AND g.owner_user_id IS NULL
      AND g.scope = 'public'
      AND g.is_exclusive = FALSE
      AND COALESCE(g.subscription_type, '') IN ('', 'standard')
      AND lower(btrim(COALESCE(g.required_account_level, ''))) IN ('free', 'plus', 'pro', 'team')
),
target_groups AS (
    SELECT DISTINCT ON (required_level)
        required_level,
        id
    FROM candidate_target_groups
    ORDER BY
        required_level,
        CASE
            WHEN required_level = 'pro' AND name = 'PRO共享号池' THEN 0
            WHEN required_level = 'pro' AND name = 'OpenAI PRO共享号池' THEN 1
            WHEN required_level = 'pro' AND name = 'OpenAI PRO共享号池(公共)' THEN 2
            WHEN name LIKE '%共享号池%' THEN 3
            ELSE 4
        END,
        sort_order,
        id
),
matched_accounts AS (
    SELECT ca.id AS account_id, tg.id AS target_group_id
    FROM candidate_accounts ca
    JOIN target_groups tg ON tg.required_level = ca.effective_level
),
stale_public_bindings AS (
    DELETE FROM account_groups ag
    USING groups g, matched_accounts ma
    WHERE ag.account_id = ma.account_id
      AND ag.group_id = g.id
      AND g.deleted_at IS NULL
      AND g.platform = 'openai'
      AND g.owner_user_id IS NULL
      AND g.scope = 'public'
      AND g.is_exclusive = FALSE
      AND COALESCE(g.subscription_type, '') IN ('', 'standard')
      AND g.id <> ma.target_group_id
    RETURNING ag.account_id, ag.priority
),
preferred_priorities AS (
    SELECT
        ma.account_id,
        ma.target_group_id,
        COALESCE(MIN(spb.priority), 50) AS priority
    FROM matched_accounts ma
    LEFT JOIN stale_public_bindings spb ON spb.account_id = ma.account_id
    GROUP BY ma.account_id, ma.target_group_id
)
INSERT INTO account_groups (account_id, group_id, priority, created_at)
SELECT account_id, target_group_id, priority, NOW()
FROM preferred_priorities
ON CONFLICT (account_id, group_id) DO NOTHING;
