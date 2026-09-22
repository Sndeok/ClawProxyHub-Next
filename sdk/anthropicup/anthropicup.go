// Package anthropicup — Anthropic 兼容上游的通用适配：统一信封 ↔ messages 协议。
package anthropicup

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// ChatBody 信封请求 → Anthropic Messages 请求体（stream=true）。
func ChatBody(req *pb.ChatRequest) map[string]interface{} {
	var system string
	var messages []map[string]interface{}
	for _, m := range req.Messages {
		switch {
		case m.Role == "system":
			system += m.Text
		case m.Role == "tool":
			// 工具结果以 tool_result 块包进 user 消息；多模态工具输出保留 content blocks。
			resultContent := interface{}(m.Text)
			if len(m.ContentJson) > 0 {
				resultContent = anthropicContent(m.ContentJson, m.Text)
			}
			messages = append(messages, map[string]interface{}{
				"role": "user",
				"content": []interface{}{map[string]interface{}{
					"type": "tool_result", "tool_use_id": m.ToolCallId, "content": resultContent,
				}},
			})
		case m.Role == "assistant" && len(m.ToolCalls) > 0:
			blocks := anthropicContent(m.ContentJson, m.Text)
			if len(m.ContentJson) == 0 && m.Text == "" {
				blocks = nil
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, map[string]interface{}{
					"type": "tool_use", "id": tc.Id, "name": tc.Name,
					"input": rawJSON(tc.Arguments),
				})
			}
			messages = append(messages, map[string]interface{}{"role": "assistant", "content": blocks})
		default:
			messages = append(messages, map[string]interface{}{
				"role":    m.Role,
				"content": anthropicContent(m.ContentJson, m.Text),
			})
		}
	}
	body := map[string]interface{}{
		"model":      req.Model,
		"messages":   messages,
		"max_tokens": orInt(req.MaxTokens, 8192),
		"stream":     true,
	}
	if system != "" {
		body["system"] = system
	}
	if len(req.Tools) > 0 {
		var tools []interface{}
		for _, t := range req.Tools {
			tools = append(tools, map[string]interface{}{
				"name": t.Name, "description": t.Description,
				"input_schema": rawJSON(orDefault(t.ParametersSchema, `{"type":"object"}`)),
			})
		}
		body["tools"] = tools
		if tc := req.ToolChoice; tc != nil {
			switch tc.Type {
			case "auto":
				body["tool_choice"] = map[string]interface{}{"type": "auto"}
			case "none":
				body["tool_choice"] = map[string]interface{}{"type": "none"}
			case "tool":
				body["tool_choice"] = map[string]interface{}{"type": "tool", "name": tc.ToolName}
			}
		}
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	// 显式 temperature（含 0）优先；top_k / metadata.user_id 同为 Anthropic 原生字段
	if v, ok := req.Extra["temperature"]; ok && v != "" {
		body["temperature"] = jsonNumber(v)
	}
	if v, ok := req.Extra["top_k"]; ok && v != "" {
		body["top_k"] = jsonNumber(v)
	}
	if v, ok := req.Extra["user"]; ok && v != "" {
		body["metadata"] = map[string]interface{}{"user_id": v}
	}
	return body
}

// anthropicContent 把 OpenAI 风格的 content_json 转成 Anthropic content blocks。
// 纯文本/老插件路径仍返回单个 text block；未知类型原样保留，避免静默丢数据。
func anthropicContent(raw []byte, fallback string) []interface{} {
	if len(raw) == 0 {
		return []interface{}{map[string]interface{}{"type": "text", "text": fallback}}
	}
	var parts []map[string]interface{}
	if json.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
		return []interface{}{map[string]interface{}{"type": "text", "text": fallback}}
	}
	out := make([]interface{}, 0, len(parts))
	for _, p := range parts {
		switch p["type"] {
		case "text", "input_text", "output_text":
			out = append(out, map[string]interface{}{"type": "text", "text": stringValue(p["text"])})
		case "image_url":
			url := ""
			if v, ok := p["image_url"].(map[string]interface{}); ok {
				url = stringValue(v["url"])
			} else {
				url = stringValue(p["image_url"])
			}
			out = append(out, anthropicImageBlock(url))
		case "file":
			out = append(out, anthropicFileBlock(p["file"]))
		default:
			out = append(out, p)
		}
	}
	return out
}

