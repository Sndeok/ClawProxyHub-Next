// marketplace.go — 插件市场与包安装。
package admin

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
	"github.com/Sndeok/ClawProxyHub-Next/internal/plugin"
)

// 离线插件市场索引（线上索引不可达时的兜底清单）
// 内容是 ClawProxyHubPlugins 仓库 index.json 的快照，随核心发布同步；sha256 为空表示跳过校验。
//
//go:embed offline_market.json
var offlineMarketJSON []byte

// maxPluginPackageBytes 插件包体积上限（.cphplugin 内含 5 个平台二进制）。
const maxPluginPackageBytes = 256 << 20

// offlineMarket 解析内置离线索引。
func offlineMarket() []MarketEntry {
	var entries []MarketEntry
	json.Unmarshal(offlineMarketJSON, &entries)
	return entries
}

// isGitHubURL 是否 GitHub 域（这些域在国内网络常见不可达，需要代理加速）。
func isGitHubURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "github.com" || host == "raw.githubusercontent.com" ||
		strings.HasSuffix(host, ".githubusercontent.com")
}

// withGitHubProxy 给 GitHub URL 套代理前缀（ghproxy 风格：proxy + 完整原始 URL）。
func (s *Server) withGitHubProxy(raw string) string {
	proxy := s.settings.GitHubProxy()
	if proxy == "" || !isGitHubURL(raw) {
		return raw
	}
	return strings.TrimSuffix(proxy, "/") + "/" + raw
}

// MarketEntry 市场 index.json 的单条目。
// 同名插件以 author+name 判定同插件（name 不保证全局唯一）。
type MarketEntry struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Author      string            `json:"author,omitempty"`
	Label       map[string]string `json:"label"` // 品牌名（多语言，zh 优先）
	PublishedAt string            `json:"published_at,omitempty"`
	DownloadURL string            `json:"download_url"`
	SHA256      string            `json:"sha256"`
}

// marketView 市场条目 + 本机安装状态（前端直接消费）。
type marketView struct {
	MarketEntry
	Installed bool   `json:"installed"`     // 已安装（含同版本）
	Updatable bool   `json:"updatable"`     // 已安装且市场版本更新
	LocalVer  string `json:"local_version"` // 本机已装版本
}

// pluginKey 同插件判定键：author/name（author 缺省回退 name，兼容旧索引）。
func pluginKey(author, name string) string {
	if author == "" {
		return name
	}
	return author + "/" + name
}

// marketplace GET /admin/plugins/marketplace — 线上索引；不可达时回落内置离线清单。
// 每条带本机安装状态（installed/updatable，按 author+name 判定同插件）。
func (s *Server) marketplace(w http.ResponseWriter, r *http.Request) {
	entries, online := s.fetchMarket()
	source := "offline"
	if online {
		source = "online"
	}
	// 本机已装版本（manifest 落盘为准）
	local := map[string]string{}
	for _, name := range s.plugins.Names() {
		if inst, ok := s.plugins.Get(name); ok {
			local[pluginKey(inst.Manifest.Author, inst.Manifest.Name)] = inst.Manifest.Version
		}
	}
	out := make([]marketView, 0, len(entries))
	for _, e := range entries {
		v := marketView{MarketEntry: e, LocalVer: local[pluginKey(e.Author, e.Name)]}
		if v.LocalVer != "" {
			v.Installed = true
			v.Updatable = e.Version != "" && e.Version != v.LocalVer
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"plugins": out, "source": source})
}

