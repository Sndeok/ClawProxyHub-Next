package router

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/Sndeok/ClawProxyHub-Next/internal/database"
	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

func openRouterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "cph.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func mustCreate(t *testing.T, db *gorm.DB, v interface{}) {
	t.Helper()
	if err := db.Create(v).Error; err != nil {
		t.Fatal(err)
	}
}

// creditsExpiringIn 构造「N 天后到期、剩余 left」的积分快照（插件上报格式）。
func creditsExpiringIn(days int, left float64) string {
	at := time.Now().Add(time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
	return `{"packages":[{"remaining":` + strconv.FormatFloat(left, 'f', -1, 64) + `,"expiresAt":"` + at + `"}]}`
}

// seedAccounts 造一个插件 + 分组 + 两个账号：A 有快过期积分，B 没有。
func seedAccounts(t *testing.T) (*gorm.DB, model.Account, model.Account, model.Group) {
	db := openRouterTestDB(t)
	plugin := &model.Plugin{Name: "stub", Author: "cph", Enabled: true}
	mustCreate(t, db, plugin)
	group := &model.Group{Name: "g1", PluginID: plugin.ID, Strategy: "sticky_expiring"}
	mustCreate(t, db, group)
	a := model.Account{PluginID: plugin.ID, DisplayName: "A", Status: "active",
		ModelsJSON: `[{"id":"stub-mini"}]`, CreditsJSON: creditsExpiringIn(3, 100)}
	b := model.Account{PluginID: plugin.ID, DisplayName: "B", Status: "active",
		ModelsJSON: `[{"id":"stub-mini"},{"id":"stub-pro"}]`, CreditsJSON: `{}`}
	mustCreate(t, db, &a)
	mustCreate(t, db, &b)
	mustCreate(t, db, &model.AccountGroup{AccountID: a.ID, GroupID: group.ID})
	mustCreate(t, db, &model.AccountGroup{AccountID: b.ID, GroupID: group.ID})
	return db, a, b, *group
}

// TestResolveDirectUsesAccountCatalog 账号目录里的模型默认可直连（路由只做改名/映射）。
func TestResolveDirectUsesAccountCatalog(t *testing.T) {
	db, a, _, group := seedAccounts(t)
	rt := New(db)
	res := rt.ResolveDirect("stub-mini", "")
	if res == nil || res.Account == nil {
		t.Fatal("stub-mini 未解析到账号")
	}
	if res.Account.ID != a.ID {
		t.Errorf("应选有快过期积分的账号 A：want %d got %d", a.ID, res.Account.ID)
	}
	if res.GroupID != group.ID {
		t.Errorf("直连也要带上分组（出站代理解析用）：want %d got %d", group.ID, res.GroupID)
	}
	if res.PluginName != "stub" {
		t.Errorf("插件名应从账号归属推导：got %q", res.PluginName)
	}
	if res.Route != nil {
		t.Errorf("直连模型不应挂路由")
	}

	models := rt.DirectModels(nil)
	if len(models) != 2 || models[0] != "stub-mini" || models[1] != "stub-pro" {
		t.Errorf("账号目录并集错误：%v", models)
	}

	// 账号目录没声明的模型且无插件提示 → 解析不到（网关回 404）
	if rt.ResolveDirect("unknown-model", "") != nil {
		t.Errorf("未声明的模型不应解析到账号")
	}

	// key 绑定了路由授权：不给直连模型（安全边界）
	route := &model.Route{Name: "r1", Strategy: "sticky", GroupsJSON: "[]"}
	mustCreate(t, db, route)
	restricted := &model.Key{Name: "restricted", KeyCipher: "cipher-restricted", KeyHash: "hash-restricted"}
	mustCreate(t, db, restricted)
	mustCreate(t, db, &model.KeyRoute{KeyID: restricted.ID, RouteID: route.ID})
	if got := rt.DirectModels(restricted); len(got) != 0 {
		t.Errorf("受限 key 不应看到直连模型：%v", got)
	}
}

// TestStickyExpiringSticksAndPrefersExpiring 会话粘性 + 快过期积分优先：
// 新会话先烧快过期额度，同一会话内固定账号（保住上游缓存命中）。
func TestStickyExpiringSticksAndPrefersExpiring(t *testing.T) {
	db, a, b, group := seedAccounts(t)
	route := &model.Route{Name: "glm-5.3", Strategy: "sticky_expiring",
		GroupsJSON: fmt.Sprintf(`[{"group_id":%d,"weight":100,"model":"stub-mini"}]`, group.ID)}
	mustCreate(t, db, route)
	rt := New(db)
	key := &model.Key{ID: 12345}

	first := &pb.ChatRequest{Model: "glm-5.3", Messages: []*pb.EnvelopeMessage{
		{Role: "system", Text: "sys"}, {Role: "user", Text: "第一问"}}}
	res1, err := rt.Resolve(key, first)
	if err != nil {
		t.Fatal(err)
	}
	if res1.Account == nil || res1.Account.ID != a.ID {
		t.Fatalf("首轮应选快过期积分的 A：%+v", res1.Account)
	}

	// B 的到期积分反超，但同一会话仍固定 A（粘性优先）
	if err := db.Model(&model.Account{}).Where("id = ?", b.ID).
		Update("credits_json", creditsExpiringIn(2, 999)).Error; err != nil {
		t.Fatal(err)
	}
	second := &pb.ChatRequest{Model: "glm-5.3", Messages: []*pb.EnvelopeMessage{
		{Role: "system", Text: "sys"}, {Role: "user", Text: "第一问"},
		{Role: "assistant", Text: "答"}, {Role: "user", Text: "第二问"}}}
	res2, err := rt.Resolve(key, second)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Account == nil || res2.Account.ID != a.ID {
		t.Errorf("同一会话应保持 A：got %+v", res2.Account)
	}

	// 新会话让位给快过期积分更多的 B
	other := &pb.ChatRequest{Model: "glm-5.3", Messages: []*pb.EnvelopeMessage{
		{Role: "system", Text: "sys"}, {Role: "user", Text: "另一个会话"}}}
	res3, err := rt.Resolve(key, other)
	if err != nil {
		t.Fatal(err)
	}
	if res3.Account == nil || res3.Account.ID != b.ID {
		t.Errorf("新会话应选快过期积分更多的 B：got %+v", res3.Account)
	}
}
