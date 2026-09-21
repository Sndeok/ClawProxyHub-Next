// host.go — 核心侧 ClawHost 服务：插件经 broker 反调的统一入口。
// 每个插件实例持有独立的 HostService，KV 按 plugin 隔离并持久化到 plugin_storage 表。
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"gorm.io/gorm"

	"google.golang.org/grpc"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// HostService ClawHost gRPC 实现。按插件隔离 key 空间，持久化到 plugin_storage 表。
type HostService struct {
	pb.UnimplementedClawHostServer

	db         *gorm.DB
	pluginName atomic.Value
	mu         sync.RWMutex
	stores     map[string]map[string][]byte
}

func NewHostService(db *gorm.DB) *HostService {
	return &HostService{db: db, stores: map[string]map[string][]byte{}}
}

// SetPluginName 握手完成后设置插件名。
func (h *HostService) SetPluginName(name string) {
	h.pluginName.Store(name)
}

func (h *HostService) plugin() string {
	if v, ok := h.pluginName.Load().(string); ok {
		return v
	}
	return ""
}

func (h *HostService) Log(ctx context.Context, e *pb.LogEntry) (*pb.Empty, error) {
	log.Printf("[plugin:%s] %s: %s", h.plugin(), e.Level, e.Message)
	return &pb.Empty{}, nil
}

func (h *HostService) StoreGet(ctx context.Context, r *pb.StoreGetRequest) (*pb.StoreGetResponse, error) {
	plugin := h.plugin()
	h.mu.RLock()
	if m, ok := h.stores[plugin]; ok {
		if v, ok := m[r.Key]; ok {
			h.mu.RUnlock()
			return &pb.StoreGetResponse{Value: v, Found: true}, nil
		}
	}
	h.mu.RUnlock()

	var rec model.PluginStorage
	if err := h.db.Where("plugin = ? AND key = ?", plugin, r.Key).First(&rec).Error; err != nil {
		return &pb.StoreGetResponse{Found: false}, nil
	}

	h.mu.Lock()
	if h.stores[plugin] == nil {
		h.stores[plugin] = map[string][]byte{}
	}
	h.stores[plugin][r.Key] = rec.Value
	h.mu.Unlock()

	return &pb.StoreGetResponse{Value: rec.Value, Found: true}, nil
}

func (h *HostService) StorePut(ctx context.Context, r *pb.StorePutRequest) (*pb.Empty, error) {
	plugin := h.plugin()
	if err := h.db.Exec(
		`INSERT INTO plugin_storage (plugin, key, value, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(plugin, key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
		plugin, r.Key, r.Value,
	).Error; err != nil {
		return nil, fmt.Errorf("plugin storage write: %w", err)
	}

	h.mu.Lock()
	if h.stores[plugin] == nil {
		h.stores[plugin] = map[string][]byte{}
	}
	h.stores[plugin][r.Key] = r.Value
	h.mu.Unlock()

	return &pb.Empty{}, nil
}

func (h *HostService) GetProxy(ctx context.Context, r *pb.GetProxyRequest) (*pb.ProxyConfig, error) {
	var links []model.GroupProxy
	if err := h.db.Where("group_id = ?", r.GroupId).Order("proxy_id").Find(&links).Error; err != nil || len(links) == 0 {
		return nil, fmt.Errorf("no proxy bound to group %s", r.GroupId)
	}
	var proxy model.Proxy
	if err := h.db.First(&proxy, links[0].ProxyID).Error; err != nil {
		return nil, fmt.Errorf("proxy record missing")
	}
	return &pb.ProxyConfig{
		Scheme: proxy.Scheme, Host: proxy.Host, Port: proxy.Port,
		Username: proxy.Username, Password: proxy.Password,
	}, nil
}

func (h *HostService) GetSettings(ctx context.Context, r *pb.GetSettingsRequest) (*pb.GetSettingsResponse, error) {
	var p model.Plugin
	values := "{}"
	if err := h.db.Select("settings_json").Where("name = ?", r.Plugin).First(&p).Error; err == nil && p.SettingsJSON != "" {
		values = p.SettingsJSON
	}
	// 全局「出站标识」作为默认值下发：插件自身设置为空时用它，
	// 这样设置页改一次就能同时作用于所有插件，插件页仍可按插件覆盖。
	return &pb.GetSettingsResponse{Values: []byte(h.mergeOutboundDefaults(values))}, nil
}

// mergeOutboundDefaults 把 settings 里的出站标识合并进插件设置（不覆盖插件已填的值）。
func (h *HostService) mergeOutboundDefaults(values string) string {
	cfg := map[string]string{}
	if json.Unmarshal([]byte(values), &cfg) != nil {
		cfg = map[string]string{}
	}
	for k, v := range h.outboundIdentity() {
		if strings.TrimSpace(cfg[k]) == "" && v != "" {
			cfg[k] = v
		}
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return values
	}
	return string(b)
}

// outboundIdentity 读全局出站标识（settings 表，空 = 未配置）。
func (h *HostService) outboundIdentity() map[string]string {
	out := map[string]string{}
	var rows []model.Setting
	h.db.Where("key LIKE ?", "outbound.%").Find(&rows)
	for _, row := range rows {
		out[strings.TrimPrefix(row.Key, "outbound.")] = strings.TrimSpace(row.Value)
	}
	return out
}

func (h *HostService) ServeHost(broker interface {
	AcceptAndServe(id uint32, f func([]grpc.ServerOption) *grpc.Server)
}) {
	broker.AcceptAndServe(hostBrokerID, func(opts []grpc.ServerOption) *grpc.Server {
		srv := grpc.NewServer(opts...)
		pb.RegisterClawHostServer(srv, h)
		return srv
	})
}
