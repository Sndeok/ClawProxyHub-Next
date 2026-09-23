package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/Sndeok/ClawProxyHub-Next/internal/account"
	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
)

// opsTestDB 建一个只含运维接口所需表的内存库。
func opsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ops.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Plugin{}, &model.Account{}, &model.Group{}, &model.AccountGroup{},
		&model.Route{}, &model.RequestLog{}, &model.AccountCreditDaily{}, &model.TaskRule{}, &model.TaskRun{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if s, err := db.DB(); err == nil {
			_ = s.Close()
		}
	})
	return db
}

// TestModelIDs 解析账号模型目录快照。
func TestModelIDs(t *testing.T) {
	if got := modelIDs(""); got != nil {
		t.Fatalf("空快照应为 nil，实际 %v", got)
	}
	if got := modelIDs("not json"); got != nil {
		t.Fatalf("非法 JSON 应为 nil，实际 %v", got)
	}
	got := modelIDs(`[{"id":"glm-5.3"},{"id":""},{"id":"glm-4.6"}]`)
	if len(got) != 2 || got[0] != "glm-5.3" || got[1] != "glm-4.6" {
		t.Fatalf("解析结果错误：%v", got)
	}
}

// TestSyncRoutesCreatesMissingOnly 一键同步：只新建缺失路由，同名路由与跨插件分组都不动。
func TestSyncRoutesCreatesMissingOnly(t *testing.T) {
	db := opsTestDB(t)
	lb := model.Plugin{Name: "lobsterai", Version: "0.1.4"}
	wb := model.Plugin{Name: "workbuddy", Version: "0.1.4"}
	db.Create(&lb)
	db.Create(&wb)

	g1 := model.Group{Name: "主池", PluginID: lb.ID}
	g2 := model.Group{Name: "备用池", PluginID: lb.ID}
	g3 := model.Group{Name: "别的插件池", PluginID: wb.ID}
	for _, g := range []*model.Group{&g1, &g2, &g3} {
		db.Create(g)
	}

	a1 := model.Account{PluginID: lb.ID, DisplayName: "a1", Status: "active",
		ModelsJSON: `[{"id":"glm-5.3"},{"id":"glm-4.6"}]`}
	a2 := model.Account{PluginID: lb.ID, DisplayName: "a2", Status: "active",
		ModelsJSON: `[{"id":"glm-5.3"},{"id":"kimi-k3"}]`}
	a3 := model.Account{PluginID: lb.ID, DisplayName: "expired", Status: "expired",
		ModelsJSON: `[{"id":"should-not-appear"}]`}
	for _, a := range []*model.Account{&a1, &a2, &a3} {
		db.Create(a)
	}
	// a1 同时挂到别的插件的分组（必须被忽略），a3 已过期（整体跳过）
	for _, link := range []model.AccountGroup{
		{AccountID: a1.ID, GroupID: g1.ID}, {AccountID: a1.ID, GroupID: g3.ID},
		{AccountID: a2.ID, GroupID: g2.ID}, {AccountID: a3.ID, GroupID: g1.ID},
	} {
		db.Create(&link)
	}

	// 已存在的同名路由（人工配置）不能被覆盖
	existingGroups, _ := json.Marshal([]model.RouteGroupEntry{{GroupID: g2.ID, Weight: 7, Model: "glm-4.6"}})
	db.Create(&model.Route{Name: "glm-4.6", Strategy: "sticky", GroupsJSON: string(existingGroups)})

	srv := &Server{db: db}
	rec := httptest.NewRecorder()
	srv.syncRoutes(rec, httptest.NewRequest(http.MethodPost, "/admin/routes/sync-models", strings.NewReader("{}")))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d：%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Accounts int      `json:"accounts"`
		Models   int      `json:"models"`
		Created  []string `json:"created"`
		Skipped  []string `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Accounts != 2 {
		t.Errorf("参与同步的账号数 = %d，want 2（过期账号跳过）", resp.Accounts)
	}
	created := strings.Join(resp.Created, ",")
	if created != "glm-5.3,kimi-k3" {
		t.Errorf("新增路由 = %q，want glm-5.3,kimi-k3", created)
	}
	if len(resp.Skipped) != 1 || resp.Skipped[0] != "glm-4.6" {
		t.Errorf("跳过路由 = %v，want [glm-4.6]", resp.Skipped)
	}

	var glm53 model.Route
	if err := db.Where("name = ?", "glm-5.3").First(&glm53).Error; err != nil {
		t.Fatal(err)
	}
	var entries []model.RouteGroupEntry
	if err := json.Unmarshal([]byte(glm53.GroupsJSON), &entries); err != nil {
		t.Fatal(err)
	}
	got := map[int64]string{}
	for _, e := range entries {
		got[e.GroupID] = e.Model
	}
	if len(got) != 2 || got[g1.ID] != "glm-5.3" || got[g2.ID] != "glm-5.3" {
		t.Errorf("glm-5.3 分组映射错误：%+v（跨插件分组必须被忽略）", got)
	}
	for gid := range got {
		if gid == g3.ID {
			t.Error("跨插件分组被错误写入路由")
		}
	}

	// 已存在路由保持人工配置
	var glm46 model.Route
	db.Where("name = ?", "glm-4.6").First(&glm46)
	if glm46.Strategy != "sticky" {
		t.Errorf("已存在路由被覆盖：strategy=%s", glm46.Strategy)
	}

	// 幂等：再跑一次不应重复创建
	rec2 := httptest.NewRecorder()
	srv.syncRoutes(rec2, httptest.NewRequest(http.MethodPost, "/admin/routes/sync-models", strings.NewReader("")))
	var resp2 struct {
		Created []string `json:"created"`
		Skipped []string `json:"skipped"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatal(err)
	}
	if len(resp2.Created) != 0 || len(resp2.Skipped) != 3 {
		t.Errorf("第二次同步不幂等：created=%v skipped=%v", resp2.Created, resp2.Skipped)
	}
}

