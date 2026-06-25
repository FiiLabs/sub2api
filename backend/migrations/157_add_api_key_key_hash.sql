-- 为 api_keys 表新增 key_hash 列（SHA256(key) hex）及部分索引，
-- 供 consult 控制面按哈希查询 key，不暴露明文 key。
-- 幂等，可重复执行。
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS key_hash varchar(64);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash) WHERE deleted_at IS NULL;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
UPDATE api_keys SET key_hash = encode(digest(key,'sha256'),'hex') WHERE key_hash IS NULL;
