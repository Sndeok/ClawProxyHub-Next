package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	"github.com/Sndeok/ClawProxyHub-Next/internal/database"
	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
	"github.com/Sndeok/ClawProxyHub-Next/internal/router"
	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// stubRegistry 只实现网关用到的插件目录接口，测试里不发真实 RPC。
type stubRegistry struct{ models map[string]string }

func (s stubRegistry) Models() map[string]string { return s.models }

func (s stubRegistry) ResolveModel(name string) (string, bool) {
	plugin, ok := s.models[name]
	return plugin, ok
}

func (s stubRegistry) Endpoints(string) []string { return nil }

func (s stubRegistry) Chat(context.Context, *pb.ChatRequest, string, *pb.CredentialBlob) (chan *pb.StreamEvent, error) {
	return nil, nil
}

func openGatewayTestDB(t *testing.T) *gorm.DB {
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

// TestModelsEndpointUnionsRoutesAndAccountCatalog 回归：实例里只要存在路由，
// /v1/models 也必须把账号目录里的直连模型透出来（此前只返回路由名）。
func TestModelsEndpointUnionsRoutesAndAccountCatalog(t *testing.T) {
	db := openGatewayTestDB(t)

	plugin := &model.Plugin{Name: "stub", Author: "cph", Enabled: true}
	if err := db.Create(plugin).Error; err != nil {
		t.Fatal(err)
	}
	group := &model.Group{Name: "g1", PluginID: plugin.ID, Strategy: "sticky"}
	if err := db.Create(group).Error; err != nil {
		t.Fatal(err)
	}
	acct := model.Account{PluginID: plugin.ID, DisplayName: "A", Status: "active",
		ModelsJSON: `[{"id":"kmodel_latest"}]`}
	if err := db.Create(&acct).Error; err != nil {
		t.Fatal(err)
	}
	route := &model.Route{Name: "alias-1", Strategy: "sticky",
		GroupsJSON: fmt.Sprintf(`[{"group_id":%d,"weight":1,"model":"kmodel_latest"}]`, group.ID)}
	if err := db.Create(route).Error; err != nil {
		t.Fatal(err)
	}

	const raw = "cph-test-key"
	sum := sha256.Sum256([]byte(raw))
	key := &model.Key{Name: "k", KeyHash: hex.EncodeToString(sum[:]), Enabled: true}
	if err := db.Create(key).Error; err != nil {
		t.Fatal(err)
	}

	srv := New(db, t.TempDir(), stubRegistry{models: map[string]string{"kmodel_latest": "stub"}}, router.New(db), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v (%s)", err, rec.Body.String())
	}
	got := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		got = append(got, m.ID)
	}
	want := map[string]bool{"alias-1": true, "kmodel_latest": true}
	if len(got) != len(want) {
		t.Fatalf("对外模型 = 路由名 ∪ 账号目录模型，want [alias-1 kmodel_latest] got %v", got)
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("出现未预期模型 %q（got %v）", id, got)
		}
	}
}
