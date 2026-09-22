// ops.go — 批量运维动作：一键刷新账号、一键同步上游模型到路由、一键执行任务规则。
//
// 这三件事的共同点是「按列表逐个调插件、部分失败不能整体失败」，所以统一返回
// {total, ok, failed, items:[...]} 形状，前端逐条展示结果而不是只看一个红叉。
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
)

// readOptionalBody 解析可选请求体：空 body 或非法 JSON 都按零值处理。
// 与 readBody 的区别是「没带参数」不算客户端错误（如 POST /routes/sync-models 不传 body）。
func readOptionalBody(r *http.Request, v interface{}) {
	if r.Body == nil {
		return
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

// opFailure 单条明细失败项。
type opFailure struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Message string `json:"message"`
}

// ---------- 账号：一键刷新 ----------

// refreshAllAccounts POST /admin/accounts/refresh-all
// body: {ids?: [], plugin?: "lobsterai", concurrency?: 3}
// 并发上限 3：SQLite 是单写库，刷新本身又是上游 HTTP，够快也不会把库锁死。
func (s *Server) refreshAllAccounts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs         []int64 `json:"ids"`
		Plugin      string  `json:"plugin"`
		Concurrency int     `json:"concurrency"`
	}
	readOptionalBody(r, &body)

	q := s.db.Model(&model.Account{})
	if len(body.IDs) > 0 {
		q = q.Where("id IN ?", body.IDs)
	}
	if body.Plugin != "" {
		var p model.Plugin
		if err := s.db.Where("name = ?", body.Plugin).First(&p).Error; err != nil {
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

	limit := body.Concurrency
	if limit < 1 || limit > 6 {
		limit = 3
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []opFailure
	for i := range accts {
		a := accts[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			// 单账号 60s：插件内部还有自己的上游超时，这里只兜底不让 goroutine 挂死
			ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
			defer cancel()
			if _, err := s.accounts.Refresh(ctx, a.ID); err != nil {
				mu.Lock()
				failed = append(failed, opFailure{ID: a.ID, Name: a.DisplayName, Message: err.Error()})
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	sort.Slice(failed, func(i, j int) bool { return failed[i].ID < failed[j].ID })

	resp := map[string]interface{}{
		"total": len(accts), "refreshed": len(accts) - len(failed), "failed": len(failed),
		"items": failed,
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------- 路由：一键同步上游模型 ----------

// syncRoutes POST /admin/routes/sync-models
// body: {refresh?: bool, plugin?: "lobsterai"}
//
// 语义：把每个分组下账号「上游真实可见的模型」并集，补齐成对外路由。
// 只新建缺失的同名路由，已存在的路由（含用户手改的策略/权重/降级）一律不动，
// 避免一键操作把人工配置覆盖掉。需要刷上游目录时传 refresh=true（慢，但要最新）。
func (s *Server) syncRoutes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Refresh bool   `json:"refresh"`
		Plugin  string `json:"plugin"`
	}
	readOptionalBody(r, &body)

	pluginFilter := int64(0)
	if body.Plugin != "" {
		var p model.Plugin
		if err := s.db.Where("name = ?", body.Plugin).First(&p).Error; err != nil {
			http.Error(w, `{"error":"unknown plugin"}`, http.StatusNotFound)
			return
		}
		pluginFilter = p.ID
	}

	var accts []model.Account
	aq := s.db.Model(&model.Account{}).Where("status = ?", "active")
	if pluginFilter > 0 {
		aq = aq.Where("plugin_id = ?", pluginFilter)
	}
	if err := aq.Order("id").Find(&accts).Error; err != nil {
		http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
		return
	}

	// 分组 → 插件：只有插件一致的分组才能承接该账号的模型
	type groupRow struct {
		ID       int64
		PluginID int64
		Name     string
	}
	var groups []groupRow
	s.db.Model(&model.Group{}).Select("id, plugin_id, name").Scan(&groups)
	groupPlugin := make(map[int64]int64, len(groups))
	groupName := make(map[int64]string, len(groups))
	for _, g := range groups {
		groupPlugin[g.ID] = g.PluginID
		groupName[g.ID] = g.Name
	}
	var links []model.AccountGroup
	s.db.Find(&links)
	acctGroups := map[int64][]int64{}
	for _, l := range links {
		acctGroups[l.AccountID] = append(acctGroups[l.AccountID], l.GroupID)
	}

	// 需要刷上游目录时：顺序拉（插件侧是 HTTP，顺序足够且不会撞上游限流）
	refreshed, refreshFail := 0, 0
	var failures []opFailure
	if body.Refresh {
		for _, a := range accts {
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			_, err := s.accounts.SyncModels(ctx, a.ID)
			cancel()
			if err != nil {
				refreshFail++
				failures = append(failures, opFailure{ID: a.ID, Name: a.DisplayName, Message: err.Error()})
				continue
			}
			refreshed++
		}
		// 重新读模型目录（上面更新了 models_json）
		if err := aq.Order("id").Find(&accts).Error; err != nil {
			http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
			return
		}
	}

	// 模型 → 需要它的分组集合（同一模型的多个分组 = 路由的多个候选入口）
	modelGroups := map[string]map[int64]bool{}
	modelPlugin := map[string]int64{}
	for _, a := range accts {
		ids := modelIDs(a.ModelsJSON)
		if len(ids) == 0 {
			continue
		}
		for _, gid := range acctGroups[a.ID] {
			if pid, ok := groupPlugin[gid]; !ok || pid != a.PluginID {
				continue // 跨插件分组不承接
			}
			for _, m := range ids {
				if modelGroups[m] == nil {
					modelGroups[m] = map[int64]bool{}
				}
				modelGroups[m][gid] = true
				modelPlugin[m] = a.PluginID
			}
		}
	}

	var existing []model.Route
	s.db.Find(&existing)
	existingNames := make(map[string]bool, len(existing))
	for _, rt := range existing {
		existingNames[rt.Name] = true
	}

	names := make([]string, 0, len(modelGroups))
	for name := range modelGroups {
		names = append(names, name)
	}
	sort.Strings(names)

	created, skipped, noGroup := []string{}, []string{}, []string{}
	for _, name := range names {
		if len(modelGroups[name]) == 0 {
			noGroup = append(noGroup, name)
			continue
		}
		if existingNames[name] {
			skipped = append(skipped, name)
			continue
		}
		entries := make([]model.RouteGroupEntry, 0, len(modelGroups[name]))
		gids := make([]int64, 0, len(modelGroups[name]))
		for gid := range modelGroups[name] {
			gids = append(gids, gid)
		}
		sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
		for _, gid := range gids {
			entries = append(entries, model.RouteGroupEntry{GroupID: gid, Weight: 1, Model: name})
		}
		groupsJSON, _ := json.Marshal(entries)
		// 与手动新建保持一致：默认会话粘性，同一会话固定账号（上游缓存才可能命中）
		rt := model.Route{Name: name, Strategy: "", GroupsJSON: string(groupsJSON)} // 留空 = 跟随全局默认策略
		// Select 显式列出列：空策略才不会被列默认值 round_robin 顶掉
		if err := s.db.Select(routeInsertFields).Create(&rt).Error; err != nil {
			failures = append(failures, opFailure{ID: 0, Name: name, Message: err.Error()})
			continue
		}
		existingNames[name] = true
		created = append(created, name)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"accounts": len(accts), "models": len(names),
		"created": created, "skipped": skipped, "no_group": noGroup,
		"refreshed": refreshed, "refresh_failed": refreshFail,
		"items": failures, "groups": groupName,
	})
}

// modelIDs 解析 accounts.models_json（[]ModelInfo 的 JSON 快照）。
func modelIDs(modelsJSON string) []string {
	if strings.TrimSpace(modelsJSON) == "" {
		return nil
	}
	var raw []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal([]byte(modelsJSON), &raw) != nil {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, m := range raw {
		if strings.TrimSpace(m.ID) != "" {
			out = append(out, m.ID)
		}
	}
	return out
}

// ---------- 任务：一键执行（全部/勾选） ----------

// runAllTaskRules POST /admin/task-rules/run-all
// body: {ids?: [], plugin_id?: 0}
// 执行放在后台 goroutine：签到类任务串行跑几十个账号会超过 HTTP 超时，
// 立即返回「已触发 N 条」，结果去任务历史看。
func (s *Server) runAllTaskRules(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs      []int64 `json:"ids"`
		PluginID int64   `json:"plugin_id"`
	}
	readOptionalBody(r, &body)
	if s.engine == nil {
		http.Error(w, `{"error":"engine not available"}`, http.StatusServiceUnavailable)
		return
	}

	q := s.db.Model(&model.TaskRule{})
	if len(body.IDs) > 0 {
		q = q.Where("id IN ?", body.IDs)
	}
	if body.PluginID > 0 {
		q = q.Where("plugin_id = ?", body.PluginID)
	}
	var rules []model.TaskRule
	if err := q.Order("id").Find(&rules).Error; err != nil {
		http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
		return
	}

	go func() {
		// 后台不再持有请求 ctx（请求返回即取消），用独立超时
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		for i := range rules {
			s.engine.RunNow(ctx, &rules[i])
			if ctx.Err() != nil {
				return
			}
		}
	}()

	ids := make([]int64, 0, len(rules))
	for _, rule := range rules {
		ids = append(ids, rule.ID)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"triggered": len(rules), "ids": ids,
		"message": fmt.Sprintf("已触发 %d 条规则，执行结果见任务历史", len(rules)),
	})
}
