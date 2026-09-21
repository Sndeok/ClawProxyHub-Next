// Package gateway — 对外 HTTP 入口：三协议归一化、鉴权、路由到插件。
package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	accountpkg "github.com/Sndeok/ClawProxyHub-Next/internal/account"
	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
	"github.com/Sndeok/ClawProxyHub-Next/internal/router"
	"github.com/Sndeok/ClawProxyHub-Next/internal/util"
	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// Server 对外网关。
type Server struct {
	db       *gorm.DB
	dataDir  string
	plugins  PluginRegistry
	router   *router.Router
	accounts AccountExpirer
	settings SettingsReader
}

// AccountExpirer 账号管理（由 account.Service 注入）。
type AccountExpirer interface {
	MarkExpired(accountID int64)
	// Refresh 刷新凭据：成功返回更新后的账号，失败时已按需标记过期。
	Refresh(ctx context.Context, accountID int64) (*model.Account, error)
	// MarkAutoPause 自动暂停选号（429 限时恢复 / 402 等手动恢复）。
	MarkAutoPause(accountID int64, reason string, resumeAt *time.Time)
}

// PluginRegistry 由 plugin.Manager 适配：解析模型并代理 Chat 调用。
type PluginRegistry interface {
	// Models 聚合模型目录：model id → plugin name。
	Models() map[string]string
	// ResolveModel 模型名 → 插件名。ok=false 表示模型不存在。
	ResolveModel(model string) (pluginName string, ok bool)
	// Endpoints 插件声明的对外端点方言（空 = 全部支持）。
	Endpoints(pluginName string) []string
	// Chat 发起一次信封请求，返回事件流。pluginName 为空按模型目录解析。
	Chat(ctx context.Context, req *pb.ChatRequest, pluginName string, cred *pb.CredentialBlob) (events chan *pb.StreamEvent, err error)
}

// SettingsReader 全局设置读取（setting.Store 注入，nil 时走默认值）。
type SettingsReader interface {
	FirstEventTimeout() time.Duration
}

// New 创建网关。
func New(db *gorm.DB, dataDir string, plugins PluginRegistry, r *router.Router, accounts AccountExpirer, settings SettingsReader) *Server {
	return &Server{db: db, dataDir: dataDir, plugins: plugins, router: r, accounts: accounts, settings: settings}
}

// Handler 组装网关路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /v1/models", s.handleModels)
	mux.HandleFunc("POST /v1/messages", s.handleMessages)
	mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("POST /v1/responses", s.handleResponses)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleModels 对外模型列表：key 授权的路由名；无路由时 fallback 插件目录。
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authorize(w, r)
	if !ok {
		return
	}
	var data []map[string]interface{}
	for _, name := range s.router.AuthorizedModels(key) {
		data = append(data, map[string]interface{}{
			"id": name, "object": "model", "owned_by": "cph",
		})
	}
	if data == nil {
		// 未配置任何路由：透出插件真实模型名，保持开箱可用
		for id := range s.plugins.Models() {
			data = append(data, map[string]interface{}{
				"id": id, "object": "model", "owned_by": "cph",
			})
		}
	}
	if data == nil {
		data = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"object": "list", "data": data})
}

// handleMessages POST /v1/messages（Anthropic）。
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authorize(w, r)
	if !ok {
		return
	}
	req, ok := s.parseBody(w, r, parseAnthropicRequest)
	if !ok {
		return
	}
	s.serve(w, r, key, req, "messages")
}

// handleChatCompletions POST /v1/chat/completions（OpenAI）。
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authorize(w, r)
	if !ok {
		return
	}
	req, ok := s.parseBody(w, r, parseChatCompletions)
	if !ok {
		return
	}
	s.serve(w, r, key, req, "chat_completions")
}

// handleResponses POST /v1/responses（OpenAI Responses，Codex CLI）。
func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authorize(w, r)
	if !ok {
		return
	}
	req, ok := s.parseBody(w, r, parseResponsesRequest)
	if !ok {
		return
	}
	s.serve(w, r, key, req, "responses")
}

