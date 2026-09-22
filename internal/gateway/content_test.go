package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

func TestNormalizeResponsesContentMultimodal(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"input_text","text":"看这张图"},
		{"type":"input_image","image_url":"data:image/png;base64,AA=="},
		{"type":"input_file","file":{"file_data":"data:text/plain;base64,SGk="}}
	]`)
	got := normalizeResponsesContent(raw)
	if len(got) == 0 {
		t.Fatal("expected normalized content_json")
	}
	var parts []map[string]any
	if err := json.Unmarshal(got, &parts); err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 || parts[0]["type"] != "text" || parts[1]["type"] != "image_url" || parts[2]["type"] != "file" {
		t.Fatalf("unexpected normalized parts: %s", got)
	}
}

func TestParseResponsesRequestPreservesMultimodalContent(t *testing.T) {
	body := `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"看图"},{"type":"input_image","image_url":"data:image/png;base64,AA=="}]}]}`
	req, err := parseResponsesRequest([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].ContentJson) == 0 {
		t.Fatalf("multimodal content was not preserved: %+v", req.Messages)
	}
	if !strings.Contains(string(req.Messages[0].ContentJson), "image_url") {
		t.Fatalf("content_json lost image block: %s", req.Messages[0].ContentJson)
	}
}

func TestParseAnthropicImageOnlyMessagePreservesContent(t *testing.T) {
	body := `{"model":"m","max_tokens":100,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}}]}]}`
	req, err := parseAnthropicRequest([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].ContentJson) == 0 {
		t.Fatalf("image-only Anthropic message was dropped: %+v", req.Messages)
	}
	if !strings.Contains(string(req.Messages[0].ContentJson), "image_url") {
		t.Fatalf("Anthropic image was not normalized: %s", req.Messages[0].ContentJson)
	}
}

func TestEnvelopeMessageContentJSONProtoRoundTrip(t *testing.T) {
	in := &pb.EnvelopeMessage{Role: "user", Text: "看图", ContentJson: []byte(`[{"type":"image_url"}]`)}
	b, err := proto.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out pb.EnvelopeMessage
	if err := proto.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if string(out.ContentJson) != string(in.ContentJson) {
		t.Fatalf("content_json lost over protobuf: %q != %q", out.ContentJson, in.ContentJson)
	}
}

// TestNormalizeContentKeepsCacheControl 客户端设置的 prompt 缓存断点必须透传到插件。
//
// 缓存按前缀计算：丢掉 cache_control 会让 Claude Code 等客户端设置的断点失效，
// 上游只能按默认前缀缓存，表现为缓存命中率长期偏低（且不会有报错，极难排查）。
func TestNormalizeContentKeepsCacheControl(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"text","text":"系统提示","cache_control":{"type":"ephemeral"}},
		{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="},"cache_control":{"type":"ephemeral","ttl":"1h"}}
	]`)
	got := normalizeAnthropicContent(raw)
	var parts []map[string]any
	if err := json.Unmarshal(got, &parts); err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 {
		t.Fatalf("期望 2 个内容块，实际 %d: %s", len(parts), got)
	}
	cc0, ok := parts[0]["cache_control"].(map[string]any)
	if !ok || cc0["type"] != "ephemeral" {
		t.Errorf("文本块的 cache_control 丢失: %v", parts[0])
	}
	cc1, ok := parts[1]["cache_control"].(map[string]any)
	if !ok || cc1["ttl"] != "1h" {
		t.Errorf("图片块的 cache_control 丢失（含 ttl）: %v", parts[1])
	}
}

// TestNormalizeContentCacheControlOptional 没设断点时不应凭空添加该字段。
func TestNormalizeContentCacheControlOptional(t *testing.T) {
	got := normalizeAnthropicContent(json.RawMessage(`[{"type":"text","text":"hi"}]`))
	if strings.Contains(string(got), "cache_control") {
		t.Errorf("无断点时不应出现 cache_control: %s", got)
	}
}
