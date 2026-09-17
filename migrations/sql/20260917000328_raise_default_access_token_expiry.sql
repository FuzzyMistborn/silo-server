-- +goose Up
-- The original schema seeds 1h; later runtime defaults used 8h. There is
-- no settings provenance, so both exact legacy values move to the new
-- default. Other administrator-selected values remain unchanged.
UPDATE public.server_settings
SET value = '24h'
WHERE key = 'auth.access_token_expiry'
  AND value IN ('1h', '8h');

-- +goose Down
-- Keep configured lifetimes on rollback: a 24h value may have been selected
-- by an administrator, and the previous binary accepts it.
SELECT 1;
