// migrate_test.go — 迁移链的编号与升级路径保障。
//
// 背景：上游在 v1.0.3 里把 000002 整份重写（删掉 key_hash 版、换成 account_proxy 版）。
// 对已执行过旧 000002 的库来说，重写等于「这份 schema 永远不会执行」——账号级代理与
// 模型目录会在运行期报缺表/缺列。所以本仓库的约定是：已发布的迁移文件只增不改，
// 上游新增的 schema 一律按序追加为新编号。这个测试守住这条约定。
package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"testing"
)

// migrationVersions 扫描嵌入的迁移文件，返回 version → {up:bool, down:bool}。
func migrationVersions(t *testing.T) map[int]map[string]bool {
	t.Helper()
	entries, err := sqliteMigrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^(\d+)_[^/]+\.(up|down)\.sql$`)
	out := map[int]map[string]bool{}
	for _, e := range entries {
		m := re.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, _ := strconv.Atoi(m[1])
		if out[v] == nil {
			out[v] = map[string]bool{}
		}
		out[v][m[2]] = true
	}
	return out
}

// TestMigrationVersionsContiguous 版本号必须从 1 连续递增，且 up/down 成对。
func TestMigrationVersionsContiguous(t *testing.T) {
	versions := migrationVersions(t)
	if len(versions) == 0 {
		t.Fatal("未找到迁移文件")
	}
	nums := make([]int, 0, len(versions))
	for v := range versions {
		nums = append(nums, v)
	}
	sort.Ints(nums)
	for i, v := range nums {
		if v != i+1 {
			t.Fatalf("迁移版本号不连续：期望 %d，实际 %d（已发布编号不可改写，新变更请追加到末尾）", i+1, v)
		}
		if !versions[v]["up"] || !versions[v]["down"] {
			t.Fatalf("迁移 %d 缺少 up 或 down 文件", v)
		}
	}
	t.Logf("迁移链：1..%d，共 %d 个版本", len(nums), len(nums))
}

// latestDownSQL 取最新一版迁移的 down 脚本（含文件名），供存量库回退模拟使用。
// 用 down 脚本而不是手写 DROP：新增迁移时这里不用跟着改，回退口径始终与迁移文件一致。
func latestDownSQL(t *testing.T, version int) string {
	t.Helper()
	entries, err := sqliteMigrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^(\d+)_[^/]+\.down\.sql$`)
	for _, e := range entries {
		m := re.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if v, _ := strconv.Atoi(m[1]); v == version {
			b, err := sqliteMigrations.ReadFile("migrations/" + e.Name())
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
	}
	t.Fatalf("未找到 000%03d 的 down 脚本", version)
	return ""
}

// TestMigrationUpgradeFromPreviousVersion 模拟存量库升级：把最新一版的 schema 用该版本的
// down 脚本回退并把版本号减 1，再走一次 Open，必须重新补齐到最新版本。
// 这条覆盖「新增迁移对已有库确实会执行」——正是上游重写 000002 踩到的坑。
func TestMigrationUpgradeFromPreviousVersion(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "cph.db")

	db, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	latest := len(migrationVersions(t))
	var got uint
	if err := db.Raw("SELECT version FROM schema_migrations").Scan(&got).Error; err != nil {
		t.Fatal(err)
	}
	if int(got) != latest {
		t.Fatalf("全新库应迁移到 %d，实际 %d", latest, got)
	}
	// 最新一版必须是 000005（积分消耗列 + 每日积分快照表）
	if !db.Migrator().HasColumn("request_logs", "credit_used") {
		t.Fatal("request_logs.credit_used 未创建")
	}
	if !db.Migrator().HasTable("account_credit_daily") {
		t.Fatal("account_credit_daily 未创建")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	// 回退成「上一版存量库」：执行最新一版的 down 脚本并把版本号改回 latest-1
	raw, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Exec(latestDownSQL(t, latest)).Error; err != nil {
		t.Fatalf("执行 down 脚本失败: %v", err)
	}
	if err := raw.Exec(fmt.Sprintf("UPDATE schema_migrations SET version = %d, dirty = 0", latest-1)).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB2, _ := raw.DB()
	_ = sqlDB2.Close()

	// 再开一次：只应执行最新一版，并把对象补回来
	again, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("存量库升级失败: %v", err)
	}
	defer func() {
		if s, err := again.DB(); err == nil {
			_ = s.Close()
		}
	}()
	if err := again.Raw("SELECT version FROM schema_migrations").Scan(&got).Error; err != nil {
		t.Fatal(err)
	}
	if int(got) != latest {
		t.Fatalf("存量库应升级到 %d，实际 %d", latest, got)
	}
	if !again.Migrator().HasColumn("request_logs", "credit_used") {
		t.Fatal("升级后 request_logs.credit_used 仍未创建")
	}
	if !again.Migrator().HasTable("account_credit_daily") {
		t.Fatal("升级后 account_credit_daily 仍未创建")
	}
	// 已有的 000002/000003/000004 成果不能被破坏
	if !again.Migrator().HasColumn("keys", "key_hash") {
		t.Fatal("升级后 keys.key_hash 丢失（说明 000002 被改写）")
	}
	if !again.Migrator().HasTable("plugin_storage") {
		t.Fatal("升级后 plugin_storage 丢失（说明 000002 被改写）")
	}
	if !again.Migrator().HasColumn("request_logs", "attempts") {
		t.Fatal("升级后 request_logs.attempts 丢失")
	}
	if !again.Migrator().HasTable("account_proxies") {
		t.Fatal("升级后 account_proxies 丢失")
	}
	if !again.Migrator().HasColumn("accounts", "models_json") {
		t.Fatal("升级后 accounts.models_json 丢失")
	}
	_ = os.Remove(filepath.Join(dir, "cph.db-wal"))
	_ = os.Remove(filepath.Join(dir, "cph.db-shm"))
}
