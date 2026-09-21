-- 000003 down: 回滚请求日志诊断字段与索引
DROP INDEX IF EXISTS idx_request_logs_model_created;
DROP INDEX IF EXISTS idx_request_logs_status_created;
-- DROP COLUMN 需 SQLite >= 3.35（mattn/go-sqlite3 内置版本远高于此）
ALTER TABLE request_logs DROP COLUMN error_type;
ALTER TABLE request_logs DROP COLUMN attempts;
ALTER TABLE request_logs DROP COLUMN finish_reason;
ALTER TABLE request_logs DROP COLUMN stream;
ALTER TABLE request_logs DROP COLUMN group_id;
ALTER TABLE request_logs DROP COLUMN route_id;
ALTER TABLE request_logs DROP COLUMN requested_model;