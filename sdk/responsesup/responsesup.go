package responsesup

import (
	"encoding/json"
	"fmt"
	"strings"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// Parser Responses SSE 解析器。
type Parser struct {
	emit        func(*pb.StreamEvent)
	sentStart   bool
	sentFinish  bool
	pendingStop string
	usage       *pb.Usage
	// 工具调用身份按 output_index 记忆：上游首帧给 id/name，后续帧只给参数增量
	toolIDs   map[int]string
	toolNames map[int]string
	// 参数是否已按增量下发（按 item id）：上游只在 done 给全量参数时补发一次，避免重复
	argSeen map[string]bool
	// item id → call id：done 帧只有 item id 时也能给增量帧带上正确的调用 id，避免信封侧拆成新块
	callIDOf map[string]string
	sawTool  bool
}

// NewParser 创建解析器；emit 为 CPH 事件出口。
func NewParser(emit func(*pb.StreamEvent)) *Parser {
	return &Parser{
		emit: emit, toolIDs: map[int]string{}, toolNames: map[int]string{},
		argSeen: map[string]bool{}, callIDOf: map[string]string{},
	}
}

// Feed 处理一行（"event: xxx" / "data: {...}" / "[DONE]"）。
func (p *Parser) Feed(line string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
		return // 事件名由 data 里的 type 字段判定，无需单独处理
	}
	if !strings.HasPrefix(line, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return
	}
	var ev struct {
		Type      string `json:"type"`
		Model     string `json:"model"`
		Delta     string `json:"delta"`
		Index     int    `json:"output_index"`
		ItemID    string `json:"item_id"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Item      struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"item"`
		Response struct {
			Model string `json:"model"`
			Usage *struct {
				InputTokens        int64 `json:"input_tokens"`
				OutputTokens       int64 `json:"output_tokens"`
				InputTokensDetails struct {
					CachedTokens int64 `json:"cached_tokens"`
				} `json:"input_tokens_details"`
			} `json:"usage"`
			IncompleteDetails struct {
				Reason string `json:"reason"`
			} `json:"incomplete_details"`
			Error *struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		} `json:"response"`
		Error *struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return
	}

	switch ev.Type {
	case "response.created", "response.in_progress":
		model := ev.Model
		if model == "" {
			model = ev.Response.Model
		}
		if !p.sentStart {
			p.sentStart = true
			p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_MessageStart{
				MessageStart: &pb.MessageStart{Model: model},
			}})
		}

	case "response.output_text.delta":
		if ev.Delta != "" {
			p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_ContentDelta{
				ContentDelta: &pb.ContentDelta{Text: ev.Delta},
			}})
		}

	case "response.output_item.added":
		if ev.Item.Type == "function_call" {
			p.sawTool = true
			id := ev.Item.CallID
			if id == "" {
				id = ev.Item.ID
			}
			if id == "" {
				id = fmt.Sprintf("call_%d", ev.Index)
			}
			p.toolIDs[ev.Index] = id
			p.toolNames[ev.Index] = ev.Item.Name
			if itemKey := firstNonEmpty(ev.Item.ID, ev.ItemID); itemKey != "" {
				p.callIDOf[itemKey] = id
			}
			p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_ToolCallDelta{
				ToolCallDelta: &pb.ToolCallDelta{Id: id, Name: ev.Item.Name},
			}})
		}

	case "response.function_call_arguments.delta":
		id := ev.CallID
		if id == "" {
			id = p.toolIDs[ev.Index]
		}
		if id == "" {
			id = fmt.Sprintf("call_%d", ev.Index)
		}
		// argSeen 用 item id 计数：done 帧只带 item id，两边必须同键才能去重
		p.argSeen[firstNonEmpty(ev.ItemID, id)] = true
		p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_ToolCallDelta{
			ToolCallDelta: &pb.ToolCallDelta{
				Id:             id,
				Name:           p.toolNames[ev.Index],
				ArgumentsDelta: ev.Delta,
			},
		}})

	// 上游可能不按增量给参数，只在 done 里给全量：没走过增量就补发一次，
	// 否则工具调用会带着空 arguments 到客户端，工具直接执行失败。
	case "response.function_call_arguments.done":
		p.toolArgsDone(firstNonEmpty(ev.ItemID, ev.CallID), ev.Arguments)
	case "response.output_item.done":
		if ev.Item.Type == "function_call" {
			p.toolArgsDone(firstNonEmpty(ev.Item.ID, ev.Item.CallID), ev.Item.Arguments)
		}

	case "response.completed", "response.incomplete":
		if u := ev.Response.Usage; u != nil {
			p.usage = &pb.Usage{
				InputTokens:  u.InputTokens,
				OutputTokens: u.OutputTokens,
				CachedTokens: u.InputTokensDetails.CachedTokens,
			}
		}
		// 被 max_output_tokens 截断是「正常结束但内容不全」，不是失败：
		// 映射成 length，客户端据此决定是否续写。
		if ev.Type == "response.incomplete" && ev.Response.IncompleteDetails.Reason == "max_output_tokens" {
			p.pendingStop = "length"
		}
		p.finish()

	case "response.failed":
		msg := "上游返回失败事件"
		if ev.Response.Error != nil && ev.Response.Error.Message != "" {
			msg = ev.Response.Error.Message
		}
		p.FinishWithError(502, msg)

	case "error":
		msg := "上游返回错误事件"
		if ev.Error != nil && ev.Error.Message != "" {
			msg = ev.Error.Message
		}
		p.FinishWithError(502, msg)
	}
}

