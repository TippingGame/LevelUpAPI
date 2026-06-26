-- Align the user portal defaults with the LevelUpAPI deployment.
-- Keep custom branding intact; only replace legacy Sub2API defaults.

INSERT INTO settings (key, value, updated_at)
VALUES
    ('site_name', 'LevelUpAPI', NOW()),
    ('site_subtitle', 'Game-ready AI API Gateway', NOW()),
    ('registration_enabled', 'true', NOW()),
    ('available_channels_enabled', 'true', NOW()),
    ('affiliate_enabled', 'true', NOW())
ON CONFLICT (key) DO NOTHING;

UPDATE settings
SET value = 'LevelUpAPI', updated_at = NOW()
WHERE key = 'site_name'
  AND (value = '' OR value = 'Sub2API');

UPDATE settings
SET value = 'Game-ready AI API Gateway', updated_at = NOW()
WHERE key = 'site_subtitle'
  AND (value = '' OR value = 'Subscription to API Conversion Platform');

UPDATE settings
SET value = 'true', updated_at = NOW()
WHERE key IN (
    'registration_enabled',
    'available_channels_enabled',
    'affiliate_enabled'
);

DO $$
BEGIN
    IF to_regclass('public.user_attribute_definitions') IS NOT NULL THEN
        INSERT INTO user_attribute_definitions (
            key,
            name,
            description,
            type,
            options,
            required,
            validation,
            placeholder,
            display_order,
            enabled,
            created_at,
            updated_at
        )
        SELECT
            'shared_account_owner',
            '共享号主',
            '授权后用户可以使用“我的账号”功能并管理自有共享账号。',
            'select',
            '[{"value":"true","label":"共享号主"},{"value":"false","label":"普通用户"}]'::jsonb,
            FALSE,
            '{}'::jsonb,
            '',
            COALESCE((SELECT MAX(display_order) + 10 FROM user_attribute_definitions WHERE deleted_at IS NULL), 1000),
            TRUE,
            NOW(),
            NOW()
        WHERE NOT EXISTS (
            SELECT 1
            FROM user_attribute_definitions
            WHERE key = 'shared_account_owner'
              AND deleted_at IS NULL
        );
    END IF;
END $$;
