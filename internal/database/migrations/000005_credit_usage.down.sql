-- 000005 down: 回滚积分列与每日快照表
DROP INDEX IF EXISTS idx_account_credit_daily_day;
DROP TABLE IF EXISTS account_credit_daily;
ALTER TABLE request_logs DROP COLUMN credit_used;