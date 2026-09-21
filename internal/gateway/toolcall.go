// toolcall.go — 工具调用增量分组（openai / anthropic / responses 三个编码器共用）。
//
// 统一信封的 ToolCallDelta 只有 id / name / arguments_delta 三个字段，没有 index。
// 而 OpenAI 方言的上游只在某个 tool call 的首块带 id，后续 arguments 增量 id 为空
// （sdk/openaiup 旧版本会把空 id 原样透传）。编码器若直接按 id 建块，同一调用会被
// 拆成两个块：先开一个只有 id+name 的空块，再把所有 arguments 记到一个 id 为空的新块上。
// 客户端（new-api / Codex CLI）据此拿不到完整的 function_call，工具永远不会被执行，
// 表现为「一般对话正常，一涉及工具调用就没回复」。
//
// toolCallTracker 负责把空 id 归一为「最近一次开启的调用」，并保证 id 始终非空。
package gateway

import pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"

// toolCallTracker 有状态：同一路事件流内的调用顺序即为分组依据。
type toolCallTracker struct {
	lastID   string
	lastName string
}

// resolve 返回本次增量应归属的调用 id 与工具名，两者都保证非空。
// id 为空 = 延续最近一次开启的调用；从未开启过则合成一个稳定 id。
func (t *toolCallTracker) resolve(ev *pb.ToolCallDelta) (id, name string) {
	if ev.Id != "" {
		t.lastID = ev.Id
	} else if t.lastID == "" {
		t.lastID = "call_" + randHex(8)
	}
	if ev.Name != "" {
		t.lastName = ev.Name
	}
	return t.lastID, t.lastName
}
