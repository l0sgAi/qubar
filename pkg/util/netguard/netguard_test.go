package netguard

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestIsBlockedAddr(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", "::1", "0.0.0.0", "::",
		"10.0.0.1", "172.16.5.4", "192.168.1.1", "fc00::1", "fd12::1",
		"169.254.169.254", "fe80::1", // link-local / cloud metadata
		"100.64.0.1", "198.18.0.1", "240.0.0.1", "255.255.255.255",
		"224.0.0.1", "ff02::1",
		"::ffff:127.0.0.1", "::ffff:169.254.169.254", // v4-mapped
		"::127.0.0.1", "64:ff9b::a00:1", "2002:7f00:1::1",
	}
	for _, s := range blocked {
		if !IsBlockedAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
	allowed := []string{"1.1.1.1", "8.8.8.8", "104.18.0.1", "2606:4700::1111"}
	for _, s := range allowed {
		if IsBlockedAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
}

func TestCheckURL(t *testing.T) {
	ok := []string{
		"https://api.openai.com/v1",
		"https://api.anthropic.com",
		"https://api.deepseek.com:443/v1",
		"https://1.1.1.1/v1",
		"https://example.cafe/v1",
	}
	for _, s := range ok {
		if _, err := CheckURL(s, false); err != nil {
			t.Errorf("%q: unexpected error %v", s, err)
		}
	}
	bad := []string{
		"",
		"http://api.openai.com/v1",   // not https
		"ftp://api.openai.com",       // scheme
		"file:///etc/passwd",         // scheme
		"https://user:pw@api.x.com",  // userinfo
		"https:///v1",                // empty host
		"https://127.0.0.1/v1",       // loopback
		"https://[::1]/v1",           // loopback v6
		"https://169.254.169.254/",   // metadata
		"https://10.0.0.5:9200/_doc", // private
		"https://[::ffff:10.0.0.1]/", // v4-mapped private
		"https://2130706433/",        // decimal 127.0.0.1
		"https://0x7f.0.0.1/",        // hex
		"https://0177.0.0.1/",        // octal
		"https://localhost/",
		"https://api.localhost/",
		"mailto:a@b.c",
	}
	for _, s := range bad {
		if _, err := CheckURL(s, false); !errors.Is(err, ErrBlocked) {
			t.Errorf("%q: want ErrBlocked, got %v", s, err)
		}
	}
}

func TestCheckURL_AllowPrivate(t *testing.T) {
	for _, s := range []string{"http://127.0.0.1:11434/v1", "http://gateway.internal/v1", "https://10.0.0.5/v1"} {
		if _, err := CheckURL(s, true); err != nil {
			t.Errorf("%q: allowPrivate should accept, got %v", s, err)
		}
	}
	if _, err := CheckURL("ftp://x", true); err == nil {
		t.Error("allowPrivate must still reject non-http(s) schemes")
	}
}

type fakeResolver map[string][]netip.Addr

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if a, ok := f[host]; ok {
		return a, nil
	}
	return nil, errors.New("no such host")
}

func TestCheckHostResolves(t *testing.T) {
	r := fakeResolver{
		"public.example":  {netip.MustParseAddr("93.184.216.34")},
		"rebind.example":  {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.1")},
		"private.example": {netip.MustParseAddr("192.168.0.10")},
	}
	ctx := context.Background()
	if err := CheckHostResolves(ctx, r, "public.example", false); err != nil {
		t.Errorf("public: %v", err)
	}
	for _, h := range []string{"rebind.example", "private.example", "missing.example", "127.0.0.1"} {
		if err := CheckHostResolves(ctx, r, h, false); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: want ErrBlocked, got %v", h, err)
		}
	}
	if err := CheckHostResolves(ctx, r, "private.example", true); err != nil {
		t.Errorf("allowPrivate: %v", err)
	}
}

func TestControl(t *testing.T) {
	c := Control(false)
	if err := c("tcp", "93.184.216.34:443", nil); err != nil {
		t.Errorf("public: %v", err)
	}
	for _, a := range []string{"127.0.0.1:9200", "[::1]:443", "169.254.169.254:80", "10.1.2.3:6379", "bad"} {
		if err := c("tcp", a, nil); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: want ErrBlocked, got %v", a, err)
		}
	}
	if err := Control(true)("tcp", "127.0.0.1:9200", nil); err != nil {
		t.Errorf("allowPrivate: %v", err)
	}
}

// 端到端：受限客户端连不上本机（环回）服务，含经域名解析与重定向两种绕过路径；allowPrivate 时可连。
func TestNewHTTPClient_BlocksLoopbackAtDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("internal"))
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	guarded := NewHTTPClient(5*time.Second, false)
	for _, target := range []string{srv.URL, "http://localhost:" + port} {
		if _, err := guarded.Get(target); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: want ErrBlocked at dial, got %v", target, err)
		}
	}

	open := NewHTTPClient(5*time.Second, true)
	resp, err := open.Get(srv.URL)
	if err != nil {
		t.Fatalf("allowPrivate client: %v", err)
	}
	resp.Body.Close()
}

func TestNewHTTPClient_RejectsDowngradeRedirect(t *testing.T) {
	c := NewHTTPClient(time.Second, false)
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err := c.CheckRedirect(req, []*http.Request{{}}); !errors.Is(err, ErrBlocked) {
		t.Errorf("want ErrBlocked on https->http redirect, got %v", err)
	}
}

// Transport 按模式共享（连接复用），两种模式的连接池互不相通。
func TestNewHTTPClient_SharesTransportPerMode(t *testing.T) {
	if NewHTTPClient(time.Second, false).Transport != NewHTTPClient(2*time.Second, false).Transport {
		t.Error("guarded clients should share one transport")
	}
	if NewHTTPClient(time.Second, true).Transport != NewHTTPClient(2*time.Second, true).Transport {
		t.Error("allowPrivate clients should share one transport")
	}
	if NewHTTPClient(time.Second, false).Transport == NewHTTPClient(time.Second, true).Transport {
		t.Error("guarded and allowPrivate clients must not share a transport")
	}
}

// allowPrivate 客户端先连上内网并留下空闲连接后，受限客户端仍须在拨号时被拦截（不能复用对方连接池）。
func TestNewHTTPClient_PooledPrivateConnNotReusedByGuarded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("internal"))
	}))
	defer srv.Close()

	resp, err := NewHTTPClient(5*time.Second, true).Get(srv.URL)
	if err != nil {
		t.Fatalf("allowPrivate client: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close() // 连接回到 allowPrivate 的空闲池

	if _, err := NewHTTPClient(5*time.Second, false).Get(srv.URL); !errors.Is(err, ErrBlocked) {
		t.Errorf("want ErrBlocked at dial, got %v", err)
	}
}
