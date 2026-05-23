package httpx

import (
	"context"
	"time"

	"gochen/contextx"
)

// RequestContext 是协议无关的 HTTP 请求上下文实现。
//
// 它只承载 context 派生能力，不绑定具体 HTTP server 适配器。
type RequestContext struct{ context.Context }

// NewRequestContext 创建请求上下文。
func NewRequestContext(ctx context.Context) (IRequestContext, error) {
	ctx, err := contextx.Ensure(ctx)
	if err != nil {
		return nil, err
	}
	return &RequestContext{Context: ctx}, nil
}

// NewRequestContextWithValues 创建请求上下文并附加一组 context value。
func NewRequestContextWithValues(ctx context.Context, values map[any]any) (IRequestContext, error) {
	ctx, err := contextx.Ensure(ctx)
	if err != nil {
		return nil, err
	}
	for k, v := range values {
		ctx = context.WithValue(ctx, k, v)
	}
	return &RequestContext{Context: ctx}, nil
}

// WithContext 返回携带指定基础 context 的副本。
func (r *RequestContext) WithContext(ctx context.Context) IRequestContext {
	ensured, err := contextx.Ensure(ctx)
	if err != nil {
		ensured = contextx.Background()
	}
	return &RequestContext{Context: ensured}
}

// WithValue 返回附加一项 context value 的副本。
func (r *RequestContext) WithValue(key any, value any) IRequestContext {
	return &RequestContext{Context: context.WithValue(r.Context, key, value)}
}

// WithTimeout 返回带超时控制的副本。
func (r *RequestContext) WithTimeout(timeout time.Duration) (IRequestContext, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(r.Context, timeout)
	return &RequestContext{Context: ctx}, cancel
}

// WithCancel 返回带取消控制的副本。
func (r *RequestContext) WithCancel() (IRequestContext, context.CancelFunc) {
	ctx, cancel := context.WithCancel(r.Context)
	return &RequestContext{Context: ctx}, cancel
}

// WithDeadline 返回带截止时间的副本。
func (r *RequestContext) WithDeadline(deadline time.Time) (IRequestContext, context.CancelFunc) {
	ctx, cancel := context.WithDeadline(r.Context, deadline)
	return &RequestContext{Context: ctx}, cancel
}

// Clone 复制当前请求上下文。
func (r *RequestContext) Clone() IRequestContext {
	return &RequestContext{Context: r.Context}
}