// installMarket POST /admin/plugins/install-market — body: {name, author}
// 同插件判定 = author+name（name 不保证全局唯一，与市场列表/已装列表同一判定键）。
//
// 响应是 NDJSON 进度流（插件包 20MB+，安装期间前端需要看到阶段而不是一个转圈）：
//
//	{"phase":"downloading","received":n,"total":m}
//	{"phase":"stopping"|"installing"|"starting"}
//	{"installed":"name"} 或 {"error":"..."}
//
// 客户端断开（前端取消）→ r.Context() 取消 → 下载/安装立即中断。
func (s *Server) installMarket(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		Author string `json:"author"`
	}
	if !readBody(w, r, &body) || body.Name == "" || body.Author == "" {
		http.Error(w, `{"error":"name and author required"}`, http.StatusBadRequest)
		return
	}
	entries, _ := s.fetchMarket()
	var entry *MarketEntry
	for i := range entries {
		if pluginKey(entries[i].Author, entries[i].Name) == pluginKey(body.Author, body.Name) {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		http.Error(w, `{"error":"not found in marketplace"}`, http.StatusNotFound)
		return
	}

	dl := *entry // 下载 URL 套 GitHub 代理（索引里的原始地址保持干净）
	dl.DownloadURL = s.withGitHubProxy(entry.DownloadURL)

	pw := newProgressWriter(w)
	zipPath, err := s.downloadToTemp(r.Context(), &dl, func(received, total int64) {
		pw.send(map[string]interface{}{"phase": "downloading", "received": received, "total": total})
	})
	if err != nil {
		pw.send(map[string]string{"error": err.Error()})
		return
	}
	defer os.Remove(zipPath)

	name, err := s.installZip(r.Context(), zipPath, func(phase string) {
		pw.send(map[string]string{"phase": phase})
	})
	if err != nil {
		pw.send(map[string]string{"error": err.Error()})
		return
	}
	pw.send(map[string]string{"installed": name})
}

// progressWriter NDJSON 进度流：每行一个 JSON 事件，写后即刷。
type progressWriter struct {
	w  http.ResponseWriter
	fl http.Flusher
}

func newProgressWriter(w http.ResponseWriter) *progressWriter {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // 反代不缓冲
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	return &progressWriter{w: w, fl: fl}
}

func (p *progressWriter) send(v interface{}) {
	_ = json.NewEncoder(p.w).Encode(v) // Encode 自带换行
	if p.fl != nil {
		p.fl.Flush()
	}
}

