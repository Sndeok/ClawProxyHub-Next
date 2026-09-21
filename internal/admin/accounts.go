// accounts.go — 账号状态调度与详情。
package admin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"gorm.io/gorm"

	"github.com/Sndeok/ClawProxyHub-Next/internal/account"
	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// updateAccount PUT /admin/accounts/{id} — body: {display_name?, group_ids?}
// group_ids 给定即全量替换账号分组（须同插件分组），空数组 = 移出全部分组。
func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	id := parseInt(r.PathValue("id"))
	var body struct {
		DisplayName string  `json:"display_name"`
		GroupIDs    []int64 `json:"group_ids"`
		HasGroups   bool    `json:"-"`
	}
	if !readBody(w, r, &body) {
		return
	}
	var acct model.Account
	if err := s.db.First(&acct, id).Error; err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	if body.DisplayName != "" {
		s.db.Model(&acct).Update("display_name", body.DisplayName)
	}
	if body.GroupIDs != nil {
		// 分组必须属于同一插件
		for _, gid := range body.GroupIDs {
			var g model.Group
			if err := s.db.First(&g, gid).Error; err != nil || g.PluginID != acct.PluginID {
				http.Error(w, `{"error":"分组不存在或与账号插件不一致"}`, http.StatusBadRequest)
				return
			}
		}
		s.db.Where("account_id = ?", id).Delete(&model.AccountGroup{})
		for _, gid := range body.GroupIDs {
			s.db.Create(&model.AccountGroup{AccountID: id, GroupID: gid})
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// accountGroupIDs 账号的全部分组 id。
func accountGroupIDs(db *gorm.DB, accountID int64) []int64 {
	var ids []int64
	db.Model(&model.AccountGroup{}).Where("account_id = ?", accountID).
		Order("group_id").Pluck("group_id", &ids)
	return ids
}

// pauseAccount POST /admin/accounts/{id}/pause — 手动停用调度（不参与选号，需手动恢复）。
func (s *Server) pauseAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	readBody(w, r, &body) // body 可省略
	s.db.Model(&model.Account{}).Where("id = ?", parseInt(r.PathValue("id"))).
		Updates(map[string]interface{}{
			"status":       "disabled",
			"pause_reason": truncStr(body.Reason, 250),
		})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// resumeAccount POST /admin/accounts/{id}/resume — 恢复调度（清自动暂停）。
// expired 账号恢复为 active 前提是凭据已重新可用，统一交由用户判断；此处一并置 active。
func (s *Server) resumeAccount(w http.ResponseWriter, r *http.Request) {
	s.db.Model(&model.Account{}).Where("id = ?", parseInt(r.PathValue("id"))).
		Updates(map[string]interface{}{
			"status": "active", "paused_until": nil, "pause_reason": "",
		})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// accountModels GET /admin/accounts/{id}/models — 默认读库快照；?refresh=1 才实时拉上游并落库。
func (s *Server) accountModels(w http.ResponseWriter, r *http.Request) {
	id := parseInt(r.PathValue("id"))
	if r.URL.Query().Get("refresh") == "1" {
		models, err := s.accounts.SyncModels(r.Context(), id)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"models": models})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"models": s.accounts.StoredModels(id)})
}

// saveAccountModels PUT /admin/accounts/{id}/models — 存用户勾选的模型目录（以用户为准）。
func (s *Server) saveAccountModels(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Models []json.RawMessage `json:"models"`
	}
	if !readBody(w, r, &body) {
		return
	}
	models := make([]*pb.ModelInfo, 0, len(body.Models))
	for _, raw := range body.Models {
		m := &pb.ModelInfo{}
		if protojson.Unmarshal(raw, m) == nil && m.Id != "" {
			models = append(models, m)
		}
	}
	s.accounts.SaveModels(parseInt(r.PathValue("id")), models)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// testAccount POST /admin/accounts/{id}/test — body: {endpoint, model, question?}
