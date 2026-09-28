// Package netguard 限制服务端向用户可控 URL 发起的出站请求（防 SSRF，l0sgAi/qubar#48）。
//
// 两道闸：
//   - 保存时 CheckURL + CheckHostResolves：只收 https、拒绝 userinfo / 内网字面量 IP / 解析到内网的域名，
//     及早给用户反馈。
//   - 连接时 NewHTTPClient：net.Dialer.Control 校验「实际要连的 IP」，挡住 DNS rebinding、
//     重定向到内网、以及保存后才变坏的历史数据。这一道才是真正的强制点。
//
// allowPrivate=true（配置 aiagent.allow_private_base_url）时两道闸都放行 http 与内网地址，
// 仅用于本地开发或内网自建模型网关。
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrBlocked 目标 URL 或地址不被允许。对外统一文案，不区分具体原因（避免成为内网探测 oracle）。
var ErrBlocked = errors.New("destination not allowed")

// blockedPrefixes net/netip 的 Is* 判断之外还需拦截的特殊用途网段。
var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8",       // "this network"
	"100.64.0.0/10",   // CGNAT（部分云内网/元数据服务在此段）
	"192.0.0.0/24",    // IETF protocol assignments
	"192.0.2.0/24",    // TEST-NET-1
	"198.18.0.0/15",   // benchmarking
	"198.51.100.0/24", // TEST-NET-2
	"203.0.113.0/24",  // TEST-NET-3
	"240.0.0.0/4",     // reserved + broadcast
	"::/96",           // IPv4-compatible（已废弃，可能映射到内网 v4）
	"64:ff9b::/96",    // NAT64：可映射到任意 v4（含内网）
	"64:ff9b:1::/48",  // local-use NAT64
	"100::/64",        // discard-only
	"2001::/32",       // Teredo
	"2001:db8::/32",   // documentation
	"2002::/16",       // 6to4：内嵌 v4
	"fec0::/10",       // site-local（已废弃）
)

func mustPrefixes(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		out = append(out, netip.MustParsePrefix(c))
	}
	return out
}

// IsBlockedAddr 地址是否属于环回 / 私网 / 链路本地 / 组播 / 保留等不可作为出站目标的网段。
func IsBlockedAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// CheckURL 静态校验（不做 DNS）：https、无 userinfo、host 非空、字面量 IP 不在拦截网段、
// 拒绝整数/八进制/十六进制等歧义数字 host（部分解析器会把 0177.0.0.1 / 2130706433 当作 127.0.0.1）。
func CheckURL(raw string, allowPrivate bool) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, ErrBlocked
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !allowPrivate {
			return nil, ErrBlocked
		}
	default:
		return nil, ErrBlocked
	}
	if u.User != nil || u.Opaque != "" {
		return nil, ErrBlocked
	}
	host := u.Hostname()
	if host == "" {
		return nil, ErrBlocked
	}
	if allowPrivate {
		return u, nil
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if IsBlockedAddr(ip) {
			return nil, ErrBlocked
		}
		return u, nil
	}
	if isAmbiguousNumericHost(host) {
		return nil, ErrBlocked
	}
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return nil, ErrBlocked
	}
	return u, nil
}

// isAmbiguousNumericHost host 不是合法 IP 却只由数字、点、x/a-f 组成（如 2130706433、0x7f.1、0177.0.0.1）。
// 公网域名的顶级域不会是纯数字/十六进制串，直接拒绝最稳妥。
func isAmbiguousNumericHost(host string) bool {
	labels := strings.Split(strings.TrimSuffix(host, "."), ".")
	tld := labels[len(labels)-1]
	if tld == "" {
		return true
	}
	for _, r := range strings.ToLower(tld) {
		if (r < '0' || r > '9') && r != 'x' && (r < 'a' || r > 'f') {
			return false
		}
	}
	// 形如 "cafe"/"bad"/"dad" 的真实 TLD 全由 a-f 构成：必须至少含一个数字才算歧义。
	return strings.ContainsAny(tld, "0123456789")
}

// Resolver 便于测试替换的 DNS 解析器（*net.Resolver 满足）。
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// CheckHostResolves 解析 host，任一结果落在拦截网段即拒绝；解析失败同样拒绝（统一文案）。
// 仅用于保存时的提前反馈，连接时由 NewHTTPClient 再次强制校验。
func CheckHostResolves(ctx context.Context, r Resolver, host string, allowPrivate bool) error {
	if allowPrivate {
		return nil
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if IsBlockedAddr(ip) {
			return ErrBlocked
		}
		return nil
	}
	if r == nil {
		r = net.DefaultResolver
	}
	addrs, err := r.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return ErrBlocked
	}
	for _, a := range addrs {
		if IsBlockedAddr(a) {
			return ErrBlocked
		}
	}
	return nil
}

// Control 供 net.Dialer.Control 使用：在建立连接前校验解析后的真实 IP。
func Control(allowPrivate bool) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, _ syscall.RawConn) error {
		if allowPrivate {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrBlocked, address)
		}
		ip, err := netip.ParseAddr(host)
		if err != nil || IsBlockedAddr(ip) {
			return fmt.Errorf("%w: %s", ErrBlocked, address)
		}
		return nil
	}
}

// maxRedirects 与 net/http 默认值一致。
const maxRedirects = 10

// NewHTTPClient 返回出站受限的 http.Client：
//   - 每次拨号经 Control 校验目标 IP（含重定向后的连接）
//   - 不走环境变量代理（否则只能校验到代理地址，真实目标失控）
//   - 重定向不得降级为 http（allowPrivate 时除外）
func NewHTTPClient(timeout time.Duration, allowPrivate bool) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   Control(allowPrivate),
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if !allowPrivate && req.URL.Scheme != "https" {
				return fmt.Errorf("%w: redirect to %s", ErrBlocked, req.URL.Scheme)
			}
			return nil
		},
	}
}
