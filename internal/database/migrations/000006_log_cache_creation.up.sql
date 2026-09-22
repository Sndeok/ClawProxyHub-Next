-- 000006 — 调用日志新增「缓存写入 token」
-- 背景：命中（cache read）与写入（cache write）是两份不同的用量，单价也不同。
-- 过去只存 cached_tokens，Anthropic 系上游的 cache_creation 与 OpenAI 系
-- cache_write_tokens 被丢弃，日志里既看不到写入量也核不出账。
-- 旧插件不填该字段，恒为 0，天然向后兼容。

ALTER TABLE request_logs ADD COLUMN cache_creation_tokens INTEGER NOT NULL DEFAULT 0;
