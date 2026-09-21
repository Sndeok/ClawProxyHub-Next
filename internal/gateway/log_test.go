package gateway

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/Sndeok/ClawProxyHub-Next/internal/database"
	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
)

// openTestDB 建库（含迁移）并在用例结束前关闭连接——SQLite 文件被占用时
// testing.TempDir 的清理会失败。
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "cph.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// TestRequestLogLatencyIsTotalDuration 总耗时必须以 serve 入口为基准。
// 旧实现由调用方传入 time.Since(局部 start)，streamOut / nonStreamOut 只统计
// 首事件之后的输出阶段，于是出现「首字 5001ms、总耗时 1ms」这种自相矛盾的数据；
// 失败/重试路径又各用各的基准，导致列与列之间无法横向比较。
func TestRequestLogLatencyIsTotalDuration(t *testing.T) {
	db := openTestDB(t)

	c := &requestLogCtx{
		start:        time.Now().Add(-250 * time.Millisecond),
		status:       200,
		firstTokenMs: 120,
		protocol:     "responses",
	}
	c.write(db)

	var got model.RequestLog
	if err := db.Order("id DESC").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.LatencyMs < 200 {
		t.Errorf("LatencyMs must be measured from serve entry: want >= 200, got %d", got.LatencyMs)
	}
	if got.FirstTokenMs != 120 {
		t.Errorf("FirstTokenMs lost: got %d", got.FirstTokenMs)
	}
	if got.FirstTokenMs > got.LatencyMs {
		t.Errorf("FirstTokenMs (%d) must not exceed LatencyMs (%d)", got.FirstTokenMs, got.LatencyMs)
	}
}

// TestRequestLogLatencyWithoutStart 未设 start 时兜底为 0 附近，而不是把零值时间
// （time.Time{}）当起点算出巨大值。
func TestRequestLogLatencyWithoutStart(t *testing.T) {
	db := openTestDB(t)

	c := &requestLogCtx{status: 200}
	c.write(db)

	var got model.RequestLog
	if err := db.Order("id DESC").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.LatencyMs < 0 || got.LatencyMs > 1000 {
		t.Errorf("expected near-zero fallback latency, got %d", got.LatencyMs)
	}
	if got.Attempts != 1 {
		t.Errorf("attempts must default to 1, got %d", got.Attempts)
	}
}