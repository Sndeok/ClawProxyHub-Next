// Package fingerprint — 按客户端协议生成上游请求的客户端指纹头。
// messages 入口伪装 Claude Code，openai 系入口（chat_completions/responses）伪装 Codex。
// 仅生成头集合下发（JSON map），是否采用由插件决定。
package fingerprint

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const (
	// DefaultClaudeUserAgent Claude Code 官方 CLI 的 UA（版本随上游发布变动，留空用此值）。
	DefaultClaudeUserAgent = "claude-cli/2.1.2 (external, cli)"
	// DefaultClaudeBetaFeatures Claude Code 固定的 anthropic-beta 组合。
	DefaultClaudeBetaFeatures = "oauth-2025-04-20,interleaved-thinking-2025-05-14,claude-code-20250219"
	// DefaultCodexUserAgent Codex CLI (codex-tui) 的 UA。
	DefaultCodexUserAgent = "codex-tui/0.144.4 (Windows 10.0.26200; x86_64) WindowsTerminal (codex-tui; 0.144.4)"

	// HeaderSessionID 等 Codex 会话标识头；codexHeaders 一次生成并写全。
	HeaderSessionID      = "session_id"
	HeaderThreadID       = "thread_id"
	HeaderTurnID         = "turn_id"
	HeaderInstallationID = "x-codex-installation-id"
	HeaderTurnMetadata   = "x-codex-turn-metadata"
	HeaderWindowID       = "x-codex-window-id"
)

// ClaudeHeaders 生成 Claude Code 客户端指纹头。ua 非空时覆盖默认 UA。
// 不含 authorization —— 凭据头由插件按自身鉴权方式设置。
func ClaudeHeaders(ua string) http.Header {
	if ua == "" {
		ua = DefaultClaudeUserAgent
	}
	h := http.Header{}
	h.Set("user-agent", ua)
	h.Set("x-app", "cli")
	h.Set("anthropic-beta", DefaultClaudeBetaFeatures)
	return h
}

// CodexHeaders 生成 Codex 客户端指纹头（UA/originator + 全部会话标识）。
// ua 非空时覆盖默认 UA，originator 随 UA 产品名派生。不含 authorization。
func CodexHeaders(ua string) http.Header {
	if ua == "" {
		ua = DefaultCodexUserAgent
	}
	originator := ua
	if i := strings.IndexByte(originator, '/'); i >= 0 {
		originator = originator[:i]
	}
	sessionID := uuidV7()
	turnID := uuidV7()
	installationID := uuidV7()

	h := http.Header{}
	h.Set("user-agent", ua)
	h.Set("originator", originator)
	h.Set(HeaderSessionID, sessionID)
	h.Set(HeaderThreadID, sessionID)
	h.Set(HeaderTurnID, turnID)
	h.Set(HeaderInstallationID, installationID)
	h.Set(HeaderWindowID, sessionID+":0")
	h.Set(HeaderTurnMetadata, codexTurnMetadataJSON(installationID, sessionID, sessionID, turnID, sessionID+":0"))
	return h
}

// Headers 按入口协议返回指纹头（空 source 或未知协议返回 nil）。
// source：messages = Claude Code；chat_completions / responses = Codex。
func Headers(source, ua string) http.Header {
	switch source {
	case "messages":
		return ClaudeHeaders(ua)
	case "chat_completions", "responses":
		return CodexHeaders(ua)
	default:
		return nil
	}
}

// ToMap 头集合 → 小写键 map（下发到插件 ChatRequest.extra 用）。
func ToMap(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	m := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) == 0 {
			continue
		}
		m[strings.ToLower(k)] = v[0]
	}
	return m
}

// codexTurnMetadataJSON 生成 x-codex-turn-metadata 头的 JSON 值。
func codexTurnMetadataJSON(installationID, sessionID, threadID, turnID, windowID string) string {
	b, _ := json.Marshal(map[string]any{
		"installation_id":         installationID,
		"session_id":              sessionID,
		"thread_id":               threadID,
		"turn_id":                 turnID,
		"window_id":               windowID,
		"request_kind":            "turn",
		"thread_source":           "user",
		"sandbox":                 "none",
		"turn_started_at_unix_ms": nowUnixMilli(),
	})
	return string(b)
}

// uuidV7 优先生成 UUIDv7（时间有序，贴近官方客户端形态），失败回退 v4。
func uuidV7() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	return uuid.NewString()
}
