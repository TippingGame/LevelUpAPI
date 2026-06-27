-- LevelUpAPI share policy defaults: 85% owner share, 5% invite share,
-- and 10% platform retention for new or still-default global policies.
--
-- Do not modify migration 193: it may already be applied in production and
-- is protected by checksum validation.

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
        SET owner_share_ratio = 0.850000,
            invite_share_ratio = 0.050000,
            version = version + 1,
            updated_at = NOW()
        WHERE deleted_at IS NULL
          AND enabled = TRUE
          AND scope_type = 'global'
          AND owner_share_ratio = 0.950000
          AND invite_share_ratio = 0.050000;
    END IF;
END $$;
