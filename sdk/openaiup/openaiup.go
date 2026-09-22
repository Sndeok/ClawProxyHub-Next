// Package openaiup — OpenAI 兼容上游的通用适配：统一信封 ↔ chat/completions。
// 大多数 Claw 类上游都讲 OpenAI 协议，插件作者复用本包即可只写差异部分。
package openaiup

import (
	"encoding/json"
	"fmt"
	"strings"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// ChatBody 信封请求 → OpenAI chat/completions 请求体。
// 始终 stream=true 并带 include_usage：上游聚合与非聚合都由事件流表达。
func ChatBody(req *pb.ChatRequest) map[string]interface{} {
	var messages []map[string]interface{}
	for _, m := range req.Messages {
		content := interface{}(m.Text)
		if len(m.ContentJson) > 0 {
			content = rawJSON(string(m.ContentJson))
		}
		msg := map[string]interface{}{"role": m.Role, "content": content}
		if len(m.ToolCalls) > 0 {
			if m.Role == "assistant" && len(m.ContentJson) == 0 {
				msg["content"] = nilIfEmpty(m.Text)
				var tcs []map[string]interface{}
				for _, tc := range m.ToolCalls {
					tcs = append(tcs, map[string]interface{}{
						"id": tc.Id, "type": "function",
						"function": map[string]interface{}{
							"name": tc.Name, "arguments": tc.Arguments,
						},
					})
				}
				msg["tool_calls"] = tcs
			}
		}
		if m.ToolCallId != "" {
			msg["tool_call_id"] = m.ToolCallId
		}
		messages = append(messages, msg)
	}
	body := map[string]interface{}{
		"model":          req.Model,
		"messages":       messages,
		"stream":         true,
		"stream_options": map[string]interface{}{"include_usage": true},
	}
	if len(req.Tools) > 0 {
		var tools []map[string]interface{}
		for _, t := range req.Tools {
			tools = append(tools, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name": t.Name, "description": t.Description,
					"parameters": rawJSON(t.ParametersSchema),
				},
			})
		}
		body["tools"] = tools
		if tc := req.ToolChoice; tc != nil {
			switch tc.Type {
			case "auto", "none":
				body["tool_choice"] = tc.Type
			case "tool":
				body["tool_choice"] = map[string]interface{}{
					"type": "function", "function": map[string]interface{}{"name": tc.ToolName},
				}
			}
		}
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if v, ok := req.Extra["top_p"]; ok && v != "" {
		body["top_p"] = jsonNumber(v)
	}
	if v, ok := req.Extra["stop"]; ok && v != "" {
		body["stop"] = rawJSON(v)
	}
	if v, ok := req.Extra["reasoning_effort"]; ok && v != "" {
		body["reasoning_effort"] = v
	}
	// 显式 temperature（含 0）：客户端传了就以它为准，覆盖信封的浮点字段
	if v, ok := req.Extra["temperature"]; ok && v != "" {
		body["temperature"] = jsonNumber(v)
	}
	// 采样/控制参数原样透传（值在核心侧已校验为合法 JSON 片段）
	for _, k := range []string{"frequency_penalty", "presence_penalty", "seed", "parallel_tool_calls", "response_format"} {
		if v, ok := req.Extra[k]; ok && v != "" {
			body[k] = rawJSON(v)
		}
	}
	if v, ok := req.Extra["user"]; ok && v != "" {
		body["user"] = v
	}
	return body
}

