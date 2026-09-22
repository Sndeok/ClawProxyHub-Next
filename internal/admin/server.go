// Package admin — 管理后台 API。管理员密码存 users 表（bcrypt），
// 首启经 /setup 引导设置；CPH_ADMIN_PASSWORD 仅作容器化引导注入。
package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"google.golang.org/protobuf/encoding/protojson"
	"gorm.io/gorm"

	"github.com/Sndeok/ClawProxyHub-Next/internal/account"
	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
	"github.com/Sndeok/ClawProxyHub-Next/internal/plugin"
	"github.com/Sndeok/ClawProxyHub-Next/internal/router"
	"github.com/Sndeok/ClawProxyHub-Next/internal/setting"
	"github.com/Sndeok/ClawProxyHub-Next/internal/task"
	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// Server 管理后台。
type Server struct {
	db             *gorm.DB
	accounts       *account.Service
	plugins        *plugin.Manager
	engine         *task.Engine
	settings       *setting.Store
	marketplaceURL string
	routes         *router.Router // 会话粘性策略热更新用（可空）
}

// New 创建管理后台；表空且配置了 CPH_ADMIN_PASSWORD 时自动引导建号。
// marketplaceURL / marketProxy 来自环境变量，仅作为首次启动的默认值写入设置表。
func New(db *gorm.DB, accounts *account.Service, plugins *plugin.Manager, engine *task.Engine, settings *setting.Store, marketplaceURL, marketProxy string, routes *router.Router) *Server {
	s := &Server{
		db: db, accounts: accounts, plugins: plugins, engine: engine,
		settings: settings, marketplaceURL: marketplaceURL, routes: routes,
	}
	// 市场地址与出站代理初始化：未配置时落环境变量默认值（env 缺省 = 内置默认 / 直连），
	// 用户后续可在系统设置修改；生效顺序：settings 配置 > env 默认 > 离线兜底
	settings.EnsureDefault(setting.KeyMarketplaceURL, marketplaceURL)
	settings.EnsureDefault(setting.KeyMarketProxy, marketProxy)
	s.ensureAdminSeed()
	return s
}

