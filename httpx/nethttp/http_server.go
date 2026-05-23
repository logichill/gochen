package nethttp

import (
	"net/http"
	"net/netip"
	"sync"

	"gochen/httpx"
)

// Server 承载一组模块的轻量服务实现。
type Server struct {
	mux         *http.ServeMux
	config      *httpx.WebConfig
	server      *http.Server
	routes      map[string]*route
	groups      map[string]*RouteGroup
	middlewares []httpx.Middleware
	mu          sync.RWMutex

	trustedProxyAddrs    map[netip.Addr]struct{}
	trustedProxyPrefixes []netip.Prefix
}

type route struct {
	method      string
	pattern     string
	handler     httpx.Handler
	middlewares []httpx.Middleware
}

// NewServer 创建 net/http 适配层服务器。
func NewServer(config *httpx.WebConfig) *Server {
	if config == nil {
		config = &httpx.WebConfig{}
	}
	return &Server{
		mux:               http.NewServeMux(),
		config:            config,
		routes:            make(map[string]*route),
		groups:            make(map[string]*RouteGroup),
		middlewares:       make([]httpx.Middleware, 0),
		trustedProxyAddrs: make(map[netip.Addr]struct{}),
	}
}