// openAIUsage 覆盖 OpenAI Chat/Responses 常见的缓存 token 与积分字段。
// 积分字段各家中转命名不统一，这里把常见写法都收进来，取第一个非零值。
type openAIUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	CachedTokens     int64 `json:"cached_tokens"`
	// 缓存写入：各家命名不统一（Anthropic 系 cache_creation_input_tokens、
	// OpenAI 系 cache_write_tokens / cached_write_tokens），取最大值去重。
	CacheWriteTokens    int64   `json:"cache_write_tokens"`
	CachedWriteTokens   int64   `json:"cached_write_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_input_tokens"`
	CreditsUsed         float64 `json:"credits_used"`
	CreditUsed          float64 `json:"credit_used"`
	Credits             float64 `json:"credits"`
	CreditsConsumed     float64 `json:"credits_consumed"`
	// Raw 保留整段 usage JSON：中转站对「积分」的命名五花八门，
	// 固定字段匹配不到时用 fuzzyCredit 按名字兜底。
	Raw           map[string]json.RawMessage `json:"-"`
	PromptDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
		CacheRead    int64 `json:"cache_read_input_tokens"`
		CacheWrite   int64 `json:"cache_write_tokens"`
		CachedWrite  int64 `json:"cached_write_tokens"`
		CacheCreate  int64 `json:"cache_creation_input_tokens"`
	} `json:"prompt_tokens_details"`
	InputDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
		CacheRead    int64 `json:"cache_read_input_tokens"`
		CacheWrite   int64 `json:"cache_write_tokens"`
		CachedWrite  int64 `json:"cached_write_tokens"`
		CacheCreate  int64 `json:"cache_creation_input_tokens"`
	} `json:"input_tokens_details"`
}

// Parser 把上游 OpenAI SSE 行解析为信封事件。
// 用法：每读一行调 Feed；流结束时调 Finish 把挂起的 finish_reason 落地。
type Parser struct {
	emit        func(*pb.StreamEvent)
	pendingStop string
	toolIDs     map[int]string // tool_calls index → 该调用的 id
	toolNames   map[int]string // tool_calls index → 该调用的 name
	sentFinish  bool
}

func NewParser(emit func(*pb.StreamEvent)) *Parser {
	return &Parser{emit: emit, toolIDs: map[int]string{}, toolNames: map[int]string{}}
}

// Feed 处理一行（"data: {...}" 或 "data: [DONE]"）。
func (p *Parser) Feed(line string) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return
	}
	var chunk struct {
		Choices []struct {
			Delta struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage *openAIUsage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return
	}
	if chunk.Usage != nil {
		// 单独再解一次原始 usage，供模糊匹配积分字段用
		var env struct {
			Usage map[string]json.RawMessage `json:"usage"`
		}
		if json.Unmarshal([]byte(payload), &env) == nil {
			chunk.Usage.Raw = env.Usage
		}
	}
	for _, c := range chunk.Choices {
		if c.Delta.Content != "" {
			p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_ContentDelta{
				ContentDelta: &pb.ContentDelta{Text: c.Delta.Content},
			}})
		}
		for _, tc := range c.Delta.ToolCalls {
			// 上游只在首块带 id/name，后续 arguments 增量两者皆空。信封侧按 id
			// 分组，因此这里按 index 记住身份并在每次增量补齐——否则同一调用会
			// 被拆成「空 id 的新块」，客户端拿到残缺的 tool_calls，工具不会执行。
			if tc.ID != "" {
				p.toolIDs[tc.Index] = tc.ID
			}
			if tc.Function.Name != "" {
				p.toolNames[tc.Index] = tc.Function.Name
			}
			id := p.toolIDs[tc.Index]
			if id == "" {
				id = fmt.Sprintf("call_%d", tc.Index) // 上游从未给 id 时兜底，保证非空稳定
			}
			p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_ToolCallDelta{
				ToolCallDelta: &pb.ToolCallDelta{
					Id:             id,
					Name:           p.toolNames[tc.Index],
					ArgumentsDelta: tc.Function.Arguments,
				},
			}})
		}
		if c.FinishReason != nil && *c.FinishReason != "" {
			p.pendingStop = *c.FinishReason
			// usage 一般随终止块或其后的 usage 块到达；若已到则立即收尾
			if chunk.Usage != nil {
				p.finish(chunk.Usage)
			}
		}
	}
	if chunk.Usage != nil {
		p.finish(chunk.Usage)
	}
}

// Finish 流结束：把挂起的 finish_reason 落地（从未发过时补一个空 usage 的收尾）。
func (p *Parser) Finish() {
	if !p.sentFinish {
		p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
			MessageFinish: &pb.MessageFinish{FinishReason: orDefault(p.pendingStop, "stop")},
		}})
	}
}

// FinishWithError 流异常结束：发失败事件。
func (p *Parser) FinishWithError(code int32, message string) {
	p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_TaskFailed{
		TaskFailed: &pb.TaskFailed{Error: &pb.Error{Code: code, Message: message}},
	}})
}

