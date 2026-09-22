// logs.go — 调用日志查询：服务端分页 + 多维过滤 + 手动/自动清理。
//
// 旧实现只有 limit（默认 100，最多 1000），没有分页游标、没有过滤，且每查一次
// 就把 keys 全表扫一遍做 id→name 映射。日志量上来后既翻不动也拖慢数据库。
// 这里改为：过滤条件集中在 logQuery.apply（count 与列表共用，保证 total 一致）、
// 只在当前页出现的 id 上反查名称、单页上限硬约束。
package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
)

const (
	// maxLogPageSize 单页硬上限：防止客户端用超大 page_size 把整表拉走。
	maxLogPageSize     = 200
	defaultLogPageSize = 20
)

// logQuery 日志列表的解析后查询条件。
type logQuery struct {
	page       int
	pageSize   int
	legacy     bool // 旧版 limit 语义：只取前 N 条，不返回分页元数据
	keyID      int64
	pluginID   int64
	accountID  int64
	protocol   string
	model      string
	status     string
	keyword    string
	from       *time.Time
	to         *time.Time
	minLatency int64
}

// parseLogQuery 解析查询参数。
// 兼容旧调用 /admin/logs?limit=N（Dashboard 与旧前端），此时按「前 N 条」处理。
func parseLogQuery(r *http.Request) logQuery {
	q := r.URL.Query()
	out := logQuery{
		page:       int(int64Param(q.Get("page"), 1, 1, 1<<30)),
		pageSize:   int(int64Param(q.Get("page_size"), defaultLogPageSize, 1, maxLogPageSize)),
		keyID:      int64Param(q.Get("key_id"), 0, 0, 1<<62),
		pluginID:   int64Param(q.Get("plugin_id"), 0, 0, 1<<62),
		accountID:  int64Param(q.Get("account_id"), 0, 0, 1<<62),
		protocol:   strings.TrimSpace(q.Get("protocol")),
		model:      strings.TrimSpace(q.Get("model")),
		status:     strings.TrimSpace(q.Get("status")),
		keyword:    strings.TrimSpace(q.Get("q")),
		from:       parseLogTime(q.Get("from"), false),
		to:         parseLogTime(q.Get("to"), true),
		minLatency: int64Param(q.Get("min_latency"), 0, 0, 1<<62),
	}
	if raw := q.Get("limit"); raw != "" && q.Get("page") == "" && q.Get("page_size") == "" {
		out.legacy = true
		out.pageSize = int(int64Param(raw, 100, 1, 1000))
	}
	return out
}

// int64Param 解析整数参数并夹到 [min,max]；非法或缺失返回 def。
func int64Param(raw string, def, min, max int64) int64 {
	if raw == "" {
		return def
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

// parseLogTime 解析时间参数，接受 RFC3339 / "2006-01-02T15:04:05" /
// "2006-01-02 15:04:05" / "2006-01-02"（日期精度时可按 endOfDay 补到当天 23:59:59）。
func parseLogTime(raw string, endOfDay bool) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		t, err := time.ParseInLocation(layout, raw, time.Local)
		if err != nil {
			continue
		}
		if endOfDay && layout == "2006-01-02" {
			t = t.Add(24*time.Hour - time.Second)
		}
		return &t
	}
	return nil
}

// apply 把过滤条件落到查询上；count 与列表共用同一份谓词，避免两边口径不一致。
func (q logQuery) apply(db *gorm.DB) *gorm.DB {
	if q.keyID > 0 {
		db = db.Where("key_id = ?", q.keyID)
	}
	if q.pluginID > 0 {
		db = db.Where("plugin_id = ?", q.pluginID)
	}
	if q.accountID > 0 {
		db = db.Where("account_id = ?", q.accountID)
	}
	if q.protocol != "" {
		db = db.Where("protocol = ?", q.protocol)
	}
	if q.model != "" {
		like := "%" + q.model + "%"
		db = db.Where("(model LIKE ? OR requested_model LIKE ?)", like, like)
	}
	if q.keyword != "" {
		like := "%" + q.keyword + "%"
		db = db.Where("(error_brief LIKE ? OR client_ip LIKE ? OR user_agent LIKE ?)", like, like, like)
	}
	switch q.status {
	case "ok":
		db = db.Where("status < 400")
	case "error":
		db = db.Where("status >= 400")
	case "4xx":
		db = db.Where("status >= 400 AND status < 500")
	case "5xx":
		db = db.Where("status >= 500")
	}
	if q.from != nil {
		db = db.Where("created_at >= ?", *q.from)
	}
	if q.to != nil {
		db = db.Where("created_at <= ?", *q.to)
	}
	if q.minLatency > 0 {
		db = db.Where("latency_ms >= ?", q.minLatency)
	}
	return db
}