// parseBody 读体并解析为信封，失败时已写响应。
func (s *Server) parseBody(w http.ResponseWriter, r *http.Request, parse func([]byte) (*pb.ChatRequest, error)) (*pb.ChatRequest, bool) {
	if r.ContentLength > 32<<20 {
		writeJSON(w, http.StatusRequestEntityTooLarge, errBody("invalid_request_error", fmt.Errorf("request body exceeds 32MB limit")))
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid body", err))
		return nil, false
	}
	if len(body) == 32<<20 {
		if extra, _ := io.ReadAll(io.LimitReader(r.Body, 1)); len(extra) > 0 {
			writeJSON(w, http.StatusRequestEntityTooLarge, errBody("invalid_request_error", fmt.Errorf("request body exceeds 32MB limit")))
			return nil, false
		}
	}
	req, err := parse(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid_request_error", err))
		return nil, false
	}
	return req, true
}

// authorize 校验 Authorization / X-Api-Key，失败时已写响应。
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) (*model.Key, bool) {
	key := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(key) > len(prefix) && key[:len(prefix)] == prefix {
		key = key[len(prefix):]
	} else {
		key = r.Header.Get("X-Api-Key")
	}
	if key == "" {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return nil, false
	}
	// 快路径：SHA-256(raw) 等值索引（O(1)）
	sum := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(sum[:])
	var k model.Key
	if err := s.db.Where("key_hash = ? AND enabled = ?", hash, true).First(&k).Error; err != nil {
		// 慢路径：存量 key_hash 为空的记录（旧版数据），全量解密比对并回填 hash
		var keys []model.Key
		s.db.Where("key_hash = ?", "").Find(&keys)
		matched := false
		for _, cand := range keys {
			if keyMatches(cand.KeyCipher, key, s.dataDir) {
				k = cand
				matched = true
				s.db.Model(&model.Key{}).Where("id = ?", cand.ID).Update("key_hash", hash)
				break
			}
		}
		if !matched {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return nil, false
		}
	}
	if !k.Enabled {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return nil, false
	}
	if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "key expired"})
		return nil, false
	}
	return &k, true
}

// keyMatches 校验请求密钥与存储密文是否匹配：双通道（加密格式解密比对 / 存量 sha256 hex）。
func keyMatches(cipher string, raw string, dataDir string) bool {
	// 存量：sha256 hex（64 字符）
	if len(cipher) == 64 && cipher[0] != 0x01 {
		sum := sha256.Sum256([]byte(raw))
		return hex.EncodeToString(sum[:]) == cipher
	}
	// 新建：AES-256-GCM 密文（0x01 前缀）
	plain := accountpkg.DecryptCredential(dataDir, []byte(cipher))
	return len(cipher) > 0 && cipher[0] == 0x01 && string(plain) == raw
}

// endpointAllowed 插件端点方言校验：声明为空 = 全支持。
func (s *Server) endpointAllowed(pluginName, protocol string) bool {
	endpoints := s.plugins.Endpoints(pluginName)
	if len(endpoints) == 0 {
		return true
	}
	for _, e := range endpoints {
		if e == protocol {
			return true
		}
	}
	return false
}

