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
  AND (owner_user_id IS NULL OR lower(btrim(COALESCE(scope, ''))) = 'public')
  AND (
      lower(btrim(COALESCE(required_account_level, ''))) = 'pro'
      OR btrim(name) IN ('PRO共享号池', 'OpenAI PRO共享号池', 'OpenAI PRO共享号池(公共)')
  )
  AND (
      lower(btrim(COALESCE(scope, ''))) <> 'public'
      OR owner_user_id IS NOT NULL
      OR lower(btrim(COALESCE(subscription_type, ''))) <> 'standard'
      OR lower(btrim(COALESCE(required_account_level, ''))) <> 'pro'
      OR is_exclusive = TRUE
  );
