package anthropicup

import (
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

func TestChatBody(t *testing.T) {
	req := &pb.ChatRequest{
		Model: "kimi-k3",
		Messages: []*pb.EnvelopeMessage{
			{Role: "system", Text: "sys"},
			{Role: "user", Text: "hi"},
			{Role: "assistant", Text: "calling", ToolCalls: []*pb.ToolCall{
				{Id: "tu_1", Name: "f", Arguments: `{"x":1}`},
			}},
			{Role: "tool", Text: "result", ToolCallId: "tu_1"},
		},
		Tools: []*pb.ToolDefinition{{Name: "f", ParametersSchema: `{"type":"object"}`}},
	}
	body := ChatBody(req)
	b := mustJSON(body)
	for _, want := range []string{
		`"system":"sys"`,
		`"text":"hi"`,
		`"type":"tool_use"`,
		`"type":"tool_result"`,
		`"input_schema"`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("body missing %q\n%s", want, b)
		}
	}
}

func TestParser(t *testing.T) {
	var out []*pb.StreamEvent
	p := NewParser(func(ev *pb.StreamEvent) { out = append(out, ev) })
	lines := []string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"model":"kimi-k3","usage":{"input_tokens":10}}}`,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}`,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu_1","name":"f","input":{}}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"x\":1}"}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
	}
	for _, l := range lines {
		p.Feed(l)
	}
	p.Finish()

	var hasStart, hasText, hasTool, hasFinish bool
	for _, ev := range out {
		switch e := ev.Event.(type) {
		case *pb.StreamEvent_MessageStart:
			hasStart = e.MessageStart.Model == "kimi-k3"
		case *pb.StreamEvent_ContentDelta:
			hasText = e.ContentDelta.Text == "你好"
		case *pb.StreamEvent_ToolCallDelta:
			hasTool = e.ToolCallDelta.Id == "tu_1" && e.ToolCallDelta.Name == "f"
		case *pb.StreamEvent_MessageFinish:
			hasFinish = e.MessageFinish.FinishReason == "tool_calls" &&
				e.MessageFinish.Usage.OutputTokens == 5
		}
	}
	if !hasStart || !hasText || !hasTool || !hasFinish {
		t.Errorf("events incomplete: start=%v text=%v tool=%v finish=%v", hasStart, hasText, hasTool, hasFinish)
	}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
func TestChatBodyUsesMultimodalContentJSON(t *testing.T) {
	req := &pb.ChatRequest{Messages: []*pb.EnvelopeMessage{{
		Role: "user", Text: "看图", ContentJson: []byte(`[{"type":"text","text":"看图"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]`),
	}}}
	body := ChatBody(req)
	messages := body["messages"].([]map[string]interface{})
	blocks := messages[0]["content"].([]interface{})
	if len(blocks) != 2 {
		t.Fatalf("expected two Anthropic content blocks, got %#v", blocks)
	}
	image := blocks[1].(map[string]interface{})
	if image["type"] != "image" {
		t.Fatalf("second block is not image: %#v", image)
	}
	source := image["source"].(map[string]interface{})
	if source["type"] != "base64" || source["media_type"] != "image/png" {
		t.Fatalf("image source not normalized: %#v", source)
	}
}

// TestParserCacheUsage 回归：message_start 里的缓存命中/写入必须合并进 message_finish 的 usage。
// 之前这里只取 message_delta 的 output_tokens，缓存与输入 token 全丢，日志里缓存命中恒为 0。
func TestParserCacheUsage(t *testing.T) {
	var out []*pb.StreamEvent
	p := NewParser(func(ev *pb.StreamEvent) { out = append(out, ev) })
	for _, l := range []string{
		`data: {"type":"message_start","message":{"model":"claude-x","usage":{"input_tokens":1200,"cache_creation_input_tokens":300,"cache_read_input_tokens":900}}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42}}`,
	} {
		p.Feed(l)
	}
	p.Finish()

	var fin *pb.StreamEvent_MessageFinish
	for _, ev := range out {
		if f, ok := ev.Event.(*pb.StreamEvent_MessageFinish); ok {
			fin = f
		}
	}
	if fin == nil {
		t.Fatalf("no message_finish: %+v", out)
	}
	u := fin.MessageFinish.Usage
	if u == nil {
		t.Fatal("usage 丢失")
	}
	// Anthropic 语义：input_tokens(1200) 不含缓存，完整输入 = 1200 + 300 + 900
	if u.InputTokens != 2400 {
		t.Errorf("input = %d, want 2400（含缓存命中与写入）", u.InputTokens)
	}
	if u.OutputTokens != 42 {
		t.Errorf("output = %d, want 42", u.OutputTokens)
	}
	if u.CachedTokens != 900 {
		t.Errorf("cached = %d, want 900（cache_read_input_tokens）", u.CachedTokens)
	}
	if fin.MessageFinish.FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", fin.MessageFinish.FinishReason)
	}
}

// TestParserCacheUsageFromDelta 少数上游只在 message_delta 里补一次输入侧用量。
func TestParserCacheUsageFromDelta(t *testing.T) {
	var out []*pb.StreamEvent
	p := NewParser(func(ev *pb.StreamEvent) { out = append(out, ev) })
	for _, l := range []string{
		`data: {"type":"message_start","message":{"model":"claude-x","usage":{"input_tokens":500}}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10,"input_tokens":500,"cache_read_input_tokens":400}}`,
	} {
		p.Feed(l)
	}
	p.Finish()

	var fin *pb.StreamEvent_MessageFinish
	for _, ev := range out {
		if f, ok := ev.Event.(*pb.StreamEvent_MessageFinish); ok {
			fin = f
		}
	}
	if fin == nil || fin.MessageFinish.Usage == nil {
		t.Fatalf("usage 丢失: %+v", out)
	}
	if got := fin.MessageFinish.Usage.CachedTokens; got != 400 {
		t.Errorf("cached = %d, want 400", got)
	}
}

// TestChatBodyTopKUser 回归：top_k 与 metadata.user_id 是 Anthropic 原生字段，必须透传。
func TestChatBodyTopKUser(t *testing.T) {
	req := &pb.ChatRequest{
		Model: "claude-x", Stream: true, MaxTokens: 64, Temperature: 1,
		Messages: []*pb.EnvelopeMessage{{Role: "user", Text: "hi"}},
		Extra:    map[string]string{"temperature": "0", "top_k": "40", "user": "u-123"},
	}
	body := ChatBody(req)
	b, _ := json.Marshal(body)
	s := string(b)
	for _, want := range []string{`"temperature":0`, `"top_k":40`, `"metadata":{"user_id":"u-123"}`} {
		if !strings.Contains(s, want) {
			t.Errorf("body missing %q:\n%s", want, s)
		}
	}
}
