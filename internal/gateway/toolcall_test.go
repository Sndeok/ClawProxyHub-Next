package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// ssePayloads 从 SSE 文本里抽出所有 data: 的 JSON 对象。
func ssePayloads(t *testing.T, out string) []map[string]interface{} {
	t.Helper()
	var out2 []map[string]interface{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if body == "" || body == "[DONE]" {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("bad SSE payload %q: %v", body, err)
		}
		out2 = append(out2, m)
	}
	return out2
}

// toolDelta 构造信封工具调用增量。
func toolDelta(id, name, args string) *pb.StreamEvent {
	return &pb.StreamEvent{Event: &pb.StreamEvent_ToolCallDelta{
		ToolCallDelta: &pb.ToolCallDelta{Id: id, Name: name, ArgumentsDelta: args},
	}}
}

// TestOpenAISSEGroupsIDLessToolDeltas 复刻 sdk/openaiup 旧版行为：
// 只有首块带 id，后续 arguments 增量 id 为空。编码器必须把它们归入同一个
// tool_calls[index]，而不是把增量丢到一个空 id 的新块里。
func TestOpenAISSEGroupsIDLessToolDeltas(t *testing.T) {
	enc := newEncoder("chat_completions", "m")
	out := enc.convertEvent(toolDelta("call_1", "fetch_url", ""))
	out += enc.convertEvent(toolDelta("", "", `{"url"`))
	out += enc.convertEvent(toolDelta("", "", `:"x"}`))

	var chunks []map[string]interface{}
	for _, p := range ssePayloads(t, out) {
		choices, _ := p["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		ch, _ := choices[0].(map[string]interface{})
		if tc, ok := ch["delta"].(map[string]interface{})["tool_calls"].([]interface{}); ok {
			for _, x := range tc {
				chunks = append(chunks, x.(map[string]interface{}))
			}
		}
	}
	if len(chunks) != 3 {
		t.Fatalf("want 3 tool_call chunks, got %d", len(chunks))
	}
	// 所有增量必须落在 index 0，且 id 只允许出现在首块
	for i, c := range chunks {
		if int(c["index"].(float64)) != 0 {
			t.Errorf("chunk %d: want index 0, got %v", i, c["index"])
		}
		if i == 0 {
			if c["id"] != "call_1" {
				t.Errorf("chunk 0: want id call_1, got %v", c["id"])
			}
			fn := c["function"].(map[string]interface{})
			if fn["name"] != "fetch_url" {
				t.Errorf("chunk 0: want name fetch_url, got %v", fn["name"])
			}
		} else if _, has := c["id"]; has {
			t.Errorf("chunk %d: continuation must not carry id, got %v", i, c["id"])
		}
	}
	// arguments 必须完整拼接
	var args string
	for _, c := range chunks {
		args += c["function"].(map[string]interface{})["arguments"].(string)
	}
	if args != `{"url":"x"}` {
		t.Errorf("arguments not concatenated: %q", args)
	}
}

// TestResponsesSSEEmitsFunctionCallDone Codex CLI 只认 output_item.done 里的
// function_call；缺失时工具不会被调度执行（复杂操作无回复）。
func TestResponsesSSEEmitsFunctionCallDone(t *testing.T) {
	enc := newEncoder("responses", "m")
	out := enc.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_MessageStart{
		MessageStart: &pb.MessageStart{Model: "m"},
	}})
	out += enc.convertEvent(toolDelta("call_1", "fetch_url", ""))
	out += enc.convertEvent(toolDelta("", "", `{"url":"x"}`))
	out += enc.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
		MessageFinish: &pb.MessageFinish{FinishReason: "tool_calls", Usage: &pb.Usage{}},
	}})

	var added, done []map[string]interface{}
	var completed map[string]interface{}
	for _, p := range ssePayloads(t, out) {
		typ, _ := p["type"].(string)
		switch typ {
		case "response.output_item.added":
			added = append(added, p)
		case "response.output_item.done":
			done = append(done, p)
		case "response.completed":
			completed = p
		}
	}
	if len(added) != 1 {
		t.Fatalf("want 1 output_item.added (no phantom block), got %d", len(added))
	}
	if len(done) != 1 {
		t.Fatalf("want 1 output_item.done for function_call, got %d", len(done))
	}
	item := done[0]["item"].(map[string]interface{})
	if item["type"] != "function_call" || item["call_id"] != "call_1" ||
		item["name"] != "fetch_url" || item["arguments"] != `{"url":"x"}` || item["status"] != "completed" {
		t.Errorf("function_call done item wrong: %+v", item)
	}
	if completed == nil {
		t.Fatal("missing response.completed")
	}
	resp := completed["response"].(map[string]interface{})
	output, _ := resp["output"].([]interface{})
	if len(output) != 1 {
		t.Errorf("response.completed.output must carry the function_call, got %+v", output)
	}
	if _, ok := resp["usage"]; !ok {
		t.Error("response.completed missing usage")
	}
}

// TestAnthropicSSEGroupsIDLessToolDeltas Anthropic 出口同样要归入同一个 content block。
func TestAnthropicSSEGroupsIDLessToolDeltas(t *testing.T) {
	enc := newEncoder("messages", "m")
	out := enc.convertEvent(toolDelta("tu_1", "fetch_url", ""))
	out += enc.convertEvent(toolDelta("", "", `{"url":"x"}`))
	out += enc.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
		MessageFinish: &pb.MessageFinish{FinishReason: "tool_calls"},
	}})

	var starts, deltas int
	for _, p := range ssePayloads(t, out) {
		switch p["type"] {
		case "content_block_start":
			cb := p["content_block"].(map[string]interface{})
			if cb["type"] == "tool_use" {
				starts++
				if cb["id"] != "tu_1" || cb["name"] != "fetch_url" {
					t.Errorf("tool_use block wrong: %+v", cb)
				}
			}
		case "content_block_delta":
			d := p["delta"].(map[string]interface{})
			if d["type"] == "input_json_delta" {
				deltas++
			}
		}
	}
	if starts != 1 {
		t.Errorf("want 1 tool_use content_block_start, got %d", starts)
	}
	if deltas != 1 {
		t.Errorf("want 1 input_json_delta, got %d", deltas)
	}
}

// TestNonStreamAggregatesToolCalls 非流式聚合同样按 id 归一。
func TestNonStreamAggregatesToolCalls(t *testing.T) {
	for _, protocol := range []string{"chat_completions", "responses", "messages"} {
		agg := newAggregate(protocol, "m")
		agg.feed(toolDelta("call_1", "fetch_url", ""))
		agg.feed(toolDelta("", "", `{"url":"x"}`))
		agg.feed(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
			MessageFinish: &pb.MessageFinish{FinishReason: "tool_calls"},
		}})
		b, _ := json.Marshal(agg.result())
		if !strings.Contains(string(b), `"call_1"`) {
			t.Errorf("%s: aggregated result lost the tool call id: %s", protocol, b)
		}
		// arguments 各协议形态不同（原始字符串 / 解析后的 JSON 对象），只校验未丢失
		if !strings.Contains(string(b), "url") || !strings.Contains(string(b), "x") {
			t.Errorf("%s: aggregated result lost the tool arguments: %s", protocol, b)
		}
	}
}
