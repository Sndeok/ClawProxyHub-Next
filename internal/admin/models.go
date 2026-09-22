// models.go — 模型目录聚合：已保存的账号模型并集。
package admin

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// modelView 模型中心一行：账号目录并集 + 选型元数据（同 id 取首个非空值）。
// mergeModelMeta 同 id 的多个账号目录合并：只补空缺字段，不覆盖已有值。
func mergeModelMeta(v *modelView, m *pb.ModelInfo) {
	if v.Label == nil && len(m.Label) > 0 {
		v.Label = m.Label
	}
	if v.Series == "" {
		v.Series = m.Series
	}
	if v.ContextWindow == 0 {
		v.ContextWindow = m.ContextWindow
	}
	if v.MaxOutputTokens == 0 {
		v.MaxOutputTokens = m.MaxOutputTokens
	}
	if len(v.ReasoningEfforts) == 0 {
		v.ReasoningEfforts = m.ReasoningEfforts
	}
	if v.DefaultReasoningEffort == "" {
		v.DefaultReasoningEffort = m.DefaultReasoningEffort
	}
	if v.CreditsMultiplier == 0 {
		v.CreditsMultiplier = m.CreditsMultiplier
	}
	if len(v.Tags) == 0 {
		v.Tags = m.Tags
	}
	if v.Description == "" {
		v.Description = m.Description
	}
}

type modelView struct {
	ID       string `json:"id"`
	Accounts int    `json:"accounts"` // 有多少个活跃账号提供该模型
	Plugin   string `json:"plugin"`   // 首个提供者的插件名

	Label                  map[string]string `json:"label,omitempty"`
	Series                 string            `json:"series,omitempty"`
	ContextWindow          int32             `json:"context_window,omitempty"`
	MaxOutputTokens        int32             `json:"max_output_tokens,omitempty"`
	ReasoningEfforts       []string          `json:"reasoning_efforts,omitempty"`
	DefaultReasoningEffort string            `json:"default_reasoning_effort,omitempty"`
	CreditsMultiplier      float64           `json:"credits_multiplier,omitempty"`
	Tags                   []string          `json:"tags,omitempty"`
	Description            string            `json:"description,omitempty"`
}

// syncAccountModels POST /admin/models/sync-accounts —— 逐个账号拉上游模型目录。
// 模型中心的一键同步：目录是「账号可见模型」的来源，刷新后直连模型立即生效。
func (s *Server) syncAccountModels(w http.ResponseWriter, r *http.Request) {
	var accts []model.Account
	if err := s.db.Where("status = ?", "active").Order("id").Find(&accts).Error; err != nil {
		http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
		return
	}
	refreshed, failed := 0, 0
	failures := make([]opFailure, 0)
	for _, a := range accts {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		_, err := s.accounts.SyncModels(ctx, a.ID)
		cancel()
		if err != nil {
			failed++
			failures = append(failures, opFailure{ID: a.ID, Name: a.DisplayName, Message: err.Error()})
			continue
		}
		refreshed++
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total": len(accts), "refreshed": refreshed, "failed": failed, "items": failures,
	})
}

// listModels GET /admin/models?plugin=workbuddy
// 账号已保存（同步/勾选）的模型目录并集，供路由与账号编辑弹窗选择，
// 免去用户手打上游模型名。没同步过模型目录的账号不贡献条目。
func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	q := s.db.Model(&model.Account{}).Where("status = ?", "active")
	if name := r.URL.Query().Get("plugin"); name != "" {
		var p model.Plugin
		if err := s.db.Where("name = ?", name).First(&p).Error; err != nil {
			http.Error(w, `{"error":"unknown plugin"}`, http.StatusNotFound)
			return
		}
		q = q.Where("plugin_id = ?", p.ID)
	}
	var accts []model.Account
	if err := q.Order("id").Find(&accts).Error; err != nil {
		http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
		return
	}
	var plugs []model.Plugin
	s.db.Find(&plugs)
	pluginName := make(map[int64]string, len(plugs))
	for _, p := range plugs {
		pluginName[p.ID] = p.Name
	}

	var out []modelView
	index := map[string]int{}
	for _, a := range accts {
		for _, m := range s.accounts.StoredModels(a.ID) {
			if m.Id == "" {
				continue
			}
			i, ok := index[m.Id]
			if !ok {
				i = len(out)
				index[m.Id] = i
				out = append(out, modelView{ID: m.Id, Plugin: pluginName[a.PluginID]})
			}
			out[i].Accounts++
			mergeModelMeta(&out[i], m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, http.StatusOK, map[string]interface{}{"models": out})
}