// serve 统一出口：路由名解析（fallback 真实模型名）→ 端点能力校验 → 选账号 → 插件流 → 回写 + 落日志。
func (s *Server) serve(w http.ResponseWriter, r *http.Request, key *model.Key, req *pb.ChatRequest, protocol string) {
	start := time.Now()    // 计时基准：包含路由解析 / 选号 / 重试在内的完整请求耗时
	origModel := req.Model // 对外模型名（换号重试时用于重新解析路由）
	req.Source = protocol  // 告知插件客户端进入的协议
	var (
		account *model.Account
		isRoute bool
	)

	// 优先按对外模型名（路由）解析
	resolved, err := s.router.Resolve(key, req)
	if err != nil {
		if err == router.ErrRouteForbidden {
			// 路由存在但 key 未授权：403（区别于 404 未知模型）
			writeJSON(w, http.StatusForbidden, errBody("invalid_request_error",
				fmt.Errorf("key not authorized for route %q", origModel)))
			return
		}
		writeJSON(w, http.StatusBadGateway, errBody("api_error", err))
		return
	}
	var pluginName string
	var groupID int64 // 路由命中的分组（凭据出站代理优先用它）
	if resolved != nil {
		isRoute = true
		req.Model = resolved.RealModel // 对外名 → 分组真实模型
		account = resolved.Account
		if account == nil {
			// 路由分组里没有可用账号（未分组 / 全部过期）——明确报错，
			// 不把空凭据丢给插件
			writeJSON(w, http.StatusServiceUnavailable, errBody("api_error",
				fmt.Errorf("route %q: no active account in group (check account grouping / status)", origModel)))
			return
		}
		// 路由场景从分组取插件：真实模型名直接透传上游，不要求出现在插件目录
		pluginName = resolved.PluginName
		groupID = resolved.GroupID
		if pluginName == "" {
			pluginName, _ = s.plugins.ResolveModel(req.Model)
		}
	} else if pn, ok := s.plugins.ResolveModel(req.Model); ok {
		pluginName = pn
	} else {
		// 不是路由名也不是插件真实模型名
		writeJSON(w, http.StatusNotFound, errBody("invalid_request_error",
			fmt.Errorf("model %q not found", req.Model)))
		return
	}
	// 端点能力：任意协议入口一律归一化为统一信封后投递（自动协议转换）。
	// 插件声明的 endpoints 仅是方言描述：不在声明内时记运营日志，供插件侧
	// 结合 req.Source 做针对性优化（如上游原生 anthropic 直通）。
	if pluginName != "" && !s.endpointAllowed(pluginName, protocol) {
		log.Printf("[gateway] plugin %q declared endpoints %v; %s request converted via envelope",
			pluginName, s.plugins.Endpoints(pluginName), protocol)
	}
	// 未命中路由时 key 若绑定了授权范围，则只允许路由名（安全边界）
	if !isRoute {
		if models := s.router.AuthorizedModels(key); len(models) > 0 {
			writeJSON(w, http.StatusForbidden, errBody("invalid_request_error",
				fmt.Errorf("model %q not in authorized routes", req.Model)))
			return
		}
	}

	var cred *pb.CredentialBlob
	if account != nil {
		cred = s.buildCred(account, groupID)
	}

	// 发起调用。失败恢复顺序：401 先保凭据（刷新同账号 → 换号，保住会话粘性），
	// 穷尽后或非凭据错误走路由降级（状态类匹配，每次请求至多降一次）。
	// 注意：req.Model 已替换为真实模型名，重试解析路由需用原始对外名
	log := &requestLogCtx{start: start, key: key, account: account, requestedModel: origModel, model: req.Model,
		protocol: protocol, stream: req.Stream,
		clientIP: clientIP(r), userAgent: util.TruncStr(r.UserAgent(), 250)}
	var route *model.Route
	if resolved != nil {
		route = resolved.Route
		log.routeID = &resolved.Route.ID
		log.groupID = &resolved.GroupID
	}
	failoverUsed := false
	timeout := s.firstEventTimeout(route)

	// switchAccount 重新解析路由换一个可用账号（不刷新凭据，暂停/过期账号已被选号条件排除）。
	switchAccount := func() bool {
		req.Model = origModel
		again, err := s.router.Resolve(key, req)
		if err == nil && again != nil && again.Account != nil && (account == nil || again.Account.ID != account.ID) {
			account = again.Account
			pluginName = again.PluginName
			groupID = again.GroupID
			log.groupID = &again.GroupID
			req.Model = again.RealModel
			cred = s.buildCred(account, groupID)
			log.account = account
			log.model = req.Model
			return true
		}
		return false
	}

	// recoverCredential 凭据失效恢复：刷新同账号；终态失效标 expired 换号。
	recoverCredential := func() (recovered bool, transient error) {
		if account == nil || s.accounts == nil {
			return false, nil
		}
		refreshed, rerr := s.accounts.Refresh(r.Context(), account.ID)
		if rerr == nil {
			account = refreshed
			cred = s.buildCred(account, groupID)
			log.account = account
			return true, nil
		}
		if !accountpkg.IsAuthFailure(rerr) {
			return false, rerr // 网络抖动 / 上游 5xx：保留账号原状
		}
		// 终态失效（Refresh 内已标记 expired）→ 换号
		if switchAccount() {
			return true, nil
		}
		return false, nil // 换号也无号可用
	}

	// switchFailover 路由降级：状态类匹配且未降过级时切到降级分组（不递归）。
	switchFailover := func(status int) bool {
		if route == nil || !route.FailoverEnabled || failoverUsed {
			return false
		}
		if !failoverMatch(route, status) {
			return false
		}
		res := s.router.PickFailover(route)
		if res == nil || res.Account == nil {
			return false
		}
		failoverUsed = true
		account = res.Account
		pluginName = res.PluginName
		groupID = res.GroupID
		log.groupID = &res.GroupID
		req.Model = res.RealModel
		cred = s.buildCred(account, groupID)
		log.account = account
		log.model = req.Model
		return true
	}

	// recoverFrom 单次失败后的恢复决策。
	// retry=true 已切换可重试；transient 非 nil 表示暂时性失败（保留账号，不降级）。
	recoverFrom := func(status, attempt int, brief string) (retry bool, transient error) {
		switch {
		case status == 401 && attempt < 2 && account != nil && s.accounts != nil:
			recovered, transient := recoverCredential()
			if recovered || transient != nil {
				return recovered, transient
			}
		case (status == 429 || status == 402) && account != nil && s.accounts != nil && attempt < 2:
			// 429 限速：暂停 10 分钟后自动恢复；402 无积分：暂停且需手动恢复
			var resumeAt *time.Time
			if status == 429 {
				t := time.Now().Add(10 * time.Minute)
				resumeAt = &t
			}
			s.accounts.MarkAutoPause(account.ID, brief, resumeAt)
			if switchAccount() {
				return true, nil
			}
		}
		return switchFailover(status), nil
	}

	// failWith 终态失败收尾：写响应 + 落日志。
	failWith := func(status int, errType, brief string) {
		log.status = status
		log.errorType = errType
		log.errBrief = brief
		writeJSON(w, status, errBody(errType, errors.New(brief)))
		log.write(s.db)
	}
	// finishUnrecovered 恢复穷尽后的统一出口（区分 401 无号 503 / 其它 502）。
	finishUnrecovered := func(code int32, brief string, transient error) {
		switch {
		case transient != nil:
			failWith(http.StatusBadGateway, "upstream_error", transient.Error())
		case code == 401:
			failWith(http.StatusServiceUnavailable, "api_error",
				"no active account available (credential failed and failover exhausted)")
		default:
			failWith(http.StatusBadGateway, "api_error", brief)
		}
	}

	attemptCtx, attemptCancel := context.WithCancel(r.Context())
	// 闭包捕获变量：重试换新 context 后，defer 取消的是最后一个（避免泄漏）
	defer func() { attemptCancel() }()

	for attempt := 0; ; attempt++ {
		log.attempts = int32(attempt + 1)
		events, err := s.plugins.Chat(attemptCtx, req, pluginName, cred)
		if err != nil {
			// 通道级失败（插件崩溃等）按 5xx 类参与降级判定
			if retry, _ := recoverFrom(502, attempt, err.Error()); retry {
				attemptCancel()
				attemptCtx, attemptCancel = context.WithCancel(r.Context())
				continue
			}
			failWith(http.StatusBadGateway, "upstream_error", err.Error())
			attemptCancel()
			return
		}
		// 首事件超时兜底：插件/上游挂死时按配置时限返回 504，而不是让客户端永久等待
		var first *pb.StreamEvent
		{
			var ok bool
			select {
			case first, ok = <-events:
				if !ok {
					first = nil
				}
			case <-time.After(timeout):
				if retry, _ := recoverFrom(504, attempt, ""); retry {
					go drain(events)
					attemptCancel()
					attemptCtx, attemptCancel = context.WithCancel(r.Context())
					continue
				}
				failWith(http.StatusGatewayTimeout, "upstream_error",
					fmt.Sprintf("upstream produced no events within %s (check proxy / upstream reachability)", timeout))
				attemptCancel()
				return
			}
		}
		log.firstTokenMs = int32(time.Since(start).Milliseconds())
		if first != nil {
			if code := failedCode(first); code != 0 {
				brief := failedBrief(first)
				retry, transient := recoverFrom(int(code), attempt, brief)
				if retry {
					go drain(events)
					attemptCancel()
					attemptCtx, attemptCancel = context.WithCancel(r.Context())
					continue
				}
				finishUnrecovered(code, brief, transient)
				attemptCancel()
				return
			}
		}
		// channel 无法塞回首事件，经参数带入输出层
		if req.Stream {
			s.streamOut(w, events, first, log, newEncoder(protocol, req.Model), nil)
			attemptCancel()
			return
		}
		if code, brief := s.nonStreamOut(w, events, first, log, newAggregate(protocol, req.Model)); code != 0 {
			// 聚合中途失败且响应未写：尝试恢复后重试
			retry, transient := recoverFrom(int(code), attempt, brief)
			if retry {
				attemptCancel()
				attemptCtx, attemptCancel = context.WithCancel(r.Context())
				continue
			}
			finishUnrecovered(code, brief, transient)
			attemptCancel()
			return
		}
		attemptCancel()
		return
	}
}

