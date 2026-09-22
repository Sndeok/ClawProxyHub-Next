package plugin

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
)

// hostTestDB 建一个只含 settings 表的内存库，并落一条全局出站标识。
func hostTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "host.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&model.Setting{Key: "outbound.client_name", Value: "GlobalClient"})
	db.Create(&model.Setting{Key: "outbound.user_agent", Value: "GlobalUA/1.0"})
	t.Cleanup(func() {
		if s, err := db.DB(); err == nil {
			_ = s.Close()
		}
	})
	return db
}

// TestMergeOutboundDefaults 回归：插件设置里出现非字符串值（数字/布尔）时，
// 不能整体解析失败把插件自己的设置丢掉；同时「不覆盖插件已填的非空值」。
func TestMergeOutboundDefaults(t *testing.T) {
	h := NewHostService(hostTestDB(t))

	cases := []struct {
		name    string
		values  string
		want    map[string]interface{}
		missing []string
	}{
		{
			name:   "非字符串值必须保留",
			values: `{"timeout":30,"verbose":true,"client_name":"PluginClient"}`,
			want: map[string]interface{}{
				"timeout": float64(30), "verbose": true, "client_name": "PluginClient",
			},
		},
		{
			name:   "插件空值用全局填充",
			values: `{"client_name":"","user_agent":"  "}`,
			want: map[string]interface{}{
				"client_name": "GlobalClient", "user_agent": "GlobalUA/1.0",
			},
		},
		{
			name:   "插件非空值不被覆盖",
			values: `{"client_name":"Mine","user_agent":"Mine/2"}`,
			want: map[string]interface{}{
				"client_name": "Mine", "user_agent": "Mine/2",
			},
		},
		{
			name:   "空输入落全局默认",
			values: "",
			want: map[string]interface{}{
				"client_name": "GlobalClient", "user_agent": "GlobalUA/1.0",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := h.mergeOutboundDefaults(tc.values)
			got := map[string]interface{}{}
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("输出不是合法 JSON: %s (%v)", out, err)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("%s = %v，want %v（完整输出 %s）", k, got[k], v, out)
				}
			}
		})
	}

	// 非法 JSON / 非对象：原样返回，不能吞掉插件设置
	for _, raw := range []string{"not json", "[1,2,3]", "\"str\""} {
		if got := h.mergeOutboundDefaults(raw); got != raw {
			t.Fatalf("非法输入应原样返回，in=%q out=%q", raw, got)
		}
	}
}
