// Package sdk — 插件开发工具包：实现 ClawPluginServer 即可接入核心。
package sdk

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	pb "github.com/Sndeok/ClawProxyHub-Next/sdk/proto/cphv1"
)

// 与核心约定的常量。protocol_version 不一致时核心拒载。
const (
	ProtocolVersion int32  = 1
	MagicCookieKey  string = "CPH_PLUGIN"
	MagicCookieVal  string = "claw-proxy-hub-plugin"
	// HostBrokerID 宿主 ClawHost 服务在 broker 上的固定通道号。
	HostBrokerID uint32 = 1000

	// ExtraClientUserAgent ChatRequest.extra 键：本次对话应使用的 User-Agent。
	// 核心按「路由 UA > 全局网关 UA > 客户端 UA」解析后注入；插件按需透传上游。
	ExtraClientUserAgent string = "client_user_agent"

	// ExtraFingerprintHeaders ChatRequest.extra 键：按入口协议生成的客户端指纹头
	// （JSON map，messages = Claude Code / chat_completions·responses = Codex）。
	// 插件按需采用，让上游看到「官方客户端」形态。
	ExtraFingerprintHeaders string = "fingerprint_headers"

	// SettingBrowserUserAgent 宿主 GetSettings 合并视图的保留键：全局浏览器 UA
	// （空 / 缺失 = 插件用内置值）。
	SettingBrowserUserAgent string = "_browser_user_agent"
)

// HandshakeConfig go-plugin 进程握手配置。
func HandshakeConfig() goplugin.HandshakeConfig {
	return goplugin.HandshakeConfig{
		ProtocolVersion:  uint(ProtocolVersion),
		MagicCookieKey:   MagicCookieKey,
		MagicCookieValue: MagicCookieVal,
	}
}

// hostDialRetry 宿主回调拨号失败后的重试间隔。
const hostDialRetry = 30 * time.Second

// Host 宿主回调能力（由核心注入，插件实现里可取用）。
//
// 连接在插件启动阶段就建立（warmup），失败则退避重试。
// 不能改成「首次使用时懒加载」：go-plugin 的 broker 在发出 ConnInfo 后只保留
// 5 秒（grpc_broker.go 的 timeoutWait），而插件的第一次宿主调用（日志 / 状态
// 读写 / 读设置）通常发生在核心启动很久之后的真实请求里，届时 ConnInfo 已被
// 回收，Dial 会等满 5 秒后超时；若把该失败缓存成终态，插件日志与插件状态持久化
// 会在此进程生命周期内静默失效。
type Host struct {
	dial    func() (pb.ClawHostClient, error)
	mu      sync.Mutex
	client  pb.ClawHostClient
	nextTry time.Time
}

// warmup 在插件启动阶段主动建立宿主连接（此时 ConnInfo 尚未过期）。
func (h *Host) warmup() {
	if c := h.conn(); c == nil {
		fmt.Fprintln(os.Stderr, "[cph-sdk] host callback unavailable at startup; will retry on first use")
	}
}

func (h *Host) conn() pb.ClawHostClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.client != nil {
		return h.client
	}
	if time.Now().Before(h.nextTry) {
		return nil // 退避窗口内：本次跳过，避免每个调用都卡满 5 秒
	}
	client, err := h.dial()
	if err != nil {
		h.nextTry = time.Now().Add(hostDialRetry)
		fmt.Fprintf(os.Stderr, "[cph-sdk] host dial failed (retry in %s): %v\n", hostDialRetry, err)
		return nil
	}
	h.client = client
	return h.client
}

// Log 写统一日志管道。
func (h *Host) Log(level, message string) {
	if c := h.conn(); c != nil {
		c.Log(context.Background(), &pb.LogEntry{Level: level, Message: message})
	}
}

// StoreGet 读插件状态。
func (h *Host) StoreGet(key string) ([]byte, bool) {
	c := h.conn()
	if c == nil {
		return nil, false
	}
	resp, err := c.StoreGet(context.Background(), &pb.StoreGetRequest{Key: key})
	if err != nil || !resp.Found {
		return nil, false
	}
	return resp.Value, true
}

// StorePut 写插件状态。
func (h *Host) StorePut(key string, value []byte) {
	if c := h.conn(); c != nil {
		c.StorePut(context.Background(), &pb.StorePutRequest{Key: key, Value: value})
	}
}

// Settings 读插件设置（核心管理界面在线编辑；pluginName 为本插件 id）。
// 返回原始 JSON（结构由 manifest.settings_schema 定义），读取失败回 nil 由调用方用默认值。
func (h *Host) Settings(pluginName string) []byte {
	c := h.conn()
	if c == nil {
		return nil
	}
	resp, err := c.GetSettings(context.Background(), &pb.GetSettingsRequest{Plugin: pluginName})
	if err != nil {
		return nil
	}
	return resp.Values
}

// Plugin 插件作者需要实现的全部：gRPC 服务 + 宿主注入点。
type Plugin interface {
	pb.ClawPluginServer
}

// HostAware 可选：实现后核心会把宿主回调注入插件。
type HostAware interface {
	SetHost(host *Host)
}

// pluginServer 包装用户实现，注册进 gRPC 并接通宿主回调。
type pluginServer struct {
	goplugin.NetRPCUnsupportedPlugin
	impl Plugin
}

func (s *pluginServer) GRPCServer(broker *goplugin.GRPCBroker, srv *grpc.Server) error {
	if ha, ok := s.impl.(HostAware); ok {
		host := &Host{dial: func() (pb.ClawHostClient, error) {
			conn, err := broker.Dial(HostBrokerID)
			if err != nil {
				return nil, err
			}
			return pb.NewClawHostClient(conn), nil
		}}
		ha.SetHost(host)
		// 必须异步：GRPCServer 要在 gRPC server 开始 Serve 之前返回，同步拨号
		// 会让核心侧的连接建立（grpc.WithBlock 等 HTTP/2 握手）一直卡到拨号
		// 超时为止。异步拨号在 Serve 启动后立刻发起，正好落在 ConnInfo 的
		// 5 秒窗口内。
		go host.warmup()
	}
	pb.RegisterClawPluginServer(srv, s.impl)
	return nil
}

// GRPCClient 插件进程不作为客户端使用，仅为满足 goplugin.GRPCPlugin。
func (s *pluginServer) GRPCClient(ctx context.Context, broker *goplugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return nil, fmt.Errorf("plugin does not act as a grpc client")
}

// Serve 启动插件进程，阻塞至核心将其关闭。
// 插件二进制的 main 只需一行：sdk.Serve(impl)。
func Serve(impl Plugin) {
	opts := &goplugin.ServeConfig{
		HandshakeConfig: HandshakeConfig(),
		Plugins: goplugin.PluginSet{
			"claw_plugin": &pluginServer{impl: impl},
		},
		GRPCServer: func(opts []grpc.ServerOption) *grpc.Server {
			return grpc.NewServer(opts...)
		},
	}
	goplugin.Serve(opts)
}