func anthropicImageBlock(url string) map[string]interface{} {
	if strings.HasPrefix(url, "data:") {
		header, data := splitDataURL(url)
		return map[string]interface{}{"type": "image", "source": map[string]interface{}{
			"type": "base64", "media_type": header, "data": data,
		}}
	}
	return map[string]interface{}{"type": "image", "source": map[string]interface{}{
		"type": "url", "url": url,
	}}
}

func anthropicFileBlock(value interface{}) map[string]interface{} {
	file, _ := value.(map[string]interface{})
	data := stringValue(file["file_data"])
	if data == "" {
		data = stringValue(file["data"])
	}
	if strings.HasPrefix(data, "data:") {
		media, encoded := splitDataURL(data)
		return map[string]interface{}{"type": "document", "source": map[string]interface{}{
			"type": "base64", "media_type": media, "data": encoded,
		}}
	}
	if url := stringValue(file["file_url"]); url != "" {
		return map[string]interface{}{"type": "document", "source": map[string]interface{}{
			"type": "url", "url": url,
		}}
	}
	return map[string]interface{}{"type": "document", "source": map[string]interface{}{
		"type": "text", "media_type": "text/plain", "data": data,
	}}
}

func splitDataURL(raw string) (media, data string) {
	const prefix = "data:"
	if !strings.HasPrefix(raw, prefix) {
		return "application/octet-stream", raw
	}
	parts := strings.SplitN(strings.TrimPrefix(raw, prefix), ",", 2)
	if len(parts) != 2 {
		return "application/octet-stream", raw
	}
	media = strings.TrimSuffix(parts[0], ";base64")
	data = parts[1]
	if decoded, err := base64.StdEncoding.DecodeString(data); err == nil {
		data = base64.StdEncoding.EncodeToString(decoded)
	}
	return media, data
}

func stringValue(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	if value == nil {
		return ""
	}
	b, _ := json.Marshal(value)
	return string(b)
}

// Parser 把上游 Anthropic SSE 行解析为信封事件。
type Parser struct {
	emit       func(*pb.StreamEvent)
	blocks     map[int]blockInfo // content block index → 身份
	nextToolID int
	pending    anthropicUsage
	sentFinish bool
}

// anthropicUsage 上游用量原始字段。
// 注意 Anthropic 语义：input_tokens **不含**缓存命中/写入部分，
// 完整输入 = input_tokens + cache_read_input_tokens + cache_creation_input_tokens。
// 信封（pb.Usage）统一用「含缓存的完整输入」，换算在 finish() 里做一次。
type anthropicUsage struct {
	input       int64
	cacheRead   int64
	cacheCreate int64
	output      int64
}

type blockInfo struct {
	kind string // text / tool_use
	id   string
	name string
}

func NewParser(emit func(*pb.StreamEvent)) *Parser {
	return &Parser{emit: emit, blocks: map[int]blockInfo{}}
}

