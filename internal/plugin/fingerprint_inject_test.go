package plugin

import (
	"encoding/json"
	"testing"

	"github.com/Sndeok/ClawProxyHub-Next/sdk"
	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// 指纹注入：按入口协议下发客户端指纹头（messages=Claude Code；openai 系=Codex）。
func TestInjectFingerprintBySource(t *testing.T) {
	cases := []struct {
		source string
		key    string // 该协议必须出现的指纹头
		value  string
	}{
		{"messages", "x-app", "cli"},
		{"messages", "anthropic-beta", ""},
		{"chat_completions", "originator", "codex-tui"},
		{"responses", "originator", "codex-tui"},
		{"chat_completions", "session_id", ""},
	}
	for _, c := range cases {
		req := &pb.ChatRequest{Source: c.source}
		injectFingerprint(req)
		raw := req.Extra[sdk.ExtraFingerprintHeaders]
		if raw == "" {
			t.Fatalf("source=%s 未注入指纹头", c.source)
		}
		var m map[string]string
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("source=%s 指纹头不是合法 JSON: %v", c.source, err)
		}
		got, ok := m[c.key]
		if !ok {
			t.Errorf("source=%s 缺少指纹头 %s: %v", c.source, c.key, m)
			continue
		}
		if c.value != "" && got != c.value {
			t.Errorf("source=%s 头 %s = %q，期望 %q", c.source, c.key, got, c.value)
		}
	}
}

// 未知协议不注入；已有同名键不覆盖；extra 其他键保留。
func TestInjectFingerprintGuards(t *testing.T) {
	unknown := &pb.ChatRequest{Source: "grpc"}
	injectFingerprint(unknown)
	if unknown.Extra != nil {
		t.Errorf("未知协议不应注入: %v", unknown.Extra)
	}

	existing := &pb.ChatRequest{Source: "messages", Extra: map[string]string{sdk.ExtraFingerprintHeaders: `{"x-app":"custom"}`}}
	injectFingerprint(existing)
	if got := existing.Extra[sdk.ExtraFingerprintHeaders]; got != `{"x-app":"custom"}` {
		t.Errorf("已有指纹头被覆盖: %s", got)
	}

	keep := &pb.ChatRequest{Source: "messages", Extra: map[string]string{"top_p": "0.5"}}
	injectFingerprint(keep)
	if keep.Extra["top_p"] != "0.5" {
		t.Errorf("extra 其他键丢失: %v", keep.Extra)
	}
	if keep.Extra[sdk.ExtraFingerprintHeaders] == "" {
		t.Error("应注入指纹头")
	}
}