// installUpload POST /admin/plugins/install-upload — multipart 上传 .cphplugin。
func (s *Server) installUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, `{"error":"invalid upload"}`, http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("package")
	if err != nil {
		http.Error(w, `{"error":"missing package field"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "cph-*.cphplugin")
	if err != nil {
		http.Error(w, `{"error":"temp file"}`, http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		http.Error(w, `{"error":"save upload"}`, http.StatusInternalServerError)
		return
	}
	tmp.Close()
	name, err := s.installZip(r.Context(), tmp.Name(), nil)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"installed": name})
}

// installZipPath 安装本地包文件并刷新目录。
// installZip 安装本地包文件并刷新目录（市场安装 / 上传共用）。
// onPhase 透传给 InstallZip 报安装阶段（可为 nil）。
func (s *Server) installZip(ctx context.Context, zipPath string, onPhase func(string)) (string, error) {
	name, err := s.plugins.InstallZip(ctx, zipPath, onPhase)
	if err != nil {
		return "", err
	}
	// 安装/更新 = 显式启用：清掉持久化停用标记，与 InstallZip 已启动的实例保持一致
	s.db.Exec(`INSERT INTO plugins (name, version, author, protocol_version, manifest_json, enabled) VALUES (?,?,?,?,?,1)
		ON CONFLICT(name) DO UPDATE SET enabled = 1`, name, "", "cph", 0, "{}")
	s.plugins.RefreshCatalog(ctx)
	return name, nil
}

// stopPlugin POST /admin/plugins/{name}/stop
// 停用是持久的（plugins.enabled=0）：重启后保持停止，直到管理页点「启动」。
func (s *Server) stopPlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.plugins.Stop(name)
	s.db.Model(&model.Plugin{}).Where("name = ?", name).Update("enabled", false)
	writeJSON(w, http.StatusOK, map[string]bool{"stopped": true})
}

// startPlugin POST /admin/plugins/{name}/start — 从插件目录重新启动。
func (s *Server) startPlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	bins, err := s.plugins.Scan()
	if err != nil {
		http.Error(w, `{"error":"scan"}`, http.StatusInternalServerError)
		return
	}
	for _, bin := range bins {
		if filepath.Base(filepath.Dir(bin)) == name {
			if _, err := s.plugins.Start(r.Context(), bin); err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
				return
			}
			// 解除持久化停用（与 stopPlugin 对称）
			s.db.Model(&model.Plugin{}).Where("name = ?", name).Update("enabled", true)
			s.plugins.RefreshCatalog(r.Context())
			writeJSON(w, http.StatusOK, map[string]bool{"started": true})
			return
		}
	}
	http.Error(w, `{"error":"plugin binary not found"}`, http.StatusNotFound)
}

// uninstallPlugin DELETE /admin/plugins/{name} — 停止 + 删除文件 + 清 DB 记录。
func (s *Server) uninstallPlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.plugins.Uninstall(name); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	s.db.Exec("DELETE FROM plugins WHERE name = ?", name)
	s.plugins.RefreshCatalog(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"uninstalled": true})
}

// marketURL 当前生效的市场索引地址：用户自建（settings）> config 默认（env / 官方）。
func (s *Server) marketURL() string {
	if u := strings.TrimSpace(s.settings.MarketplaceURL()); u != "" {
		return u
	}
	return s.marketplaceURL
}

// fetchMarket 拉线上市场索引（走代理配置）；不可达时回落内置离线清单。
// online=false 表示返回的是离线兜底。
// marketCache 市场索引结果缓存：远端拉取带超时，避免每次打开页面都干等。
var marketCache struct {
	mu      sync.Mutex
	at      time.Time
	entries []MarketEntry
	online  bool
}

const marketCacheTTL = 60 * time.Second

func (s *Server) fetchMarket() (entries []MarketEntry, online bool) {
	marketCache.mu.Lock()
	if time.Since(marketCache.at) < marketCacheTTL && marketCache.entries != nil {
		cached, ok := marketCache.entries, marketCache.online
		marketCache.mu.Unlock()
		return cached, ok
	}
	marketCache.mu.Unlock()

	entries, err := s.fetchMarketRemote()
	marketCache.mu.Lock()
	defer marketCache.mu.Unlock()
	marketCache.at = time.Now()
	if err != nil {
		marketCache.entries, marketCache.online = offlineMarket(), false
		return marketCache.entries, false
	}
	marketCache.entries, marketCache.online = entries, true
	return entries, true
}

// fetchMarketRemote 只走线上并返回失败原因（市场页与「测试连接」共用）。
func (s *Server) fetchMarketRemote() ([]MarketEntry, error) {
	client, err := marketClient(s.settings.MarketProxy(), marketIndexTimeout)
	if err != nil {
		return nil, err
	}
	return fetchIndex(client, s.withGitHubProxy(s.marketURL()))
}

// downloadToTemp 下载市场包（走代理配置）并校验 sha256；report 上报进度（total 未知为 -1）。
// ctx 取消（前端断开）时读取立即中断。
func (s *Server) downloadToTemp(ctx context.Context, entry *MarketEntry, report func(received, total int64)) (string, error) {
	client, err := marketClient(s.settings.MarketProxy(), marketDownloadTimeout)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", entry.DownloadURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	tmp, err := os.CreateTemp("", "cph-*.cphplugin")
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	src := &progressReader{r: resp.Body, total: resp.ContentLength, report: report}
	n, err := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(src, maxPluginPackageBytes))
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if n >= maxPluginPackageBytes {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("插件包超过 %dMB 上限", maxPluginPackageBytes>>20)
	}
	tmp.Close()
	if entry.SHA256 != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if got != entry.SHA256 {
			os.Remove(tmp.Name())
			return "", fmt.Errorf("sha256 mismatch: want %s got %s", entry.SHA256, got)
		}
	}
	return tmp.Name(), nil
}

var _ = plugin.Manager{} // 保持引用（安装逻辑在 manager 侧）

// progressReader 下载进度读取器：按块回调，200ms 节流避免刷爆前端。
type progressReader struct {
	r      io.Reader
	total  int64
	read   int64
	last   time.Time
	report func(received, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.read += int64(n)
		now := time.Now()
		done := err == io.EOF
		if p.report != nil && (done || now.Sub(p.last) >= 200*time.Millisecond) {
			p.last = now
			p.report(p.read, p.total)
		}
	}
	return n, err
}
