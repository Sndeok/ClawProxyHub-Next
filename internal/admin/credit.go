// credit.go — 今日账号维度的用量聚合（token / 缓存 / 积分）。
//
// 两条独立口径：
//  1. request_logs.credit_used —— 单次请求级，插件在 usage 里透出 credit_used 时才有值；
//  2. account_credit_daily.used —— 上游积分快照差值推算（见 internal/account/credit.go）。
//
// 展示优先用口径 1，没有则回落到口径 2（前端标注「估算」）。
package admin

import (
	"time"

	"gorm.io/gorm"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
)

// acctToday 单账号今日聚合结果。
type acctToday struct {
	Tokens   int64
	Cached   int64
	Credits  float64
	Requests int64
	Snapshot float64
	HasSnap  bool
}

// todayStart 今天 00:00（进程本地时区）。
func todayStart() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// todayStatsByAccount 今日按账号聚合；空账号维度返回空 map（调用方按 0 处理）。
func todayStatsByAccount(db *gorm.DB) map[int64]*acctToday {
	out := map[int64]*acctToday{}
	get := func(id int64) *acctToday {
		v, ok := out[id]
		if !ok {
			v = &acctToday{}
			out[id] = v
		}
		return v
	}

	type logRow struct {
		AccountID *int64
		Tokens    int64
		Cached    int64
		Credits   float64
		Requests  int64
	}
	var rows []logRow
	db.Model(&model.RequestLog{}).
		Select("account_id AS account_id, COUNT(*) AS requests, "+
			"COALESCE(SUM(input_tokens + output_tokens), 0) AS tokens, "+
			"COALESCE(SUM(cached_tokens), 0) AS cached, "+
			"COALESCE(SUM(credit_used), 0) AS credits").
		Where("account_id IS NOT NULL AND created_at >= ?", todayStart()).
		Group("account_id").Scan(&rows)
	for _, row := range rows {
		if row.AccountID == nil {
			continue
		}
		v := get(*row.AccountID)
		v.Tokens, v.Cached, v.Credits, v.Requests = row.Tokens, row.Cached, row.Credits, row.Requests
	}

	var snaps []model.AccountCreditDaily
	db.Where("day = ?", time.Now().Format("2006-01-02")).Find(&snaps)
	for i := range snaps {
		if snaps[i].Samples <= 0 {
			continue
		}
		v := get(snaps[i].AccountID)
		v.Snapshot, v.HasSnap = snaps[i].Used, true
	}
	return out
}
