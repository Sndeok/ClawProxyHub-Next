-- 000002 down: 回滚密钥哈希列与插件存储表
DROP TABLE IF EXISTS plugin_storage;
DROP INDEX IF EXISTS idx_keys_key_hash;
-- SQLite 不支持 DROP COLUMN（旧版本），重建 keys 表去掉 key_hash
CREATE TABLE IF NOT EXISTS keys_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    key_cipher TEXT    NOT NULL UNIQUE,
    name       TEXT    NOT NULL DEFAULT '',
    enabled    INTEGER NOT NULL DEFAULT 1,
    expires_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO keys_new SELECT id, key_cipher, name, enabled, expires_at, created_at FROM keys;
DROP TABLE keys;
ALTER TABLE keys_new RENAME TO keys;
