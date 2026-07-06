-- Map persisted Antigravity Gemini 3.1 Pro High routes to gemini-pro-agent.
--
-- Accounts without persisted model_mapping use DefaultAntigravityModelMapping
-- directly. Existing custom targets are left untouched.

UPDATE accounts
SET credentials = jsonb_set(
    credentials,
    '{model_mapping,gemini-3.1-pro}',
    '"gemini-pro-agent"'::jsonb,
    true
)
WHERE platform = 'antigravity'
  AND deleted_at IS NULL
  AND jsonb_typeof(credentials->'model_mapping') = 'object'
  AND (
      credentials->'model_mapping'->>'gemini-3.1-pro' IS NULL
      OR credentials->'model_mapping'->>'gemini-3.1-pro' = 'gemini-3.1-pro-high'
  );

UPDATE accounts
SET credentials = jsonb_set(
    credentials,
    '{model_mapping,gemini-3.1-pro-high}',
    '"gemini-pro-agent"'::jsonb,
    true
)
WHERE platform = 'antigravity'
  AND deleted_at IS NULL
  AND jsonb_typeof(credentials->'model_mapping') = 'object'
  AND (
      credentials->'model_mapping'->>'gemini-3.1-pro-high' IS NULL
      OR credentials->'model_mapping'->>'gemini-3.1-pro-high' = 'gemini-3.1-pro-high'
  );

UPDATE accounts
SET credentials = jsonb_set(
    credentials,
    '{model_mapping,gemini-3.1-pro-preview}',
    '"gemini-pro-agent"'::jsonb,
    true
)
WHERE platform = 'antigravity'
  AND deleted_at IS NULL
  AND jsonb_typeof(credentials->'model_mapping') = 'object'
  AND (
      credentials->'model_mapping'->>'gemini-3.1-pro-preview' IS NULL
      OR credentials->'model_mapping'->>'gemini-3.1-pro-preview' = 'gemini-3.1-pro-high'
  );
