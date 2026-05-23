package crud

import (
	"context"
	"strings"
	"sync"

	"gochen/contextx"
	"gochen/errors"
)

// ITenantResolver 定义应用层解析当前 tenant 的策略。
type ITenantResolver interface {
	ResolveTenantID(ctx context.Context) (string, error)
}

// TenantResolverFunc 允许使用函数直接实现 ITenantResolver。
type TenantResolverFunc func(ctx context.Context) (string, error)

// ResolveTenantID 解析当前 tenant。
func (f TenantResolverFunc) ResolveTenantID(ctx context.Context) (string, error) {
	return f(ctx)
}

var (
	tenantResolverMu sync.RWMutex
	tenantResolver   ITenantResolver = TenantResolverFunc(defaultTenantResolver)
)

func defaultTenantResolver(ctx context.Context) (string, error) {
	tenantID := strings.TrimSpace(contextx.TenantID(ctx))
	if tenantID == "" {
		return "", errors.NewCode(errors.InvalidInput, "tenant ID is required in context")
	}
	return tenantID, nil
}

// SetTenantResolver 设置全局 tenant 解析策略；传 nil 时恢复默认策略。
func SetTenantResolver(resolver ITenantResolver) {
	tenantResolverMu.Lock()
	defer tenantResolverMu.Unlock()
	if resolver == nil {
		tenantResolver = TenantResolverFunc(defaultTenantResolver)
		return
	}
	tenantResolver = resolver
}

// ResolveTenantID 使用当前全局策略解析 tenant。
func ResolveTenantID(ctx context.Context) (string, error) {
	tenantResolverMu.RLock()
	resolver := tenantResolver
	tenantResolverMu.RUnlock()
	return resolver.ResolveTenantID(ctx)
}
