package openaiup

import (
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

func collect(lines []string) []*pb.StreamEvent {
	var out []*pb.StreamEvent
	p := NewParser(func(ev *pb.StreamEvent) { out = append(out, ev) })
	for _, l := range lines {
		p.Feed(l)
	}
	p.Finish()
	return out
}

func TestChatBody(t *testing.T) {
	req := &pb.ChatRequest{
		Model: "kimi-k3", Stream: true,
		Messages: []*pb.EnvelopeMessage{
			{Role: "system", Text: "sys"},
			{Role: "assistant", Text: "", ToolCalls: []*pb.ToolCall{
				{Id: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`},
			}},
			{Role: "tool", Text: "晴", ToolCallId: "call_1"},
		},
		Tools: []*pb.ToolDefinition{{
			Name: "get_weather", Description: "查天气",
			ParametersSchema: `{"type":"object"}`,
		}},
		ToolChoice: &pb.ToolChoice{Type: "auto"},
		MaxTokens:  100,
		Extra:      map[string]string{"top_p": "0.9", "stop": `["end"]`},
	}
	body := ChatBody(req)
	b, _ := json.Marshal(body)
	s := string(b)

	for _, want := range []string{
		`"stream":true`, `"include_usage":true`,
		`"role":"system"`, `"role":"assistant"`, `"tool_call_id":"call_1"`,
		`"get_weather"`, `"tool_choice":"auto"`, `"max_tokens":100`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("body missing %q\n%s", want, s)
		}
	}
}

func TestParserTextAndUsage(t *testing.T) {
	events := collect([]string{
		`data: {"choices":[{"delta":{"role":"assistant","content":"你"}}]}`,
		`data: {"choices":[{"delta":{"content":"好"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		`data: [DONE]`,
	})
	if len(events) != 3 {
		t.Fatalf("want 3 events, got %d: %+v", len(events), events)
	}
	d1, ok := events[0].Event.(*pb.StreamEvent_ContentDelta)
	if !ok || d1.ContentDelta.Text != "你" {
		t.Errorf("first delta wrong: %+v", events[0])
	}
	fin, ok := events[2].Event.(*pb.StreamEvent_MessageFinish)
	if !ok || fin.MessageFinish.FinishReason != "stop" ||
		fin.MessageFinish.Usage.InputTokens != 5 || fin.MessageFinish.Usage.OutputTokens != 2 {
		t.Errorf("finish wrong: %+v", events[2])
	}
}

func TestParserToolCalls(t *testing.T) {
	events := collect([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"f","arguments":"{}"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"more\""}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
	})
	var toolEvents []*pb.ToolCallDelta
	var finish *pb.MessageFinish
	for _, ev := range events {
		switch e := ev.Event.(type) {
		case *pb.StreamEvent_ToolCallDelta:
			toolEvents = append(toolEvents, e.ToolCallDelta)
		case *pb.StreamEvent_MessageFinish:
			finish = e.MessageFinish
		}
	}
	if len(toolEvents) != 2 {
		t.Fatalf("want 2 tool events, got %d", len(toolEvents))
	}
	if toolEvents[0].Name != "f" || toolEvents[0].Id != "call_1" {
		t.Errorf("first tool event wrong: %+v", toolEvents[0])
	}
	if toolEvents[1].ArgumentsDelta != `"more"` {
		t.Errorf("second tool delta wrong: %+v", toolEvents[1])
		// 后续 arguments 增量必须补齐同一个 id/name：核心按 id 分组，空 id 会被
		// 当成新调用开新块，客户端最终拿到残缺的 tool_calls（工具不执行）。
		if toolEvents[1].Id != "call_1" || toolEvents[1].Name != "f" {
			t.Errorf("continuation delta must keep id/name, got %+v", toolEvents[1])
		}
	}
	if finish == nil || finish.FinishReason != "tool_calls" {
		t.Errorf("finish wrong: %+v", finish)
	}
}

func TestParserEmptyStreamFallback(t *testing.T) {
	// 上游空流 / 只有 [DONE]：Finish 补一个 stop
	events := collect([]string{`data: [DONE]`})
	if len(events) != 1 {
		t.Fatalf("want 1 fallback event, got %d", len(events))
	}
	fin, ok := events[0].Event.(*pb.StreamEvent_MessageFinish)
	if !ok || fin.MessageFinish.FinishReason != "stop" {
		t.Errorf("fallback finish wrong: %+v", events[0])
	}
}
func TestChatBodyUsesMultimodalContentJSON(t *testing.T) {
	req := &pb.ChatRequest{Messages: []*pb.EnvelopeMessage{{
		Role: "user", Text: "看图", ContentJson: []byte(`[{"type":"text","text":"看图"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]`),
	}}}
	body := ChatBody(req)
	messages := body["messages"].([]map[string]interface{})
	content, ok := messages[0]["content"].([]interface{})
	if !ok || len(content) != 2 {
		t.Fatalf("expected two multimodal content parts, got %#v", messages[0]["content"])
	}
	if content[1].(map[string]interface{})["type"] != "image_url" {
		t.Fatalf("second part is not image_url: %#v", content[1])
	}
}

// TestParserCachedAndCreditUsage 回归：缓存命中字段（各家命名不一）与积分消耗必须落到 usage。
func TestParserCachedAndCreditUsage(t *testing.T) {
	cases := []struct {
		name       string
		line       string
		wantCached int64
		wantCredit float64
	}{
		{
			name:       "anthropic 风格 input_tokens_details",
			line:       `data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":5,"input_tokens_details":{"cached_tokens":80}}}`,
			wantCached: 80,
		},
		{
			name:       "顶层 cached_tokens + credits_used",
			line:       `data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":5,"cached_tokens":60,"credits_used":1.25}}`,
			wantCached: 60, wantCredit: 1.25,
		},
		{
			// 两个字段是同一份命中的不同叫法，取最大而不是相加
			name:       "prompt_tokens_details 别名去重",
			line:       `data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":30,"cache_read_input_tokens":20}}}`,
			wantCached: 30,
		},
		{
			name:       "顶层与 details 同值时不去重相加",
			line:       `data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":5,"cached_tokens":80,"prompt_tokens_details":{"cached_tokens":80}}}`,
			wantCached: 80,
		},
		{
			name:       "命中量大于输入时按输入截断",
			line:       `data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":5,"cached_tokens":500}}`,
			wantCached: 100,
		},
		{
			name:       "无缓存字段时为 0",
			line:       `data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1}}`,
			wantCached: 0, wantCredit: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := collect([]string{tc.line})
			var fin *pb.StreamEvent_MessageFinish
			for _, ev := range events {
				if f, ok := ev.Event.(*pb.StreamEvent_MessageFinish); ok {
					fin = f
				}
			}
			if fin == nil {
				t.Fatalf("no message_finish event: %+v", events)
			}
			u := fin.MessageFinish.Usage
			if u == nil {
				t.Fatal("usage 丢失")
			}
			if u.CachedTokens != tc.wantCached {
				t.Errorf("cached = %d, want %d", u.CachedTokens, tc.wantCached)
			}
			if u.CreditUsed != tc.wantCredit {
				t.Errorf("credit = %v, want %v", u.CreditUsed, tc.wantCredit)
			}
		})
	}
}

// TestChatBodyExtraPassthrough 回归：核心解析出的采样/控制参数必须落到上游请求体，
// 否则 new-api 传来的 seed / penalties / response_format 会被静默吞掉。
func TestChatBodyExtraPassthrough(t *testing.T) {
	req := &pb.ChatRequest{
		Model: "kimi-k3", Stream: true, MaxTokens: 128, Temperature: 0.7,
		Messages: []*pb.EnvelopeMessage{{Role: "user", Text: "hi"}},
		Extra: map[string]string{
			"temperature":         "0",
			"reasoning_effort":    "high",
			"frequency_penalty":   "0.5",
			"presence_penalty":    "-0.25",
			"seed":                "42",
			"parallel_tool_calls": "false",
			"response_format":     `{"type":"json_object"}`,
			"user":                "kylin",
		},
	}
	body := ChatBody(req)
	b, _ := json.Marshal(body)
	s := string(b)
	for _, want := range []string{
		`"temperature":0`, // Extra 覆盖信封浮点字段（显式 0 不被吞）
		`"reasoning_effort":"high"`,
		`"frequency_penalty":0.5`,
		`"presence_penalty":-0.25`,
		`"seed":42`,
		`"parallel_tool_calls":false`,
		`"response_format":{"type":"json_object"}`,
		`"user":"kylin"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("body missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"temperature":0.7`) {
		t.Errorf("显式 temperature 未覆盖信封字段:\n%s", s)
	}
}
