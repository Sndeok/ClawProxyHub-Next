package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

func TestParseAnthropicRequest(t *testing.T) {
	body := `{
		"model": "kimi-k3",
		"system": "你是个助手",
		"max_tokens": 1024,
		"temperature": 0.7,
		"stream": true,
		"messages": [
			{"role": "user", "content": "你好"},
			{"role": "assistant", "content": [
				{"type": "text", "text": "我来调用工具"},
				{"type": "tool_use", "id": "tu_1", "name": "get_weather", "input": {"city": "北京"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "tu_1", "content": "晴"}
			]}
		],
		"tools": [{"name": "get_weather", "description": "查天气", "input_schema": {"type": "object"}}],
		"tool_choice": {"type": "auto"}
	}`
	req, err := parseAnthropicRequest([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "kimi-k3" || !req.Stream || req.MaxTokens != 1024 || req.Temperature != 0.7 {
		t.Fatalf("basic fields wrong: %+v", req)
	}
	// system + user + assistant + tool_result拆出的tool = 4条
	if len(req.Messages) != 4 {
		t.Fatalf("want 4 messages, got %d: %+v", len(req.Messages), req.Messages)
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Text != "你是个助手" {
		t.Errorf("system message wrong: %+v", req.Messages[0])
	}
	asst := req.Messages[2]
	if asst.Role != "assistant" || len(asst.ToolCalls) != 1 || asst.ToolCalls[0].Id != "tu_1" {
		t.Errorf("assistant tool_calls wrong: %+v", asst)
	}
	if asst.ToolCalls[0].Arguments != `{"city":"北京"}` {
		t.Errorf("tool arguments wrong: %s", asst.ToolCalls[0].Arguments)
	}
	toolMsg := req.Messages[3]
	if toolMsg.Role != "tool" || toolMsg.ToolCallId != "tu_1" || toolMsg.Text != "晴" {
		t.Errorf("tool_result message wrong: %+v", toolMsg)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "get_weather" {
		t.Errorf("tools wrong: %+v", req.Tools)
	}
	if req.ToolChoice == nil || req.ToolChoice.Type != "auto" {
		t.Errorf("tool_choice wrong: %+v", req.ToolChoice)
	}
}

func TestAnthropicSSE(t *testing.T) {
	st := newAnthSSEState("kimi-k3")
	var sb strings.Builder

	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_MessageStart{
		MessageStart: &pb.MessageStart{Model: "kimi-k3"},
	}}))
	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_ContentDelta{
		ContentDelta: &pb.ContentDelta{Text: "你好"},
	}}))
	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_ContentDelta{
		ContentDelta: &pb.ContentDelta{Text: "，世界"},
	}}))
	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_ToolCallDelta{
		ToolCallDelta: &pb.ToolCallDelta{Id: "tu_1", Name: "get_weather", ArgumentsDelta: `{"city":`},
	}}))
	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_ToolCallDelta{
		ToolCallDelta: &pb.ToolCallDelta{Id: "tu_1", ArgumentsDelta: `"北京"}`},
	}}))
	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
		MessageFinish: &pb.MessageFinish{
			FinishReason: "tool_calls",
			Usage:        &pb.Usage{InputTokens: 10, OutputTokens: 20},
		},
	}}))

	out := sb.String()
	for _, want := range []string{
		"event: message_start",
		"event: content_block_start",
		"event: content_block_delta",
		"\"text_delta\"",
		"\"tool_use\"",
		"\"input_json_delta\"",
		"\"partial_json\":\"{\\\"city\\\":\"",
		"event: content_block_stop",
		"event: message_delta",
		"\"stop_reason\":\"tool_use\"",
		"\"input_tokens\":10",
		"\"output_tokens\":20",
		"event: message_stop",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("SSE missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestParseChatCompletions(t *testing.T) {
	body := `{
		"model": "kimi-k3",
		"messages": [
			{"role": "system", "content": "sys"},
			{"role": "user", "content": [{"type": "text", "text": "hi"}]},
			{"role": "assistant", "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "f", "arguments": "{}"}}
			], "content": null},
			{"role": "tool", "tool_call_id": "call_1", "content": "result"}
		],
		"tools": [{"type": "function", "function": {"name": "f", "parameters": {"type": "object"}}}],
		"tool_choice": "auto",
		"stream": false
	}`
	req, err := parseChatCompletions([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 4 {
		t.Fatalf("want 4 messages, got %d", len(req.Messages))
	}
	if req.Messages[1].Text != "hi" {
		t.Errorf("user text wrong: %q", req.Messages[1].Text)
	}
	if len(req.Messages[2].ToolCalls) != 1 || req.Messages[2].ToolCalls[0].Id != "call_1" {
		t.Errorf("assistant tool_calls wrong: %+v", req.Messages[2])
	}
	if req.Messages[3].Role != "tool" || req.Messages[3].ToolCallId != "call_1" {
		t.Errorf("tool message wrong: %+v", req.Messages[3])
	}
	if req.Tools[0].ParametersSchema == "" {
		t.Errorf("parameters schema empty")
	}
	if req.ToolChoice.Type != "auto" {
		t.Errorf("tool_choice wrong: %+v", req.ToolChoice)
	}
}

func TestOpenAISSEAndAggregate(t *testing.T) {
	st := newOpenAISSEState()
	var sb strings.Builder
	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_ContentDelta{
		ContentDelta: &pb.ContentDelta{Text: "hello"},
	}}))
	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
		MessageFinish: &pb.MessageFinish{FinishReason: "stop", Usage: &pb.Usage{InputTokens: 5, OutputTokens: 1}},
	}}))
	out := sb.String()
	if !strings.Contains(out, `"content":"hello"`) {
		t.Errorf("missing content delta:\n%s", out)
	}
	if !strings.Contains(out, `"finish_reason":"stop"`) {
		t.Errorf("missing finish_reason:\n%s", out)
	}
	if !strings.Contains(out, `"total_tokens":6`) || !strings.Contains(out, "data: [DONE]") {
		// usage 块在 finish 里，[DONE] 由 server 层补，这里只验证 usage
		if !strings.Contains(out, `"total_tokens":6`) {
			t.Errorf("missing usage chunk:\n%s", out)
		}
	}

	// 非流式聚合
	agg := &openaiAggregate{}
	agg.feed(&pb.StreamEvent{Event: &pb.StreamEvent_MessageStart{MessageStart: &pb.MessageStart{Model: "m"}}})
	agg.feed(&pb.StreamEvent{Event: &pb.StreamEvent_ContentDelta{ContentDelta: &pb.ContentDelta{Text: "a"}}})
	agg.feed(&pb.StreamEvent{Event: &pb.StreamEvent_ContentDelta{ContentDelta: &pb.ContentDelta{Text: "b"}}})
	agg.feed(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
		MessageFinish: &pb.MessageFinish{FinishReason: "stop", Usage: &pb.Usage{InputTokens: 1, OutputTokens: 2}},
	}})
	res := agg.result()
	if res["model"] != "m" {
		t.Errorf("model wrong: %v", res["model"])
	}
	b, _ := json.Marshal(res)
	if !strings.Contains(string(b), `"content":"ab"`) {
		t.Errorf("aggregated content wrong: %s", b)
	}
}

