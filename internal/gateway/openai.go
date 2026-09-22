// openai.go — OpenAI Chat Completions 协议 ↔ 统一信封。
package gateway

import (
	"encoding/json"
	"fmt"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// parseChatCompletions 把 /v1/chat/completions 请求体转成统一信封。
func parseChatCompletions(body []byte) (*pb.ChatRequest, error) {
	var raw struct {
		Model     string          `json:"model"`
		Messages  []openaiMessage `json:"messages"`
		MaxTokens int32           `json:"max_tokens"`
		// 新版字段：o 系 / gpt-5 系只认 max_completion_tokens，二者取其一
		MaxCompletionTokens int32           `json:"max_completion_tokens"`
		Temperature         *float64        `json:"temperature"`
		TopP                *float64        `json:"top_p"`
		Stop                json.RawMessage `json:"stop"`
		Tools               []openaiTool    `json:"tools"`
		ToolChoice          json.RawMessage `json:"tool_choice"`
		Stream              bool            `json:"stream"`
		ReasoningEffort     string          `json:"reasoning_effort"`
		// 只对 OpenAI 系上游有意义的采样/控制参数：原样透传给插件
		FrequencyPenalty  json.RawMessage `json:"frequency_penalty"`
		PresencePenalty   json.RawMessage `json:"presence_penalty"`
		Seed              json.RawMessage `json:"seed"`
		ParallelToolCalls json.RawMessage `json:"parallel_tool_calls"`
		ResponseFormat    json.RawMessage `json:"response_format"`
		User              string          `json:"user"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	if len(raw.Messages) == 0 {
		return nil, fmt.Errorf("messages is required")
	}
	if raw.MaxTokens == 0 {
		raw.MaxTokens = raw.MaxCompletionTokens
	}

	req := &pb.ChatRequest{
		Model:       raw.Model,
		Stream:      raw.Stream,
		MaxTokens:   raw.MaxTokens,
		Temperature: deref(raw.Temperature),
		Extra:       map[string]string{},
	}
	setTemperature(req, raw.Temperature)
	if raw.TopP != nil {
		req.Extra["top_p"] = fmt.Sprintf("%g", *raw.TopP)
	}
	if len(raw.Stop) > 0 {
		req.Extra["stop"] = string(raw.Stop)
	}
	if raw.ReasoningEffort != "" {
		req.Extra["reasoning_effort"] = raw.ReasoningEffort
	}
	for k, v := range map[string]json.RawMessage{
		"frequency_penalty": raw.FrequencyPenalty, "presence_penalty": raw.PresencePenalty,
		"seed": raw.Seed, "parallel_tool_calls": raw.ParallelToolCalls, "response_format": raw.ResponseFormat,
	} {
		if len(v) > 0 && string(v) != "null" {
			req.Extra[k] = string(v)
		}
	}
	if raw.User != "" {
		req.Extra["user"] = raw.User
	}

	for i := range raw.Messages {
		m := &raw.Messages[i]
		em := &pb.EnvelopeMessage{Role: normalizeRole(m.Role), Text: extractText(m.Content), Raw: m.Content, ContentJson: normalizeOpenAIContent(m.Content)}
		for _, tc := range m.ToolCalls {
			em.ToolCalls = append(em.ToolCalls, &pb.ToolCall{
				Id: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments,
			})
		}
		if m.ToolCallID != "" {
			em.ToolCallId = m.ToolCallID
		}
		req.Messages = append(req.Messages, em)
	}

	for _, t := range raw.Tools {
		if t.Function.Name != "" {
			req.Tools = append(req.Tools, &pb.ToolDefinition{
				Name: t.Function.Name, Description: t.Function.Description,
				ParametersSchema: string(t.Function.Parameters),
			})
		}
	}
	if tc, err := convertOpenAIToolChoice(raw.ToolChoice); err == nil && tc != nil {
		req.ToolChoice = tc
	}
	return req, nil
}

type openaiMessage struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	ToolCalls []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
	ToolCallID string `json:"tool_call_id"`
}

type openaiTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

func convertOpenAIToolChoice(raw json.RawMessage) (*pb.ToolChoice, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch s {
		case "auto", "none":
			return &pb.ToolChoice{Type: s}, nil
		case "required":
			return &pb.ToolChoice{Type: "tool"}, nil
		}
		return nil, fmt.Errorf("unsupported tool_choice: %s", s)
	}
	var tc struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &tc); err != nil {
		return nil, err
	}
	if tc.Type == "function" {
		return &pb.ToolChoice{Type: "tool", ToolName: tc.Function.Name}, nil
	}
	return &pb.ToolChoice{Type: tc.Type}, nil
}

// ---------- 信封事件 → OpenAI SSE ----------

type openaiSSEState struct {
	model   string
	id      string
	created int64
	// tool call 聚合（OpenAI 的 delta.tool_calls 用 index 标识）
	toolIdx map[string]int
	nextIdx int
	tools   toolCallTracker
}

func newOpenAISSEState() *openaiSSEState {
	return &openaiSSEState{id: "chatcmpl-" + randHex(12), toolIdx: map[string]int{}}
}

// convertEvent 信封事件 → OpenAI chat.completion.chunk SSE 行。
func (s *openaiSSEState) convertEvent(ev *pb.StreamEvent) string {
	switch e := ev.Event.(type) {
	case *pb.StreamEvent_MessageStart:
		s.model = e.MessageStart.Model
		return ""

	case *pb.StreamEvent_ContentDelta:
		return s.chunk(map[string]interface{}{
			"role": "assistant", "content": e.ContentDelta.Text,
		}, "")

	case *pb.StreamEvent_ToolCallDelta:
		id, name := s.tools.resolve(e.ToolCallDelta)
		idx, ok := s.toolIdx[id]
		if !ok {
			idx = s.nextIdx
			s.nextIdx++
			s.toolIdx[id] = idx
		}
		// 首块带 id/type/name，后续增量只带 arguments（OpenAI 流式惯例）
		tc := map[string]interface{}{"index": idx}
		fn := map[string]interface{}{"arguments": e.ToolCallDelta.ArgumentsDelta}
		if !ok {
			tc["id"], tc["type"] = id, "function"
			fn["name"] = name
		}
		tc["function"] = fn
		return s.chunk(map[string]interface{}{
			"tool_calls": []interface{}{tc},
		}, "")

	case *pb.StreamEvent_MessageFinish:
		out := s.chunk(map[string]interface{}{}, e.MessageFinish.FinishReason)
		// 带 usage 的终止块（stream_options.include_usage 的客户端靠它收用量）
		if e.MessageFinish.Usage != nil {
			out += s.chunkRaw(map[string]interface{}{
				"id": s.id, "object": "chat.completion.chunk", "created": s.created,
				"model": s.model, "choices": []interface{}{},
				"usage": openAIUsagePayload(e.MessageFinish.Usage),
			})
		}
		return out
	}
	return ""
}

// openAIUsagePayload 生成 OpenAI 兼容的 usage：prompt_tokens 含缓存命中，
// prompt_tokens_details.cached_tokens 是其中的子集（OpenAI 官方口径）。
func openAIUsagePayload(u *pb.Usage) map[string]interface{} {
	in, out, cached, write := int64(0), int64(0), int64(0), int64(0)
	if u != nil {
		in, out, cached, write = u.InputTokens, u.OutputTokens, u.CachedTokens, u.CacheCreationTokens
	}
	payload := map[string]interface{}{
		"prompt_tokens": in, "completion_tokens": out, "total_tokens": in + out,
	}
	if cached > 0 || write > 0 {
		details := map[string]interface{}{"cached_tokens": cached}
		if write > 0 {
			details["cache_write_tokens"] = write
		}
		payload["prompt_tokens_details"] = details
	}
	return payload
}

// chunk 生成一个 choices[0] 带 delta 与可选 finish_reason 的 chunk。
func (s *openaiSSEState) chunk(delta map[string]interface{}, finish string) string {
	choice := map[string]interface{}{"index": 0, "delta": delta}
	if finish != "" {
		choice["finish_reason"] = finish
	}
	return s.chunkRaw(map[string]interface{}{
		"id": s.id, "object": "chat.completion.chunk", "created": s.created,
		"model": s.model, "choices": []interface{}{choice},
	})
}

func (s *openaiSSEState) chunkRaw(payload map[string]interface{}) string {
	b, _ := json.Marshal(payload)
	return "data: " + string(b) + "\n\n"
}

// openaiAggregate 非流式聚合。
// openaiAggregate 内嵌公共聚合核心，只负责渲染 OpenAI Chat 的 JSON。
type openaiAggregate struct {
	aggregateCore
}

func (a *openaiAggregate) result() map[string]interface{} {
	msg := map[string]interface{}{"role": "assistant", "content": a.text}
	if len(a.tools) > 0 {
		msg["content"] = nil
		var tcs []interface{}
		for _, id := range sortedKeys(a.tools) {
			t := a.tools[id]
			tcs = append(tcs, map[string]interface{}{
				"id": t.id, "type": "function",
				"function": map[string]interface{}{"name": t.name, "arguments": t.input},
			})
		}
		msg["tool_calls"] = tcs
	}
	var choices []interface{}
	choices = append(choices, map[string]interface{}{
		"index": 0, "message": msg, "finish_reason": a.finish,
	})
	return map[string]interface{}{
		"id": "chatcmpl-" + randHex(12), "object": "chat.completion",
		"created": 0, "model": a.model, "choices": choices,
		"usage": openAIUsagePayload(&a.usage),
	}
}
