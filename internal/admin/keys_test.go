package admin

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
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
