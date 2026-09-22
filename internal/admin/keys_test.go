package admin

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
	"github.com/Sndeok/ClawProxyHub-Next/internal/plugin"
)

// TestCreateRouteStrategyRegression 回归 GORM 零值陷阱：
// Strategy 留空 = 跟随全局默认，落库必须为空串；
// 不能被列默认值 round_robin / GORM default 标签顶掉。
func TestCreateRouteStrategyRegression(t *testing.T) {
	cases := []struct {
		name     string
		suffix   string
		strategy string
		wantCode int
		wantDB   string
	}{
		{"留空跟随全局", "a", "", 200, ""},
		{"显式 sticky", "b", "sticky", 200, "sticky"},
		{"显式轮询", "c", "round_robin", 200, "round_robin"},
		{"未知策略拒绝", "d", "bogus", 400, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := opsTestDB(t)
			s := &Server{db: db}
			name := "stub-mini-" + tc.suffix
			payload, err := json.Marshal(map[string]interface{}{
				"name":     name,
				"strategy": tc.strategy,
				"groups":   []map[string]interface{}{{"group_id": 1, "weight": 100}},
			})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/admin/routes", strings.NewReader(string(payload)))
			w := httptest.NewRecorder()
			s.createRoute(w, req)
			if w.Code != tc.wantCode {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.wantCode, w.Body.String())
			}
			if tc.wantCode != 200 {
				return
			}
			var rt model.Route
			if err := db.Where("name = ?", name).First(&rt).Error; err != nil {
				t.Fatal(err)
			}
			if rt.Strategy != tc.wantDB {
				t.Fatalf("落库策略 = %q，want %q（零值/默认值陷阱回归）", rt.Strategy, tc.wantDB)
			}
		})
	}
}

// TestUpdateGroupRejectsStrategy 回归：分组级策略是废弃字段，
// 传了必须报错（而不是悄悄写进没人读的列，让调用方以为生效）。
func TestUpdateGroupRejectsStrategy(t *testing.T) {
	db := opsTestDB(t)
	s := &Server{db: db}
	g := model.Group{Name: "主池", PluginID: 1}
	if err := db.Create(&g).Error; err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]interface{}{"name": "主池", "strategy": "random"})
	req := httptest.NewRequest("PUT", "/admin/groups/1", strings.NewReader(string(payload)))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	s.updateGroup(w, req)
	if w.Code != 400 {
		t.Fatalf("带 strategy 的分组更新应被拒绝（400），实际 %d body=%s", w.Code, w.Body.String())
	}
	// 只改名（不带 strategy）仍可用
	payload2, _ := json.Marshal(map[string]interface{}{"name": "备用池"})
	req2 := httptest.NewRequest("PUT", "/admin/groups/1", strings.NewReader(string(payload2)))
	req2.SetPathValue("id", "1")
	w2 := httptest.NewRecorder()
	s.updateGroup(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("纯改名应成功，实际 %d body=%s", w2.Code, w2.Body.String())
	}
	var got model.Group
	if err := db.First(&got, g.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Name != "备用池" {
		t.Fatalf("改名未生效：%q", got.Name)
	}
}

// TestStopPluginPersistsDisabled 回归：停止插件要落库 enabled=false，
// 否则重启（容器重建）后插件又会被拉起来，「停止」形同虚设。
func TestStopPluginPersistsDisabled(t *testing.T) {
	db := opsTestDB(t)
	rec := model.Plugin{Name: "lobsterai", Version: "0.1.13", Author: "cph", ProtocolVersion: 1, ManifestJSON: "{}", Enabled: true}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, plugins: plugin.NewManager(t.TempDir(), db)}
	req := httptest.NewRequest("POST", "/admin/plugins/lobsterai/stop", nil)
	req.SetPathValue("name", "lobsterai")
	w := httptest.NewRecorder()
	s.stopPlugin(w, req)
	if w.Code != 200 {
		t.Fatalf("停止插件应返回 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	var got model.Plugin
	if err := db.First(&got, rec.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatal("停止后 plugins.enabled 应为 false（重启后保持停止）")
	}
}