// Handler 管理路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// 首启引导（免鉴权）
	mux.HandleFunc("GET /admin/setup-status", s.setupStatus)
	mux.HandleFunc("POST /admin/setup", s.setup)
	// 管理操作（需鉴权）
	mux.HandleFunc("POST /admin/password", s.auth(s.changePassword))
	mux.HandleFunc("GET /admin/me", s.auth(s.me))
	mux.HandleFunc("GET /admin/plugins", s.auth(s.listPlugins))
	mux.HandleFunc("GET /admin/plugins/installed", s.auth(s.listInstalledPlugins))
	mux.HandleFunc("GET /admin/plugins/{name}/auth-methods", s.auth(s.authMethods))
	mux.HandleFunc("GET /admin/plugins/{name}/settings", s.auth(s.pluginSettings))
	mux.HandleFunc("GET /admin/plugins/{name}/task-capabilities", s.auth(s.pluginTaskCapabilities))
	mux.HandleFunc("PUT /admin/plugins/{name}/settings", s.auth(s.putPluginSettings))
	// 插件分发
	mux.HandleFunc("GET /admin/plugins/marketplace", s.auth(s.marketplace))
	mux.HandleFunc("POST /admin/plugins/install-market", s.auth(s.installMarket))
	mux.HandleFunc("POST /admin/plugins/install-upload", s.auth(s.installUpload))
	mux.HandleFunc("POST /admin/plugins/{name}/stop", s.auth(s.stopPlugin))
	mux.HandleFunc("POST /admin/plugins/{name}/start", s.auth(s.startPlugin))
	mux.HandleFunc("DELETE /admin/plugins/{name}", s.auth(s.uninstallPlugin))
	mux.HandleFunc("POST /admin/accounts/login", s.auth(s.submitLogin))
	mux.HandleFunc("GET /admin/accounts", s.auth(s.listAccounts))
	mux.HandleFunc("GET /admin/accounts/{id}/detail", s.auth(s.accountDetail))
	mux.HandleFunc("GET /admin/accounts/{id}/models", s.auth(s.accountModels))
	mux.HandleFunc("DELETE /admin/accounts/{id}", s.auth(s.deleteAccount))
	mux.HandleFunc("POST /admin/accounts/{id}/refresh", s.auth(s.refreshAccount))
	mux.HandleFunc("POST /admin/accounts/refresh-all", s.auth(s.refreshAllAccounts))
	mux.HandleFunc("POST /admin/accounts/{id}/pause", s.auth(s.pauseAccount))
	mux.HandleFunc("POST /admin/accounts/{id}/resume", s.auth(s.resumeAccount))
	mux.HandleFunc("PUT /admin/accounts/{id}", s.auth(s.updateAccount))
	mux.HandleFunc("PUT /admin/accounts/{id}/models", s.auth(s.saveAccountModels))
	mux.HandleFunc("GET /admin/accounts/{id}/proxies", s.auth(s.listAccountProxies))
	mux.HandleFunc("PUT /admin/accounts/{id}/proxies", s.auth(s.bindAccountProxies))
	mux.HandleFunc("POST /admin/accounts/{id}/test", s.auth(s.testAccount))
	mux.HandleFunc("GET /admin/keys", s.auth(s.listKeys))
	mux.HandleFunc("GET /admin/keys/{id}/reveal", s.auth(s.revealKey))
	mux.HandleFunc("PUT /admin/keys/{id}", s.auth(s.updateKey))
	mux.HandleFunc("POST /admin/keys", s.auth(s.createKey))
	mux.HandleFunc("DELETE /admin/keys/{id}", s.auth(s.deleteKey))
	mux.HandleFunc("POST /admin/keys/{id}/toggle", s.auth(s.toggleKey))
	mux.HandleFunc("PUT /admin/keys/{id}/routes", s.auth(s.bindKeyRoutes))
	mux.HandleFunc("GET /admin/groups", s.auth(s.listGroups))
	mux.HandleFunc("POST /admin/groups", s.auth(s.createGroup))
	mux.HandleFunc("DELETE /admin/groups/{id}", s.auth(s.deleteGroup))
	mux.HandleFunc("PUT /admin/groups/{id}/proxies", s.auth(s.bindGroupProxies))
	mux.HandleFunc("GET /admin/groups/{id}/proxies", s.auth(s.listGroupProxies))
	mux.HandleFunc("GET /admin/proxies", s.auth(s.listProxies))
	mux.HandleFunc("POST /admin/proxies", s.auth(s.createProxy))
	mux.HandleFunc("PUT /admin/proxies/{id}", s.auth(s.updateProxy))
	mux.HandleFunc("DELETE /admin/proxies/{id}", s.auth(s.deleteProxy))
	mux.HandleFunc("POST /admin/proxies/{id}/test", s.auth(s.testProxy))
	mux.HandleFunc("GET /admin/models", s.auth(s.listModels))
	mux.HandleFunc("GET /admin/routes", s.auth(s.listRoutes))
	mux.HandleFunc("POST /admin/routes", s.auth(s.createRoute))
	mux.HandleFunc("PUT /admin/routes/{id}", s.auth(s.updateRoute))
	mux.HandleFunc("DELETE /admin/routes/{id}", s.auth(s.deleteRoute))
	mux.HandleFunc("POST /admin/routes/sync-models", s.auth(s.syncRoutes))
	mux.HandleFunc("GET /admin/settings", s.auth(s.getSettings))
	mux.HandleFunc("PUT /admin/settings", s.auth(s.putSettings))
	mux.HandleFunc("POST /admin/settings/test-market", s.auth(s.testMarket))
	mux.HandleFunc("GET /admin/task-rules", s.auth(s.listTaskRules))
	mux.HandleFunc("POST /admin/task-rules", s.auth(s.createTaskRule))
	mux.HandleFunc("POST /admin/task-rules/{id}/toggle", s.auth(s.toggleTaskRule))
	mux.HandleFunc("DELETE /admin/task-rules/{id}", s.auth(s.deleteTaskRule))
	mux.HandleFunc("POST /admin/task-rules/{id}/run", s.auth(s.runTaskRule))
	mux.HandleFunc("POST /admin/task-rules/run-all", s.auth(s.runAllTaskRules))
	mux.HandleFunc("GET /admin/task-runs", s.auth(s.listTaskRuns))
	mux.HandleFunc("GET /admin/logs", s.auth(s.listLogs))
	mux.HandleFunc("POST /admin/logs/cleanup", s.auth(s.logCleanup))
	mux.HandleFunc("GET /admin/stats", s.auth(s.dashboardStats))
	mux.HandleFunc("GET /admin/stats/quota", s.auth(s.dashboardQuota))
	mux.HandleFunc("GET /admin/stats/trend", s.auth(s.dashboardTrend))
	mux.HandleFunc("GET /admin/version", s.auth(s.coreVersion))
	return mux
}

// ---------- 视图映射（proto → JSON，前端直接消费） ----------

// ---------- 工具 ----------

// brandName 插件品牌名：manifest.label.zh 优先，缺省用插件 id。
func brandName(m *pb.Manifest) string {
	if v, ok := m.Label["zh"]; ok && v != "" {
		return v
	}
	return m.Name
}

// pluginBrandByID 品牌名 by 插件 id（优先运行实例，回退 DB manifest 快照解析）。
func (s *Server) pluginBrandByID(pluginID int64) string {
	var p model.Plugin
	if err := s.db.First(&p, pluginID).Error; err != nil {
		return ""
	}
	if inst, ok := s.plugins.Get(p.Name); ok {
		return brandName(inst.Manifest)
	}
	var m pb.Manifest
	if err := protojson.Unmarshal([]byte(p.ManifestJSON), &m); err == nil {
		if v := m.Label["zh"]; v != "" {
			return v
		}
	}
	return p.Name
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func readBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return false
	}
	return true
}

func parseInt(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
