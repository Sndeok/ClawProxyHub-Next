package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 无暂存备份时必须是空操作（不影响正常启动路径）。
func TestApplyPendingRestoreNoPending(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cph.db")
	if err := os.WriteFile(dbPath, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPendingRestore(dbPath, dir); err != nil {
		t.Fatalf("空操作出错: %v", err)
	}
	raw, _ := os.ReadFile(dbPath)
	if string(raw) != "original" {
		t.Errorf("库文件被意外改动: %s", raw)
	}
}

// 暂存存在 → 当前库另存 .bak-*、暂存换入、secret.key 一并换入、restore 目录清空。
func TestApplyPendingRestoreSwapsIn(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cph.db")
	if err := os.WriteFile(dbPath, []byte("old-db"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 旧库的 WAL/SHM 残留必须被清掉（否则会污染新库）
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(dbPath+suffix, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.key"), []byte("old-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	restoreDir := filepath.Join(dir, "restore")
	if err := os.MkdirAll(restoreDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(restoreDir, "cph.db"), []byte("new-db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(restoreDir, "secret.key"), []byte("new-key"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ApplyPendingRestore(dbPath, dir); err != nil {
		t.Fatalf("换入失败: %v", err)
	}

	if raw, _ := os.ReadFile(dbPath); string(raw) != "new-db" {
		t.Errorf("暂存库未换入: %s", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "secret.key")); string(raw) != "new-key" {
		t.Errorf("secret.key 未换入: %s", raw)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + suffix); !os.IsNotExist(err) {
			t.Errorf("%s 残留未清理", suffix)
		}
	}
	if _, err := os.Stat(restoreDir); !os.IsNotExist(err) {
		t.Errorf("restore 目录未清理")
	}
	// 旧库与旧密钥都应留下带时间戳的备份
	entries, _ := os.ReadDir(dir)
	var bakDB, bakKey int
	for _, e := range entries {
		switch {
		case strings.HasPrefix(e.Name(), "cph.db.bak-"):
			bakDB++
		case strings.HasPrefix(e.Name(), "secret.key.bak-"):
			bakKey++
		}
	}
	if bakDB != 1 {
		t.Errorf("旧库备份数不符: %d（%v）", bakDB, entries)
	}
	if bakKey != 1 {
		t.Errorf("旧密钥备份数不符: %d", bakKey)
	}
}

// 当前库缺失（首次部署即恢复）时也要能换入。
func TestApplyPendingRestoreWithoutCurrentDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cph.db")
	restoreDir := filepath.Join(dir, "restore")
	if err := os.MkdirAll(restoreDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(restoreDir, "cph.db"), []byte("new-db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPendingRestore(dbPath, dir); err != nil {
		t.Fatalf("换入失败: %v", err)
	}
	if raw, _ := os.ReadFile(dbPath); string(raw) != "new-db" {
		t.Errorf("库未换入: %s", raw)
	}
}