// 用账号凭据直调插件 Chat，绕过路由/key，收集事件为日志返回，不落 request_logs。
func (s *Server) testAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint string `json:"endpoint"`
		Model    string `json:"model"`
		Question string `json:"question"`
	}
	if !readBody(w, r, &body) {
		return
	}
	var acct model.Account
	if err := s.db.First(&acct, parseInt(r.PathValue("id"))).Error; err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	pluginName := pluginNameByID(s.db, acct.PluginID)
	if pluginName == "" {
		http.Error(w, `{"error":"plugin not found"}`, http.StatusBadRequest)
		return
	}
	question := body.Question
	if question == "" {
		question = "你好，请用一句话自我介绍。"
	}
	req := &pb.ChatRequest{
		Model:  body.Model,
		Source: body.Endpoint,
		Messages: []*pb.EnvelopeMessage{
			{Role: "user", Text: question},
		},
	}
	cred := &pb.CredentialBlob{
		AccountId: strconv.FormatInt(acct.ID, 10),
		Blob:      account.DecryptCredential(s.accounts.DataDir(), acct.CredentialBlob),
	}
	if acct.LastRefreshAt != nil {
		cred.UpdatedAt = acct.LastRefreshAt.Unix()
	}
	cred.Proxy = account.ProxyForAccount(s.db, acct.ID)

	// 本仓库保留了可取消 context（重试/超时时能中断上游流，避免 goroutine 泄漏）
	events, err := s.plugins.Chat(r.Context(), req, pluginName, cred)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadGateway)
		return
	}
	var text string
	var logs []string
	for ev := range events {
		switch e := ev.Event.(type) {
		case *pb.StreamEvent_MessageStart:
			logs = append(logs, "→ model: "+e.MessageStart.Model)
		case *pb.StreamEvent_ContentDelta:
			text += e.ContentDelta.Text
		case *pb.StreamEvent_ToolCallDelta:
			logs = append(logs, "→ tool_call: "+e.ToolCallDelta.Name+" "+e.ToolCallDelta.ArgumentsDelta)
		case *pb.StreamEvent_MessageFinish:
			if e.MessageFinish.Usage != nil {
				logs = append(logs, fmt.Sprintf("→ finish: %s (in=%d out=%d)",
					e.MessageFinish.FinishReason, e.MessageFinish.Usage.InputTokens, e.MessageFinish.Usage.OutputTokens))
			} else {
				logs = append(logs, "→ finish: "+e.MessageFinish.FinishReason)
			}
		case *pb.StreamEvent_TaskFailed:
			logs = append(logs, fmt.Sprintf("✗ failed: code=%d %s", e.TaskFailed.Error.GetCode(), e.TaskFailed.Error.GetMessage()))
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"text": text, "logs": logs,
	})
}

// accountDetail GET /admin/accounts/{id}/detail — 账号详情：基本信息 + 套餐/积分 + 任务执行情况。
func (s *Server) accountDetail(w http.ResponseWriter, r *http.Request) {
	var acct model.Account
	if err := s.db.First(&acct, parseInt(r.PathValue("id"))).Error; err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	var runs []model.TaskRun
	s.db.Where("account_id = ?", acct.ID).Order("started_at DESC").Limit(20).Find(&runs)
	var pauseUntil *string
	if acct.PausedUntil != nil {
		t := acct.PausedUntil.Format("2006-01-02 15:04:05")
		pauseUntil = &t
	}
	manualPause := acct.PausedUntil != nil && acct.PausedUntil.After(time.Now().AddDate(50, 0, 0))

	out := map[string]interface{}{
		"id":              acct.ID,
		"plugin_id":       acct.PluginID,
		"group_ids":       accountGroupIDs(s.db, acct.ID),
		"display_name":    acct.DisplayName,
		"status":          acct.Status,
		"pause_reason":    acct.PauseReason,
		"paused_until":    pauseUntil,
		"manual_pause":    manualPause, // true = 需手动恢复（402 无积分等）
		"last_refresh_at": acct.LastRefreshAt,
		"last_used_at":    acct.LastUsedAt,
		"created_at":      acct.CreatedAt,
		"profile":         jsonOrNull(acct.ProfileJSON),
		"credits":         jsonOrNull(acct.CreditsJSON),
		"models":          s.accounts.StoredModels(acct.ID),
		"runs":            s.runViews(runs),
	}
	// 套餐/积分信息在 profile 快照里（插件 GetProfile / 登录返回）
	writeJSON(w, http.StatusOK, out)
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// jsonOrNull 原样透出存储的 JSON 快照（异常时回空对象）。
func jsonOrNull(s string) json.RawMessage {
	if s == "" {
		return json.RawMessage("{}")
	}
	return json.RawMessage(s)
}

// ---------- 账号 CRUD 与登录（从 server.go 拆出） ----------

// submitLogin POST /admin/accounts/login — 提交一步登录（首步或后续步）。
// body: {plugin, method_id, form: {..}, state: "<base64>"}
func (s *Server) submitLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Plugin   string            `json:"plugin"`
		MethodID string            `json:"method_id"`
		Form     map[string]string `json:"form"`
		State    string            `json:"state"`
	}
	if !readBody(w, r, &body) {
		return
	}
	var state []byte
	if body.State != "" {
		var err error
		state, err = base64.StdEncoding.DecodeString(body.State)
		if err != nil {
			http.Error(w, `{"error":"invalid state"}`, http.StatusBadRequest)
			return
		}
	}
	outcome, err := s.accounts.SubmitLogin(r.Context(), body.Plugin, body.MethodID, body.Form, state)
	if err != nil {
		// 业务错误（验证码错误/凭据格式/上游拒绝）用 400：401 专属管理员会话失效，
		// 前端见 401 会清 token 跳登录页
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	if outcome.Next != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"done": false, "next": viewNextStep(outcome.Next)})
		return
	}
	// 建档完成：按插件账号级任务能力自动生成规则（默认停用，任务页手动启用）
	if outcome.AccountID > 0 {
		s.engine.EnsureAccountRules(r.Context(), body.Plugin, outcome.AccountID)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"done": true, "account_id": outcome.AccountID})
}

