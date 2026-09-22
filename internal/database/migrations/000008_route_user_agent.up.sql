-- 000008 — 路由级 User-Agent
-- 背景：同一账号池里，不同路由可能对接不同上游形态（例如 A 路由装 Claude Code、
-- B 路由装 Codex）。允许每条路由指定出站 UA，插件按需透传。
-- 解析优先级：路由 UA > 全局网关 UA（settings: gateway.user_agent）> 客户端 UA。

ALTER TABLE routes ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