// TestTodayStatsByAccount 今日聚合：token/缓存/积分按账号分组，昨天的不计入。
func TestTodayStatsByAccount(t *testing.T) {
	db := opsTestDB(t)
	a1, a2 := int64(1), int64(2)
	// 以「今天 00:00」为基准构造样本，避免在凌晨运行时 now-2h 落到昨天（曾因此偶发失败）
	dayStart := todayStart()
	rows := []model.RequestLog{
		{AccountID: &a1, InputTokens: 100, OutputTokens: 10, CachedTokens: 40, CreditUsed: 1.5, CreatedAt: dayStart.Add(2 * time.Hour)},
		{AccountID: &a1, InputTokens: 200, OutputTokens: 20, CachedTokens: 0, Status: 502, CreatedAt: dayStart.Add(time.Hour)},
		{AccountID: &a2, InputTokens: 999, OutputTokens: 1, CreatedAt: dayStart.Add(-time.Hour)},
	}
	for i := range rows {
		db.Create(&rows[i])
	}
	stats := todayStatsByAccount(db)
	if got := stats[a1]; got == nil || got.Tokens != 330 || got.Cached != 40 || got.Requests != 2 {
		t.Fatalf("a1 今日聚合错误：%+v", got)
	}
	if got := stats[a1].Credits; got != 1.5 {
		t.Errorf("a1 今日积分 = %v，want 1.5", got)
	}
	if got, ok := stats[a2]; ok && got.Tokens != 0 {
		t.Errorf("昨天的日志不应计入今日：%+v", got)
	}
}

// TestListModelsReportsSources 模型中心「来源渠道」：同名模型被多个插件提供时要列全，
// 并给出每个插件下的账号数（前端据此打「多源」标记）。
func TestListModelsReportsSources(t *testing.T) {
	db := opsTestDB(t)
	qoder := model.Plugin{Name: "qoder", Version: "0.1.7"}
	qwork := model.Plugin{Name: "qoderwork", Version: "0.1.13"}
	if err := db.Create(&qoder).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&qwork).Error; err != nil {
		t.Fatal(err)
	}
	accts := []model.Account{
		{PluginID: qoder.ID, DisplayName: "a1", Status: "active",
			ModelsJSON: `[{"id":"qmodel_38max","label":{"zh":"Qwen3.8-Max"}},{"id":"only-qoder"}]`},
		{PluginID: qwork.ID, DisplayName: "a2", Status: "active", ModelsJSON: `[{"id":"qmodel_38max"}]`},
		{PluginID: qwork.ID, DisplayName: "a3", Status: "active", ModelsJSON: `[{"id":"qmodel_38max"}]`},
		{PluginID: qwork.ID, DisplayName: "paused", Status: "paused", ModelsJSON: `[{"id":"qmodel_38max"}]`},
	}
	for i := range accts {
		if err := db.Create(&accts[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	srv := &Server{db: db, accounts: account.New(db, t.TempDir(), nil)}
	req := httptest.NewRequest(http.MethodGet, "/admin/models", nil)
	rec := httptest.NewRecorder()
	srv.listModels(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Models []struct {
			ID       string   `json:"id"`
			Accounts int      `json:"accounts"`
			Plugin   string   `json:"plugin"`
			Plugins  []string `json:"plugins"`
			Sources  []struct {
				Plugin   string `json:"plugin"`
				Accounts int    `json:"accounts"`
			} `json:"sources"`
		} `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	byID := map[string]int{}
	for i, m := range body.Models {
		byID[m.ID] = i
	}
	shared, ok := byID["qmodel_38max"]
	if !ok {
		t.Fatal("缺少 qmodel_38max")
	}
	m := body.Models[shared]
	if len(m.Plugins) != 2 {
		t.Fatalf("plugins = %v，期望 2 个来源", m.Plugins)
	}
	if m.Plugins[0] != "qoder" || m.Plugins[1] != "qoderwork" {
		t.Errorf("来源顺序应稳定排序：%v", m.Plugins)
	}
	counts := map[string]int{}
	for _, s := range m.Sources {
		counts[s.Plugin] = s.Accounts
	}
	if counts["qoder"] != 1 || counts["qoderwork"] != 2 {
		t.Errorf("每渠道账号数错误：%v（暂停账号不应计入）", counts)
	}
	if m.Accounts != 3 {
		t.Errorf("活跃账号总数 = %d，期望 3", m.Accounts)
	}
	if only, ok := byID["only-qoder"]; !ok {
		t.Fatal("缺少 only-qoder")
	} else if got := body.Models[only]; len(got.Plugins) != 1 || got.Plugins[0] != "qoder" {
		t.Errorf("单来源模型应只有 1 个插件：%v", got.Plugins)
	}
}
