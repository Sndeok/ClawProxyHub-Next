package gateway

import (
	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// aggrTool 非流式聚合中累积的一次工具调用。
type aggrTool struct {
	id    string
	name  string
	input string // 参数 JSON 片段拼接结果
}

// aggregateCore 是三种协议「非流式聚合」的公共部分。
//
// 三种出口（OpenAI Chat / Responses / Anthropic Messages）在非流式路径上做的事完全一样：
// 把信封事件流折叠成「文本 + 若干工具调用 + 用量 + 结束原因」，差别只在最后怎么渲染成 JSON。
// 这里收敛公共折叠逻辑，各协议的结构体只需内嵌它并实现自己的 result()。
//
// 注：通过方法提升，调用方仍然写 `agg.feed(ev)`，与重构前一致。
type aggregateCore struct {
	model  string
	text   string
	tools  map[string]*aggrTool
	order  []string // 工具调用首次出现的顺序（Responses 需要按顺序输出项）
	track  toolCallTracker
	usage  pb.Usage // 最近一次 MessageFinish 携带的用量
	finish string   // 信封原始 finish_reason（各协议自行映射）
}

// feed 消费一条信封事件。未知事件类型直接忽略。
func (c *aggregateCore) feed(ev *pb.StreamEvent) {
	switch e := ev.Event.(type) {
	case *pb.StreamEvent_MessageStart:
		c.model = e.MessageStart.Model
	case *pb.StreamEvent_ContentDelta:
		c.text += e.ContentDelta.Text
	case *pb.StreamEvent_ToolCallDelta:
		// 上游可能分片只给 index 不给 id：由 tracker 按 index 归位补 id
		id, name := c.track.resolve(e.ToolCallDelta)
		if c.tools == nil {
			c.tools = map[string]*aggrTool{}
		}
		t, ok := c.tools[id]
		if !ok {
			t = &aggrTool{id: id, name: name}
			c.tools[id] = t
			c.order = append(c.order, id)
		}
		t.input += e.ToolCallDelta.ArgumentsDelta
	case *pb.StreamEvent_MessageFinish:
		c.finish = e.MessageFinish.FinishReason
		if u := e.MessageFinish.Usage; u != nil {
			// 逐字段复制：pb.Usage 内含 protoimpl.MessageState（sync.Mutex），整体赋值会复制锁
			c.usage = pb.Usage{
				InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
				CachedTokens: u.CachedTokens, CreditUsed: u.CreditUsed,
				CacheCreationTokens: u.CacheCreationTokens,
			}
		}
	}
}

// hasTools 是否产生了工具调用。
func (c *aggregateCore) hasTools() bool { return len(c.tools) > 0 }

// toolsByName 按首次出现顺序返回工具调用（Responses 用）。id 缺失的兜底在 tracker 里已处理。
func (c *aggregateCore) toolsInOrder() []*aggrTool {
	out := make([]*aggrTool, 0, len(c.tools))
	for _, id := range c.order {
		if t, ok := c.tools[id]; ok {
			out = append(out, t)
		}
	}
	return out
}
