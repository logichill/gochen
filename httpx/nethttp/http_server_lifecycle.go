package nethttp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"gochen/errors"
	"gochen/httpx"
)

const defaultStaticCacheMaxAgeSeconds = 3600

// Start 启动底层 net/http 服务，并在启用 TLS 时完成证书与版本校验。
func (s *Server) Start(addr string) error {
	if addr == "" {
		addr = fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	}

	if err := s.applySecurityDefaults(); err != nil {
		return err
	}
	s.applyTimeoutDefaults()

	s.registerRoutes()
	srv, err := s.newHTTPServer(addr)
	if err != nil {
		return err
	}
	s.server = srv
	if s.config.TLSEnabled {
		// 证书来源二选一：
		// 1) CertFile/KeyFile；或
		// 2) TLSConfig 内置证书（Certificates/GetCertificate/GetConfigForClient）。
		if s.config.CertFile != "" || s.config.KeyFile != "" {
			return s.server.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile)
		}
		if s.server.TLSConfig == nil {
			return errors.NewCode(errors.InvalidInput, "tls enabled but tls config is nil")
		}
		if len(s.server.TLSConfig.Certificates) == 0 && s.server.TLSConfig.GetCertificate == nil && s.server.TLSConfig.GetConfigForClient == nil {
			return errors.NewCode(errors.InvalidInput, "tls enabled but no certificate configured")
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		return s.server.Serve(tls.NewListener(ln, s.server.TLSConfig))
	}
	return s.server.ListenAndServe()
}

// newHTTPServer 根据当前 WebConfig 构造底层 `*http.Server`。
func (s *Server) newHTTPServer(addr string) (*http.Server, error) {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: s.config.ReadHeaderTimeout,
		ReadTimeout:       s.config.ReadTimeout,
		WriteTimeout:      s.config.WriteTimeout,
		IdleTimeout:       s.config.IdleTimeout,
		MaxHeaderBytes:    s.config.MaxHeaderBytes,
	}

	if !s.config.TLSEnabled {
		return srv, nil
	}

	minVersion, err := parseTLSMinVersion(s.config.TLSMinVersion)
	if err != nil {
		return nil, err
	}
	if minVersion < tls.VersionTLS12 {
		return nil, errors.NewCode(errors.InvalidInput, "tls_min_version too low").WithContext("value", s.config.TLSMinVersion)
	}

	var tlsCfg *tls.Config
	if s.config.TLSConfig != nil {
		tlsCfg = s.config.TLSConfig.Clone()
	} else {
		tlsCfg = &tls.Config{}
	}

	// 如果调用方未显式指定，则使用 TLSMinVersion（或默认 TLS1.2）。
	if tlsCfg.MinVersion == 0 {
		tlsCfg.MinVersion = minVersion
	} else if tlsCfg.MinVersion < minVersion {
		return nil, errors.NewCode(errors.InvalidInput, "tls config min version lower than tls_min_version").
			WithContext("tls_min_version", s.config.TLSMinVersion).
			WithContext("tls_config_min_version", tlsCfg.MinVersion)
	}
	if tlsCfg.MinVersion < tls.VersionTLS12 {
		return nil, errors.NewCode(errors.InvalidInput, "tls config min version too low").WithContext("value", tlsCfg.MinVersion)
	}
	if len(tlsCfg.NextProtos) == 0 {
		tlsCfg.NextProtos = []string{"h2", "http/1.1"}
	}
	srv.TLSConfig = tlsCfg

	return srv, nil
}