// listAccounts GET /admin/accounts?plugin=stub
func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	pluginName := r.URL.Query().Get("plugin")
	var accts []model.Account
	q := s.db
	if pluginName != "" {
		var p model.Plugin
		if err := s.db.Where("name = ?", pluginName).First(&p).Error; err != nil {
			http.Error(w, `{"error":"unknown plugin"}`, http.StatusNotFound)
			return
		}
		q = q.Where("plugin_id = ?", p.ID)
	}
	if err := q.Order("id").Find(&accts).Error; err != nil {
		http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
		return
	}
	type acctView struct {
		ID          int64   `json:"id"`
		PluginID    int64   `json:"plugin_id"`
		GroupIDs    []int64 `json:"group_ids"`
		Name        string  `json:"display_name"`
		Status      string  `json:"status"`
		PauseReason string  `json:"pause_reason"`
		PausedUntil *string `json:"paused_until"`
		RefreshAt   *string `json:"last_refresh_at"`
		Credits     *struct {
			Remaining string `json:"remaining,omitempty"`
			Total     string `json:"total,omitempty"`
			// 积分包到期概览：快过期（7 天内）的剩余合计 + 最近一个到期时间
			Expiring   float64 `json:"expiring,omitempty"`
			NextExpiry string  `json:"next_expiry,omitempty"`
			NextLeft   float64 `json:"next_left,omitempty"`
			Packages   int     `json:"packages,omitempty"`
		} `json:"credits,omitempty"`
		// 今日用量：token / 缓存 / 积分（积分优先取插件上报的逐次累加，缺失时用积分快照差值估算）
		TodayTokens   int64   `json:"today_tokens"`
		TodayCached   int64   `json:"today_cached"`
		TodayCredits  float64 `json:"today_credits"`
		TodayCreditsE bool    `json:"today_credits_estimated"`
		TodayRequests int64   `json:"today_requests"`
	}
	today := todayStatsByAccount(s.db)
	var out []acctView
	for _, a := range accts {
		v := acctView{ID: a.ID, PluginID: a.PluginID, GroupIDs: accountGroupIDs(s.db, a.ID), Name: a.DisplayName,
			Status: a.Status, PauseReason: a.PauseReason}
		if t := today[a.ID]; t != nil {
			v.TodayTokens, v.TodayCached = t.Tokens, t.Cached
			v.TodayRequests, v.TodayCredits = t.Requests, t.Credits
			if t.Credits == 0 && t.HasSnap && t.Snapshot > 0 {
				// 插件还没上报逐次积分：用上游积分快照差值兜底（前端标注估算）
				v.TodayCredits, v.TodayCreditsE = t.Snapshot, true
			}
		}
		if a.PausedUntil != nil {
			t := a.PausedUntil.Format("2006-01-02T15:04:05Z07:00")
			v.PausedUntil = &t
		}
		if a.LastRefreshAt != nil {
			t := a.LastRefreshAt.Format("2006-01-02T15:04:05Z07:00")
			v.RefreshAt = &t
		}
		// 积分列：credits_json 快照里的剩余/总（插件解析了才有，无则不渲染该列）
		if a.CreditsJSON != "" {
			var c struct {
				Total     string `json:"total"`
				Remaining string `json:"remaining"`
			}
			if json.Unmarshal([]byte(a.CreditsJSON), &c) == nil && (c.Total != "" || c.Remaining != "") {
				exp := account.CreditExpiryOf(a.CreditsJSON)
				next := ""
				if !exp.NextAt.IsZero() {
					next = exp.NextAt.Format("2006-01-02 15:04:05")
				}
				v.Credits = &struct {
					Remaining  string  `json:"remaining,omitempty"`
					Total      string  `json:"total,omitempty"`
					Expiring   float64 `json:"expiring,omitempty"`
					NextExpiry string  `json:"next_expiry,omitempty"`
					NextLeft   float64 `json:"next_left,omitempty"`
					Packages   int     `json:"packages,omitempty"`
				}{
					Remaining: c.Remaining, Total: c.Total,
					Expiring: exp.Expiring, NextExpiry: next, NextLeft: exp.NextLeft, Packages: exp.Packages,
				}
			}
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"accounts": out})
}

// deleteAccount DELETE /admin/accounts/{id}
func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	if err := s.accounts.Delete(parseInt(r.PathValue("id"))); err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// refreshAccount POST /admin/accounts/{id}/refresh
func (s *Server) refreshAccount(w http.ResponseWriter, r *http.Request) {
	acct, err := s.accounts.Refresh(r.Context(), parseInt(r.PathValue("id")))
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": acct.Status})
}
