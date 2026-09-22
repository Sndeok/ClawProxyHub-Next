package gateway

import (
	"testing"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// TestChatCompletionsParamPassthrough 回归：new-api / Codex 常发的采样与控制参数
// 过去被解析层直接丢弃（既不进信封也不进 Extra），上游只能吃默认值。
func TestChatCompletionsParamPassthrough(t *testing.T) {
	body := []byte(`{
		"model": "glm-5.3",
		"max_completion_tokens": 4096,
		"temperature": 0,
		"reasoning_effort": "high",
		"frequency_penalty": 0.5,
		"presence_penalty": -0.25,
		"seed": 42,
		"parallel_tool_calls": false,
		"response_format": {"type":"json_object"},
		"user": "kylin",
		"messages": [{"role":"user","content":"hi"}]
	}`)
	req, err := parseChatCompletions(body)
	if err != nil {
		t.Fatal(err)
	}
	if req.MaxTokens != 4096 {
		t.Errorf("max_completion_tokens 未生效：%d", req.MaxTokens)
	}
	// temperature=0 是显式要求，必须能从 Extra 区分出"客户端没传"
	if got := req.Extra["temperature"]; got != "0" {
		t.Errorf("显式 temperature=0 未记录：%q", got)
	}
	want := map[string]string{
		"reasoning_effort":    "high",
		"frequency_penalty":   "0.5",
		"presence_penalty":    "-0.25",
		"seed":                "42",
		"parallel_tool_calls": "false",
		"response_format":     `{"type":"json_object"}`,
		"user":                "kylin",
	}
	for k, v := range want {
		if req.Extra[k] != v {
			t.Errorf("Extra[%s] want %q got %q", k, v, req.Extra[k])
		}
	}

	// 未传的参数不得凭空出现（避免插件误发 null）
	bare, err := parseChatCompletions([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bare.Extra["temperature"]; ok {
		t.Errorf("未传 temperature 却写入了 Extra：%q", bare.Extra["temperature"])
	}
	if _, ok := bare.Extra["seed"]; ok {
		t.Errorf("未传 seed 却写入了 Extra")
	}
}

// TestAnthropicTopKAndUserID 回归：top_k 与 metadata.user_id 过去被丢弃。
func TestAnthropicTopKAndUserID(t *testing.T) {
	body := []byte(`{
		"model": "claude-x",
		"max_tokens": 64,
		"top_k": 40,
		"metadata": {"user_id": "u-123"},
		"messages": [{"role":"user","content":"hi"}]
	}`)
	req, err := parseAnthropicRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if req.Extra["top_k"] != "40" {
		t.Errorf("top_k 未透传：%q", req.Extra["top_k"])
	}
	if req.Extra["user"] != "u-123" {
		t.Errorf("metadata.user_id 未透传：%q", req.Extra["user"])
	}
	_ = pb.ChatRequest{}
}
