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
