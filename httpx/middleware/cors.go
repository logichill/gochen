package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"gochen/httpx"
)

// CORSConfig CORS 配置。
type CORSConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int
}

func CORSFromWebConfig(cfg *httpx.WebConfig) httpx.Middleware {
	// 安全默认：nil config 等价于未启用 CORS。
	if cfg == nil || !cfg.CORSEnabled {
		return func(ctx httpx.IContext, next func() error) error { return next() }
	}
	// 显式 allowlist：启用 CORS 时必须显式设置允许的 origins。
	if len(cfg.CORSAllowOrigins) == 0 {
		return func(ctx httpx.IContext, next func() error) error { return next() }
	}
	return CORS(&CORSConfig{
		AllowOrigins:  cfg.CORSAllowOrigins,
		AllowMethods:  cfg.CORSAllowMethods,
		AllowHeaders:  cfg.CORSAllowHeaders,
		ExposeHeaders: nil,
		// 当 AllowOrigins 包含 "*" 时，不允许 credentials（浏览器会拒绝）。
		AllowCredentials: false,
		MaxAge:           86400,
	})
}

// CORS CORS 中间件。
func CORS(cfg *CORSConfig) httpx.Middleware {
	// 安全默认：必须显式传入 allowlist，否则视为未启用。
	if cfg == nil || len(cfg.AllowOrigins) == 0 {
		return func(ctx httpx.IContext, next func() error) error { return next() }
	}
	c := cfg

	return func(ctx httpx.IContext, next func() error) error {
		origin := ctx.Header("Origin")
		// 仅将满足 W3C CORS 预检条件的 OPTIONS 请求视为 preflight：
		// - method=OPTIONS
		// - 存在 Origin
		// - 存在 Access-Control-Request-Method
		//
		// 这样可以避免把“业务自定义 OPTIONS 端点”误判为 preflight 而被短路。
		isPreflight := ctx.Method() == http.MethodOptions && origin != "" && ctx.Header("Access-Control-Request-Method") != ""
		allowAllOrigins := contains(c.AllowOrigins, "*")

		if len(c.AllowOrigins) > 0 {
			if allowAllOrigins {
				ctx.SetHeader("Access-Control-Allow-Origin", "*")
			} else if origin != "" && contains(c.AllowOrigins, origin) {
				ctx.SetHeader("Access-Control-Allow-Origin", origin)
				// 多 origin 场景下建议设置 Vary，避免缓存污染
				ctx.SetHeader("Vary", "Origin")
			}
		}

		if len(c.AllowMethods) > 0 {
			ctx.SetHeader("Access-Control-Allow-Methods", strings.Join(c.AllowMethods, ", "))
		}
		if len(c.AllowHeaders) > 0 {
			ctx.SetHeader("Access-Control-Allow-Headers", strings.Join(c.AllowHeaders, ", "))
		}
		if len(c.ExposeHeaders) > 0 {
			ctx.SetHeader("Access-Control-Expose-Headers", strings.Join(c.ExposeHeaders, ", "))
		}
		if c.AllowCredentials && !allowAllOrigins {
			ctx.SetHeader("Access-Control-Allow-Credentials", "true")
		}
		if c.MaxAge > 0 {
			ctx.SetHeader("Access-Control-Max-Age", strconv.Itoa(c.MaxAge))
		}

		if isPreflight {
			return ctx.String(http.StatusNoContent, "")
		}

		return next()
	}
}

func contains(list []string, item string) bool {
	for _, s := range list {
		if s == item {
			return true
		}
	}
	return false
}
