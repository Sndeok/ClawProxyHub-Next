-- 000003: 请求日志诊断字段 —— 让日志页本身足以定位协议 / 路由 / 账号问题
-- requested_model 与 model 分离：前者是客户端请求的名字（可能是路由别名），
-- 后者是最终投递上游的真实模型名；两者不一致时一眼能看出路由是否生效。
ALTER TABLE request_logs ADD COLUMN requested_model TEXT NOT NULL DEFAULT '';
ALTER TABLE request_logs ADD COLUMN route_id        INTEGER;
ALTER TABLE request_logs ADD COLUMN group_id        INTEGER;
ALTER TABLE request_logs ADD COLUMN stream          INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN finish_reason   TEXT NOT NULL DEFAULT '';
-- attempts：含重试 / 换号 / 降级在内的总尝试次数（1 = 一次成功）
ALTER TABLE request_logs ADD COLUMN attempts        INTEGER NOT NULL DEFAULT 1;
-- error_type：invalid_request_error / api_error / upstream_error（空 = 成功）
ALTER TABLE request_logs ADD COLUMN error_type      TEXT NOT NULL DEFAULT '';

-- 日志页过滤谓词：状态类（<400 / >=400）与模型维度
CREATE INDEX IF NOT EXISTS idx_request_logs_status_created ON request_logs(status, created_at);
CREATE INDEX IF NOT EXISTS idx_request_logs_model_created  ON request_logs(model, created_at);