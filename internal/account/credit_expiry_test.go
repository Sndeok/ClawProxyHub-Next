package account

import (
	"testing"
	"time"
)

// TestCreditExpiryOf 覆盖插件真实写入形状：remaining 是字符串、expiresAt 是日期。
func TestCreditExpiryOf(t *testing.T) {
	soon := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	far := time.Now().AddDate(0, 0, 40).Format("2006-01-02")
	doc := `{"remaining":"500","packages":[` +
		`{"remaining":"120.5","label":"新人包","expiresAt":"` + soon + `"},` +
		`{"remaining":"80","label":"月度包","expiresAt":"` + far + `"}]}`
	e := CreditExpiryOf(doc)
	if e.Packages != 2 {
		t.Fatalf("packages = %d, want 2", e.Packages)
	}
	if e.Expiring != 120.5 {
		t.Errorf("expiring = %v, want 120.5（只统计 7 天内到期的包）", e.Expiring)
	}
	if e.NextAt.Format("2006-01-02") != soon || e.NextLeft != 120.5 {
		t.Errorf("next = %v/%v, want %s/120.5", e.NextAt, e.NextLeft, soon)
	}
	if got := CreditExpiryOf(`{"remaining":"10"}`); got.Packages != 0 {
		t.Errorf("无 packages 时 = %+v", got)
	}
	if got := CreditExpiryOf("not json"); got.Packages != 0 {
		t.Errorf("非法 JSON 时 = %+v", got)
	}
}
