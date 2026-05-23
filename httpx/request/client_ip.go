package request

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ResolveClientIP 在可信代理场景下解析客户端 IP。
func ResolveClientIP(r *http.Request, trustedProxyChecker func(netip.Addr) bool) string {
	if r == nil {
		return ""
	}
	remoteIP, remoteOK := ParseRemoteIP(r.RemoteAddr)
	if remoteOK && trustedProxyChecker != nil && trustedProxyChecker(remoteIP) {
		if ip, ok := ParseForwardedFor(r.Header.Get("X-Forwarded-For")); ok {
			return ip.String()
		}
		if ip, ok := ParseForwardedIP(r.Header.Get("X-Real-IP")); ok {
			return ip.String()
		}
	}
	if remoteOK {
		return remoteIP.String()
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// ParseRemoteIP 解析RemoteIP。
func ParseRemoteIP(remoteAddr string) (netip.Addr, bool) {
	if remoteAddr == "" {
		return netip.Addr{}, false
	}
	host := strings.TrimSpace(remoteAddr)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip, true
}

// ParseForwardedFor 解析Forwarded。
func ParseForwardedFor(xff string) (netip.Addr, bool) {
	if xff == "" {
		return netip.Addr{}, false
	}
	parts := strings.Split(xff, ",")
	for _, p := range parts {
		if ip, ok := ParseForwardedIP(p); ok {
			return ip, true
		}
	}
	return netip.Addr{}, false
}

// ParseForwardedIP 解析ForwardedIP。
func ParseForwardedIP(v string) (netip.Addr, bool) {
	s := strings.TrimSpace(v)
	if s == "" {
		return netip.Addr{}, false
	}
	ip, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip, true
}
