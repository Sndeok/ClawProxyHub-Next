// settings.go — 系统设置 API（网关全局参数）。
package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sndeok/ClawProxyHub-Next/internal/setting"
)

// getSettings GET /admin/settings
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"settings": map[string]interface{}{
			"first_event_timeout": int(s.settings.FirstEventTimeout().Seconds()),
			"github_proxy":        s.settings.GitHubProxy(),
			"marketplace_url":     s.marketURL(),
			"market_proxy":        s.settings.MarketProxy(),
			"log_retention_days":  s.settings.LogRetentionDays(),
			// 出站标识（留空 = 插件内置默认）
			"outbound_user_agent":     s.settings.Get(setting.KeyOutboundUserAgent, ""),
			"outbound_client_name":    s.settings.Get(setting.KeyOutboundClientName, ""),
			"outbound_client_version": s.settings.Get(setting.KeyOutboundClientVersion, ""),
			"outbound_cli_version":    s.settings.Get(setting.KeyOutboundCLIVersion, ""),
			// 会话粘性策略
			"sticky_ttl":            shortDuration(s.settings.StickyTTL()),
			"sticky_cleanup_period": shortDuration(s.settings.StickyCleanPeriod()),
		},
	})
}

// putSettings PUT /admin/settings — body: {first_event_timeout, github_proxy,
// marketplace_url, market_proxy, log_retention_days}。
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FirstEventTimeout int    `json:"first_event_timeout"`
		GitHubProxy       string `json:"github_proxy"`
		MarketplaceURL    string `json:"marketplace_url"`
		MarketProxy       string `json:"market_proxy"`
		LogRetentionDays  int    `json:"log_retention_days"`
		// 出站标识
		OutboundUserAgent     string `json:"outbound_user_agent"`
		OutboundClientName    string `json:"outbound_client_name"`
		OutboundClientVersion string `json:"outbound_client_version"`
		OutboundCLIVersion    string `json:"outbound_cli_version"`
		// 会话粘性
		StickyTTL           string `json:"sticky_ttl"`
		StickyCleanupPeriod string `json:"sticky_cleanup_period"`
	}
	if !readBody(w, r, &body) {
		return
	}
	if body.FirstEventTimeout < 5 || body.FirstEventTimeout > 3600 {
		http.Error(w, `{"error":"首事件超时需在 5–3600 秒之间"}`, http.StatusBadRequest)
		return
	}
	proxy := strings.TrimSuffix(strings.TrimSpace(body.GitHubProxy), "/")
	if proxy != "" && !strings.HasPrefix(proxy, "http://") && !strings.HasPrefix(proxy, "https://") {
		http.Error(w, `{"error":"GitHub 代理需以 http:// 或 https:// 开头（如 https://ghproxy.com），留空则直连"}`, http.StatusBadRequest)
		return
	}
	marketURL := strings.TrimSpace(body.MarketplaceURL)
	if marketURL != "" && !strings.HasPrefix(marketURL, "http://") && !strings.HasPrefix(marketURL, "https://") {
		http.Error(w, `{"error":"插件市场地址需以 http:// 或 https:// 开头（指向 index.json），留空则用默认地址"}`, http.StatusBadRequest)
		return
	}
	marketProxy := strings.TrimSpace(body.MarketProxy)
	if !validMarketProxy(marketProxy) {
		http.Error(w, `{"error":"代理地址无效：支持 socks5://host:port、socks5://user:pass@host:port、http://host:port（省略协议头按 socks5 处理）"}`, http.StatusBadRequest)
		return
	}
	if body.LogRetentionDays < 0 || body.LogRetentionDays > 3650 {
		http.Error(w, `{"error":"日志保留天数需在 0–3650 之间（0 = 永久保留）"}`, http.StatusBadRequest)
		return
	}
	s.settings.Set(setting.KeyFirstEventTimeout, strconv.Itoa(body.FirstEventTimeout))
	s.settings.Set(setting.KeyGitHubProxy, proxy)
	s.settings.Set(setting.KeyMarketplaceURL, marketURL)
	s.settings.Set(setting.KeyMarketProxy, marketProxy)
	s.settings.Set(setting.KeyLogRetentionDays, strconv.Itoa(body.LogRetentionDays))
	// 出站标识：只做长度与换行校验（UA 里出现 CR/LF 会有请求头注入风险）
	identity := map[string]string{
		setting.KeyOutboundUserAgent:     strings.TrimSpace(body.OutboundUserAgent),
		setting.KeyOutboundClientName:    strings.TrimSpace(body.OutboundClientName),
		setting.KeyOutboundClientVersion: strings.TrimSpace(body.OutboundClientVersion),
		setting.KeyOutboundCLIVersion:    strings.TrimSpace(body.OutboundCLIVersion),
	}
	for _, v := range identity {
		if len(v) > 256 || strings.ContainsAny(v, "\r\n") {
			http.Error(w, `{"error":"出站标识含非法字符（换行）或过长（>256）"}`, http.StatusBadRequest)
			return
		}
	}
	// 会话粘性：非法时长直接拒绝，避免用户以为生效了其实回退默认
	if strings.TrimSpace(body.StickyTTL) == "" {
		body.StickyTTL = s.settings.StickyTTL().String()
	}
	if strings.TrimSpace(body.StickyCleanupPeriod) == "" {
		body.StickyCleanupPeriod = s.settings.StickyCleanPeriod().String()
	}
	stickyTTL, err := parseStickyDuration(body.StickyTTL, time.Minute, 24*time.Hour)
	if err != nil {
		http.Error(w, `{"error":"会话保持时长格式无效：用 Go 时长写法（30m / 1h），范围 1m–24h"}`, http.StatusBadRequest)
		return
	}
	stickyClean, err := parseStickyDuration(body.StickyCleanupPeriod, 30*time.Second, 24*time.Hour)
	if err != nil {
		http.Error(w, `{"error":"会话清理周期格式无效：用 Go 时长写法（5m / 10m），范围 30s–24h"}`, http.StatusBadRequest)
		return
	}
	for k, v := range identity {
		s.settings.Set(k, v)
	}
	s.settings.Set(setting.KeyStickyTTL, stickyTTL.String())
	s.settings.Set(setting.KeyStickyCleanPeriod, stickyClean.String())
	// 立即对运行中的路由生效（不必重启）
	if s.routes != nil {
		s.routes.SetStickyPolicy(stickyTTL, stickyClean)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// testMarket POST /admin/settings/test-market — 用给定（或当前生效）的地址与代理
// 试拉一次市场索引，返回条数 / 耗时 / 插件清单，便于在页面上确认代理是否通。
// 请求体可省略；传入的值不落库。
func (s *Server) testMarket(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MarketplaceURL string `json:"marketplace_url"`
		MarketProxy    string `json:"market_proxy"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)

	rawURL := strings.TrimSpace(body.MarketplaceURL)
	if rawURL == "" {
		rawURL = s.marketURL()
	}
	rawProxy := strings.TrimSpace(body.MarketProxy)
	if rawProxy == "" {
		rawProxy = s.settings.MarketProxy()
	}
	target := s.withGitHubProxy(rawURL)

	client, err := marketClient(rawProxy, marketIndexTimeout)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": false, "error": err.Error(), "url": target, "proxy": rawProxy,
		})
		return
	}
	started := time.Now()
	entries, err := fetchIndex(client, target)
	elapsed := time.Since(started).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": false, "error": err.Error(), "url": target, "proxy": rawProxy, "elapsed_ms": elapsed,
		})
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name+"@"+e.Version)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "url": target, "proxy": rawProxy,
		"elapsed_ms": elapsed, "count": len(entries), "plugins": names,
	})
}

// parseStickyDuration 解析用户输入的时长（空 = 用默认值）。
func parseStickyDuration(raw string, min, max time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty")
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if d < min || d > max {
		return 0, fmt.Errorf("out of range")
	}
	return d, nil
}

// shortDuration 人类可读的时长：1h / 30m / 90s（Go 默认会输出 30m0s，界面不好看）。
func shortDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}