// listLogs GET /admin/logs — 调用日志（服务端分页 + 多维过滤）。
//
// 参数：page / page_size（≤200）· key_id · plugin_id · account_id · protocol ·
// model（模糊匹配请求名与上游名）· status(ok|error|4xx|5xx) ·
// q（error_brief / client_ip / user_agent 关键词）· from / to · min_latency(ms)
// 返回：{logs, total, page, page_size, has_more}；旧版 limit 参数仍可用。
func (s *Server) listLogs(w http.ResponseWriter, r *http.Request) {
	q := parseLogQuery(r)

	var total int64
	if !q.legacy {
		if err := q.apply(s.db.Model(&model.RequestLog{})).Count(&total).Error; err != nil {
			http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
			return
		}
	}

	sel := q.apply(s.db.Model(&model.RequestLog{})).Order("id DESC").Limit(q.pageSize)
	if !q.legacy {
		sel = sel.Offset((q.page - 1) * q.pageSize)
	}
	var logs []model.RequestLog
	if err := sel.Find(&logs).Error; err != nil {
		http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{"logs": s.logViews(logs)}
	if !q.legacy {
		resp["total"] = total
		resp["page"] = q.page
		resp["page_size"] = q.pageSize
		resp["has_more"] = int64(q.page*q.pageSize) < total
	}
	writeJSON(w, http.StatusOK, resp)
}

// logDetail GET /admin/logs/{id}/detail —— 单条日志详情：
// 列表里不含请求原文与上游返回（体积大），只有打开详情时才取。
func (s *Server) logDetail(w http.ResponseWriter, r *http.Request) {
	id := parseInt(r.PathValue("id"))
	var lg model.RequestLog
	if err := s.db.First(&lg, id).Error; err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	views := s.logViews([]model.RequestLog{lg})
	out := map[string]interface{}{
		"request_body": lg.RequestBody,
		"error_detail": lg.ErrorDetail,
	}
	if len(views) > 0 {
		out["log"] = views[0]
	}
	writeJSON(w, http.StatusOK, out)
}

// logView 日志行视图：附加密钥名与账号名（原始日志只存 id）。
type logView struct {
	model.RequestLog
	KeyName     string `json:"key_name"`
	AccountName string `json:"account_name"`
}

// logViews 装配视图。名称只按当前页出现的 id 反查，替掉旧实现的全表 keys 扫描。
func (s *Server) logViews(logs []model.RequestLog) []logView {
	keyIDs := make([]int64, 0, 8)
	acctIDs := make([]int64, 0, 8)
	seenKey := map[int64]bool{}
	seenAcct := map[int64]bool{}
	for i := range logs {
		if id := logs[i].KeyID; id != nil && !seenKey[*id] {
			seenKey[*id] = true
			keyIDs = append(keyIDs, *id)
		}
		if id := logs[i].AccountID; id != nil && !seenAcct[*id] {
			seenAcct[*id] = true
			acctIDs = append(acctIDs, *id)
		}
	}

	keyNames := map[int64]string{}
	if len(keyIDs) > 0 {
		var rows []struct {
			ID   int64
			Name string
		}
		s.db.Model(&model.Key{}).Select("id, name").Where("id IN ?", keyIDs).Scan(&rows)
		for _, row := range rows {
			keyNames[row.ID] = row.Name
		}
	}
	acctNames := map[int64]string{}
	if len(acctIDs) > 0 {
		var rows []struct {
			ID          int64
			DisplayName string
		}
		s.db.Model(&model.Account{}).Select("id, display_name").Where("id IN ?", acctIDs).Scan(&rows)
		for _, row := range rows {
			acctNames[row.ID] = row.DisplayName
		}
	}

	out := make([]logView, 0, len(logs))
	for i := range logs {
		l := logs[i]
		v := logView{RequestLog: l}
		if l.KeyID != nil {
			v.KeyName = keyNames[*l.KeyID]
		}
		if l.AccountID != nil {
			v.AccountName = acctNames[*l.AccountID]
		}
		out = append(out, v)
	}
	return out
}

// logCleanup POST /admin/logs/cleanup — body: {days: N} 删除 N 天前的日志；
// {all: true} 清空全部。返回删除条数。
func (s *Server) logCleanup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Days int  `json:"days"`
		All  bool `json:"all"`
	}
	if !readBody(w, r, &body) {
		return
	}
	if !body.All && body.Days <= 0 {
		http.Error(w, `{"error":"days must be > 0, or set all=true"}`, http.StatusBadRequest)
		return
	}
	q := s.db.Session(&gorm.Session{AllowGlobalUpdate: true})
	if !body.All {
		q = q.Where("created_at < ?", time.Now().AddDate(0, 0, -body.Days))
	}
	res := q.Delete(&model.RequestLog{})
	if res.Error != nil {
		http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": res.RowsAffected})
}
