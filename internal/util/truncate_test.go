package util

import "testing"

func TestTruncStr(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},           // 短于上限
		{"hello", 5, "hello"},            // 恰好等于上限
		{"hello world", 5, "hello"},      // ASCII 正常截断
		{"你好世界", 12, "你好世界"},           // 中文完整
		{"你好世界", 6, "你好"},              // 中文按字节截断到字符边界
		{"你好世界", 7, "你好"},              // 7 字节落在「好」中间 → 回退到 6
		{"你好世界", 8, "你好"},              // 8 字节仍是半个字符 → 回退到 6
		{"你好世界", 9, "你好世"},             // 9 字节落在「世」边界
		{"a你好", 2, "a"},                 // 混合：a(1) + 你(3)，2 落在「你」中间
		{"a你好", 4, "a你"},                // 4 = a + 你
		{"", 5, ""},                      // 空串
		{"abc", 0, ""},                   // 0 上限
	}
	for _, c := range cases {
		got := TruncStr(c.in, c.n)
		if got != c.want {
			t.Errorf("TruncStr(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}