// failedCode TaskFailed 事件携带的上游状态码（非失败事件返回 0）。
func failedCode(ev *pb.StreamEvent) int32 {
	if failed, ok := ev.Event.(*pb.StreamEvent_TaskFailed); ok && failed.TaskFailed != nil && failed.TaskFailed.Error != nil {
		return failed.TaskFailed.Error.Code
	}
	return 0
}

// failedBrief TaskFailed 事件的上游错误信息。
func failedBrief(ev *pb.StreamEvent) string {
	if failed, ok := ev.Event.(*pb.StreamEvent_TaskFailed); ok && failed.TaskFailed != nil && failed.TaskFailed.Error != nil {
		return failed.TaskFailed.Error.Message
	}
	return ""
}

// failoverMatch 降级触发状态类：4xx=400–499；5xx=500+（通道失败按 502、超时按 504 计入）。
func failoverMatch(route *model.Route, status int) bool {
	switch {
	case status >= 400 && status < 500:
		return route.FailoverOn4xx
	case status >= 500:
		return route.FailoverOn5xx
	default:
		return false
	}
}

// firstEventTimeout 首事件超时：路由级 > 全局设置 > 90s 默认。
func (s *Server) firstEventTimeout(route *model.Route) time.Duration {
	if route != nil && route.TimeoutSeconds > 0 {
		return time.Duration(route.TimeoutSeconds) * time.Second
	}
	if s.settings != nil {
		if d := s.settings.FirstEventTimeout(); d > 0 {
			return d
		}
	}
	return 90 * time.Second
}