// TestAnthropicSSECacheTokens 回归：上游透出缓存命中时，Anthropic 出口要带
// cache_read_input_tokens（客户端据此展示缓存节省），没有命中则不应出现该字段。
func TestAnthropicSSECacheTokens(t *testing.T) {
	st := newAnthSSEState("glm-5.3")
	var sb strings.Builder
	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_MessageStart{
		MessageStart: &pb.MessageStart{Model: "glm-5.3"},
	}}))
	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
		MessageFinish: &pb.MessageFinish{
			FinishReason: "stop",
			Usage:        &pb.Usage{InputTokens: 100, OutputTokens: 5, CachedTokens: 80},
		},
	}}))
	out := sb.String()
	if !strings.Contains(out, `"cache_read_input_tokens":80`) {
		t.Errorf("命中缓存时缺少 cache_read_input_tokens:\n%s", out)
	}
	// 信封的 input 含缓存命中，Anthropic 客户端要的是不含的那部分：100-80=20
	if !strings.Contains(out, `"input_tokens":20`) {
		t.Errorf("input_tokens 未按 Anthropic 语义减去缓存命中:\n%s", out)
	}

	// 无命中：不输出该字段（0 值对 Anthropic 客户端无意义）
	st2 := newAnthSSEState("glm-5.3")
	var sb2 strings.Builder
	sb2.WriteString(st2.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_MessageStart{
		MessageStart: &pb.MessageStart{Model: "glm-5.3"},
	}}))
	sb2.WriteString(st2.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
		MessageFinish: &pb.MessageFinish{
			FinishReason: "stop",
			Usage:        &pb.Usage{InputTokens: 100, OutputTokens: 5},
		},
	}}))
	if strings.Contains(sb2.String(), "cache_read_input_tokens") {
		t.Errorf("未命中缓存时不应输出 cache_read_input_tokens:\n%s", sb2.String())
	}
}

// TestOpenAIAndResponsesUsagePayload 回归：缓存命中以明细字段透出，
// prompt_tokens / input_tokens 保持「含缓存」的总量，不再把命中算两遍。
func TestOpenAIAndResponsesUsagePayload(t *testing.T) {
	u := &pb.Usage{InputTokens: 100, OutputTokens: 5, CachedTokens: 80}

	oa := openAIUsagePayload(u)
	if oa["prompt_tokens"] != int64(100) || oa["total_tokens"] != int64(105) {
		t.Errorf("OpenAI usage 口径错误：%+v", oa)
	}
	details, ok := oa["prompt_tokens_details"].(map[string]interface{})
	if !ok || details["cached_tokens"] != int64(80) {
		t.Errorf("OpenAI usage 缺少 prompt_tokens_details.cached_tokens：%+v", oa)
	}

	rp := responsesUsagePayload(100, 5, 80)
	if rp["input_tokens"] != int64(100) || rp["total_tokens"] != int64(105) {
		t.Errorf("Responses usage 口径错误：%+v", rp)
	}
	rd, ok := rp["input_tokens_details"].(map[string]interface{})
	if !ok || rd["cached_tokens"] != int64(80) {
		t.Errorf("Responses usage 缺少 input_tokens_details.cached_tokens：%+v", rp)
	}

	// 无命中时不输出明细字段（避免客户端显示 0 命中）
	noCache := openAIUsagePayload(&pb.Usage{InputTokens: 10, OutputTokens: 1})
	if _, ok := noCache["prompt_tokens_details"]; ok {
		t.Errorf("无命中时不应输出 prompt_tokens_details：%+v", noCache)
	}
}