// Feed 处理一行（"event: xxx" 与 "data: {...}"）。
func (p *Parser) Feed(line string) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" {
		return
	}
	var ev struct {
		Type    string `json:"type"`
		Index   int    `json:"index"`
		Message struct {
			Model string `json:"model"`
			Usage struct {
				InputTokens              int64 `json:"input_tokens"`
				CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
		ContentBlock struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
			Text  string          `json:"text"`
		} `json:"content_block"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
			StopReason  string `json:"stop_reason"`
		} `json:"delta"`
		Usage struct {
			OutputTokens             int64 `json:"output_tokens"`
			InputTokens              int64 `json:"input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return
	}
	switch ev.Type {
	case "message_start":
		// message_start 携带输入侧用量（含缓存命中/写入），先挂起，
		// 等 message_delta / message_stop 拿到 output_tokens 后一起上报。
		p.pending.merge(anthropicUsage{
			input:       ev.Message.Usage.InputTokens,
			cacheRead:   ev.Message.Usage.CacheReadInputTokens,
			cacheCreate: ev.Message.Usage.CacheCreationInputTokens,
		})
		p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_MessageStart{
			MessageStart: &pb.MessageStart{Model: ev.Message.Model},
		}})
	case "content_block_start":
		p.blocks[ev.Index] = blockInfo{kind: ev.ContentBlock.Type, id: ev.ContentBlock.ID, name: ev.ContentBlock.Name}
	case "content_block_delta":
		switch ev.Delta.Type {
		case "text_delta":
			p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_ContentDelta{
				ContentDelta: &pb.ContentDelta{Text: ev.Delta.Text},
			}})
		case "input_json_delta":
			info := p.blocks[ev.Index]
			p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_ToolCallDelta{
				ToolCallDelta: &pb.ToolCallDelta{
					Id: info.id, Name: info.name, ArgumentsDelta: ev.Delta.PartialJSON,
				},
			}})
		}
	case "message_delta":
		// stop_reason + output_tokens 通常都在这里；部分上游会在 message_delta 里再带一次
		// 输入侧用量（Anthropic 官方在新版本里会补 input_tokens / cache_read_input_tokens）。
		p.pending.merge(anthropicUsage{
			input:       ev.Usage.InputTokens,
			cacheRead:   ev.Usage.CacheReadInputTokens,
			cacheCreate: ev.Usage.CacheCreationInputTokens,
			output:      ev.Usage.OutputTokens,
		})
		if ev.Delta.StopReason != "" {
			p.finish(mapStop(ev.Delta.StopReason))
		}
	case "message_stop":
		p.finish("stop")
	}
}

// Finish 流结束兜底。
func (p *Parser) Finish() {
	if !p.sentFinish {
		p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
			MessageFinish: &pb.MessageFinish{FinishReason: "stop"},
		}})
	}
}

// FinishWithError 流异常结束：发失败事件。
func (p *Parser) FinishWithError(code int32, message string) {
	p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_TaskFailed{
		TaskFailed: &pb.TaskFailed{Error: &pb.Error{Code: code, Message: message}},
	}})
}

func (p *Parser) finish(reason string) {
	if p.sentFinish {
		return
	}
	p.sentFinish = true
	if reason == "tool_use" {
		// 信封语义用 tool_calls
		reason = "tool_calls"
	}
	p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
		MessageFinish: &pb.MessageFinish{
			FinishReason: reason,
			Usage:        p.pending.envelope(),
		},
	}})
}

// merge 合并两次上游用量快照，取每个字段的较大值（上游分片上报，非累加语义）。
func (u *anthropicUsage) merge(src anthropicUsage) {
	if src.input > u.input {
		u.input = src.input
	}
	if src.cacheRead > u.cacheRead {
		u.cacheRead = src.cacheRead
	}
	if src.cacheCreate > u.cacheCreate {
		u.cacheCreate = src.cacheCreate
	}
	if src.output > u.output {
		u.output = src.output
	}
}

// envelope 换算成信封口径：input_tokens 含缓存部分，cached_tokens 是其中的命中子集。
func (u anthropicUsage) envelope() *pb.Usage {
	return &pb.Usage{
		InputTokens:  u.input + u.cacheRead + u.cacheCreate,
		OutputTokens: u.output,
		CachedTokens: u.cacheRead,
		// 写入与命中分列上报：Anthropic 单独计费 cache_creation，混进命中会算错账
		CacheCreationTokens: u.cacheCreate,
	}
}

// ---------- 工具 ----------

// jsonNumber 数字字符串 → 数字；解析失败返回 0（绝不把 "0.7" 当字符串发给上游）。
func jsonNumber(s string) interface{} {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return 0
}

func rawJSON(s string) interface{} {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return map[string]interface{}{}
	}
	return v
}

func mapStop(reason string) string {
	switch reason {
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	default:
		return "stop"
	}
}

func orInt(v, def int32) int32 {
	if v > 0 {
		return v
	}
	return def
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
