// plugins.go — 插件列表与授权方式视图（管理后台 → 前端 JSON）。
package admin

import (
	"encoding/base64"
	"net/http"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

type authFieldView struct {
	Name        string            `json:"name"`
	Label       map[string]string `json:"label"`
	Type        string            `json:"type"`
	Required    bool              `json:"required"`
	Placeholder string            `json:"placeholder"`
}

type authMethodView struct {
	ID           string            `json:"id"`
	Label        map[string]string `json:"label"`
	Fields       []authFieldView   `json:"fields"`
	Capabilities []string          `json:"capabilities"`
	Callback     string            `json:"callback,omitempty"` // auto / wait / auto_wait
}

type nextStepView struct {
	Action string            `json:"action"`
	URL    string            `json:"url,omitempty"`
	Prompt map[string]string `json:"prompt,omitempty"`
	Fields []authFieldView   `json:"fields,omitempty"`
	State  string            `json:"state,omitempty"`
	Wait   bool              `json:"wait,omitempty"`
}

// listPlugins GET /admin/plugins — 已启动插件概览（含授权方式）。
func (s *Server) listPlugins(w http.ResponseWriter, r *http.Request) {
	type pluginView struct {
		ID          int64             `json:"id"`
		Name        string            `json:"name"`
		Label       string            `json:"label"` // 品牌名（关联字段统一显示它）
		Version     string            `json:"version"`
		Author      string            `json:"author"`
		Icon        string            `json:"icon"` // 包内相对路径（空 = 前端兜底）
		Capability  []string          `json:"capabilities"`
		AuthMethods []*authMethodView `json:"auth_methods"`
	}
	var out []pluginView
	for _, name := range s.plugins.Names() {
		inst, ok := s.plugins.Get(name)
		if !ok {
			continue
		}
		m := inst.Manifest
		v := pluginView{Name: m.Name, Label: brandName(m), Version: m.Version, Author: m.Author,
			Capability: m.Capabilities}
		// icon：以落盘文件为准（前端 <img> 直接引用，免鉴权静态端点）
		if _, ok := s.plugins.IconFile(m.Name); ok {
			v.Icon = "/assets/plugins/" + m.Name + "/icon"
		}
		var rec model.Plugin // DB id（建分组/规则时引用）
		if err := s.db.Where("name = ?", m.Name).First(&rec).Error; err == nil {
			v.ID = rec.ID
		}
		for _, am := range m.AuthMethods {
			v.AuthMethods = append(v.AuthMethods, viewAuthMethod(am))
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"plugins": out})
}

// authMethods GET /admin/plugins/{name}/auth-methods — 授权方式详情（渲染 tab + 表单）。
func (s *Server) authMethods(w http.ResponseWriter, r *http.Request) {
	methods, err := s.accounts.AuthMethods(r.PathValue("name"))
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
		return
	}
	var out []*authMethodView
	for _, m := range methods {
		out = append(out, viewAuthMethod(m))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"auth_methods": out})
}

// listInstalledPlugins GET /admin/plugins/installed — DB 记录的已安装插件（含运行状态）。
// 「已安装」列表靠它渲染：停止中的插件也必须可见并能重新启动，避免"装完就消失"。
func (s *Server) listInstalledPlugins(w http.ResponseWriter, r *http.Request) {
	type installedView struct {
		ID           int64    `json:"id"`
		Name         string   `json:"name"`
		Label        string   `json:"label"`
		Version      string   `json:"version"`
		Author       string   `json:"author"`
		Icon         string   `json:"icon"`
		Capabilities []string `json:"capabilities"`
		Running      bool     `json:"running"`
		// Enabled 持久化启停状态：false = 管理页主动停用（重启后仍保持停止）
		Enabled bool `json:"enabled"`
	}
	var recs []model.Plugin
	if err := s.db.Order("id").Find(&recs).Error; err != nil {
		http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
		return
	}
	out := make([]installedView, 0, len(recs))
	for _, rec := range recs {
		v := installedView{ID: rec.ID, Name: rec.Name, Label: rec.Name, Version: rec.Version, Author: rec.Author, Enabled: rec.Enabled}
		// 运行中：以实例 manifest 为准（DB 快照可能只有 name/author）
		if inst, ok := s.plugins.Get(rec.Name); ok && inst.Manifest != nil {
			m := inst.Manifest
			v.Running = true
			v.Label, v.Version, v.Author = brandName(m), m.Version, m.Author
			v.Capabilities = m.Capabilities
			if _, ok := s.plugins.IconFile(m.Name); ok {
				v.Icon = "/assets/plugins/" + m.Name + "/icon"
			}
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"plugins": out})
}

func viewAuthMethod(m *pb.AuthMethod) *authMethodView {
	v := &authMethodView{ID: m.Id, Label: m.Label, Capabilities: m.Capabilities, Callback: m.Callback}
	for _, f := range m.Fields {
		v.Fields = append(v.Fields, viewAuthField(f))
	}
	return v
}

func viewAuthField(f *pb.AuthField) authFieldView {
	return authFieldView{
		Name: f.Name, Label: f.Label, Type: f.Type,
		Required: f.Required, Placeholder: f.Placeholder,
	}
}

func viewNextStep(n *pb.LoginNextStep) *nextStepView {
	v := &nextStepView{Action: n.Action, URL: n.Url, Prompt: n.Prompt, Wait: n.Wait}
	for _, f := range n.Fields {
		v.Fields = append(v.Fields, viewAuthField(f))
	}
	if len(n.State) > 0 {
		v.State = base64.StdEncoding.EncodeToString(n.State)
	}
	return v
}
