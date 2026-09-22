package fingerprint

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClaudeHeaders(t *testing.T) {
	h := ClaudeHeaders("")
	if got := h.Get("user-agent"); got != DefaultClaudeUserAgent {
		t.Errorf("默认 UA 不符: %s", got)
	}
	if h.Get("x-app") != "cli" {
		t.Errorf("x-app 缺失: %v", h)
	}
	if h.Get("anthropic-beta") != DefaultClaudeBetaFeatures {
		t.Errorf("anthropic-beta 不符: %s", h.Get("anthropic-beta"))
	}
	if h.Get("authorization") != "" {
		t.Error("指纹头不应携带 authorization")
	}
	if got := ClaudeHeaders("my-cli/9.9.9").Get("user-agent"); got != "my-cli/9.9.9" {
		t.Errorf("自定义 UA 未生效: %s", got)
	}
}

func TestCodexHeaders(t *testing.T) {
	h := CodexHeaders("")
	if got := h.Get("user-agent"); got != DefaultCodexUserAgent {
		t.Errorf("默认 UA 不符: %s", got)
	}
	if got := h.Get("originator"); got != "codex-tui" {
		t.Errorf("originator 应从 UA 产品名派生，实际 %s", got)
	}
	sessionID := h.Get(HeaderSessionID)
	if sessionID == "" || h.Get(HeaderThreadID) != sessionID {
		t.Errorf("session/thread 标识不一致: %s / %s", sessionID, h.Get(HeaderThreadID))
	}
	if h.Get(HeaderTurnID) == "" || h.Get(HeaderInstallationID) == "" {
		t.Errorf("turn/installation 标识缺失: %v", h)
	}
	if got := h.Get(HeaderWindowID); !strings.HasPrefix(got, sessionID+":") {
		t.Errorf("window_id 格式不符: %s", got)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(h.Get(HeaderTurnMetadata)), &meta); err != nil {
		t.Fatalf("turn metadata 不是 JSON: %v", err)
	}
	for _, k := range []string{"installation_id", "session_id", "thread_id", "turn_id", "window_id", "request_kind"} {
		if _, ok := meta[k]; !ok {
			t.Errorf("turn metadata 缺字段 %s: %v", k, meta)
		}
	}
	// 两次生成的会话标识必须不同（每次请求新会话）
	if CodexHeaders("").Get(HeaderSessionID) == sessionID {
		t.Error("两次调用应生成不同 session_id")
	}
}

func TestHeadersBySource(t *testing.T) {
	if Headers("messages", "").Get("x-app") != "cli" {
		t.Error("messages 入口应给 Claude Code 指纹")
	}
	for _, src := range []string{"chat_completions", "responses"} {
		if Headers(src, "").Get("originator") == "" {
			t.Errorf("%s 入口应给 Codex 指纹", src)
		}
	}
	if Headers("", "") != nil || Headers("unknown", "") != nil {
		t.Error("未知协议不应生成指纹头")
	}
}

func TestToMap(t *testing.T) {
	m := ToMap(ClaudeHeaders(""))
	if m["user-agent"] != DefaultClaudeUserAgent {
		t.Errorf("map 键应为小写头名: %v", m)
	}
	if ToMap(nil) != nil {
		t.Error("空头集合应返回 nil")
	}
}
