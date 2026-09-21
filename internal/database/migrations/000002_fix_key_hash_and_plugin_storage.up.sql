-- 000002: 密钥哈希索引（消除全表扫描）+ 插件持久化存储
-- key_hash 存 SHA-256 hex(raw key)，用于等值索引查询快速定位
ALTER TABLE keys ADD COLUMN key_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_keys_key_hash ON keys(key_hash);

-- 回填存量 SHA-256 格式密钥（key_cipher 为 64 位纯 hex 字符串 = sha256）
UPDATE keys SET key_hash = key_cipher
WHERE LENGTH(key_cipher) = 64 AND key_cipher NOT GLOB '*[^0-9a-f]*';

-- 插件持久化 KV 存储（按插件隔离）
CREATE TABLE IF NOT EXISTS plugin_storage (
    plugin     TEXT NOT NULL,
    key        TEXT NOT NULL,
    value      BLOB,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (plugin, key)
);