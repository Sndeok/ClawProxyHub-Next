// direct.go — 非路由模型直连：账号目录里声明过的模型默认可调用。
//
// 设计：对外模型 = 路由名（别名/映射）∪ 账号目录模型（直连）。路由只是多一层改名，
// 删掉路由不该让模型消失；直连时同样注入账号凭据与出站代理。
package router

import (
	"encoding/json"
	"sort"

	"github.com/Sndeok/ClawProxyHub-Next/internal/account"
	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
)

// HasRouteBinding key 是否显式绑定了路由（key_routes 有记录）。
// 绑定即受限：只允许被授权的路由名，直连模型与账号目录都不透出（安全边界）。
// 注意：不能用 AuthorizedModels 判空代替——未绑定 key 返回的是「全部路由名」，
// 非空，用它做判断会把所有直连模型一刀切成 403。
func (r *Router) HasRouteBinding(key *model.Key) bool {
	if key == nil {
		return false
	}
	var count int64
	r.db.Model(&model.KeyRoute{}).Where("key_id = ?", key.ID).Count(&count)
	return count > 0
}

// DirectModels 账号目录里可见的模型 id（去重排序）。
// key 绑定了路由授权时返回空：受限 key 只能使用被授权的路由名（安全边界）。
func (r *Router) DirectModels(key *model.Key) []string {
	if r.HasRouteBinding(key) {
		return nil
	}
	var accts []model.Account
	r.activeWhere(r.db).Find(&accts)
	seen := map[string]bool{}
	var out []string
	for _, a := range accts {
		for _, id := range modelsOf(a.ModelsJSON) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ResolveDirect 非路由模型解析：挑一个目录里声明了该模型的可用账号。
// pluginHint 非空且没有账号声明该模型时，退化为该插件下的可用账号（插件目录兜底）。
func (r *Router) ResolveDirect(name, pluginHint string) *Resolved {
	if name == "" {
		return nil
	}
	var accts []model.Account
	r.activeWhere(r.db).Order("last_used_at IS NULL DESC, last_used_at").Find(&accts)
	var declared []model.Account
	for _, a := range accts {
		for _, id := range modelsOf(a.ModelsJSON) {
			if id == name {
				declared = append(declared, a)
				break
			}
		}
	}
	cands := declared
	if len(cands) == 0 && pluginHint != "" {
		for _, a := range accts {
			if r.pluginNameByID(a.PluginID) == pluginHint {
				cands = append(cands, a)
			}
		}
	}
	acct := pickPreferred(cands)
	if acct == nil {
		return nil
	}
	return &Resolved{
		Account:    acct,
		RealModel:  name,
		GroupID:    r.firstGroupOf(acct.ID),
		PluginName: r.pluginNameByID(acct.PluginID),
	}
}

// pickPreferred 候选里挑一个：快过期积分多的优先，其次最久未用。
func pickPreferred(accts []model.Account) *model.Account {
	if len(accts) == 0 {
		return nil
	}
	best := accts[0]
	bestExp := account.CreditExpiryOf(best.CreditsJSON).Expiring
	for _, a := range accts[1:] {
		e := account.CreditExpiryOf(a.CreditsJSON).Expiring
		if e > bestExp || (e == bestExp && lastUsedAsc(a, best)) {
			best, bestExp = a, e
		}
	}
	return &best
}

// modelsOf 解析账号的模型目录快照（models_json 为 protojson 数组）。
func modelsOf(modelsJSON string) []string {
	if modelsJSON == "" {
		return nil
	}
	var raws []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal([]byte(modelsJSON), &raws) != nil {
		return nil
	}
	out := make([]string, 0, len(raws))
	for _, m := range raws {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out
}

// pluginNameByID 插件名（分组归属插件 / 账号插件）。
func (r *Router) pluginNameByID(id int64) string {
	if id == 0 {
		return ""
	}
	var p model.Plugin
	if err := r.db.Select("name").First(&p, id).Error; err != nil {
		return ""
	}
	return p.Name
}

// firstGroupOf 账号的第一个分组（出站代理按分组解析；无分组返回 0）。
func (r *Router) firstGroupOf(accountID int64) int64 {
	var link model.AccountGroup
	if err := r.db.Where("account_id = ?", accountID).Order("group_id").First(&link).Error; err != nil {
		return 0
	}
	return link.GroupID
}
