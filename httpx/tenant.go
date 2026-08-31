package httpx

import (
	"strings"

	"gochen/contextx"
	"gochen/errors"
)

const (
	// HeaderTenantID 表示租户 ID 的 HTTP Header 名称。
	HeaderTenantID = "X-Tenant-ID"
)

// TenantResolver 从抽象 HTTP 上下文中解析当前 tenant。
//
// 说明：
// - 公网 API 应优先从已鉴权 principal/session/token 推导 tenant；
// - 只有在可信网关已经完成身份校验并重写请求头时，才使用 TrustedTenantHeaderResolver。
type TenantResolver func(ctx IContext) (string, error)

// TrustedTenantHeaderResolver 从可信边界传入的 X-Tenant-ID 解析 tenant。
func TrustedTenantHeaderResolver(ctx IContext) (string, error) {
	if ctx == nil {
		return "", nil
	}
	rawTenantID := ctx.Header(HeaderTenantID)
	tenantID := SanitizeIdentifierFromHeader(rawTenantID, 128)
	if strings.TrimSpace(rawTenantID) != "" && tenantID == "" {
		return "", errors.NewCode(errors.InvalidInput, "invalid tenant id")
	}
	return tenantID, nil
}

// TenantContextMiddleware 返回 `gochenhttp.Middleware` 形态的租户提取中间件。
//
// resolver 负责解析可信 tenant，middleware 只负责写入 Context；resolver 为 nil 时 fail-closed。
func TenantContextMiddleware(resolver TenantResolver) Middleware {
	return func(ctx IContext, next func() error) error {
		if ctx == nil {
			return errors.NewCode(errors.InvalidInput, "http context is nil")
		}
		if resolver == nil {
			return errors.NewCode(errors.Unauthorized, "tenant resolver is required")
		}
		tenantID, err := resolver(ctx)
		if err != nil {
			return err
		}

		reqCtx := ctx.RequestContext()
		if reqCtx == nil {
			return errors.NewCode(errors.Internal, "request context is nil")
		}
		nextCtx, err := contextx.WithTenantID(reqCtx, tenantID)
		if err != nil {
			return errors.NewCode(errors.Internal, "set tenant id failed")
		}
		if wrapped, ok := nextCtx.(IRequestContext); ok {
			ctx.SetContext(wrapped)
		} else {
			ctx.SetContext(reqCtx.WithContext(nextCtx))
		}
		if tenantID != "" {
			ctx.SetHeader(HeaderTenantID, tenantID)
		}
		return next()
	}
}