// toolArgsDone 只在「没走过参数增量」时补发一次全量参数。
func (p *Parser) toolArgsDone(itemID, args string) {
	if itemID == "" || args == "" || p.argSeen[itemID] {
		return
	}
	p.argSeen[itemID] = true
	p.sawTool = true
	// 增量帧带上调用 id（能查到映射时），保证信封侧归到同一个工具调用块
	callID := firstNonEmpty(p.callIDOf[itemID], itemID)
	p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_ToolCallDelta{
		ToolCallDelta: &pb.ToolCallDelta{Id: callID, ArgumentsDelta: args},
	}})
}

func (p *Parser) finish() {
	if p.sentFinish {
		return
	}
	p.sentFinish = true
	reason := p.pendingStop
	if reason == "" {
		reason = "stop"
		if p.sawTool {
			reason = "tool_calls"
		}
	}
	if !p.sentStart {
		p.sentStart = true
		p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_MessageStart{}})
	}
	p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
		MessageFinish: &pb.MessageFinish{FinishReason: reason, Usage: p.usage},
	}})
}

// Finish 上游流结束但没发 response.completed 时的兜底收尾。
func (p *Parser) Finish() { p.finish() }

// FinishWithError 上报失败事件（已收尾则忽略，避免同一个流出现两个终止事件）。
func (p *Parser) FinishWithError(code int32, message string) {
	if p.sentFinish {
		return
	}
	p.sentFinish = true
	p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_TaskFailed{
		TaskFailed: &pb.TaskFailed{Error: &pb.Error{Code: code, Message: message}},
	}})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ChatBody 把 CPH 请求转成 Responses API 请求体。
