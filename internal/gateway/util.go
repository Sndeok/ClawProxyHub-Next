// util.go — gateway 内部共用的小工具。
package gateway

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// setTemperature 显式给出的 temperature 记进 Extra（含 0）：插件据此区分
// 「客户端没传」与「客户端明确要求 0」——后者在代码生成场景很常见。
func setTemperature(req *pb.ChatRequest, t *float64) {
	if t != nil {
		req.Extra["temperature"] = strconv.FormatFloat(*t, 'g', -1, 64)
	}
}

// maxLoggedBody 请求原文 / 上游返回的落库上限（超出截断并标注）。
const maxLoggedBody = 8 << 10

// ctxKeyRawBody 请求原文在 context 里传递的 key（parseBody 读体后写入，serve 组装日志时取）。
type ctxKeyRawBody struct{}

// clipBody 截断到 maxLoggedBody，并标注被截断的字节数。
func clipBody(s string) string {
	if len(s) <= maxLoggedBody {
		return s
	}
	return s[:maxLoggedBody] + fmt.Sprintf("\n…[truncated %d bytes]", len(s)-maxLoggedBody)
}

// rawBodyOf 从请求上下文取原文（没有则空串）。
func rawBodyOf(r *http.Request) string {
	if v, ok := r.Context().Value(ctxKeyRawBody{}).(string); ok {
		return v
	}
	return ""
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

// normalizeRole 把 developer 角色归一为 system（上游多不认 developer）。
func normalizeRole(role string) string {
	if role == "developer" {
		return "system"
	}
	return role
}

// extractText 从 string / blocks 数组 / nil 里提取文本拼接。
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var texts []string
		for _, b := range blocks {
			if b.Type == "text" || b.Type == "input_text" || b.Type == "output_text" {
				texts = append(texts, b.Text)
			}
		}
		return joinTexts(texts)
	}
	return ""
}

func isArray(raw json.RawMessage) bool {
	for _, c := range raw {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		return c == '['
	}
	return false
}

// compactJSON 压掉无关空白，保证 Arguments 是规范 JSON 文本。
func compactJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

func joinTexts(texts []string) string {
	out := ""
	for i, t := range texts {
		if i > 0 {
			out += "\n"
		}
		out += t
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedValues[V int](m map[string]V) []V {
	vals := make([]V, 0, len(m))
	for _, v := range m {
		vals = append(vals, v)
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	return vals
}
