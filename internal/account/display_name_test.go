package account

import "testing"

// TestShouldAdoptDisplayName 刷新不得覆盖用户手动改过的账号名。
func TestShouldAdoptDisplayName(t *testing.T) {
	cases := []struct {
		current, fromPlugin string
		want                bool
	}{
		{"", "插件名", true},       // 空名 → 采用插件值
		{"  ", "插件名", true},     // 只有空白 → 采用
		{"我改的名字", "插件名", false}, // 用户改过 → 保留
		{"我改的名字", "", false},    // 插件没给 → 不动
		{"", "", false},         // 两边都空 → 不动
	}
	for _, c := range cases {
		if got := shouldAdoptDisplayName(c.current, c.fromPlugin); got != c.want {
			t.Errorf("shouldAdoptDisplayName(%q, %q) = %v, want %v", c.current, c.fromPlugin, got, c.want)
		}
	}
}
