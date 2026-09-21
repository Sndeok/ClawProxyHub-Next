package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

func TestParseResponsesRequest(t *testing.T) {
	body := `{
		"model": "gpt-5",
		"instructions": "你是助手",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "你好"}]},
			{"type": "function_call", "call_id": "call_1", "name": "f", "arguments": "{}"},
			{"type": "function_call_output", "call_id": "call_1", "output": "结果"}
		],
		"tools": [{"type": "function", "name": "f", "parameters": {"type": "object"}}],
		"max_output_tokens": 512,
		"stream": true
	}`
	req, err := parseResponsesRequest([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	// instructions(system) + user + function_call + function_call_output = 4
	if len(req.Messages) != 4 {
		t.Fatalf("want 4 messages, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Text != "你是助手" {
		t.Errorf("instructions wrong: %+v", req.Messages[0])
	}
	if req.Messages[2].Role != "assistant" || req.Messages[2].ToolCalls[0].Id != "call_1" {
		t.Errorf("function_call wrong: %+v", req.Messages[2])
	}
	if req.Messages[3].Role != "tool" || req.Messages[3].ToolCallId != "call_1" {
		t.Errorf("function_call_output wrong: %+v", req.Messages[3])
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "f" {
		t.Errorf("tools wrong: %+v", req.Tools)
	}
	if req.MaxTokens != 512 || !req.Stream {
		t.Errorf("basic fields wrong")
	}
}

// TestParseResponsesRequestCodex 复刻 Codex CLI 的实际请求：developer 角色 +
// input_text 内容块 + reasoning.effort，回归三处修复（文本不再被丢空 / 角色归一 / effort 透传）。
func TestParseResponsesRequestCodex(t *testing.T) {
	body := `{
		"model": "deepseek-flash",
		"instructions": "system prompt",
		"input": [
			{"type": "message", "role": "developer", "content": [{"type": "input_text", "text": "dev rule"}]},
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "今天是几号了"}]}
		],
		"reasoning": {"effort": "xhigh"},
		"stream": true
	}`
	req, err := parseResponsesRequest([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	// instructions(system) + developer(→system) + user = 3
	if len(req.Messages) != 3 {
		t.Fatalf("want 3 messages, got %d: %+v", len(req.Messages), req.Messages)
	}
	if req.Messages[1].Role != "system" || req.Messages[1].Text != "dev rule" {
		t.Errorf("developer 未归一或文本丢失: %+v", req.Messages[1])
	}
	if req.Messages[2].Role != "user" || req.Messages[2].Text != "今天是几号了" {
		t.Errorf("input_text 提取失败: %+v", req.Messages[2])
	}
	// v1.0.2 起不再透传 reasoning.effort：Codex 发 "xhigh" 这类私有值，
	// 上游不认会直接 500（Responses 本就没有顶层 reasoning_effort）。
	if _, ok := req.Extra["reasoning_effort"]; ok {
		t.Errorf("reasoning.effort 不应透传: %q", req.Extra["reasoning_effort"])
	}
	if _, ok := req.Extra["reasoning"]; ok {
		t.Errorf("reasoning 原始字段不应透传: %q", req.Extra["reasoning"])
	}
}

// TestParseResponsesParallelFunctionCalls 并行工具调用：相邻的 function_call 必须
// 合并进同一条 assistant 的 tool_calls，否则 tool 消息与声明它的 assistant 错位，
// 上游会拒绝整段历史（Codex 多工具并行时必现）。
func TestParseResponsesParallelFunctionCalls(t *testing.T) {
	body := `{
		"model": "m",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "并行查两个链接"}]},
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "我来并行查"}]},
			{"type": "function_call", "call_id": "call_a", "name": "fetch_url", "arguments": "{\"url\":\"a\"}"},
			{"type": "function_call", "call_id": "call_b", "name": "fetch_url", "arguments": ""},
			{"type": "function_call_output", "call_id": "call_a", "output": "A"},
			{"type": "function_call_output", "call_id": "call_b", "output": "B"}
		]
	}`
	req, err := parseResponsesRequest([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	// user + assistant(带 2 个 tool_call) + tool + tool = 4
	if len(req.Messages) != 4 {
		t.Fatalf("want 4 messages, got %d: %+v", len(req.Messages), req.Messages)
	}
	asst := req.Messages[1]
	if asst.Role != "assistant" || asst.Text != "我来并行查" {
		t.Errorf("assistant 文本未与 tool_calls 合并: %+v", asst)
	}
	if len(asst.ToolCalls) != 2 {
		t.Fatalf("两个并行调用应合并进同一条 assistant，got %d: %+v", len(asst.ToolCalls), asst.ToolCalls)
	}
	if asst.ToolCalls[0].Id != "call_a" || asst.ToolCalls[1].Id != "call_b" {
		t.Errorf("tool_call 顺序或 id 不对: %+v", asst.ToolCalls)
	}
	if asst.ToolCalls[1].Arguments != "{}" {
		t.Errorf("空 arguments 应补成 {}，got %q", asst.ToolCalls[1].Arguments)
	}
	if req.Messages[2].Role != "tool" || req.Messages[2].ToolCallId != "call_a" {
		t.Errorf("tool 消息错位: %+v", req.Messages[2])
	}
	if req.Messages[3].Role != "tool" || req.Messages[3].ToolCallId != "call_b" {
		t.Errorf("tool 消息错位: %+v", req.Messages[3])
	}
}

func TestParseResponsesRequestStringInput(t *testing.T) {
	req, err := parseResponsesRequest([]byte(`{"model":"m","input":"纯文本输入"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Text != "纯文本输入" {
		t.Errorf("string input wrong: %+v", req.Messages)
	}
}

func TestResponsesSSE(t *testing.T) {
	st := newResponsesSSEState("gpt-5")
	var sb strings.Builder
	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_MessageStart{
		MessageStart: &pb.MessageStart{Model: "gpt-5"},
	}}))
	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_ContentDelta{
		ContentDelta: &pb.ContentDelta{Text: "hello"},
	}}))
	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_ToolCallDelta{
		ToolCallDelta: &pb.ToolCallDelta{Id: "call_1", Name: "f", ArgumentsDelta: `{"x":1}`},
	}}))
	sb.WriteString(st.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
		MessageFinish: &pb.MessageFinish{FinishReason: "tool_calls",
			Usage: &pb.Usage{InputTokens: 3, OutputTokens: 4}},
	}}))
	out := sb.String()
	for _, want := range []string{
		"event: response.created",
		"event: response.output_item.added",
		"event: response.output_text.delta",
		`"delta":"hello"`,
		`"type":"function_call"`,
		"event: response.function_call_arguments.delta",
		"event: response.output_item.done",
		"event: response.completed",
		`"input_tokens":3`,
		`"output_tokens":4`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("responses sse missing %q\n%s", want, out)
		}
	}
}

func TestResponsesAggregate(t *testing.T) {
	a := &responsesAggregate{}
	a.feed(&pb.StreamEvent{Event: &pb.StreamEvent_ContentDelta{ContentDelta: &pb.ContentDelta{Text: "a"}}})
	a.feed(&pb.StreamEvent{Event: &pb.StreamEvent_ContentDelta{ContentDelta: &pb.ContentDelta{Text: "b"}}})
	a.feed(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
		MessageFinish: &pb.MessageFinish{Usage: &pb.Usage{InputTokens: 1, OutputTokens: 2}},
	}})
	res := a.result()
	b, _ := json.Marshal(res)
	if !strings.Contains(string(b), `"text":"ab"`) {
		t.Errorf("aggregate text wrong: %s", b)
	}
	if !strings.Contains(string(b), `"total_tokens":3`) {
		t.Errorf("aggregate usage wrong: %s", b)
	}
}

// ---------- 宽松解析回归（此前任何一种形状都会让整条请求 400）----------

// TestParseResponsesInputShapesTolerant 覆盖各客户端对 input 字段的类型差异。
// 事故背景：Codex（经 CC Switch / new-api）在工具调用后的历史里，
// function_call_output.output 是内容块数组而非字符串，旧实现用固定 string
// 反序列化整个数组，直接 400「input must be string or message array」。
func TestParseResponsesInputShapesTolerant(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantMsgs  int
		checkFunc func(t *testing.T, req *pb.ChatRequest)
	}{
		{
			name: "output 为内容块数组（CC Switch / Codex 实际形状）",
			body: `{"model":"m","input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"查一下"}]},
				{"type":"function_call","call_id":"c1","name":"fetch_url","arguments":"{\"url\":\"https://a\"}"},
				{"type":"function_call_output","call_id":"c1","output":[{"type":"input_text","text":"页面内容"}]}
			]}`,
			wantMsgs: 3,
			checkFunc: func(t *testing.T, req *pb.ChatRequest) {
				if req.Messages[2].Role != "tool" || req.Messages[2].Text != "页面内容" {
					t.Errorf("内容块数组未提取为工具结果: %+v", req.Messages[2])
				}
			},
		},
		{
			name: "arguments 是对象而非字符串",
			body: `{"model":"m","input":[
				{"type":"function_call","call_id":"c1","name":"f","arguments":{"url":"https://a"}}
			]}`,
			wantMsgs: 1,
			checkFunc: func(t *testing.T, req *pb.ChatRequest) {
				if got := req.Messages[0].ToolCalls[0].Arguments; got != `{"url":"https://a"}` {
					t.Errorf("对象型 arguments 未归一为 JSON 文本: %q", got)
				}
			},
		},
		{
			name:     "input 是单个对象",
			body:     `{"model":"m","input":{"type":"message","role":"user","content":"你好"}}`,
			wantMsgs: 1,
		},
		{
			name:     "数组里混入裸字符串元素",
			body:     `{"model":"m","input":["直接一段话",{"type":"message","role":"user","content":"第二句"}]}`,
			wantMsgs: 2,
			checkFunc: func(t *testing.T, req *pb.ChatRequest) {
				if req.Messages[0].Role != "user" || req.Messages[0].Text != "直接一段话" {
					t.Errorf("裸字符串元素应成为 user 消息: %+v", req.Messages[0])
				}
			},
		},
		{
			name: "custom_tool_call（apply_patch 补丁文本放在 input）",
			body: `{"model":"m","input":[
				{"type":"custom_tool_call","call_id":"c9","name":"apply_patch","input":"*** Begin Patch\n*** End Patch"},
				{"type":"custom_tool_call_output","call_id":"c9","output":"Done!"}
			]}`,
			wantMsgs: 2,
			checkFunc: func(t *testing.T, req *pb.ChatRequest) {
				tc := req.Messages[0].ToolCalls[0]
				if tc.Name != "apply_patch" || !json.Valid([]byte(tc.Arguments)) {
					t.Errorf("补丁文本应编码为合法 JSON 字符串: %+v", tc)
				}
				if req.Messages[1].Role != "tool" || req.Messages[1].ToolCallId != "c9" {
					t.Errorf("custom_tool_call_output 未转为 tool 消息: %+v", req.Messages[1])
				}
			},
		},
		{
			name:     "reasoning / web_search_call 元素应被跳过而不是报错",
			body:     `{"model":"m","input":[{"type":"reasoning","summary":[]},{"type":"web_search_call","id":"ws1"},{"type":"message","role":"user","content":"hi"}]}`,
			wantMsgs: 1,
		},
		{
			name:     "input 为 null 不报错",
			body:     `{"model":"m","input":null}`,
			wantMsgs: 0,
		},
		{
			name:     "缺 instructions 之外的 input 字段",
			body:     `{"model":"m","instructions":"sys"}`,
			wantMsgs: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := parseResponsesRequest([]byte(c.body))
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if len(req.Messages) != c.wantMsgs {
				t.Fatalf("want %d messages, got %d: %+v", c.wantMsgs, len(req.Messages), req.Messages)
			}
			if c.checkFunc != nil {
				c.checkFunc(t, req)
			}
		})
	}
}

// TestParseResponsesInputTrulyInvalid 真正无法处理的 input 才报错，且错误信息
// 要带上实际收到的 JSON 类型，便于直接定位。
func TestParseResponsesInputTrulyInvalid(t *testing.T) {
	_, err := parseResponsesRequest([]byte(`{"model":"m","input":123}`))
	if err == nil {
		t.Fatal("标量 input 应当报错")
	}
	if !strings.Contains(err.Error(), "number") {
		t.Errorf("错误信息应说明实际类型: %v", err)
	}
}