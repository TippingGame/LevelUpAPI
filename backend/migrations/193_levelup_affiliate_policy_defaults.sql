-- LevelUpAPI affiliate policy defaults: 85% owner share, 5% invite share,
-- 10% platform retention, unlimited invitees,
-- and stable invite codes by default.

INSERT INTO settings (key, value, updated_at)
VALUES ('affiliate_rebate_rate', '5', NOW())
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    updated_at = NOW();

DO $$
BEGIN
    IF to_regclass('public.account_share_policies') IS NOT NULL THEN
        INSERT INTO account_share_policies (
            scope_type,
            owner_share_ratio,
            invite_share_ratio,
            enabled,
            effective_at,
            version,
            created_at,
            updated_at
        )
        SELECT
            'global',
            0.850000,
            0.050000,
            TRUE,
            NOW(),
            1,
            NOW(),
            NOW()
        WHERE NOT EXISTS (
            SELECT 1
            FROM account_share_policies
            WHERE deleted_at IS NULL
              AND enabled = TRUE
              AND scope_type = 'global'
              AND effective_at <= NOW()
        );

        UPDATE account_share_policies
        SET owner_share_ratio = LEAST(owner_share_ratio, 0.850000),
            invite_share_ratio = 0.050000,
            updated_at = NOW()
        WHERE deleted_at IS NULL
          AND scope_type = 'global';
    END IF;
END $$;

DO $$
BEGIN
    IF to_regclass('public.user_affiliates') IS NOT NULL THEN
        ALTER TABLE user_affiliates
            ALTER COLUMN aff_weekly_limit SET DEFAULT 0,
            ALTER COLUMN aff_code_auto_rotate SET DEFAULT false;

        UPDATE user_affiliates
        SET aff_weekly_limit = 0,
            aff_code_auto_rotate = false,
            aff_code_expires_at = NULL,
            updated_at = NOW();

        COMMENT ON COLUMN user_affiliates.aff_weekly_limit IS '每周邀请码可使用次数，0 表示不限量';
        COMMENT ON COLUMN user_affiliates.aff_code_auto_rotate IS '是否每周自动轮换邀请码；默认关闭时邀请码长期有效';
    END IF;
END $$;
