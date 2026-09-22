-- 000007 down: 回滚日志详情大字段
ALTER TABLE request_logs DROP COLUMN request_body;
ALTER TABLE request_logs DROP COLUMN error_detail;
