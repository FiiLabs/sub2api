-- Seed a placeholder account row for TEE-gateway usage logging.
-- This row satisfies the usage_logs.account_id foreign key when the TEE executor
-- reports usage via POST /consult/post. It is never schedulable (i.e. never used
-- to proxy upstream API traffic); it exists solely as an FK anchor.
-- Idempotent, safe to re-run.
INSERT INTO accounts (name, platform, type, status, credentials, extra, schedulable, created_at, updated_at)
VALUES ('tee-gateway', 'anthropic', 'api_key', 'disabled', '{}', '{}', false, now(), now())
ON CONFLICT DO NOTHING;
