// system.go — 系统信息 / 备份导出 / 备份恢复。
//
// 备份包（zip）：cph.db（VACUUM INTO 一致性快照，不中断服务）+ secret.key
// （凭据加解密密钥，缺了它账号全部解不开）+ meta.json。
// 恢复走「暂存 + 重启换入」：上传落到 <data>/restore/，下次启动由
// database.ApplyPendingRestore 换入，运行中的库不原地替换（避免半写状态）。
package admin

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Sndeok/ClawProxyHub-Next/internal/database"
	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
	"github.com/Sndeok/ClawProxyHub-Next/internal/version"
	"github.com/Sndeok/ClawProxyHub-Next/sdk"
)

// startedAt 进程启动时刻（运行时长）。
var startedAt = time.Now()

// systemInfo GET /admin/system/info — 版本 / 运行时 / 路径 / 库体积 / 各表计数。
func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	var dbSize int64
	if st, err := os.Stat(s.dbPath); err == nil {
		dbSize = st.Size()
	}
	migVer, migDirty, _ := database.CachedMigrationVersion()
	counts := map[string]int64{}
	for name, m := range map[string]interface{}{
		"plugins": &model.Plugin{}, "accounts": &model.Account{},
		"groups": &model.Group{}, "routes": &model.Route{}, "keys": &model.Key{},
		"request_logs": &model.RequestLog{}, "task_rules": &model.TaskRule{},
		"task_runs": &model.TaskRun{}, "proxies": &model.Proxy{},
	} {
		var n int64
		s.db.Model(m).Count(&n)
		counts[name] = n
	}
	pendingRestore := false
	if _, err := os.Stat(filepath.Join(s.dataDir, "restore", "cph.db")); err == nil {
		pendingRestore = true
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"version":           version.Core,
		"protocol_version":  sdk.ProtocolVersion,
		"go_version":        runtime.Version(),
		"os":                runtime.GOOS,
		"arch":              runtime.GOARCH,
		"started_at":        startedAt.Format(time.RFC3339),
		"uptime_seconds":    int64(time.Since(startedAt).Seconds()),
		"data_dir":          absPath(s.dataDir),
		"db_path":           absPath(s.dbPath),
		"db_size_bytes":     dbSize,
		"migration_version": migVer,
		"migration_dirty":   migDirty,
		"mem_alloc_bytes":   ms.Alloc,
		"goroutines":        runtime.NumGoroutine(),
		"counts":            counts,
		"pending_restore":   pendingRestore,
	})
}

func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// exportBackup GET /admin/system/backup — zip：cph.db（VACUUM INTO）+ secret.key + meta.json。
func (s *Server) exportBackup(w http.ResponseWriter, r *http.Request) {
	tmp, err := os.CreateTemp("", "cph-backup-*.db")
	if err != nil {
		http.Error(w, `{"error":"创建临时文件失败"}`, http.StatusInternalServerError)
		return
	}
	tmpPath := tmp.Name()
	tmp.Close()
	os.Remove(tmpPath) // VACUUM INTO 要求目标不存在
	defer os.Remove(tmpPath)

	// SQLite 的 VACUUM INTO 不接受绑定参数，路径需转义单引号
	if err := s.db.Exec("VACUUM INTO '" + strings.ReplaceAll(filepath.ToSlash(tmpPath), "'", "''") + "'").Error; err != nil {
		http.Error(w, `{"error":"快照失败: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="cph-backup-%s.zip"`, time.Now().Format("20060102-150405")))
	zw := zip.NewWriter(w)
	defer zw.Close()

	addFile := func(name, src string) {
		f, err := os.Open(src)
		if err != nil {
			return
		}
		defer f.Close()
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()}
		if st, err := f.Stat(); err == nil {
			hdr.Modified = st.ModTime()
		}
		wr, err := zw.CreateHeader(hdr)
		if err != nil {
			return
		}
		_, _ = io.Copy(wr, f)
	}
	addFile("cph.db", tmpPath)
	key := filepath.Join(s.dataDir, "secret.key")
	if fileExists(key) {
		addFile("secret.key", key)
	}
	meta := fmt.Sprintf(`{"version":"%s","exported_at":"%s"}`, version.Core, time.Now().Format(time.RFC3339))
	if wr, err := zw.Create("meta.json"); err == nil {
		_, _ = wr.Write([]byte(meta))
	}
}

// importBackup POST /admin/system/restore — multipart file=<zip>。
// 校验后写入 <data>/restore/，重启时自动换入（当前库另存 .bak-<时间>）。
func (s *Server) importBackup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
	f, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"请选择备份 zip 文件"}`, http.StatusBadRequest)
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, `{"error":"读取上传失败"}`, http.StatusBadRequest)
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		http.Error(w, `{"error":"不是合法的 zip 备份"}`, http.StatusBadRequest)
		return
	}
	files := map[string][]byte{}
	for _, zf := range zr.File {
		name := filepath.Base(zf.Name)
		if name != "cph.db" && name != "secret.key" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		files[name], _ = io.ReadAll(rc)
		rc.Close()
	}
	dbRaw, ok := files["cph.db"]
	if !ok || !bytes.HasPrefix(dbRaw, []byte("SQLite format 3\x00")) {
		http.Error(w, `{"error":"备份包缺少 cph.db 或不是 SQLite 数据库"}`, http.StatusBadRequest)
		return
	}
	dir := filepath.Join(s.dataDir, "restore")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, `{"error":"创建暂存目录失败"}`, http.StatusInternalServerError)
		return
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			http.Error(w, `{"error":"写入暂存失败: `+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "with_key": files["secret.key"] != nil,
		"message": "备份已暂存，重启服务后自动换入（当前库会另存为 cph.db.bak-<时间>）",
	})
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
