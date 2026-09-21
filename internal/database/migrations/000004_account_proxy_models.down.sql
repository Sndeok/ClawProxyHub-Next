-- 000004 down: 回滚账号级代理与模型目录快照
-- 子表在前，再删列
ALTER TABLE accounts DROP COLUMN models_json;
DROP TABLE IF EXISTS account_proxies;
