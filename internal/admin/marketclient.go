// marketclient.go — 插件市场出站通道：拉索引与下载插件包共用的 HTTP 客户端。
//
// 与 network.github_proxy 的分工：那是「URL 前缀改写」（ghproxy 风格），只对
// GitHub 域名生效；这里是传输层代理，支持 socks5 / socks5h / http / https，
// 可直连内网出口。
//
// 地址写法：socks5://[user:pass@]host:port（域名交由代理解析，等价 socks5h）、
// http://host:port。省略协议头时按 socks5 处理。
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

const (
	// marketIndexTimeout 拉取市场索引的总超时。
	marketIndexTimeout = 20 * time.Second
	// marketDownloadTimeout 下载插件包的总超时（含 5 平台二进制，包体较大）。
	marketDownloadTimeout = 10 * time.Minute
	// marketDialTimeout 建立连接（含经代理解析）的超时。
	marketDialTimeout = 10 * time.Second
)

var (
	marketTransportMu sync.Mutex
	marketTransports  = map[string]*http.Transport{}
)

// normalizeMarketProxy 归一化代理地址：留空 = 直连；缺协议头时按 socks5 处理。
func normalizeMarketProxy(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "socks5://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("代理地址无法解析: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "socks5", "socks5h", "http", "https":
	default:
		return "", fmt.Errorf("不支持的代理协议 %q（支持 socks5 / socks5h / http / https）", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("代理地址缺少 host:port")
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return "", fmt.Errorf("代理地址需写成 host:port：%w", err)
	}
	return u.String(), nil
}

// marketTransport 返回按代理配置缓存的 Transport（同一配置复用连接池）。
func marketTransport(rawProxy string) (*http.Transport, error) {
	normalized, err := normalizeMarketProxy(rawProxy)
	if err != nil {
		return nil, err
	}
	marketTransportMu.Lock()
	defer marketTransportMu.Unlock()
	if t, ok := marketTransports[normalized]; ok {
		return t, nil
	}

	t := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: marketDialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   marketDialTimeout,
		ExpectContinueTimeout: time.Second,
	}

	switch {
	case normalized == "":
		// 直连（仍尊重进程的 HTTP(S)_PROXY 环境变量）
	case strings.HasPrefix(normalized, "socks5"):
		u, _ := url.Parse(normalized)
		var auth *proxy.Auth
		if u.User != nil {
			pw, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pw}
		}
		dialer, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("socks5 拨号器初始化失败: %w", err)
		}
		// socks5 把域名交给代理解析（等价 socks5h），不做本地 DNS
		if cd, ok := dialer.(proxy.ContextDialer); ok {
			t.DialContext = cd.DialContext
		} else {
			t.DialContext = func(_ context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			}
		}
		t.Proxy = nil // 已在拨号层代理，避免叠加
	default: // http / https
		u, _ := url.Parse(normalized)
		t.Proxy = http.ProxyURL(u)
	}

	marketTransports[normalized] = t
	return t, nil
}

// marketClient 构造走指定代理的 HTTP 客户端；timeout 为整次请求总超时。
func marketClient(rawProxy string, timeout time.Duration) (*http.Client, error) {
	t, err := marketTransport(rawProxy)
	if err != nil {
		return nil, err
	}
	return &http.Client{Timeout: timeout, Transport: t}, nil
}

// validMarketProxy 供设置项校验：空 = 合法（直连）。
func validMarketProxy(raw string) bool {
	_, err := normalizeMarketProxy(raw)
	return err == nil
}

// fetchIndex 拉取并解析市场索引（带体积上限，避免异常响应撑爆内存）。
func fetchIndex(client *http.Client, indexURL string) ([]MarketEntry, error) {
	resp, err := client.Get(indexURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("索引返回 HTTP %d", resp.StatusCode)
	}
	var entries []MarketEntry
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&entries); err != nil {
		return nil, fmt.Errorf("索引不是合法 JSON: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("索引为空")
	}
	return entries, nil
}