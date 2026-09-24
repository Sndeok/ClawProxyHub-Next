package openaiup

import (
	"testing"
)

// 思考增量必须按 ContentDelta{Reasoning:true} 上报。
// 回归背景：解析器以前只取 delta.content，模型「思考」的整段时间（几秒~几十秒）
// 客户端收不到任何事件，表现为首字极慢。
func TestReasoningDeltaParsed(t *testing.T) {
	evs := collect([]string{
		`data: {"choices":[{"delta":{"role":"assistant","reasoning_content":"先算乘除"}}]}`,
		`data: {"choices":[{"delta":{"reasoning":"再算加减"}}]}`,
		`data: {"choices":[{"delta":{"content":"答案是 201"}}]}`,
		`data: [DONE]`,
	})
	var got []string
	var flags []bool
	for _, ev := range evs {
		if d := ev.GetContentDelta(); d != nil {
			got = append(got, d.Text)
			flags = append(flags, d.Reasoning)
		}
	}
	want := []string{"先算乘除", "再算加减", "答案是 201"}
	wantFlags := []bool{true, true, false}
	if len(got) != len(want) {
		t.Fatalf("内容事件数 = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] || flags[i] != wantFlags[i] {
			t.Errorf("第 %d 条 = %q reasoning=%v, want %q reasoning=%v", i, got[i], flags[i], want[i], wantFlags[i])
		}
	}
}
