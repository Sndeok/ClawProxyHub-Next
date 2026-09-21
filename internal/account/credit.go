// credit.go — 账号积分快照记录。
//
// 上游只给「剩余积分」快照，没有「本次请求花了多少积分」。所以这里按账号×天记录
// 当天首次与最近一次看到的剩余积分，差值即当天净消耗（充值会把基线一起抬高，
// 不会算成负消耗）。单次请求级口径（插件上报的 credit_used）在网关侧单独统计。
package account

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
)

// CreditRemaining 从 credits_json 解析剩余积分，兼容字符串与数字两种写法。
func CreditRemaining(creditsJSON string) (float64, bool) {
	if strings.TrimSpace(creditsJSON) == "" {
		return 0, false
	}
	var raw struct {
		Remaining json.RawMessage `json:"remaining"`
	}
	if json.Unmarshal([]byte(creditsJSON), &raw) != nil || len(raw.Remaining) == 0 {
		return 0, false
	}
	s := strings.Trim(string(raw.Remaining), `"`)
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// RecordCreditSnapshot 记录一次积分观测（账号刷新时调用）。
// 当天首采写入基线；剩余高于基线（充值/补发）时把基线上移。
func RecordCreditSnapshot(db *gorm.DB, accountID int64, creditsJSON string) {
	remaining, ok := CreditRemaining(creditsJSON)
	if !ok || accountID <= 0 {
		return
	}
	day := time.Now().Format("2006-01-02")
	now := time.Now()

	var cur model.AccountCreditDaily
	if err := db.Where("account_id = ? AND day = ?", accountID, day).First(&cur).Error; err != nil {
		row := model.AccountCreditDaily{
			AccountID: accountID, Day: day, Remaining: remaining,
			Baseline: remaining, Used: 0, Samples: 1, UpdatedAt: now,
		}
		// 并发刷新同一账号时忽略冲突：赢家已写入当天基线
		db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		return
	}
	baseline := cur.Baseline
	if remaining > baseline {
		baseline = remaining
	}
	used := baseline - remaining
	if used < 0 {
		used = 0
	}
	db.Model(&model.AccountCreditDaily{}).
		Where("account_id = ? AND day = ?", accountID, day).
		Updates(map[string]interface{}{
			"remaining": remaining, "baseline": baseline, "used": used,
			"samples": cur.Samples + 1, "updated_at": now,
		})
}

// CreditExpiry 积分包的到期概览。
type CreditExpiry struct {
	Packages int       // 带到期时间的积分包数量
	Expiring float64   // expiringWindow 内到期的剩余积分合计
	NextAt   time.Time // 最近一个到期时间（零值 = 无）
	NextLeft float64   // 最近那个包剩余多少
}

// expiringWindow 快到期的判定窗口：7 天内到期视为「快过期」。
const expiringWindow = 7 * 24 * time.Hour

// CreditExpiry 从 credits_json 的 packages 里读出到期信息。
// 不同插件字段名略有差异（expiresAt / expireTime / expiry），时间格式也宽容解析。
func CreditExpiryOf(creditsJSON string) CreditExpiry {
	var raw struct {
		Packages []struct {
			// 插件用 trimFloat 输出：可能是字符串也可能是数字，统一用 RawMessage 宽容解析
			Remaining json.RawMessage `json:"remaining"`
			Left      json.RawMessage `json:"left"`
			ExpiresAt string          `json:"expiresAt"`
			ExpireAt2 string          `json:"expireTime"`
			Expiry    string          `json:"expiry"`
		} `json:"packages"`
	}
	if strings.TrimSpace(creditsJSON) == "" || json.Unmarshal([]byte(creditsJSON), &raw) != nil {
		return CreditExpiry{}
	}
	out := CreditExpiry{}
	now := time.Now()
	for _, p := range raw.Packages {
		rawExp := p.ExpiresAt
		if rawExp == "" {
			rawExp = p.ExpireAt2
		}
		if rawExp == "" {
			rawExp = p.Expiry
		}
		if rawExp == "" {
			continue
		}
		at, ok := parseLooseTime(rawExp)
		if !ok {
			continue
		}
		left := rawNumber(p.Remaining)
		if left == 0 {
			left = rawNumber(p.Left)
		}
		out.Packages++
		if out.NextAt.IsZero() || at.Before(out.NextAt) {
			out.NextAt, out.NextLeft = at, left
		}
		if at.After(now) && at.Sub(now) <= expiringWindow {
			out.Expiring += left
		}
	}
	return out
}

// parseLooseTime 宽容解析上游到期时间：RFC3339 / 常见日期时间 / 纯日期。
func parseLooseTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{
		time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05",
		"2006/01/02 15:04:05", "2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// rawNumber 宽容解析 JSON 数字或数字字符串（"120.5" 与 120.5 都能出值）。
func rawNumber(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if s == "" || s == "null" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
