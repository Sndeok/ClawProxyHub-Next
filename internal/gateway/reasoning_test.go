package gateway

import (
	"strings"
	"testing"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

func reasoningEv(text string) *pb.StreamEvent {
	return &pb.StreamEvent{Event: &pb.StreamEvent_ContentDelta{
		ContentDelta: &pb.ContentDelta{Text: text, Reasoning: true},
	}}
}

func contentEv(text string) *pb.StreamEvent {
	return &pb.StreamEvent{Event: &pb.StreamEvent_ContentDelta{
		ContentDelta: &pb.ContentDelta{Text: text},
	}}
}

// OpenAI 兼容入口：思考走 reasoning_content，正文走 content，两者不混。
// 回归背景：思考增量以前被整段丢弃，客户端在模型思考期间完全静默。
func TestOpenAIEncoderReasoning(t *testing.T) {
	enc := newEncoder("chat_completions", "m")
	out := enc.convertEvent(reasoningEv("想法"))
	if !strings.Contains(out, `"reasoning_content":"想法"`) {
		t.Fatalf("思考未按 reasoning_content 下发: %s", out)
	}
	if strings.Contains(out, `"content":"想法"`) {
		t.Fatalf("思考被当成正文下发: %s", out)
	}
	out = enc.convertEvent(contentEv("正文"))
	if !strings.Contains(out, `"content":"正文"`) {
		t.Fatalf("正文未按 content 下发: %s", out)
	}
}

// 非流式聚合：reasoning_content 与 content 分开返回。
func TestOpenAIAggregateReasoning(t *testing.T) {
	a := &openaiAggregate{}
	a.model = "m"
	a.feed(reasoningEv("想法"))
	a.feed(contentEv("答案"))
	res := a.result()
	choices, _ := res["choices"].([]interface{})
	if len(choices) != 1 {
		t.Fatalf("choices 异常: %v", res["choices"])
	}
	msg, _ := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	if msg["content"] != "答案" {
		t.Errorf("content = %v, want 答案", msg["content"])
	}
	if msg["reasoning_content"] != "想法" {
		t.Errorf("reasoning_content = %v, want 想法", msg["reasoning_content"])
	}
}

// Anthropic 入口：思考作为 thinking block 下发，正文前先关掉思考块。
func TestAnthEncoderReasoningBlock(t *testing.T) {
	s := newAnthSSEState("m")
	out := s.convertEvent(reasoningEv("想法"))
	if !strings.Contains(out, `"type":"thinking"`) || !strings.Contains(out, `"type":"thinking_delta"`) {
		t.Fatalf("思考未按 thinking block 下发: %s", out)
	}
	out = s.convertEvent(contentEv("正文"))
	if !strings.Contains(out, `"type":"content_block_stop"`) || !strings.Contains(out, `"type":"text_delta"`) {
		t.Fatalf("正文前未关闭思考块: %s", out)
	}
}

// Responses（Codex）入口：思考作为 reasoning item 的 summary 增量下发，收尾时给 done。
func TestResponsesEncoderReasoningItem(t *testing.T) {
	s := newResponsesSSEState("m")
	out := s.convertEvent(reasoningEv("想法"))
	if !strings.Contains(out, "response.reasoning_summary_text.delta") || !strings.Contains(out, `"type":"reasoning"`) {
		t.Fatalf("思考未按 reasoning item 下发: %s", out)
	}
	out = s.convertEvent(contentEv("正文"))
	if !strings.Contains(out, "response.output_text.delta") {
		t.Fatalf("正文未按 output_text.delta 下发: %s", out)
	}
	out = s.convertEvent(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
		MessageFinish: &pb.MessageFinish{FinishReason: "stop"},
	}})
	if !strings.Contains(out, "response.reasoning_summary_text.done") ||
		!strings.Contains(out, "response.reasoning_summary_part.done") {
		t.Fatalf("reasoning item 未收尾: %s", out)
	}
}
