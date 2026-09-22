// Package model — GORM 实体定义。
// schema 变更只经 migrations/*.sql，这里仅做映射，禁止 AutoMigrate。
package model

import "time"

// User 管理员账号（系统设置模块）。
type User struct {
	ID           int64  `gorm:"primaryKey;autoIncrement"`
	Username     string `gorm:"uniqueIndex;size:64"`
	PasswordHash string `gorm:"size:128"` // bcrypt
	Role         string `gorm:"size:16;default:admin"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Plugin 已安装插件。
type Plugin struct {
	ID              int64  `gorm:"primaryKey;autoIncrement"`
	Name            string `gorm:"uniqueIndex;size:64"`
	Version         string `gorm:"size:32"`
	Author          string `gorm:"size:64"`
	ProtocolVersion int32
	ManifestJSON    string    `gorm:"column:manifest_json"`
	SettingsJSON    string    `gorm:"column:settings_json;default:'{}'"`
	Enabled         bool      `gorm:"default:true"`
	InstalledAt     time.Time `gorm:"column:installed_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

// TableName 显式声明，保持与迁移 SQL 的表名一致。
func (Plugin) TableName() string { return "plugins" }

// Account 账号（凭据 blob 由核心代管）。
type Account struct {
	ID             int64  `gorm:"primaryKey;autoIncrement"`
	PluginID       int64  `gorm:"index"`
	DisplayName    string `gorm:"size:128;default:''"`
	CredentialBlob []byte
	ProfileJSON    string `gorm:"column:profile_json;default:'{}'"`
	// 积分明细快照：{"total","used","remaining","packages":[...]}（插件解析上游后写入，读取只走库）
	CreditsJSON string `gorm:"column:credits_json;default:''"`
	// 账号模型目录快照：ModelInfo 数组 JSON，仅同步时写入，读取默认走库
	ModelsJSON string `gorm:"column:models_json;default:''"`
	Status     string `gorm:"size:16;default:active"` // active/disabled/expired
	// 自动暂停（429 限速 / 无积分等触发）：paused_until 到期自动恢复；reason 供展示
	PausedUntil   *time.Time `gorm:"column:paused_until"`
	PauseReason   string     `gorm:"column:pause_reason;size:256;default:''"`
	LastRefreshAt *time.Time `gorm:"column:last_refresh_at"`
	LastUsedAt    *time.Time `gorm:"column:last_used_at"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (Account) TableName() string { return "accounts" }

// AccountGroup 账号↔分组多对多（仅同插件分组的组合有意义，由 API 校验）。
type AccountGroup struct {
	AccountID int64 `gorm:"primaryKey"`
	GroupID   int64 `gorm:"primaryKey"`
}

func (AccountGroup) TableName() string { return "account_groups" }

// Group 分组：某插件下的账号池（plugin_id 限定，跨插件无意义）。
type Group struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	Name      string `gorm:"uniqueIndex;size:64"`
	PluginID  int64  `gorm:"index"`
	Strategy  string `gorm:"size:32;default:round_robin"` // round_robin/random/least_used
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Key 对外密钥（加密存储，可回显）。授权路由为空 = 全部路由。
// KeyCipher：存量为 sha256 hex（仅校验用），新建为 AES-256-GCM 密文（0x01 前缀，可解密回显）。
type Key struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	KeyCipher string `gorm:"uniqueIndex;size:256;column:key_cipher"`
	KeyHash   string `gorm:"column:key_hash;index;size:64"` // SHA-256 hex(raw)
	Name      string `gorm:"size:128;default:''"`
	Enabled   bool   `gorm:"default:true"`
	ExpiresAt *time.Time
	CreatedAt time.Time
}

// KeyRoute key ↔ 路由多对多授权。
type KeyRoute struct {
	KeyID   int64 `gorm:"primaryKey"`
	RouteID int64 `gorm:"primaryKey"`
}

func (KeyRoute) TableName() string { return "key_routes" }

// 负载策略取值。路由的 Strategy 留空 = 跟随全局默认（设置页 route.default_strategy）。
const (
	RouteStrategySticky         = "sticky"
	RouteStrategyStickyExpiring = "sticky_expiring"
	RouteStrategyRoundRobin     = "round_robin"
	RouteStrategyRandom         = "random"
	RouteStrategyLeastUsed      = "least_used"
	RouteStrategyExpiring       = "expiring"
	// RouteStrategyDefault 内置默认：会话内固定账号，新会话先烧快过期积分。
	RouteStrategyDefault = RouteStrategyStickyExpiring
)

// ValidRouteStrategy 是否为已知策略（空串表示跟随全局，也算合法）。
func ValidRouteStrategy(s string) bool {
	switch s {
	case "", RouteStrategySticky, RouteStrategyStickyExpiring, RouteStrategyRoundRobin,
		RouteStrategyRandom, RouteStrategyLeastUsed, RouteStrategyExpiring:
		return true
	}
	return false
}

// IsStickyStrategy 是否需要会话粘性（sticky / sticky_expiring）。
func IsStickyStrategy(s string) bool {
	return s == RouteStrategySticky || s == RouteStrategyStickyExpiring
}

// Route 路由：对外模型名 + 分组（含真实模型映射）权重表。
// 路由名即客户端请求的 model 字段。
type Route struct {
	ID   int64  `gorm:"primaryKey;autoIncrement"`
	Name string `gorm:"uniqueIndex;size:128"` // 对外模型名
	// round_robin / random / least_used / sticky / sticky_expiring（粘性 + 快过期积分优先）/ expiring
	// 注意：这里刻意不写 default 标签 —— GORM 会跳过带 default 的零值字段，
	// 导致「跟随全局（空串）」被列默认值 round_robin 顶掉。列级默认值仅作用于历史数据。
	Strategy   string `gorm:"size:16"`
	GroupsJSON string `gorm:"column:groups_json;default:'[]'"`
	// 首事件超时（秒），0 = 跟随全局设置
	TimeoutSeconds int32 `gorm:"column:timeout_seconds;default:0"`
	// 降级：主分组失败且状态类匹配时切到 failover 分组的指定模型（每次请求至多降一次）
	FailoverEnabled bool   `gorm:"column:failover_enabled;default:false"`
	FailoverOn4xx   bool   `gorm:"column:failover_on_4xx;default:false"`
	FailoverOn5xx   bool   `gorm:"column:failover_on_5xx;default:false"`
	FailoverGroupID *int64 `gorm:"column:failover_group_id"`
	FailoverModel   string `gorm:"column:failover_model;size:128;default:''"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// RouteGroupEntry groups_json 数组元素：分组 + 该分组下使用的真实模型 id。
type RouteGroupEntry struct {
	GroupID int64  `json:"group_id"`
	Weight  int    `json:"weight"`
	Model   string `json:"model"` // 分组对应插件的真实模型 id
}

// Proxy 出站代理。
type Proxy struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	Name      string `gorm:"size:128;default:''"`
	Scheme    string `gorm:"size:16;default:http"`
	Host      string `gorm:"size:255"`
	Port      int32
	Username  string `gorm:"size:128;default:''"`
	Password  string `gorm:"size:128;default:''"`
	CreatedAt time.Time
}

// GroupProxy 分组与代理的多对多绑定。
type GroupProxy struct {
	GroupID int64 `gorm:"primaryKey"`
	ProxyID int64 `gorm:"primaryKey"`
}

func (GroupProxy) TableName() string { return "group_proxies" }

// AccountProxy 账号与代理的多对多绑定（优先级高于分组级）。
type AccountProxy struct {
	AccountID int64 `gorm:"primaryKey"`
	ProxyID   int64 `gorm:"primaryKey"`
}

func (AccountProxy) TableName() string { return "account_proxies" }

// TaskRule 调度规则：核心只描述"何时+对谁"。
type TaskRule struct {
	ID           int64 `gorm:"primaryKey;autoIncrement"`
	PluginID     int64
	CapabilityID string     `gorm:"column:capability_id;size:64"`
	TriggerType  string     `gorm:"column:trigger_type;size:16"` // interval/cron/daily/once
	TriggerValue string     `gorm:"column:trigger_value;size:128"`
	TargetScope  string     `gorm:"column:target_scope;size:16;default:all"` // all/rotate/account_ids
	TargetJSON   string     `gorm:"column:target_json;default:'[]'"`
	Enabled      bool       `gorm:"default:true"`
	LastRunAt    *time.Time `gorm:"column:last_run_at"`
	NextRunAt    *time.Time `gorm:"column:next_run_at;index"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (TaskRule) TableName() string { return "task_rules" }

// TaskRun 任务执行历史。
type TaskRun struct {
	ID        int64 `gorm:"primaryKey;autoIncrement"`
	RuleID    *int64
	AccountID *int64
	Status    string `gorm:"size:16"` // running/success/failed
	Summary   string `gorm:"size:1024;default:''"`
	// 结构化明细快照（插件 RunTask 返回，如成长任务列表）
	DetailJSON   string     `gorm:"column:detail_json;default:''"`
	ErrorMessage string     `gorm:"column:error_message;size:1024;default:''"`
	StartedAt    time.Time  `gorm:"column:started_at"`
	FinishedAt   *time.Time `gorm:"column:finished_at"`
}

func (TaskRun) TableName() string { return "task_runs" }

// RequestLog 调用日志。诊断字段（迁移 000003）用于在仪表盘上直接定位问题，
// 不必再翻进程 stdout。
type RequestLog struct {
	ID        int64 `gorm:"primaryKey;autoIncrement"`
	KeyID     *int64
	PluginID  *int64
	AccountID *int64
	// RequestedModel 客户端请求的模型名（配置路由时为对外别名）；Model 是最终
	// 投递上游的真实模型名。两者不一致即说明路由改写生效。
	RequestedModel string `gorm:"column:requested_model;size:128;default:''"`
	Model          string `gorm:"size:128;default:''"`
	RouteID        *int64 `gorm:"column:route_id"` // 命中的路由（非路由调用为空）
	GroupID        *int64 `gorm:"column:group_id"` // 命中的分组（决定出站代理）
	Protocol       string `gorm:"size:32;default:''"`
	Stream         bool   `gorm:"column:stream;default:false"` // 客户端是否请求流式
	Status         int32
	FinishReason   string `gorm:"column:finish_reason;size:32;default:''"` // stop/tool_calls/length...
	Attempts       int32  `gorm:"column:attempts;default:1"`               // 含重试/换号/降级的总尝试次数
	ErrorType      string `gorm:"column:error_type;size:32;default:''"`    // 空 = 成功
	InputTokens    int32  `gorm:"column:input_tokens;default:0"`
	OutputTokens   int32  `gorm:"column:output_tokens;default:0"`
	LatencyMs      int32  `gorm:"column:latency_ms;default:0"`
	FirstTokenMs   int32  `gorm:"column:first_token_ms;default:0"` // 首字耗时
	CachedTokens   int32  `gorm:"column:cached_tokens;default:0"`  // 缓存命中（读取）token
	// 缓存写入 token（Anthropic cache_creation_input_tokens / OpenAI 系 cache_write_tokens）
	CacheCreationTokens int32   `gorm:"column:cache_creation_tokens;default:0"`
	CreditUsed          float64 `gorm:"column:credit_used;default:0"` // 本次请求消耗积分（插件上报，0 = 未知）
	ClientIP            string  `gorm:"column:client_ip;size:64;default:''"`
	UserAgent           string  `gorm:"column:user_agent;size:256;default:''"`
	ErrorBrief          string  `gorm:"column:error_brief;size:512;default:''"`
	// 完整上游返回 / 客户端请求原文：体积大，只在日志详情接口返回（json:"-" 不进列表）。
	ErrorDetail string    `gorm:"column:error_detail;default:''" json:"-"`
	RequestBody string    `gorm:"column:request_body;default:''" json:"-"`
	CreatedAt   time.Time `gorm:"index"`
}

func (RequestLog) TableName() string { return "request_logs" }

// AccountCreditDaily 账号每日积分快照：用「当天基线 - 当前剩余」推算今日消耗积分。
// samples = 0 表示只有占位没有有效观测，此时不参与展示。
type AccountCreditDaily struct {
	AccountID int64     `gorm:"column:account_id;primaryKey" json:"account_id"`
	Day       string    `gorm:"column:day;primaryKey;size:16" json:"day"`
	Remaining float64   `gorm:"column:remaining;default:0" json:"remaining"`
	Baseline  float64   `gorm:"column:baseline;default:0" json:"baseline"`
	Used      float64   `gorm:"column:used;default:0" json:"used"`
	Samples   int64     `gorm:"column:samples;default:0" json:"samples"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (AccountCreditDaily) TableName() string { return "account_credit_daily" }

// Setting 系统设置 KV。
type Setting struct {
	Key       string `gorm:"primaryKey;size:128"`
	Value     string
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// PluginStorage 插件持久化 KV（按插件隔离）。
type PluginStorage struct {
	Plugin    string `gorm:"primaryKey;size:64"`
	Key       string `gorm:"primaryKey;size:256"`
	Value     []byte
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (PluginStorage) TableName() string { return "plugin_storage" }
