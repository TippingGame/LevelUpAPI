-- Enable the existing image-generation capability gate for Grok groups now
-- that the gateway exposes xAI image and video media endpoints.
UPDATE groups
SET allow_image_generation = true
WHERE platform = 'grok'
  AND allow_image_generation = false;
