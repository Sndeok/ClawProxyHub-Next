// output.go — 信封事件 → 协议输出的统一收尾：SSE、聚合、日志。
package gateway

import (
	"encoding/json"
	"io"
	"net/http"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// streamEncoder 流式编码器：把信封事件编码为协议 SSE 文本。
type streamEncoder interface {
	convertEvent(ev *pb.StreamEvent) string
	// failure 流中途失败时的协议错误事件。
	// 客户端据此报错；缺了它上游断流会被当成"正常结束"，表现为「复杂操作不回复」。
	failure(message string) string
	// finish 流结束后的尾部输出（OpenAI 的 [DONE] 等）。
	finish() string
}

func (s *anthSSEState) finish() string   { return "" }
func (s *openaiSSEState) finish() string { return "data: [DONE]\n\n" }

// failure Anthropic：error 事件（SDK 直接抛出 api_error）。
func (s *anthSSEState) failure(message string) string {
	return anthEvent("error", map[string]interface{}{
		"error": map[string]interface{}{"type": "api_error", "message": message},
	})
}

// failure OpenAI：错误 chunk（OpenAI SDK 按 error 字段抛异常）。
func (s *openaiSSEState) failure(message string) string {
	return s.chunkRaw(map[string]interface{}{
		"error": map[string]interface{}{"type": "upstream_error", "message": message},
	})
}

// failure Responses：response.failed + status=failed（Codex 据此展示失败原因）。
func (s *responsesSSEState) failure(message string) string {
	return respEvent("response.failed", map[string]interface{}{
		"response": map[string]interface{}{
			"id": s.respID, "object": "response", "model": s.model, "status": "failed",
			"error": map[string]interface{}{"code": "server_error", "message": message},
		},
	})
}

// aggregate 非流式聚合器。
type aggregate interface {
	feed(ev *pb.StreamEvent)
	result() map[string]interface{}
}

// newEncoder 按协议构造流式编码器。
func newEncoder(protocol, model string) streamEncoder {
	switch protocol {
	case "chat_completions":
		return newOpenAISSEState()
	case "responses":
		return newResponsesSSEState(model)
	default:
		return newAnthSSEState(model)
	}
}

// newAggregate 按协议构造聚合器。
func newAggregate(protocol, model string) aggregate {
	switch protocol {
	case "chat_completions":
		return &openaiAggregate{aggregateCore: aggregateCore{model: model}}
	case "responses":
		return &responsesAggregate{aggregateCore: aggregateCore{model: model}}
	default:
		return &anthAggregate{aggregateCore: aggregateCore{model: model}}
	}
}

// streamOut 流式：编码写回 + 失败短路 + 日志收尾。first 为已取出的首事件。
func (s *Server) streamOut(w http.ResponseWriter, events chan *pb.StreamEvent, first *pb.StreamEvent, log *requestLogCtx, enc streamEncoder, _ interface{}) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)

	log.status = http.StatusOK

	emit := func(ev *pb.StreamEvent) bool {
		if failed, ok := ev.Event.(*pb.StreamEvent_TaskFailed); ok && failed.TaskFailed != nil {
			log.status = http.StatusBadGateway
			log.errorType = "upstream_error"
			log.errBrief = failed.TaskFailed.Error.GetMessage()
			log.errorDetail = failedDetail(failed.TaskFailed)
			// 按入口协议下发错误帧：Codex(responses) 认 response.failed、
			// Anthropic SDK 认 error 事件、OpenAI SDK 认 error chunk。
			// 少了它上游断流会被当成"正常结束"，表现为「复杂操作不回复」。
			io.WriteString(w, enc.failure(log.errBrief))
			if flusher != nil {
				flusher.Flush()
			}
			return false
		}
		collectUsage(log, ev)
		if out := enc.convertEvent(ev); out != "" {
			io.WriteString(w, out)
			if flusher != nil {
				flusher.Flush()
			}
		}
		return true
	}

	if first != nil && !emit(first) {
		io.WriteString(w, enc.finish())
		log.write(s.db)
		return
	}
	for ev := range events {
		if !emit(ev) {
			break
		}
	}
	drain(events)
	io.WriteString(w, enc.finish())
	log.write(s.db)
}

// nonStreamOut 聚合：完整 JSON 一次写回。first 为已取出的首事件。
// 返回 failCode 非 0：聚合中途失败且响应/日志均未写（凭据失效或可降级错误），
// 交回 serve 层决定恢复重试或收尾；成功路径自行写响应与日志。
func (s *Server) nonStreamOut(w http.ResponseWriter, events chan *pb.StreamEvent, first *pb.StreamEvent, log *requestLogCtx, aggr ...aggregate) (failCode int32, brief string) {
	if len(aggr) == 0 {
		return 0, ""
	}
	a := aggr[0]

	handle := func(ev *pb.StreamEvent) bool {
		if failed, ok := ev.Event.(*pb.StreamEvent_TaskFailed); ok && failed.TaskFailed != nil {
			// 失败时响应尚未写出：返回码与摘要，恢复/降级决策归 serve 层
			failCode = failed.TaskFailed.Error.GetCode()
			brief = failed.TaskFailed.Error.GetMessage()
			log.errorDetail = failedDetail(failed.TaskFailed)
			return false
		}
		collectUsage(log, ev)
		a.feed(ev)
		return true
	}
	if first != nil && !handle(first) {
		drain(events)
		return failCode, brief
	}
	for ev := range events {
		if !handle(ev) {
			drain(events)
			return failCode, brief
		}
	}
	log.status = http.StatusOK
	writeJSON(w, http.StatusOK, a.result())
	log.write(s.db)
	return 0, ""
}

// collectUsage 从 MessageFinish 事件提取用量与结束原因（排查断流/工具调用时看它）。
// failedDetail 失败事件的详细上游返回：插件没带 detail 时退回错误摘要。
func failedDetail(tf *pb.TaskFailed) string {
	if tf == nil {
		return ""
	}
	if tf.Detail != "" {
		return clipBody(tf.Detail)
	}
	return tf.Error.GetMessage()
}

func collectUsage(log *requestLogCtx, ev *pb.StreamEvent) {
	if fin, ok := ev.Event.(*pb.StreamEvent_MessageFinish); ok && fin.MessageFinish != nil {
		log.finishReason = fin.MessageFinish.FinishReason
		if u := fin.MessageFinish.Usage; u != nil {
			log.input, log.output, log.cached = u.InputTokens, u.OutputTokens, u.CachedTokens
			log.cacheCreation = u.CacheCreationTokens
			log.credit = u.CreditUsed
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// errBody 错误响应体：OpenAI 形态的 error 对象 + Anthropic 要求的顶层 type=error，
// 两个 SDK 都能解析（Anthropic 缺顶层 type 会报 unexpected response shape）。
func errBody(errType string, err error) map[string]interface{} {
	return map[string]interface{}{
		"type":  "error",
		"error": map[string]string{"type": errType, "message": err.Error()},
	}
}

// drain 清空事件通道，让生产者（gRPC 流消费协程）能退出。
func drain(events chan *pb.StreamEvent) {
	for range events {
	}
}
