// Package setting — 系统设置 KV（settings 表）读写，带进程内缓存。
package setting

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
)

// KeyFirstEventTimeout 网关首事件超时（秒）。
const KeyFirstEventTimeout = "gateway.first_event_timeout"

// KeyGitHubProxy GitHub 代理前缀（ghproxy 风格，加速插件市场访问；空 = 直连）。
const KeyGitHubProxy = "network.github_proxy"

// KeyMarketplaceURL 插件市场索引地址（用户自建；空 = 默认）。
const KeyMarketplaceURL = "network.marketplace_url"

// KeyMarketProxy 插件市场的出站代理（拉索引 + 下载插件包共用；空 = 直连）。
// 与 network.github_proxy 的区别：后者是「URL 前缀改写」（ghproxy 风格），
// 只对 GitHub 域名生效；本项是传输层代理，支持 socks5 / socks5h / http(s)。
const KeyMarketProxy = "network.market_proxy"

// KeyModelCatalog 模型中心目录快照（JSON：{updated_at, models:[...]}）。
// 手工导入的目录与账号上报的模型在读取时合并，不单独建表。
const KeyModelCatalog = "models.catalog_json"

// KeyRouteDefaultStrategy 全局默认负载策略：路由未单独配置时生效。
const KeyRouteDefaultStrategy = "route.default_strategy"

// KeyLogRetentionDays 调用日志保留天数（0 = 保留全部，不自动清理）。
const KeyLogRetentionDays = "logs.retention_days"

// DefaultMarketplaceURL 默认插件市场索引地址（初始化时写入设置）。
// 二开：改为自建插件仓库（Sndeok/ClawProxyHubPlugins）的 index.json。
const DefaultMarketplaceURL = "https://raw.githubusercontent.com/Sndeok/ClawProxyHubPlugins/main/index.json"

// 出站标识：网关代插件向上游发起请求时使用的客户端身份。
// 留空 = 用插件内置默认（对齐官方分发包）；插件页单独配置可覆盖这里。
const (
	KeyOutboundUserAgent     = "outbound.user_agent"
	KeyOutboundClientName    = "outbound.client_name"
	KeyOutboundClientVersion = "outbound.client_version"
	KeyOutboundCLIVersion    = "outbound.cli_version"
)

// 会话粘性策略（路由 strategy=sticky 时生效）。
const (
	KeyStickyTTL         = "sticky.ttl"
	KeyStickyCleanPeriod = "sticky.cleanup_interval"
)

const (
	defaultStickyTTL         = 30 * time.Minute
	defaultStickyCleanPeriod = 5 * time.Minute
)

const defaultFirstEventTimeout = 90

// Store 设置存储。
type Store struct {
	db    *gorm.DB
	mu    sync.RWMutex
	cache map[string]string
}

func New(db *gorm.DB) *Store {
	return &Store{db: db, cache: map[string]string{}}
}

// Get 读设置，缺省返回 def。
func (s *Store) Get(key, def string) string {
	s.mu.RLock()
	v, ok := s.cache[key]
	s.mu.RUnlock()
	if ok {
		return v
	}
	var rec model.Setting
	if err := s.db.Where("key = ?", key).First(&rec).Error; err != nil {
		return def
	}
	s.mu.Lock()
	s.cache[key] = rec.Value
	s.mu.Unlock()
	return rec.Value
}

// Set 写设置（upsert + 刷新缓存）。
func (s *Store) Set(key, value string) {
	s.db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`, key, value)
	s.mu.Lock()
	s.cache[key] = value
	s.mu.Unlock()
}

// FirstEventTimeout 网关首事件超时；非法值回退默认。
func (s *Store) FirstEventTimeout() time.Duration {
	n, err := strconv.Atoi(s.Get(KeyFirstEventTimeout, strconv.Itoa(defaultFirstEventTimeout)))
	if err != nil || n <= 0 {
		n = defaultFirstEventTimeout
	}
	return time.Duration(n) * time.Second
}

// GitHubProxy GitHub 代理前缀（以 / 结尾与否均可；空 = 直连）。
func (s *Store) GitHubProxy() string {
	return s.Get(KeyGitHubProxy, "")
}

// LogRetentionDays 调用日志保留天数；非法值按 0（保留全部）处理。
func (s *Store) LogRetentionDays() int {
	n, err := strconv.Atoi(s.Get(KeyLogRetentionDays, "0"))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// MarketplaceURL 用户自建市场地址；空 = 未配置（回退默认）。
func (s *Store) MarketplaceURL() string {
	return s.Get(KeyMarketplaceURL, "")
}

// MarketProxy 插件市场出站代理；空 = 直连（或跟随进程环境代理）。
func (s *Store) MarketProxy() string {
	return strings.TrimSpace(s.Get(KeyMarketProxy, ""))
}

// EnsureDefault key 尚未写入时落默认值（仅初始化场景使用，不覆盖已有配置）。
func (s *Store) EnsureDefault(key, def string) {
	var rec model.Setting
	if err := s.db.Where("key = ?", key).First(&rec).Error; err != nil {
		s.Set(key, def)
	}
}

// OutboundIdentity 出站标识（空值表示用插件内置默认）。
func (s *Store) OutboundIdentity() map[string]string {
	return map[string]string{
		"user_agent":     strings.TrimSpace(s.Get(KeyOutboundUserAgent, "")),
		"client_name":    strings.TrimSpace(s.Get(KeyOutboundClientName, "")),
		"client_version": strings.TrimSpace(s.Get(KeyOutboundClientVersion, "")),
		"cli_version":    strings.TrimSpace(s.Get(KeyOutboundCLIVersion, "")),
	}
}

// RouteDefaultStrategy 全局默认负载策略（非法值回退内置默认）。
func (s *Store) RouteDefaultStrategy() string {
	v := strings.TrimSpace(s.Get(KeyRouteDefaultStrategy, ""))
	if v == "" || !model.ValidRouteStrategy(v) {
		return model.RouteStrategyDefault
	}
	return v
}

// StickyTTL 会话粘性保持时长（一次会话多久没活动就解除绑定）。
func (s *Store) StickyTTL() time.Duration {
	return parseDuration(s.Get(KeyStickyTTL, ""), defaultStickyTTL, time.Minute, 24*time.Hour)
}

// StickyCleanPeriod 会话粘性后台清理周期。
func (s *Store) StickyCleanPeriod() time.Duration {
	return parseDuration(s.Get(KeyStickyCleanPeriod, ""), defaultStickyCleanPeriod, 30*time.Second, 24*time.Hour)
}

// parseDuration 解析时长字符串；非法或越界回退默认值。
func parseDuration(raw string, def, min, max time.Duration) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < min || d > max {
		return def
	}
	return d
}
