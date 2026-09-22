package gateway

import (
	"reflect"
	"testing"
)

// TestPublicModelIDs 对外模型 = 路由名 ∪ 账号目录模型：去重、路由名在前、忽略空串。
func TestPublicModelIDs(t *testing.T) {
	got := publicModelIDs([]string{"alias-1", "shared"}, []string{"shared", "stub-mini", ""})
	want := []string{"alias-1", "shared", "stub-mini"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("publicModelIDs = %v, want %v", got, want)
	}
	if got := publicModelIDs(nil, nil); len(got) != 0 {
		t.Fatalf("空输入应返回空列表：%v", got)
	}
	// 受限 key：DirectModels 为空，只剩被授权的路由名
	if got := publicModelIDs([]string{"alias-1"}, nil); len(got) != 1 || got[0] != "alias-1" {
		t.Fatalf("受限 key 应只有路由名：%v", got)
	}
	// 只有账号目录、没有路由：模型依然对外可见
	if got := publicModelIDs(nil, []string{"kmodel_latest"}); len(got) != 1 || got[0] != "kmodel_latest" {
		t.Fatalf("无路由时应透出账号目录模型：%v", got)
	}
}
