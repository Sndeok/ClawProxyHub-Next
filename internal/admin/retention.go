// retention.go — 调用日志保留策略：按设置定期清理，避免 request_logs 无限增长。
package admin

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
	"github.com/Sndeok/ClawProxyHub-Next/internal/setting"
)

// retentionInterval 检查间隔：保留策略以「天」为粒度，6 小时检查一次足够。
const retentionInterval = 6 * time.Hour

// StartLogRetention 启动后台清理循环（retention_days <= 0 表示保留全部，不清理）。
// 启动时先跑一次，便于调小保留天数后立刻生效。
func StartLogRetention(ctx context.Context, db *gorm.DB, settings *setting.Store) {
	go func() {
		ticker := time.NewTicker(retentionInterval)
		defer ticker.Stop()
		for {
			PruneLogs(db, settings.LogRetentionDays())
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// PruneLogs 删除 days 天前的日志并返回删除条数；days <= 0 不动作。
func PruneLogs(db *gorm.DB, days int) int64 {
	if days <= 0 {
		return 0
	}
	res := db.Session(&gorm.Session{AllowGlobalUpdate: true}).
		Where("created_at < ?", time.Now().AddDate(0, 0, -days)).
		Delete(&model.RequestLog{})
	if res.Error != nil {
		return 0
	}
	return res.RowsAffected
}
