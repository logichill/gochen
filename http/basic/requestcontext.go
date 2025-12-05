package basic

import (
	"context"
	"time"

	httpx "gochen/http"
)

type RequestContext struct{ context.Context }

func NewRequestContext(ctx context.Context) httpx.IRequestContext {
	if ctx == nil {
		ctx = context.TODO()
	}
	return &RequestContext{Context: ctx}
}

func NewRequestContextWithValues(ctx context.Context, values map[string]any) httpx.IRequestContext {
	if ctx == nil {
		ctx = context.TODO()
	}
	for k, v := range values {
		ctx = context.WithValue(ctx, k, v)
	}
	return &RequestContext{Context: ctx}
}

// IRequestContext methods
func (r *RequestContext) GetTraceID() string { v, _ := r.Value(httpx.TraceIDKey).(string); return v }
func (r *RequestContext) GetUserID() int64 {
	if v := r.Value(httpx.UserIDKey); v != nil {
		switch t := v.(type) {
		case int64:
			return t
		case int:
			return int64(t)
		}
	}
	return 0
}
func (r *RequestContext) GetRequestID() string {
	v, _ := r.Value(httpx.RequestIDKey).(string)
	return v
}
func (r *RequestContext) GetTenantID() string { v, _ := r.Value(httpx.TenantIDKey).(string); return v }
func (r *RequestContext) GetSessionID() string {
	v, _ := r.Value(httpx.SessionIDKey).(string)
	return v
}
func (r *RequestContext) GetIPAddress() string {
	v, _ := r.Value(httpx.IPAddressKey).(string)
	return v
}
func (r *RequestContext) GetUserAgent() string {
	v, _ := r.Value(httpx.UserAgentKey).(string)
	return v
}
func (r *RequestContext) GetCorrelationID() string {
	v, _ := r.Value(httpx.CorrelationIDKey).(string)
	return v
}

func (r *RequestContext) WithValue(key any, value any) httpx.IRequestContext {
	return &RequestContext{Context: context.WithValue(r.Context, key, value)}
}
func (r *RequestContext) WithTimeout(timeout time.Duration) (httpx.IRequestContext, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(r.Context, timeout)
	return &RequestContext{Context: ctx}, cancel
}
func (r *RequestContext) WithCancel() (httpx.IRequestContext, context.CancelFunc) {
	ctx, cancel := context.WithCancel(r.Context)
	return &RequestContext{Context: ctx}, cancel
}
func (r *RequestContext) WithDeadline(deadline time.Time) (httpx.IRequestContext, context.CancelFunc) {
	ctx, cancel := context.WithDeadline(r.Context, deadline)
	return &RequestContext{Context: ctx}, cancel
}
// Clone 创建基于同一底层 context 的新 RequestContext。
// 注意：Clone 不复制 context 中的值或取消状态，语义等价于对原始 context 的浅包装。
func (r *RequestContext) Clone() httpx.IRequestContext { return &RequestContext{Context: r.Context} }

func WithTraceID(ctx httpx.IRequestContext, traceID string) httpx.IRequestContext {
	return ctx.WithValue(httpx.TraceIDKey, traceID)
}
func WithUserID(ctx httpx.IRequestContext, userID int64) httpx.IRequestContext {
	return ctx.WithValue(httpx.UserIDKey, userID)
}
func WithRequestID(ctx httpx.IRequestContext, requestID string) httpx.IRequestContext {
	return ctx.WithValue(httpx.RequestIDKey, requestID)
}
func WithTenantID(ctx httpx.IRequestContext, tenantID string) httpx.IRequestContext {
	return ctx.WithValue(httpx.TenantIDKey, tenantID)
}
func WithSessionID(ctx httpx.IRequestContext, sessionID string) httpx.IRequestContext {
	return ctx.WithValue(httpx.SessionIDKey, sessionID)
}
func WithIPAddress(ctx httpx.IRequestContext, ip string) httpx.IRequestContext {
	return ctx.WithValue(httpx.IPAddressKey, ip)
}
func WithUserAgent(ctx httpx.IRequestContext, ua string) httpx.IRequestContext {
	return ctx.WithValue(httpx.UserAgentKey, ua)
}
func WithCorrelationID(ctx httpx.IRequestContext, id string) httpx.IRequestContext {
	return ctx.WithValue(httpx.CorrelationIDKey, id)
}
