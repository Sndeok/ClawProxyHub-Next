-- 000007 — 调用日志新增「请求原文」与「完整上游返回」
-- 背景：error_brief 只留 512 字节摘要，排查上游 400/500 时既看不到原始响应体，
-- 也不知道 CPH 究竟发了什么过去（协议转换必须两步都可见才排得动）。
-- 两列体积大：列表接口用 json:"-" 排除，只在日志详情返回；旧数据为空，向后兼容。

ALTER TABLE request_logs ADD COLUMN error_detail TEXT NOT NULL DEFAULT '';
ALTER TABLE request_logs ADD COLUMN request_body TEXT NOT NULL DEFAULT '';