// applyTimeoutDefaults 为未显式设置的超时与 header 限制补上安全默认值。
func (s *Server) applyTimeoutDefaults() {
	if s.config == nil {
		s.config = &httpx.WebConfig{}
	}
	if s.config.ReadHeaderTimeout == 0 {
		s.config.ReadHeaderTimeout = httpx.DefaultReadHeaderTimeout
	}
	if s.config.ReadHeaderTimeout < 0 {
		s.config.ReadHeaderTimeout = 0
	}
	if s.config.ReadTimeout <= 0 {
		s.config.ReadTimeout = httpx.DefaultReadTimeout
	}
	if s.config.WriteTimeout <= 0 {
		s.config.WriteTimeout = httpx.DefaultWriteTimeout
	}
	if s.config.IdleTimeout <= 0 {
		s.config.IdleTimeout = httpx.DefaultIdleTimeout
	}
	if s.config.MaxHeaderBytes <= 0 {
		s.config.MaxHeaderBytes = httpx.DefaultMaxHeaderBytes
	}
}

// applySecurityDefaults 解析 TLS 与受信任代理配置，并执行安全基线校验。
func (s *Server) applySecurityDefaults() error {
	if s.config == nil {
		s.config = &httpx.WebConfig{}
	}

	// TLS：校验最低版本（安全基线为 TLS 1.2）。
	if s.config.TLSEnabled {
		minVersion, err := parseTLSMinVersion(s.config.TLSMinVersion)
		if err != nil {
			return err
		}
		if minVersion < tls.VersionTLS12 {
			return errors.NewCode(errors.InvalidInput, "tls_min_version too low").WithContext("value", s.config.TLSMinVersion)
		}
		// 若用户通过 TLSConfig 显式设置了更低版本，也拒绝。
		if s.config.TLSConfig != nil && s.config.TLSConfig.MinVersion != 0 && s.config.TLSConfig.MinVersion < tls.VersionTLS12 {
			return errors.NewCode(errors.InvalidInput, "tls config min version too low").WithContext("value", s.config.TLSConfig.MinVersion)
		}
	}

	s.trustedProxyAddrs = make(map[netip.Addr]struct{})
	s.trustedProxyPrefixes = nil

	for _, raw := range s.config.TrustedProxies {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if strings.Contains(v, "/") {
			prefix, err := netip.ParsePrefix(v)
			if err != nil {
				return errors.NewCode(errors.InvalidInput, "invalid trusted proxy CIDR").WithContext("value", v)
			}
			s.trustedProxyPrefixes = append(s.trustedProxyPrefixes, prefix)
			continue
		}

		addr, err := netip.ParseAddr(v)
		if err != nil {
			return errors.NewCode(errors.InvalidInput, "invalid trusted proxy IP").WithContext("value", v)
		}
		s.trustedProxyAddrs[addr] = struct{}{}
	}

	return nil
}

// parseTLSMinVersion 把配置字符串解析为 `crypto/tls` 使用的版本常量。
func parseTLSMinVersion(raw string) (uint16, error) {
	v := strings.TrimSpace(strings.ToLower(raw))
	switch v {
	case "", "default":
		return tls.VersionTLS12, nil
	case "1.0", "tls1.0", "tls10", "tlsv1.0":
		return tls.VersionTLS10, nil
	case "1.1", "tls1.1", "tls11", "tlsv1.1":
		return tls.VersionTLS11, nil
	case "1.2", "tls1.2", "tls12", "tlsv1.2":
		return tls.VersionTLS12, nil
	case "1.3", "tls1.3", "tls13", "tlsv1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, errors.NewCode(errors.InvalidInput, "invalid tls_min_version").WithContext("value", raw)
	}
}

// isTrustedProxy 判断 addr 是否属于受信任代理。
//
// 参数：
// - addr：待判断的代理 IP
func (s *Server) isTrustedProxy(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	if _, ok := s.trustedProxyAddrs[addr]; ok {
		return true
	}
	for _, p := range s.trustedProxyPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Stop 在给定上下文约束下优雅关闭底层 HTTP 服务。
func (s *Server) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

// HealthCheck 为与上层 Server 抽象对齐保留一个空实现。
func (s *Server) HealthCheck() error { return nil }

// Handler 返回底层 http.Handler。
func (s *Server) Handler() http.Handler { return s.mux }

// ServeMux 返回底层 *http.ServeMux。
func (s *Server) ServeMux() *http.ServeMux { return s.mux }

// registerRoutes 注册全部路由。
