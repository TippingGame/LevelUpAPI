UPDATE groups
SET scope = CASE lower(btrim(COALESCE(scope, '')))
        WHEN 'user_private' THEN 'user_private'
        ELSE 'public'
    END,
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND lower(btrim(COALESCE(scope, ''))) IN ('public', 'user_private')
  AND scope IS DISTINCT FROM CASE lower(btrim(COALESCE(scope, '')))
        WHEN 'user_private' THEN 'user_private'
        ELSE 'public'
    END;

UPDATE groups
SET subscription_type = CASE lower(btrim(COALESCE(subscription_type, '')))
        WHEN 'subscription' THEN 'subscription'
        ELSE 'standard'
    END,
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND lower(btrim(COALESCE(subscription_type, ''))) IN ('', 'standard', 'subscription')
  AND subscription_type IS DISTINCT FROM CASE lower(btrim(COALESCE(subscription_type, '')))
        WHEN 'subscription' THEN 'subscription'
        ELSE 'standard'
    END;
