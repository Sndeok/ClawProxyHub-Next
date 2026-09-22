package responsesup

import (
	"encoding/json"
	"testing"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// TestParserHappyPath created → 文本增量 → 结束（含 usage 与缓存命中）。
func TestParserHappyPath(t *testing.T) {
	var events []*pb.StreamEvent
	p := NewParser(func(ev *pb.StreamEvent) { events = append(events, ev) })

	lines := []string{
		`event: response.created`,
		`data: {"type":"response.created","response":{"model":"gpt-5.6-sol"}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"你"}`,
		`data: {"type":"response.output_text.delta","delta":"好"}`,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":100,"output_tokens":7,"input_tokens_details":{"cached_tokens":80}}}}`,
		"data: [DONE]",
	}
	for _, l := range lines {
		p.Feed(l)
	}

	var sawStart, sawFinish bool
	var text string
	var usage *pb.Usage
	for _, ev := range events {
		switch e := ev.Event.(type) {
		case *pb.StreamEvent_MessageStart:
			sawStart = true
			if e.MessageStart.Model != "gpt-5.6-sol" {
				t.Errorf("MessageStart.Model = %q", e.MessageStart.Model)
			}
		case *pb.StreamEvent_ContentDelta:
			text += e.ContentDelta.Text
		case *pb.StreamEvent_MessageFinish:
			sawFinish = true
			usage = e.MessageFinish.Usage
		}
	}
	if !sawStart || !sawFinish {
		t.Fatalf("缺少首/尾事件: start=%v finish=%v", sawStart, sawFinish)
	}
	if text != "你好" {
		t.Errorf("文本增量 = %q", text)
	}
	if usage == nil || usage.InputTokens != 100 || usage.OutputTokens != 7 || usage.CachedTokens != 80 {
		t.Fatalf("usage 映射错误: %+v", usage)
	}
}

// TestParserToolCall 工具调用：首帧带 id/name，后续帧只带参数增量。
func TestParserToolCall(t *testing.T) {
	var events []*pb.StreamEvent
	p := NewParser(func(ev *pb.StreamEvent) { events = append(events, ev) })
	for _, l := range []string{
		`data: {"type":"response.created","response":{"model":"m"}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_abc","name":"Read"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"path\":"}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"a.go\"}"}`,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":3}}}`,
	} {
		p.Feed(l)
	}
	var id, name, args string
	for _, ev := range events {
		if e, ok := ev.Event.(*pb.StreamEvent_ToolCallDelta); ok {
			if e.ToolCallDelta.Id != "" {
				id = e.ToolCallDelta.Id
			}
			if e.ToolCallDelta.Name != "" {
				name = e.ToolCallDelta.Name
			}
			args += e.ToolCallDelta.ArgumentsDelta
		}
	}
	if id != "call_abc" || name != "Read" {
		t.Errorf("工具身份丢失: id=%q name=%q", id, name)
	}
	if args != `{"path":"a.go"}` {
		t.Errorf("参数增量拼接错误: %q", args)
	}
}

// TestParserFailure 失败事件映射为 TaskFailed。
func TestParserFailure(t *testing.T) {
	var events []*pb.StreamEvent
	p := NewParser(func(ev *pb.StreamEvent) { events = append(events, ev) })
	p.Feed(`data: {"type":"response.failed","response":{"error":{"message":"model overloaded","code":"overloaded"}}}`)
	if len(events) != 1 {
		t.Fatalf("事件数 = %d", len(events))
	}
	failed, ok := events[0].Event.(*pb.StreamEvent_TaskFailed)
	if !ok {
		t.Fatalf("want TaskFailed, got %T", events[0].Event)
	}
	if failed.TaskFailed.Error.Message != "model overloaded" || failed.TaskFailed.Error.Code != 502 {
		t.Errorf("失败映射错误: %+v", failed.TaskFailed.Error)
	}
}

// TestParserFinishFallback 上游没发 completed 时兜底收尾（不带 usage）。
func TestParserFinishFallback(t *testing.T) {
	var events []*pb.StreamEvent
	p := NewParser(func(ev *pb.StreamEvent) { events = append(events, ev) })
	p.Feed(`data: {"type":"response.created","response":{"model":"m"}}`)
	p.Feed(`data: {"type":"response.output_text.delta","delta":"hi"}`)
	p.Finish()
	last := events[len(events)-1]
	fin, ok := last.Event.(*pb.StreamEvent_MessageFinish)
	if !ok || fin.MessageFinish.FinishReason != "stop" {
		t.Fatalf("兜底收尾错误: %+v", last.Event)
	}
	// 重复 Finish 不应重复上报
	n := len(events)
	p.Finish()
	if len(events) != n {
		t.Errorf("重复 Finish 产生了额外事件")
	}
}

// TestChatBodyShape CPH 请求 → Responses 请求体的结构映射。
func TestChatBodyShape(t *testing.T) {
	req := &pb.ChatRequest{
		Model: "gpt-5.6-sol",
		Messages: []*pb.EnvelopeMessage{
			{Role: "system", Text: "你是助手"},
			{Role: "user", Text: "看这张图", ContentJson: []byte(`[{"type":"text","text":"看这张图"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]`)},
			{Role: "assistant", ToolCalls: []*pb.ToolCall{{Id: "call_1", Name: "Read", Arguments: `{"path":"a.go"}`}}},
			{Role: "tool", Text: "文件内容", ToolCallId: "call_1"},
		},
		Tools:      []*pb.ToolDefinition{{Name: "Read", Description: "读文件", ParametersSchema: `{"type":"object"}`}},
		ToolChoice: &pb.ToolChoice{Type: "tool", ToolName: "Read"},
		MaxTokens:  4096,
	}
	raw, err := json.Marshal(ChatBody(req))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["instructions"] != "你是助手" {
		t.Errorf("system 应转成 instructions: %v", body["instructions"])
	}
	if body["stream"] != true {
		t.Error("Responses 上游必须 stream=true")
	}
	if body["max_output_tokens"] != float64(4096) {
		t.Errorf("max_tokens 应映射为 max_output_tokens: %v", body["max_output_tokens"])
	}
	items, _ := body["input"].([]interface{})
	// 4 条 CPH 消息 → 3 个 input 项：
	//   system 走 instructions（不进 input）
	//   assistant 只有 tool_calls（无文本）→ 只产出一个 function_call 项，不额外产生 message 项
	if len(items) != 3 {
		t.Fatalf("input 项数 = %d, want 3: %v", len(items), items)
	}
	first, _ := items[0].(map[string]interface{})
	if first["role"] != "user" {
		t.Errorf("首项应为 user: %v", first)
	}
	if blocks, _ := first["content"].([]interface{}); len(blocks) != 2 {
		t.Errorf("多模态块丢失: %v", first["content"])
	} else if b1, _ := blocks[1].(map[string]interface{}); b1["type"] != "input_image" {
		t.Errorf("图片块应转成 input_image: %v", blocks[1])
	}
	fc, _ := items[1].(map[string]interface{})
	if fc["type"] != "function_call" || fc["call_id"] != "call_1" || fc["name"] != "Read" {
		t.Errorf("function_call 项错误: %v", fc)
	}
	fco, _ := items[2].(map[string]interface{})
	if fco["type"] != "function_call_output" || fco["call_id"] != "call_1" || fco["output"] != "文件内容" {
		t.Errorf("function_call_output 项错误: %v", fco)
	}
	// tools 是扁平结构（Responses 方言，不嵌套 function）
	tools, _ := body["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("tools 丢失: %v", body["tools"])
	}
	tool, _ := tools[0].(map[string]interface{})
	if tool["type"] != "function" || tool["name"] != "Read" {
		t.Errorf("tools 结构错误（Responses 要求扁平）: %v", tool)
	}
	if _, nested := tool["function"]; nested {
		t.Error("tools 不应嵌套 function（那是 Chat Completions 方言）")
	}
	if tc, _ := body["tool_choice"].(map[string]interface{}); tc["name"] != "Read" {
		t.Errorf("tool_choice 映射错误: %v", body["tool_choice"])
	}
}

// TestChatBodyToolResultBlocks tool 消息的多模态结果（ContentJson）也要能取到文本。
func TestChatBodyToolResultBlocks(t *testing.T) {
	req := &pb.ChatRequest{
		Model: "m",
		Messages: []*pb.EnvelopeMessage{
			{Role: "tool", ToolCallId: "c1", ContentJson: []byte(`[{"type":"text","text":"第一段"},{"type":"text","text":"第二段"}]`)},
		},
	}
	body := ChatBody(req)
	items, _ := body["input"].([]interface{})
	out, _ := items[0].(map[string]interface{})
	if out["output"] != "第一段第二段" {
		t.Errorf("工具结果文本拼接错误: %v", out["output"])
	}
}

// TestChatBodyReasoningEffort Responses 方言只认 reasoning.effort：
// 顶层 reasoning_effort 会被上游忽略（Codex 的 xhigh 这类私有值甚至会 500）。
func TestChatBodyReasoningEffort(t *testing.T) {
	req := &pb.ChatRequest{
		Model: "gpt-5.6-sol",
		Extra: map[string]string{"reasoning_effort": "high", "top_p": "0.9", "parallel_tool_calls": "true"},
	}
	body := ChatBody(req)
	reasoning, ok := body["reasoning"].(map[string]interface{})
	if !ok || reasoning["effort"] != "high" {
		t.Fatalf("reasoning_effort 未映射成 reasoning.effort: %v", body["reasoning"])
	}
	if _, hasTop := body["reasoning_effort"]; hasTop {
		t.Error("不应下发顶层 reasoning_effort")
	}
	if body["top_p"] != 0.9 {
		t.Errorf("top_p 应按数字下发: %#v", body["top_p"])
	}
	if body["parallel_tool_calls"] != true {
		t.Errorf("parallel_tool_calls 应按布尔下发: %#v", body["parallel_tool_calls"])
	}
}

// TestParserIncompleteIsLength 被 max_output_tokens 截断是正常结束（length），不是失败。
func TestParserIncompleteIsLength(t *testing.T) {
	var events []*pb.StreamEvent
	p := NewParser(func(ev *pb.StreamEvent) { events = append(events, ev) })
	p.Feed(`data: {"type":"response.created","response":{"model":"m"}}`)
	p.Feed(`data: {"type":"response.output_text.delta","delta":"半截内容"}`)
	p.Feed(`data: {"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":10,"output_tokens":5}}}`)

	var finish *pb.MessageFinish
	for _, ev := range events {
		if f, ok := ev.Event.(*pb.StreamEvent_MessageFinish); ok {
			finish = f.MessageFinish
		}
		if _, bad := ev.Event.(*pb.StreamEvent_TaskFailed); bad {
			t.Fatal("截断不应上报为失败事件")
		}
	}
	if finish == nil {
		t.Fatal("缺少结束事件")
	}
	if finish.FinishReason != "length" {
		t.Errorf("finish_reason = %q, want length", finish.FinishReason)
	}
	if finish.Usage == nil || finish.Usage.InputTokens != 10 || finish.Usage.OutputTokens != 5 {
		t.Errorf("断流帧的 usage 未带出: %+v", finish.Usage)
	}
}

// TestParserToolArgsDoneFallback 上游只在 done 给全量参数时必须补发一次，
// 否则工具调用会带着空 arguments 到客户端（工具直接执行失败）。
func TestParserToolArgsDoneFallback(t *testing.T) {
	var args string
	p := NewParser(func(ev *pb.StreamEvent) {
		if d, ok := ev.Event.(*pb.StreamEvent_ToolCallDelta); ok {
			args += d.ToolCallDelta.ArgumentsDelta
		}
	})
	p.Feed(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"Read"}}`)
	p.Feed(`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"Read","arguments":"{\"path\":\"a.go\"}"}}`)
	p.Feed(`data: {"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":1}}}`)
	if args != `{"path":"a.go"}` {
		t.Fatalf("done 里的全量参数未补发: %q", args)
	}
}

// TestParserToolArgsNoDuplicate 走过增量时，done 的全量参数不得重复下发。
func TestParserToolArgsNoDuplicate(t *testing.T) {
	var args string
	p := NewParser(func(ev *pb.StreamEvent) {
		if d, ok := ev.Event.(*pb.StreamEvent_ToolCallDelta); ok {
			args += d.ToolCallDelta.ArgumentsDelta
		}
	})
	p.Feed(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"Read"}}`)
	p.Feed(`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"path\":"}`)
	p.Feed(`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"\"a.go\"}"}`)
	p.Feed(`data: {"type":"response.function_call_arguments.done","item_id":"fc_1","arguments":"{\"path\":\"a.go\"}"}`)
	p.Feed(`data: {"type":"response.output_item.done","item":{"type":"function_call","id":"fc_1","arguments":"{\"path\":\"a.go\"}"}}`)
	if args != `{"path":"a.go"}` {
		t.Fatalf("参数被重复下发: %q", args)
	}
}

// TestParserFinishReasonToolCalls 没给结束原因但有工具调用 → tool_calls（不是 stop）。
func TestParserFinishReasonToolCalls(t *testing.T) {
	var reason string
	p := NewParser(func(ev *pb.StreamEvent) {
		if f, ok := ev.Event.(*pb.StreamEvent_MessageFinish); ok {
			reason = f.MessageFinish.FinishReason
		}
	})
	p.Feed(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"c1","name":"Read"}}`)
	p.Finish()
	if reason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", reason)
	}
}

// TestParserNoDoubleTerminal 收尾之后再来的错误事件必须被忽略，
// 否则同一个流会先给 MessageFinish 再给 TaskFailed，核心会当成失败重试。
func TestParserNoDoubleTerminal(t *testing.T) {
	var terminals int
	p := NewParser(func(ev *pb.StreamEvent) {
		switch ev.Event.(type) {
		case *pb.StreamEvent_MessageFinish, *pb.StreamEvent_TaskFailed:
			terminals++
		}
	})
	p.Feed(`data: {"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":1}}}`)
	p.FinishWithError(502, "迟到的错误")
	p.Finish()
	if terminals != 1 {
		t.Fatalf("终止事件出现 %d 次，want 1", terminals)
	}
}
