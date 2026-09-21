// Package util — 通用工具函数。
package util

import "unicode/utf8"

// TruncStr 字节长度截断，保证不切断 UTF-8 多字节字符（中文等）。
func TruncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
