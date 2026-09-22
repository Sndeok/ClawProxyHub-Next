package fingerprint

import "time"

// nowUnixMilli 当前毫秒时间戳（独立成函数便于测试替换 / 阅读）。
func nowUnixMilli() int64 { return time.Now().UnixMilli() }
