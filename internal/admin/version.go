// version.go — 核心版本回显与检查更新（远端清单经插件市场代理出站）。
package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Sndeok/ClawProxyHub-Next/internal/version"
)

// remoteVersionURL 远端版本清单（仓库根 version.json，走 GitHub 加速前缀）。
// 二开：指向自建核心仓库。
const remoteVersionURL = "https://raw.githubusercontent.com/Sndeok/ClawProxyHub/main/version.json"

// releaseURL 版本发布页（前端「有更新」跳转）。
const releaseURL = "https://github.com/Sndeok/ClawProxyHub/releases"

// remoteManifest 远端 version.json 结构。changelog 是可选字段，兼容旧版只含
// version/release_url 的清单。
type remoteManifest struct {
	Version    string `json:"version"`
	ReleaseURL string `json:"release_url"`
	Changelog  struct {
		Title map[string]string   `json:"title"`
		Items []map[string]string `json:"items"`
	} `json:"changelog"`
}

// coreVersion GET /admin/version — 本机版本 + 远端最新版对比。
// 远端不可达时静默降级，只回本机版本；仅远端严格大于本机时提示更新，
// 避免本地开发版领先或远端版本回退时误报。
func (s *Server) coreVersion(w http.ResponseWriter, r *http.Request) {
	out := map[string]interface{}{"version": version.Core}
	if m := s.fetchLatestVersion(); m != nil && m.Version != "" {
		out["latest"] = m.Version
		out["update_available"] = compareSemver(m.Version, version.Core) > 0
		if m.ReleaseURL != "" {
			out["release_url"] = m.ReleaseURL
		} else {
			out["release_url"] = releaseURL
		}
		if len(m.Changelog.Items) > 0 || len(m.Changelog.Title) > 0 {
			out["changelog"] = m.Changelog
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// fetchLatestVersion 拉远端 version.json；任何失败返回 nil（不阻塞前端）。
// 与插件市场共用出站代理配置（network.market_proxy），便于内网环境走 socks5 出口。
func (s *Server) fetchLatestVersion() *remoteManifest {
	client, err := marketClient(s.settings.MarketProxy(), marketIndexTimeout)
	if err != nil {
		return nil
	}
	resp, err := client.Get(s.withGitHubProxy(remoteVersionURL))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var m remoteManifest
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m) != nil {
		return nil
	}
	return &m
}

// compareSemver 逐段比较点分版本号：忽略 v 前缀与预发布/构建后缀。
// 例：v1.0.5-beta > 1.0.3，1.0 == 1.0.0，1.0.10 > 1.0.9。
func compareSemver(a, b string) int {
	pa := splitSemver(a)
	pb := splitSemver(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

// splitSemver 取版本主体的数值段（如 "v1.0.5-beta" -> [1,0,5]）。
// 非数字段按 0 处理，保持版本检查接口容错而不阻塞服务。
func splitSemver(s string) []int {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	nums := make([]int, len(parts))
	for i, p := range parts {
		nums[i], _ = strconv.Atoi(p)
	}
	return nums
}