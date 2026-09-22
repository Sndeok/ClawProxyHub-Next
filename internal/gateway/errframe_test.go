package gateway

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// TestStreamFailureFrames 回归：上游中途失败必须按入口协议下发错误事件。
// 旧实现只写一个裸 data: {"error":...} 帧，三种协议都不认——Codex 把断流当成
// "正常结束"（工具调用悬空 → 复杂操作无回复），Anthropic SDK 直接报解析失败。
func TestStreamFailureFrames(t *testing.T) {
	cases := []struct {
		protocol string
		want     []string
	}{
		{"responses", []string{"response.failed", `"status":"failed"`, `"code":"server_error"`, "上游炸了"}},
		{"messages", []string{"event: error", `"type":"api_error"`, "上游炸了"}},
		{"chat_completions", []string{`"error"`, `"type":"upstream_error"`, "上游炸了"}},
	}
	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			db := openTestDB(t)
			s := &Server{db: db}
			rec := httptest.NewRecorder()
			events := make(chan *pb.StreamEvent, 1)
			events <- &pb.StreamEvent{Event: &pb.StreamEvent_TaskFailed{
				TaskFailed: &pb.TaskFailed{Error: &pb.Error{Code: 502, Message: "上游炸了"}},
			}}
			close(events)

			log := &requestLogCtx{protocol: tc.protocol}
			s.streamOut(rec, events, nil, log, newEncoder(tc.protocol, "glm-5.3"), nil)

			body := rec.Body.String()
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Errorf("错误帧缺少 %q:\n%s", want, body)
				}
			}
			// 不能把 JSON 编码失败/裸帧当兼容：必须是一行完整 SSE 事件
			if !strings.Contains(body, "data: ") {
				t.Errorf("错误帧不是 SSE 格式:\n%s", body)
			}
			// 落库的状态与错误类型不能丢
			if log.status != 502 {
				t.Errorf("log.status want 502 got %d", log.status)
			}
			if log.errorType != "upstream_error" {
				t.Errorf("log.errorType want upstream_error got %q", log.errorType)
			}
		})
	}
}

// TestErrBodyHasTopLevelType Anthropic SDK 要求顶层 type=error，缺了会报 response shape 错误。
func TestErrBodyHasTopLevelType(t *testing.T) {
	body := errBody("invalid_request_error", errString("bad input"))
	buf := &bytes.Buffer{}
	if err := writeJSONTo(buf, body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"type":"error"`) {
		t.Errorf("errBody 缺顶层 type=error: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"message":"bad input"`) {
		t.Errorf("errBody 缺 message: %s", buf.String())
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func writeJSONTo(buf *bytes.Buffer, v interface{}) error {
	return json.NewEncoder(buf).Encode(v)
}
