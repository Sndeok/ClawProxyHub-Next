package admin

import (
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestNormalizeMarketProxy(t *testing.T) {
	ok := map[string]string{
		"":                                   "",
		"   ":                                "",
		"127.0.0.1:1080":                     "socks5://127.0.0.1:1080",
		"socks5://127.0.0.1:1080":            "socks5://127.0.0.1:1080",
		"socks5h://user:pass@h.example:1080": "socks5h://user:pass@h.example:1080",
		"http://proxy.example:3128":          "http://proxy.example:3128",
		"https://proxy.example:8443":         "https://proxy.example:8443",
	}
	for in, want := range ok {
		got, err := normalizeMarketProxy(in)
		if err != nil {
			t.Errorf("normalize(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}

	for _, bad := range []string{
		"ftp://host:21",    // 协议不支持
		"socks5://host",    // 缺端口
		"socks5://",        // 缺 host
		"http://host:port", // 端口非数字
	} {
		if _, err := normalizeMarketProxy(bad); err == nil {
			t.Errorf("normalize(%q) should fail", bad)
		}
		if validMarketProxy(bad) {
			t.Errorf("validMarketProxy(%q) should be false", bad)
		}
	}
	if !validMarketProxy("socks5://127.0.0.1:1080") {
		t.Error("valid proxy rejected")
	}
}

// socks5Server 极简 SOCKS5 服务端（无认证 + CONNECT），用于验证客户端确实经代理转发。
func socks5Server(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go socks5Handler(c)
		}
	}()
	return ln.Addr().String()
}

func socks5Handler(c net.Conn) {
	defer c.Close()
	// 握手：VER NMETHODS METHODS...
	head := make([]byte, 2)
	if _, err := io.ReadFull(c, head); err != nil || head[0] != 5 {
		return
	}
	if _, err := io.ReadFull(c, make([]byte, int(head[1]))); err != nil {
		return
	}
	if _, err := c.Write([]byte{5, 0}); err != nil { // 无需认证
		return
	}
	// 请求：VER CMD RSV ATYP ADDR PORT
	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil {
		return
	}
	var host string
	switch req[3] {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		host = net.IP(b).String()
	case 3:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		host = string(b)
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		host = net.IP(b).String()
	default:
		return
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(c, pb); err != nil {
		return
	}
	target := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb))))

	up, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}) // host unreachable
		return
	}
	defer up.Close()
	if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	go func() { _, _ = io.Copy(up, c) }()
	_, _ = io.Copy(c, up)
}

func indexServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"name":"workbuddy","version":"0.1.1","author":"cph","download_url":"https://example.com/x.cphplugin","sha256":""}]`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestMarketProxySocks5 直连与经 SOCKS5 两条路径都要能拉到索引 —— 后者证明
// 「获取插件 / 下载插件」确实走了配置的代理，而不是被静默忽略。
func TestMarketProxySocks5(t *testing.T) {
	srv := indexServer(t)
	proxyAddr := socks5Server(t)

	direct, err := marketClient("", marketIndexTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fetchIndex(direct, srv.URL); err != nil {
		t.Fatalf("direct fetch failed: %v", err)
	}

	proxied, err := marketClient("socks5://"+proxyAddr, marketIndexTimeout)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fetchIndex(proxied, srv.URL)
	if err != nil {
		t.Fatalf("socks5 fetch failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "workbuddy" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

// TestMarketProxyDeadProxyFails 代理不可用时必须报错（而不是悄悄直连）。
func TestMarketProxyDeadProxyFails(t *testing.T) {
	srv := indexServer(t)
	client, err := marketClient("socks5://127.0.0.1:1", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fetchIndex(client, srv.URL); err == nil {
		t.Fatal("expected fetch to fail through an unreachable proxy")
	}
}