func (p *Parser) finish(u *openAIUsage) {
	if p.sentFinish {
		return
	}
	p.sentFinish = true
	// 各家中转对「缓存命中」的字段命名不同，是同一数字的别名而不是可累加的分项，
	// 因此取最大值（相加会把同一份命中算成两倍）。
	cached := u.CachedTokens
	if u.PromptDetails != nil {
		cached = maxInt64(cached, u.PromptDetails.CachedTokens, u.PromptDetails.CacheRead)
	}
	if u.InputDetails != nil {
		cached = maxInt64(cached, u.InputDetails.CachedTokens, u.InputDetails.CacheRead)
	}
	if cached > u.PromptTokens {
		cached = u.PromptTokens // 命中是输入的子集，上游给歪了也不能超过输入
	}
	// 缓存写入同样是输入的组成部分，别名取最大值去重
	write := maxInt64(u.CacheWriteTokens, u.CachedWriteTokens, u.CacheCreationTokens)
	if u.PromptDetails != nil {
		write = maxInt64(write, u.PromptDetails.CacheWrite, u.PromptDetails.CachedWrite, u.PromptDetails.CacheCreate)
	}
	if u.InputDetails != nil {
		write = maxInt64(write, u.InputDetails.CacheWrite, u.InputDetails.CachedWrite, u.InputDetails.CacheCreate)
	}
	if write > u.PromptTokens {
		write = u.PromptTokens
	}
	p.emit(&pb.StreamEvent{Event: &pb.StreamEvent_MessageFinish{
		MessageFinish: &pb.MessageFinish{
			FinishReason: orDefault(p.pendingStop, "stop"),
			Usage: &pb.Usage{
				InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens,
				CachedTokens: cached, CacheCreationTokens: write, CreditUsed: creditOf(u),
			},
		},
	}})
}

// maxInt64 取多个 int64 的最大值。
func maxInt64(vals ...int64) int64 {
	var out int64
	for _, v := range vals {
		if v > out {
			out = v
		}
	}
	return out
}

// creditOf 取上游 usage 里的积分消耗（各家中转命名不一，取第一个非零）。
// 固定字段都没有时，退回按名字模糊匹配（creditsUsed / 扣费 / 额度扣减 之类）。
func creditOf(u *openAIUsage) float64 {
	for _, v := range []float64{u.CreditsUsed, u.CreditUsed, u.Credits, u.CreditsConsumed} {
		if v != 0 {
			return v
		}
	}
	return fuzzyCredit(u.Raw, 0)
}

// fuzzyCredit 在 usage JSON 里按字段名找积分消耗：
// 名字含 credit/point 且含 used/consum/cost/spent/deduct 的优先，其次任何 credit/point 数字。
// 只认正整数/浮点，负值（充值、退款）忽略。递归深度限制 2 层（details 子对象）。
func fuzzyCredit(raw map[string]json.RawMessage, depth int) float64 {
	if len(raw) == 0 || depth > 2 {
		return 0
	}
	best, fallback := 0.0, 0.0
	for k, v := range raw {
		name := strings.ToLower(k)
		var nested map[string]json.RawMessage
		if json.Unmarshal(v, &nested) == nil && nested != nil {
			if got := fuzzyCredit(nested, depth+1); got > best {
				best = got
			}
			continue
		}
		if !strings.Contains(name, "credit") && !strings.Contains(name, "point") && !strings.Contains(name, "quota") {
			continue
		}
		var f float64
		if json.Unmarshal(v, &f) != nil || f <= 0 {
			continue
		}
		if strings.Contains(name, "remain") || strings.Contains(name, "balance") ||
			strings.Contains(name, "limit") || strings.Contains(name, "total") ||
			strings.Contains(name, "left") {
			continue // 余额类字段不是本次消耗
		}
		if strings.Contains(name, "used") || strings.Contains(name, "consum") ||
			strings.Contains(name, "cost") || strings.Contains(name, "spent") ||
			strings.Contains(name, "deduct") || strings.Contains(name, "charge") {
			if f > best {
				best = f
			}
			continue
		}
		if f > fallback {
			fallback = f
		}
	}
	if best > 0 {
		return best
	}
	return fallback
}

// ---------- 工具 ----------

func rawJSON(s string) interface{} {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	return v
}

func jsonNumber(s string) interface{} {
	var v json.Number
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	return s
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
