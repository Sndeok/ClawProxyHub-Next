// responses.go — OpenAI Responses 协议（Codex CLI）↔ 统一信封。
package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// parseResponsesRequest 把 /v1/responses 请求体转成统一信封。
func parseResponsesRequest(body []byte) (*pb.ChatRequest, error) {
	var raw struct {
		Model           string          `json:"model"`
		Instructions    string          `json:"instructions"`
		Input           json.RawMessage `json:"input"`
		Tools           []respTool      `json:"tools"`
		ToolChoice      json.RawMessage `json:"tool_choice"`
		MaxOutputTokens int32           `json:"max_output_tokens"`
		Temperature     *float64        `json:"temperature"`
		TopP            *float64        `json:"top_p"`
		Stream          bool            `json:"stream"`
		Reasoning       json.RawMessage `json:"reasoning"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}

	req := &pb.ChatRequest{
		Model:       raw.Model,
		Stream:      raw.Stream,
		MaxTokens:   raw.MaxOutputTokens,
		Temperature: deref(raw.Temperature),
		Extra:       map[string]string{},
	}
	if raw.TopP != nil {
		req.Extra["top_p"] = fmt.Sprintf("%g", *raw.TopP)
	}
	// 不透传 reasoning / reasoning.effort：Responses 本无顶层 reasoning_effort，
	// 而 Codex 会发 "xhigh" 这类上游不认的私有值，透传过去直接 500。
	// （对齐上游 v1.0.2：该字段在归一化时丢弃。）
	if raw.Instructions != "" {
		req.Messages = append(req.Messages, &pb.EnvelopeMessage{Role: "system", Text: raw.Instructions})
	}

	// input 可能是纯字符串、单个 item 对象，或 item 数组。
	//
	// 各客户端对同一字段的类型并不一致：function_call_output.output 可能是字符串、
	// 也可能是内容块数组；custom_tool_call 把入参放在 input 而非 arguments；部分实现
	// 还会把 arguments 直接写成对象。原先用固定 string 字段反序列化，任何一处类型
	// 不匹配都会让整条请求 400（input must be string or message array）。
	// 现改为逐元素宽松解析：能取的字段尽力取，取不到就跳过该元素，而不是整条失败。
	switch jsonKind(raw.Input) {
	case "string":
		if t := jsonText(raw.Input); t != "" {
			req.Messages = append(req.Messages, &pb.EnvelopeMessage{Role: "user", Text: t})
		}
	case "array", "object":
		items, err := responseItems(raw.Input)
		if err != nil {
			return nil, err
		}
		// 合并相邻 assistant message 与 function_call：并行调用必须归入同一条
		// assistant 的 tool_calls。否则会退化成「N 条各带 1 个 tool_call 的 assistant」，
		// 随后的 tool 消息与声明它的 assistant 错位，上游直接拒绝。
		var pendText string
		var pendContent []byte
		var pendAssistant bool
		var pendTools []*pb.ToolCall
		flushAssistant := func() {
			if !pendAssistant && len(pendTools) == 0 {
				return
			}
			req.Messages = append(req.Messages, &pb.EnvelopeMessage{
				Role: "assistant", Text: pendText, ToolCalls: pendTools, ContentJson: pendContent,
			})
			pendText, pendContent, pendAssistant, pendTools = "", nil, false, nil
		}
		for _, it := range items {
			switch it.kind() {
			case "message", "":
				role := normalizeRole(jsonText(it.Role))
				if role == "" {
					role = "user"
				}
				if role == "assistant" {
					flushAssistant()
					pendText, pendContent, pendAssistant = jsonText(it.Content), normalizeResponsesContent(it.Content), true
					continue
				}
				flushAssistant()
				req.Messages = append(req.Messages, &pb.EnvelopeMessage{
					Role: role, Text: jsonText(it.Content), ContentJson: normalizeResponsesContent(it.Content),
				})
			case "function_call", "custom_tool_call", "local_shell_call", "computer_call":
				// custom_tool_call 把原始入参放在 input（如 apply_patch 的补丁文本），
				// 信封只认 arguments 字符串，这里统一归一（非 JSON 文本会被编码成 JSON 字符串）
				args := jsonText(it.Arguments)
				if args == "" {
					args = jsonText(it.Input)
				}
				pendTools = append(pendTools, &pb.ToolCall{
					Id: jsonText(it.CallID), Name: jsonText(it.Name), Arguments: ensureJSONArgs(args),
				})
			case "function_call_output", "custom_tool_call_output", "local_shell_call_output":
				flushAssistant()
				req.Messages = append(req.Messages, &pb.EnvelopeMessage{
					Role: "tool", Text: jsonText(it.Output), ToolCallId: jsonText(it.CallID),
					ContentJson: normalizeResponsesContent(it.Output),
				})
			case "reasoning", "web_search_call", "item_reference":
				// 信封里没有对应语义（推理摘要 / 内置检索调用），跳过
			default:
				log.Printf("[gateway] responses: 跳过不支持的 input 元素类型 %q", it.kind())
			}
		}
		flushAssistant()
	case "empty", "null":
		// 没有 input（只有 instructions）：不补 user 消息
	default:
		return nil, fmt.Errorf("input must be a string or an array of items (got %s)", jsonKind(raw.Input))
	}

	for _, t := range raw.Tools {
		if t.Type == "function" && t.Name != "" {
			req.Tools = append(req.Tools, &pb.ToolDefinition{
				Name: t.Name, Description: t.Description,
				ParametersSchema: string(t.Parameters),
			})
		}
	}
	if len(raw.ToolChoice) > 0 {
		var s string
		if err := json.Unmarshal(raw.ToolChoice, &s); err == nil {
			req.ToolChoice = &pb.ToolChoice{Type: s}
		}
	}
	return req, nil
}

type respTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ---------- 信封事件 → Responses SSE ----------

// respFnItem 一个 function_call output item 的累积状态。
type respFnItem struct {
	itemID string
	name   string
	idx    int
	args   string
}

type responsesSSEState struct {
	model      string
	respID     string
	nextItem   int // 递增的 output_index；reasoning / 文本 / function_call item 共用同一序列
	reasonItem string
	reasonIdx  int
	reasoning  string
	textItem   string
	textIdx    int
	text       string
	fnItems    map[string]*respFnItem
	fnOrder    []string
	tools      toolCallTracker
}

func newResponsesSSEState(model string) *responsesSSEState {
	return &responsesSSEState{
		model: model, respID: "resp_" + randHex(16),
		reasonItem: "", reasonIdx: -1,
		textItem: "", textIdx: -1, fnItems: map[string]*respFnItem{},
	}
}

// addFn 首次出现的工具调用 → 建 item 并返回（needStart=true 表示要发 output_item.added）。
func (s *responsesSSEState) addFn(id, name string) (*respFnItem, bool) {
	if it, ok := s.fnItems[id]; ok {
		if it.name == "" && name != "" {
			it.name = name
		}
		return it, false
	}
	it := &respFnItem{itemID: fmt.Sprintf("item_%d", s.nextItem), name: name, idx: s.nextItem}
	s.nextItem++
	s.fnItems[id] = it
	s.fnOrder = append(s.fnOrder, id)
	return it, true
}

// outputItem function_call item 的 JSON 表示（added / done 共用，status 不同）。
func (it *respFnItem) outputItem(id, status string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function_call", "id": it.itemID, "call_id": id,
		"name": it.name, "arguments": it.args, "status": status,
	}
}

func (s *responsesSSEState) convertEvent(ev *pb.StreamEvent) string {
	switch e := ev.Event.(type) {
	case *pb.StreamEvent_MessageStart:
		return respEvent("response.created", map[string]interface{}{
			"response": map[string]interface{}{
				"id": s.respID, "object": "response", "model": e.MessageStart.Model,
				"status": "in_progress", "output": []interface{}{},
			},
		})

	case *pb.StreamEvent_ContentDelta:
		if e.ContentDelta.GetReasoning() {
			// 思考增量 → Responses 的 reasoning item（Codex 会把它渲染成思考摘要）。
			// 以前整段丢掉：模型思考的几秒到几十秒里客户端完全静默。
			var out string
			if s.reasonItem == "" {
				s.reasonItem = fmt.Sprintf("item_%d", s.nextItem)
				s.reasonIdx = s.nextItem
				s.nextItem++
				out += respEvent("response.output_item.added", map[string]interface{}{
					"output_index": s.reasonIdx, "item": map[string]interface{}{
						"type": "reasoning", "id": s.reasonItem, "summary": []interface{}{},
					},
				})
				out += respEvent("response.reasoning_summary_part.added", map[string]interface{}{
					"item_id": s.reasonItem, "output_index": s.reasonIdx, "summary_index": 0,
					"part": map[string]interface{}{"type": "summary_text", "text": ""},
				})
			}
			s.reasoning += e.ContentDelta.Text
			out += respEvent("response.reasoning_summary_text.delta", map[string]interface{}{
				"item_id": s.reasonItem, "output_index": s.reasonIdx, "summary_index": 0,
				"delta": e.ContentDelta.Text,
			})
			return out
		}
		var out string
		if s.textItem == "" {
			s.textItem = fmt.Sprintf("item_%d", s.nextItem)
			s.textIdx = s.nextItem
			s.nextItem++
			out += respEvent("response.output_item.added", map[string]interface{}{
				"output_index": s.textIdx, "item": map[string]interface{}{
					"type": "message", "id": s.textItem, "role": "assistant", "status": "in_progress",
					"content": []interface{}{map[string]interface{}{"type": "output_text", "text": ""}},
				},
			})
		}
		s.text += e.ContentDelta.Text
		out += respEvent("response.output_text.delta", map[string]interface{}{
			"item_id": s.textItem, "output_index": s.textIdx, "content_index": 0,
			"delta": e.ContentDelta.Text,
		})
		return out

	case *pb.StreamEvent_ToolCallDelta:
		id, name := s.tools.resolve(e.ToolCallDelta)
		it, needStart := s.addFn(id, name)
		var out string
		if needStart {
			out += respEvent("response.output_item.added", map[string]interface{}{
				"output_index": it.idx, "item": it.outputItem(id, "in_progress"),
			})
		}
		if e.ToolCallDelta.ArgumentsDelta != "" {
			it.args += e.ToolCallDelta.ArgumentsDelta
			out += respEvent("response.function_call_arguments.delta", map[string]interface{}{
				"item_id": it.itemID, "output_index": it.idx,
				"delta": e.ToolCallDelta.ArgumentsDelta,
			})
		}
		return out

	case *pb.StreamEvent_MessageFinish:
		var out string
		// 输出项必须逐个 output_item.done 收尾：Codex CLI 只认 done 事件里的
		// function_call（缺了它工具不会被调度执行，表现为「复杂操作无回复」）。
		var output []interface{}
		if s.reasonItem != "" {
			out += respEvent("response.reasoning_summary_text.done", map[string]interface{}{
				"item_id": s.reasonItem, "output_index": s.reasonIdx, "summary_index": 0, "text": s.reasoning,
			})
			out += respEvent("response.reasoning_summary_part.done", map[string]interface{}{
				"item_id": s.reasonItem, "output_index": s.reasonIdx, "summary_index": 0,
				"part": map[string]interface{}{"type": "summary_text", "text": s.reasoning},
			})
			ritem := map[string]interface{}{
				"type": "reasoning", "id": s.reasonItem,
				"summary": []interface{}{map[string]interface{}{"type": "summary_text", "text": s.reasoning}},
			}
			out += respEvent("response.output_item.done", map[string]interface{}{
				"output_index": s.reasonIdx, "item": ritem,
			})
			output = append(output, ritem)
		}
		if s.textItem != "" {
			out += respEvent("response.output_text.done", map[string]interface{}{
				"item_id": s.textItem, "output_index": s.textIdx, "content_index": 0, "text": s.text,
			})
			item := map[string]interface{}{
				"type": "message", "id": s.textItem, "role": "assistant", "status": "completed",
				"content": []interface{}{map[string]interface{}{
					"type": "output_text", "text": s.text, "annotations": []interface{}{},
				}},
			}
			out += respEvent("response.output_item.done", map[string]interface{}{
				"output_index": s.textIdx, "item": item,
			})
			output = append(output, item)
		}
		for _, id := range s.fnOrder {
			it := s.fnItems[id]
			out += respEvent("response.function_call_arguments.done", map[string]interface{}{
				"item_id": it.itemID, "output_index": it.idx, "arguments": it.args,
			})
			item := it.outputItem(id, "completed")
			out += respEvent("response.output_item.done", map[string]interface{}{
				"output_index": it.idx, "item": item,
			})
			output = append(output, item)
		}
		// usage 为必填字段，缺失时补零值（Codex 严格反序列化，否则断流）。
		usage := responsesUsagePayload(e.MessageFinish.Usage)
		out += respEvent("response.completed", map[string]interface{}{
			"response": map[string]interface{}{
				"id": s.respID, "object": "response", "model": s.model,
				"status": "completed", "output": output, "usage": usage,
			},
		})
		return out
	}
	return ""
}

func (s *responsesSSEState) finish() string { return "" }

func respEvent(eventType string, payload map[string]interface{}) string {
	payload["type"] = eventType
	b, _ := json.Marshal(payload)
	return "event: " + eventType + "\ndata: " + string(b) + "\n\n"
}

// responsesAggregate Responses 非流式聚合。
// responsesUsagePayload 生成 Responses 口径的 usage：input_tokens 含缓存命中，
// input_tokens_details.cached_tokens 为其中的子集。
func responsesUsagePayload(u *pb.Usage) map[string]interface{} {
	in, out, cached, write := int64(0), int64(0), int64(0), int64(0)
	if u != nil {
		in, out, cached, write = u.InputTokens, u.OutputTokens, u.CachedTokens, u.CacheCreationTokens
	}
	usage := map[string]interface{}{
		"input_tokens": in, "output_tokens": out, "total_tokens": in + out,
	}
	if cached > 0 || write > 0 {
		details := map[string]interface{}{"cached_tokens": cached}
		if write > 0 {
			details["cache_write_tokens"] = write
		}
		usage["input_tokens_details"] = details
	}
	return usage
}

// responsesAggregate 内嵌公共聚合核心，按「出现顺序」输出 output 项。
type responsesAggregate struct {
	aggregateCore
}

func (a *responsesAggregate) result() map[string]interface{} {
	var output []interface{}
	if a.text != "" {
		output = append(output, map[string]interface{}{
			"type": "message", "id": "item_0", "role": "assistant", "status": "completed",
			"content": []interface{}{map[string]interface{}{
				"type": "output_text", "text": a.text, "annotations": []interface{}{},
			}},
		})
	}
	// 工具项按出现顺序输出（顺序与流式编码器一致）
	for _, id := range a.order {
		t := a.tools[id]
		output = append(output, map[string]interface{}{
			"type": "function_call", "id": "item_" + id, "call_id": t.id, "name": t.name,
			"arguments": t.input, "status": "completed",
		})
	}
	return map[string]interface{}{
		"id": "resp_" + randHex(16), "object": "response", "model": a.model,
		"status": "completed", "output": output,
		"usage": responsesUsagePayload(&a.usage),
	}
}

// ---------- input 元素的宽松解析 ----------
//
// Codex / CC Switch / new-api 等客户端对 Responses input 的构造并不完全一致，
// 严格按固定类型反序列化会把「类型不符」升级成整条请求 400。以下取值一律先看
// 原始 JSON，再按需解析，取不到就退化为空值。

// respInputItem 未类型化的 input 元素：所有字段保持原始 JSON。
type respInputItem struct {
	Type      json.RawMessage `json:"type"`
	Role      json.RawMessage `json:"role"`
	Content   json.RawMessage `json:"content"`
	CallID    json.RawMessage `json:"call_id"`
	Name      json.RawMessage `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Output    json.RawMessage `json:"output"`
	Input     json.RawMessage `json:"input"` // custom_tool_call 的原始入参
}

func (it respInputItem) kind() string { return jsonText(it.Type) }

// responseItems 把 input 归一为元素列表：数组逐元素拆，单对象视为一个元素，
// 数组里的裸字符串当作一条 user 消息。
func responseItems(raw json.RawMessage) ([]respInputItem, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var one respInputItem
		if err := json.Unmarshal(trimmed, &one); err != nil {
			return nil, fmt.Errorf("input item is not an object: %w", err)
		}
		return []respInputItem{one}, nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(trimmed, &raws); err != nil {
		return nil, fmt.Errorf("input must be a string or an array of items (got %s)", jsonKind(raw))
	}
	out := make([]respInputItem, 0, len(raws))
	for _, r := range raws {
		var one respInputItem
		if err := json.Unmarshal(r, &one); err != nil {
			// 裸字符串元素 → 当作一条 user 消息；其它标量跳过并留痕
			if jsonKind(r) == "string" {
				content, _ := json.Marshal(jsonText(r))
				out = append(out, respInputItem{
					Type: json.RawMessage(`"message"`), Role: json.RawMessage(`"user"`), Content: content,
				})
				continue
			}
			log.Printf("[gateway] responses: 跳过无法解析的 input 元素（%s）: %.200s", jsonKind(r), r)
		}
		out = append(out, one)
	}
	return out, nil
}

// jsonKind 返回 JSON 值的类型名，用于容错分支与错误信息。
func jsonKind(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "empty"
	}
	switch trimmed[0] {
	case '"':
		return "string"
	case '[':
		return "array"
	case '{':
		return "object"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		return "number"
	}
}

// jsonText 从任意 JSON 值里尽力提取文本：
// 字符串直接取；内容块数组递归拼接 text 字段；对象取 text 字段；其余原样返回压缩 JSON。
func jsonText(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return ""
	}
	switch trimmed[0] {
	case '"':
		var s string
		if json.Unmarshal(trimmed, &s) == nil {
			return s
		}
	case '[':
		var elems []json.RawMessage
		if json.Unmarshal(trimmed, &elems) == nil {
			texts := make([]string, 0, len(elems))
			for _, e := range elems {
				if t := jsonText(e); t != "" {
					texts = append(texts, t)
				}
			}
			return strings.Join(texts, "\n")
		}
	case '{':
		var obj struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(trimmed, &obj) == nil && obj.Text != "" {
			return obj.Text
		}
	}
	return string(trimmed)
}

// ensureJSONArgs 保证工具调用入参是合法 JSON 文本（上游要求）：
// 已是 JSON 原样保留；否则把原始文本编码成 JSON 字符串；空值给 {}。
func ensureJSONArgs(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "{}"
	}
	if json.Valid([]byte(s)) {
		return s
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "{}"
	}
	return string(b)
}