// buildCred 账号 → 凭据信封（groupID 为路由命中的分组，出站代理优先用它）。
func (s *Server) buildCred(account *model.Account, groupID int64) *pb.CredentialBlob {
	cred := &pb.CredentialBlob{
		AccountId: fmt.Sprintf("%d", account.ID),
		Blob:      accountpkg.DecryptCredential(s.dataDir, account.CredentialBlob),
	}
	if account.LastRefreshAt != nil {
		cred.UpdatedAt = account.LastRefreshAt.Unix()
	}
	cred.Proxy = accountpkg.ProxyForAccountIn(s.db, account.ID, groupID)
	return cred
}

// requestLogCtx 单次请求的日志上下文。
type requestLogCtx struct {
	start          time.Time // 请求进入 serve 的时刻（所有耗时口径的唯一基准）
	key            *model.Key
	account        *model.Account
	requestedModel string // 客户端请求的模型名（路由别名）
	model          string // 实际投递上游的模型名
	routeID        *int64
	groupID        *int64
	protocol       string
	stream         bool
	attempts       int32
	finishReason   string
	errorType      string
	input          int64
	output         int64
	cached         int64
	credit         float64 // 本次请求消耗积分（插件上报，0 = 未知）
	status         int
	firstTokenMs   int32
	clientIP       string
	userAgent      string
	errBrief       string
}

// write 落库 request_logs。
// 耗时在这里统一计算：从 serve 入口到落库，成功 / 流式中断 / 失败 / 重试耗尽
// 所有出口共用同一口径。此前由各调用方传入 time.Since(局部 start)，导致
// streamOut / nonStreamOut 只统计「首事件之后的输出阶段」，出现
// 「首字 5001ms、总耗时 1ms」这种自相矛盾的数据。
func (c *requestLogCtx) write(db *gorm.DB) {
	if c.start.IsZero() {
		c.start = time.Now()
	}
	latency := time.Since(c.start)
	attempts := c.attempts
	if attempts < 1 {
		attempts = 1
	}
	rl := &model.RequestLog{
		RequestedModel: c.requestedModel, Model: c.model,
		RouteID: c.routeID, GroupID: c.groupID,
		Protocol: c.protocol, Stream: c.stream, Status: int32(c.status),
		FinishReason: c.finishReason, Attempts: attempts, ErrorType: c.errorType,
		InputTokens: int32(c.input), OutputTokens: int32(c.output),
		CachedTokens: int32(c.cached), CreditUsed: c.credit, LatencyMs: int32(latency.Milliseconds()),
		FirstTokenMs: c.firstTokenMs, ClientIP: c.clientIP, UserAgent: c.userAgent,
		ErrorBrief: c.errBrief,
	}
	if c.key != nil {
		rl.KeyID = &c.key.ID
	}
	if c.account != nil {
		rl.AccountID = &c.account.ID
		rl.PluginID = &c.account.PluginID
	}
	db.Create(rl)
}

// clientIP 提取客户端 IP（反代场景优先 X-Forwarded-For / X-Real-IP）。
func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		if ip, _, ok := strings.Cut(xf, ","); ok || ip != "" {
			return strings.TrimSpace(ip)
		}
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
