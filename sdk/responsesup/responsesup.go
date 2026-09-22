// Package responsesup —— 上游 OpenAI Responses API（/v1/responses）SSE → CPH 事件。
//
// 与 openaiup / anthropicup 对称：插件把客户端请求转成 Responses 请求体，
// 再把上游 SSE 逐行喂给本解析器，得到统一的 CPH StreamEvent。
//
// 覆盖事件（与核心 gateway/responses.go 输出的方言一致）：
//
//	response.created                      → MessageStart
//	response.output_text.delta            → ContentDelta
//	response.output_item.added (function) → ToolCallDelta（首帧带 id/name）
//	response.function_call_arguments.delta→ ToolCallDelta（参数增量）
//	response.completed                    → MessageFinish（含 usage）
//	response.failed / error / incomplete  → FinishWithError
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
	// 工具调用身份按 output_index 记忆：上游首帧给 id/name，后续帧只给参数增量
	toolIDs   map[int]string
	toolNames map[int]string
}

// NewParser 创建解析器；emit 为 CPH 事件出口。
func NewParser(emit func(*pb.StreamEvent)) *Parser {
	return &Parser{emit: emit, toolIDs: map[int]string{}, toolNames: map[int]string{}}
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
			id := ev.Item.CallID
			if id == "" {
				id = ev.Item.ID
			}
			if id == "" {
				id = fmt.Sprintf("call_%d", ev.Index)
			}
			p.toolIDs[ev.Index] = id
			p.toolNames[ev.Index] = ev.Item.Name
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
		p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_ToolCallDelta{
			ToolCallDelta: &pb.ToolCallDelta{
				Id:             id,
				Name:           p.toolNames[ev.Index],
				ArgumentsDelta: ev.Delta,
			},
		}})

	case "response.completed", "response.done":
		var usage *pb.Usage
		if ev.Response.Usage != nil {
			usage = &pb.Usage{
				InputTokens:  ev.Response.Usage.InputTokens,
				OutputTokens: ev.Response.Usage.OutputTokens,
				CachedTokens: ev.Response.Usage.InputTokensDetails.CachedTokens,
			}
		}
		p.finish(usage)

	case "response.failed", "response.incomplete", "error":
		msg := "上游返回失败事件"
		if ev.Response.Error != nil && ev.Response.Error.Message != "" {
			msg = ev.Response.Error.Message
		}
		if ev.Error != nil && ev.Error.Message != "" {
			msg = ev.Error.Message
		}
		p.FinishWithError(502, msg)
	}
}

func (p *Parser) finish(u *pb.Usage) {
	if p.sentFinish {
		return
	}
	p.sentFinish = true
	fin := &pb.MessageFinish{FinishReason: p.pendingStop}
	if fin.FinishReason == "" {
		fin.FinishReason = "stop"
	}
	if u != nil {
		fin.Usage = u
	}
	if !p.sentStart {
		p.sentStart = true
		p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_MessageStart{}})
	}
	p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{MessageFinish: fin}})
}

// Finish 上游流结束但没发 response.completed 时的兜底收尾。
func (p *Parser) Finish() { p.finish(nil) }

// FinishWithError 上报失败事件。
func (p *Parser) FinishWithError(code int32, message string) {
	p.sentFinish = true
	p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_TaskFailed{
		TaskFailed: &pb.TaskFailed{Error: &pb.Error{Code: code, Message: message}},
	}})
}

// ChatBody 把 CPH 请求转成 Responses API 请求体。
//
// 与核心 inbound 解析互为逆运算：
//   - system 消息 → instructions（Responses 无 system role）
//   - assistant 的 tool_calls → input 里的 function_call 项（call_id 对应）
//   - role=tool → function_call_output 项
//   - 多模态内容 → input_text / input_image 块
func ChatBody(req *pb.ChatRequest) map[string]interface{} {
	var instructions string
	items := []interface{}{}
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			if strings.TrimSpace(m.Text) != "" {
				if instructions != "" {
					instructions += "\n\n"
				}
				instructions += m.Text
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
	if instructions != "" {
		body["instructions"] = instructions
	}
	if req.MaxTokens > 0 {
		body["max_output_tokens"] = req.MaxTokens
	}
	if req.Temperature != 0 {
		body["temperature"] = req.Temperature
	}
	if v := req.Extra["top_p"]; v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			body["top_p"] = f
		}
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
