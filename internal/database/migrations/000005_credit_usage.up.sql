-- 000005 — 调用日志积分消耗 + 账号每日积分消耗快照
-- 背景：
--   1) request_logs 只统计 token，无法回答「这次请求花了多少积分」；
--      插件在 Usage 里透出 credit_used 后在这里落库（旧插件不填恒为 0）。
--   2) 上游 /usage 只给剩余积分，不给「今日消耗」。这里按账号×天记录当天首次
--      与最近一次看到的剩余积分，差值即当天净消耗（充值会抬高基线，不会算成负消耗）。

ALTER TABLE request_logs ADD COLUMN credit_used REAL NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS account_credit_daily (
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    day        TEXT    NOT NULL, -- 本地时区 YYYY-MM-DD
    remaining  REAL    NOT NULL DEFAULT 0, -- 当天最近一次看到的剩余积分
    baseline   REAL    NOT NULL DEFAULT 0, -- 当天基线（首次看到的值；充值抬升后同步上移）
    used       REAL    NOT NULL DEFAULT 0, -- baseline - remaining，下限 0
    samples    INTEGER NOT NULL DEFAULT 0, -- 当天采样次数（=0 表示仅占位无有效积分）
    updated_at DATETIME,
    PRIMARY KEY (account_id, day)
);

CREATE INDEX IF NOT EXISTS idx_account_credit_daily_day ON account_credit_daily(day);