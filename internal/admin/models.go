// models.go — 模型目录聚合：已保存的账号模型并集。
package admin

import (
	"net/http"
	"sort"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
)

// modelView 一个模型 id 及其来源统计。
type modelView struct {
	ID       string `json:"id"`
	Accounts int    `json:"accounts"` // 有多少个活跃账号提供该模型
	Plugin   string `json:"plugin"`   // 首个提供者的插件名
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

	counts := map[string]int{}
	pluginOf := map[string]string{}
	for _, a := range accts {
		for _, m := range s.accounts.StoredModels(a.ID) {
			if m.Id == "" {
				continue
			}
			counts[m.Id]++
			if _, ok := pluginOf[m.Id]; !ok {
				pluginOf[m.Id] = pluginName[a.PluginID]
			}
		}
	}
	out := make([]modelView, 0, len(counts))
	for id, n := range counts {
		out = append(out, modelView{ID: id, Accounts: n, Plugin: pluginOf[id]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, http.StatusOK, map[string]interface{}{"models": out})
}
