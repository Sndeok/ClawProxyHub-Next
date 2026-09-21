// content.go — Responses / Chat / Anthropic 内容块统一归一化。
//
// EnvelopeMessage.Text 保留纯文本兼容路径；ContentJson 是向后兼容新增的多模态
// JSON 数组字段，采用 OpenAI Chat content-part 形状，供新版本插件适配器使用。
// 老插件/旧二进制忽略未知 protobuf 字段，纯文本行为不变。
package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
)

// normalizeResponsesContent 把 Responses content 块归一为 OpenAI Chat 风格数组。
// 纯字符串返回 nil（继续走 Text），数组/对象返回规范化 JSON。
func normalizeResponsesContent(raw json.RawMessage) []byte {
	return normalizeContent(raw, "responses")
}

// normalizeOpenAIContent 规范化 Chat Completions 的 content 数组。
func normalizeOpenAIContent(raw json.RawMessage) []byte {
	return normalizeContent(raw, "openai")
}

// normalizeAnthropicContent 规范化 Anthropic content blocks。
func normalizeAnthropicContent(raw json.RawMessage) []byte {
	return normalizeContent(raw, "anthropic")
}

func normalizeContent(raw json.RawMessage, source string) []byte {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || (strings.HasPrefix(trimmed, `"`) && jsonText(raw) != "") {
		return nil
	}
	var values []any
	if strings.HasPrefix(trimmed, "{") {
		var one any
		if json.Unmarshal(raw, &one) != nil {
			return nil
		}
		values = []any{one}
	} else if strings.HasPrefix(trimmed, "[") {
		if json.Unmarshal(raw, &values) != nil {
			return nil
		}
	} else {
		return nil
	}

	parts := make([]map[string]any, 0, len(values))
	for _, value := range values {
		part, ok := normalizeContentPart(value, source)
		if ok {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	out, err := json.Marshal(parts)
	if err != nil {
		return nil
	}
	return out
}

func normalizeContentPart(value any, source string) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	if !ok {
		if text, ok := value.(string); ok {
			return map[string]any{"type": "text", "text": text}, true
		}
		return map[string]any{"type": "text", "text": fmt.Sprint(value)}, true
	}
	typ := strings.TrimSpace(stringValue(m["type"]))
	switch typ {
	case "tool_use", "tool_result":
		// 工具调用/结果由 EnvelopeMessage.ToolCalls / role=tool 表达，避免重复历史。
		return nil, false
	case "text", "input_text", "output_text":
		return map[string]any{"type": "text", "text": stringValue(m["text"])}, true
	case "image_url":
		return map[string]any{"type": "image_url", "image_url": imageURLValue(m)}, true
	case "input_image":
		return map[string]any{"type": "image_url", "image_url": map[string]any{
			"url": firstString(m["image_url"], m["url"], m["data"]),
		}}, true
	case "image": // Anthropic image block
		if src, ok := m["source"].(map[string]any); ok {
			if srcType := stringValue(src["type"]); srcType == "base64" {
				media := stringValue(src["media_type"])
				data := stringValue(src["data"])
				return map[string]any{"type": "image_url", "image_url": map[string]any{
					"url": "data:" + media + ";base64," + data,
				}}, true
			}
			if url := firstString(src["url"], src["data"]); url != "" {
				return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}, true
			}
		}
	case "input_file":
		file := m["file"]
		if file == nil {
			file = map[string]any{"file_data": firstString(m["file_data"], m["file_url"], m["url"])}
		}
		return map[string]any{"type": "file", "file": file}, true
	case "document": // Anthropic document block
		if src, ok := m["source"].(map[string]any); ok {
			file := map[string]any{}
			if stringValue(src["type"]) == "base64" {
				file["file_data"] = "data:" + stringValue(src["media_type"]) + ";base64," + stringValue(src["data"])
			} else {
				file["file_url"] = firstString(src["url"], src["data"])
			}
			return map[string]any{"type": "file", "file": file}, true
		}
	case "input_audio":
		return map[string]any{"type": "input_audio", "input_audio": m["input_audio"]}, true
	case "input_video":
		return map[string]any{"type": "video_url", "video_url": m["video_url"]}, true
	case "video_url":
		return m, true
	case "file":
		return m, true
	}

	// 未知块原样保留，避免未来 Responses 新增 content type 时静默丢数据。
	return m, true
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func imageURLValue(part map[string]any) map[string]any {
	if value, ok := part["image_url"].(map[string]any); ok {
		return value
	}
	if value := firstString(part["image_url"], part["url"], part["data"]); value != "" {
		return map[string]any{"url": value}
	}
	return map[string]any{"url": ""}
}
func firstString(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		if m, ok := v.(map[string]any); ok {
			if s := firstString(m["url"], m["file_data"], m["file_url"], m["data"]); s != "" {
				return s
			}
		}
	}
	return ""
}
