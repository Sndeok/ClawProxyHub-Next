package account

import (
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "t.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Account{}, &model.AccountCreditDaily{}); err != nil {
		t.Fatal(err)
	}
	// Windows 上不关连接会导致 TempDir 清理失败（文件被占用）
	t.Cleanup(func() {
		if s, err := db.DB(); err == nil {
			_ = s.Close()
		}
	})
	return db
}

func TestCreditRemaining(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{`{"remaining":"12.5"}`, 12.5, true},
		{`{"remaining":12.5}`, 12.5, true},
		{`{"total":"100","used":"0"}`, 0, false},
		{``, 0, false},
		{`not json`, 0, false},
	}
	for _, tc := range cases {
		got, ok := CreditRemaining(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("CreditRemaining(%q) = (%v,%v), want (%v,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRecordCreditSnapshotBaselineAndTopUp(t *testing.T) {
	db := testDB(t)
	acct := model.Account{PluginID: 1, DisplayName: "a"}
	if err := db.Create(&acct).Error; err != nil {
		t.Fatal(err)
	}

	// 首采：基线 = 剩余，消耗 0
	RecordCreditSnapshot(db, acct.ID, `{"remaining":"100"}`)
	row := loadDaily(t, db, acct.ID)
	if row.Baseline != 100 || row.Remaining != 100 || row.Used != 0 || row.Samples != 1 {
		t.Fatalf("首采错误: %+v", row)
	}

	// 用掉 30 分
	RecordCreditSnapshot(db, acct.ID, `{"remaining":"70"}`)
	row = loadDaily(t, db, acct.ID)
	if row.Used != 30 || row.Baseline != 100 || row.Samples != 2 {
		t.Fatalf("正常消耗计算错误: %+v", row)
	}

	// 充值到 500：基线上移，消耗不算成负数
	RecordCreditSnapshot(db, acct.ID, `{"remaining":"500"}`)
	row = loadDaily(t, db, acct.ID)
	if row.Baseline != 500 || row.Used != 0 {
		t.Fatalf("充值后基线未上移: %+v", row)
	}

	// 再消耗 20
	RecordCreditSnapshot(db, acct.ID, `{"remaining":"480"}`)
	row = loadDaily(t, db, acct.ID)
	if row.Used != 20 {
		t.Fatalf("充值后消耗计算错误: %+v", row)
	}

	// 无积分字段的观测不落库、不覆盖
	before := row
	RecordCreditSnapshot(db, acct.ID, `{"total":"1"}`)
	row = loadDaily(t, db, acct.ID)
	if row.Samples != before.Samples || row.Remaining != before.Remaining {
		t.Fatalf("无积分观测不应改写快照: before=%+v after=%+v", before, row)
	}
}

func loadDaily(t *testing.T, db *gorm.DB, accountID int64) model.AccountCreditDaily {
	t.Helper()
	var row model.AccountCreditDaily
	if err := db.Where("account_id = ? AND day = ?", accountID, time.Now().Format("2006-01-02")).
		First(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}
