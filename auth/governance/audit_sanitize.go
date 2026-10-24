package governance

import (
	"context"

	"gochen/errors"
)

// IResourceLogSanitizer 对写入审计日志的资源做脱敏。
//
// 默认策略（见 DefaultSanitizeLoggedResource）会剔除 TenantID / OwnerID
// 这类可用于横向关联的标识，避免审计表成为二次泄露面。
// 需要保留完整字段的合规场景可注入自定义实现。
type IResourceLogSanitizer interface {
	SanitizeResourceForLog(ctx context.Context, resource AuthzLoggedResource) AuthzLoggedResource
}

// ResourceLogSanitizerFunc 允许用函数直接实现 IResourceLogSanitizer。
type ResourceLogSanitizerFunc func(ctx context.Context, resource AuthzLoggedResource) AuthzLoggedResource

// SanitizeResourceForLog 对单个资源执行脱敏。
func (f ResourceLogSanitizerFunc) SanitizeResourceForLog(ctx context.Context, resource AuthzLoggedResource) AuthzLoggedResource {
	if f == nil {
		return DefaultSanitizeLoggedResource(resource)
	}
	return f(ctx, resource)
}

type resourceLogSanitizerContextKey struct{}

// WithResourceLogSanitizer 将资源脱敏器绑定到 context。
func WithResourceLogSanitizer(ctx context.Context, sanitizer IResourceLogSanitizer) (context.Context, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if sanitizer == nil {
		return ctx, nil
	}
	return context.WithValue(ctx, resourceLogSanitizerContextKey{}, sanitizer), nil
}

// ResourceLogSanitizerFromContext 从 context 读取资源脱敏器。
func ResourceLogSanitizerFromContext(ctx context.Context) (IResourceLogSanitizer, bool) {
	if ctx == nil {
		return nil, false
	}
	sanitizer, ok := ctx.Value(resourceLogSanitizerContextKey{}).(IResourceLogSanitizer)
	return sanitizer, ok && sanitizer != nil
}

// DefaultSanitizeLoggedResource 是默认脱敏策略：只保留授权判定必需的结构化事实。
func DefaultSanitizeLoggedResource(resource AuthzLoggedResource) AuthzLoggedResource {
	resource.TenantID = ""
	resource.OwnerID = ""
	return resource
}

// SanitizeLoggedResources 依据 ctx 中的脱敏器（或默认策略）处理资源列表。
func SanitizeLoggedResources(ctx context.Context, resources []AuthzLoggedResource) []AuthzLoggedResource {
	if len(resources) == 0 {
		return nil
	}
	sanitizer, ok := ResourceLogSanitizerFromContext(ctx)
	out := make([]AuthzLoggedResource, 0, len(resources))
	for _, resource := range resources {
		if ok {
			out = append(out, sanitizer.SanitizeResourceForLog(ctx, resource))
			continue
		}
		out = append(out, DefaultSanitizeLoggedResource(resource))
	}
	return out
}
