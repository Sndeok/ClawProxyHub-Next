-- 000006 down: 回滚缓存写入列
ALTER TABLE request_logs DROP COLUMN cache_creation_tokens;