//
// 与核心 inbound 解析互为逆运算：
//   - system 消息 → instructions（Responses 无 system role）
//   - assistant 的 tool_calls → input 里的 function_call 项（call_id 对应）
//   - role=tool → function_call_output 项
//   - 多模态内容 → input_text / input_image 块
//   - reasoning_effort → reasoning.effort（Responses 方言没有顶层该字段）
func ChatBody(req *pb.ChatRequest) map[string]interface{} {
	var instructions []string
	items := []interface{}{}
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			if strings.TrimSpace(m.Text) != "" {
				instructions = append(instructions, m.Text)
			}
		case "tool":
			items = append(items, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": m.ToolCallId,
				"output":  textOf(m, "output_text"),
			})
		case "assistant":
			if text := textOf(m, "output_text"); text != "" {
				items = append(items, map[string]interface{}{
					"type": "message", "role": "assistant",
					"content": []interface{}{map[string]interface{}{"type": "output_text", "text": text}},
				})
			}
			for _, tc := range m.ToolCalls {
				items = append(items, map[string]interface{}{
					"type": "function_call", "call_id": tc.Id, "name": tc.Name, "arguments": tc.Arguments,
				})
			}
		default: // user 及其他
			role := m.Role
			if role == "" {
				role = "user"
			}
			items = append(items, map[string]interface{}{
				"type": "message", "role": role,
				"content": contentBlocks(m, "input_text"),
			})
		}
	}
	body := map[string]interface{}{
		"model":  req.Model,
		"stream": true,
		"input":  items,
	}
	if len(instructions) > 0 {
		body["instructions"] = strings.Join(instructions, "\n\n")
	}
	if req.MaxTokens > 0 {
		body["max_output_tokens"] = req.MaxTokens
	}
	// temperature 显式给出（含 0）优先于顶层字段
	if v := req.Extra["temperature"]; v != "" {
		body["temperature"] = rawJSON(v)
	} else if req.Temperature != 0 {
		body["temperature"] = req.Temperature
	}
	if v := req.Extra["top_p"]; v != "" {
		body["top_p"] = rawJSON(v)
	}
	if v := req.Extra["parallel_tool_calls"]; v != "" {
		body["parallel_tool_calls"] = rawJSON(v)
	}
	if v := req.Extra["user"]; v != "" {
		body["user"] = v
	}
	if v := req.Extra["reasoning_effort"]; v != "" {
		// Responses 方言只认 reasoning.effort；直接发顶层会被上游忽略/拒绝
		body["reasoning"] = map[string]interface{}{"effort": v}
	}
	if len(req.Tools) > 0 {
		tools := make([]interface{}, 0, len(req.Tools))
		for _, t := range req.Tools {
			tool := map[string]interface{}{
				"type": "function", "name": t.Name, "description": t.Description,
			}
			if strings.TrimSpace(t.ParametersSchema) != "" {
				var schema interface{}
				if json.Unmarshal([]byte(t.ParametersSchema), &schema) == nil {
					tool["parameters"] = schema
				}
			}
			tools = append(tools, tool)
		}
		body["tools"] = tools
		if tc := req.ToolChoice; tc != nil {
			switch tc.Type {
			case "auto", "none", "required":
				body["tool_choice"] = tc.Type
			case "tool", "function":
				body["tool_choice"] = map[string]interface{}{"type": "function", "name": tc.ToolName}
			}
		}
	}
	return body
}

// rawJSON 解析 extra 里的原始 JSON（数字/布尔），失败则按字符串下发。
func rawJSON(s string) interface{} {
	var v interface{}
	if json.Unmarshal([]byte(s), &v) == nil {
		return v
	}
	return s
}

// contentBlocks 优先用客户端原始多模态块（ContentJson），否则包成文本块。
func contentBlocks(m *pb.EnvelopeMessage, textType string) []interface{} {
	if len(m.ContentJson) > 0 {
		var parts []map[string]interface{}
		if json.Unmarshal(m.ContentJson, &parts) == nil && len(parts) > 0 {
			out := make([]interface{}, 0, len(parts))
			for _, p := range parts {
				switch fmt.Sprint(p["type"]) {
				case "text", "input_text", "output_text":
					out = append(out, map[string]interface{}{"type": textType, "text": fmt.Sprint(p["text"])})
				case "image_url":
					if img, ok := p["image_url"].(map[string]interface{}); ok {
						out = append(out, map[string]interface{}{
							"type": "input_image", "image_url": fmt.Sprint(img["url"]),
						})
					}
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return []interface{}{map[string]interface{}{"type": textType, "text": m.Text}}
}

func textOf(m *pb.EnvelopeMessage, textType string) string {
	if strings.TrimSpace(m.Text) != "" {
		return m.Text
	}
	if len(m.ContentJson) > 0 {
		var parts []map[string]interface{}
		if json.Unmarshal(m.ContentJson, &parts) == nil {
			var sb strings.Builder
			for _, p := range parts {
				sb.WriteString(fmt.Sprint(p["text"]))
			}
			if sb.Len() > 0 {
				return sb.String()
			}
		}
	}
	return ""
}
