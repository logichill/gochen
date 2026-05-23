package nethttp

import (
	"encoding/json"
	stdErrors "errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"os"

	"gochen/errors"
	"gochen/httpx"
	"gochen/httpx/request"
)

// Context 是 `httpx.IContext` 在 net/http 适配层上的实现。
type Context struct {
	request *http.Request
	writer  http.ResponseWriter
	params  map[string]string
	reqCtx  httpx.IRequestContext
	status  int
	// bytesWritten 记录通过 ctx.JSON/String/Data 写出的响应体字节数（best-effort）。
	bytesWritten int64
	aborted      bool
	values       map[string]any

	trustedProxyChecker func(netip.Addr) bool
}

// NewBaseContext 基于原始请求和响应对象创建适配层上下文。
func NewBaseContext(w http.ResponseWriter, r *http.Request) (*Context, error) {
	if w == nil {
		return nil, errors.NewCode(errors.InvalidInput, "w is nil")
	}
	if r == nil {
		return nil, errors.NewCode(errors.InvalidInput, "r is nil")
	}
	reqCtx, err := httpx.NewRequestContext(r.Context())
	if err != nil {
		var appErr *errors.AppError
		if stdErrors.As(err, &appErr) && appErr != nil {
			return nil, appErr.WithContext("component", "httpx.nethttp.context").WithContext("op", "NewBaseContext")
		}
		return nil, errors.Wrap(err, errors.InvalidInput, "invalid request context").
			WithContext("component", "httpx.nethttp.context").
			WithContext("op", "NewBaseContext")
	}

	return &Context{
		request: r,
		writer:  w,
		params:  make(map[string]string),
		reqCtx:  reqCtx,
		status:  http.StatusOK,
		values:  make(map[string]any),
	}, nil
}

func (c *Context) Method() string { return c.request.Method }

func (c *Context) Path() string { return c.request.URL.Path }

func (c *Context) Query(key string) string { return c.request.URL.Query().Get(key) }

func (c *Context) Param(key string) string { return c.params[key] }

func (c *Context) Header(key string) string { return c.request.Header.Get(key) }

// Body 返回请求体的缓存副本，必要时会先读取并缓存原始 body。
func (c *Context) Body() ([]byte, error) {
	return request.ReadBody(c.writer, c.request, c.values, httpContextKeyRequestBodyCache)
}

// BindJSON 解析请求体 JSON 并绑定到目标对象。
func (c *Context) BindJSON(obj any) error {
	return request.BindJSON(c.writer, c.request, c.values, httpContextKeyRequestBodyCache, obj)
}

// ShouldBindJSON 是 BindJSON 的语义别名。
func (c *Context) ShouldBindJSON(obj any) error { return c.BindJSON(obj) }

// BindQuery 把查询参数绑定到目标对象。
func (c *Context) BindQuery(obj any) error {
	return request.BindQuery(obj, c.request.URL.Query())
}

// SetStatus 记录即将写回客户端的 HTTP 状态码。
func (c *Context) SetStatus(code int) { c.status = code }

// SetHeader 设置一项响应头。
func (c *Context) SetHeader(key, value string) { c.writer.Header().Set(key, value) }

// JSON 写入 JSON 响应，并在不允许返回 body 的状态码上主动失败。
func (c *Context) JSON(code int, body httpx.JSONBody) error {
	// fail-fast：如果调用方明确传了 payload（非 nil），说明期望写 body，但状态码不允许。
	// 这里不应提交 header，交由上层错误链路统一返回可观测的错误响应。
	if !isBodyAllowedForStatus(code) && !body.IsNil() {
		return errors.NewCode(errors.InvalidInput, "response status does not allow body").
			WithContext("op", "Context.JSON").
			WithContext("status", code)
	}

	c.SetHeader("Content-Type", "application/json")
	c.SetStatus(code)

	// 1xx/204/205/304 等响应不允许写入 body；此处仅提交 header，避免触发 net/http 的 ErrBodyNotAllowed，
	// 也避免在错误链路中二次写入响应。
	if !isBodyAllowedForStatus(code) {
		c.writer.WriteHeader(c.status)
		c.values[httpContextKeyResponseWritten] = httpx.ValueOf(true)
		return nil
	}

	data, err := json.Marshal(body)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to serialize JSON")
	}
	c.writer.WriteHeader(c.status)
	c.values[httpContextKeyResponseWritten] = httpx.ValueOf(true)
	n, err := c.writer.Write(data)
	if err == nil {
		if n > 0 {
			c.bytesWritten += int64(n)
		}
	}
	return err
}

// String 写入纯文本响应，并避免在 204/304 等状态码下静默丢弃 body。
func (c *Context) String(code int, text string) error {
	// fail-fast：避免 ctx.String(204, "msg") 这类误用变成无声失败。
	if !isBodyAllowedForStatus(code) && text != "" {
		return errors.NewCode(errors.InvalidInput, "response status does not allow body").
			WithContext("op", "Context.String").
			WithContext("status", code).
			WithContext("body_len", len(text))
	}

	c.SetHeader("Content-Type", "text/plain")
	c.SetStatus(code)

	// 1xx/204/205/304 等响应不允许写入 body。
	if !isBodyAllowedForStatus(code) {
		c.writer.WriteHeader(c.status)
		c.values[httpContextKeyResponseWritten] = httpx.ValueOf(true)
		return nil
	}

	c.writer.WriteHeader(c.status)
	c.values[httpContextKeyResponseWritten] = httpx.ValueOf(true)
	data := []byte(text)
	n, err := c.writer.Write(data)
	if err == nil {
		if n > 0 {
			c.bytesWritten += int64(n)
		}
	}
	return err
}

// Data 写入原始字节响应并设置状态码。
func (c *Context) Data(code int, contentType string, data []byte) error {
	// fail-fast：避免误用时静默丢弃 body。
	if !isBodyAllowedForStatus(code) && len(data) > 0 {
		return errors.NewCode(errors.InvalidInput, "response status does not allow body").
			WithContext("op", "Context.Data").
			WithContext("status", code).
			WithContext("body_len", len(data))
	}

	c.SetHeader("Content-Type", contentType)
	c.SetStatus(code)

	// 1xx/204/205/304 等响应不允许写入 body。
	if !isBodyAllowedForStatus(code) {
		c.writer.WriteHeader(c.status)
		c.values[httpContextKeyResponseWritten] = httpx.ValueOf(true)
		return nil
	}

	c.writer.WriteHeader(c.status)
	c.values[httpContextKeyResponseWritten] = httpx.ValueOf(true)
	n, err := c.writer.Write(data)
	if err == nil {
		if n > 0 {
			c.bytesWritten += int64(n)
		}
	}
	return err
}

// isBodyAllowedForStatus 判断该状态码是否允许携带响应体。
func isBodyAllowedForStatus(code int) bool {
	// RFC 9110: 1xx/204/205/304 响应不应包含消息体；net/http 会在写 body 时返回 ErrBodyNotAllowed。
	if code >= 100 && code <= 199 {
		return false
	}
	switch code {
	case http.StatusNoContent, http.StatusResetContent, http.StatusNotModified:
		return false
	default:
		return true
	}
}

// StatusCode 返回当前响应状态码（best-effort）。
func (c *Context) StatusCode() int { return c.status }

func (c *Context) BytesWritten() int64 { return c.bytesWritten }

func (c *Context) RequestContext() httpx.IRequestContext { return c.reqCtx }

// SetContext 替换当前请求绑定的框架级上下文。
func (c *Context) SetContext(ctx httpx.IRequestContext) { c.reqCtx = ctx }

func (c *Context) QueryParams() url.Values { return c.request.URL.Query() }

// Set 在上下文内保存一项临时值，供后续 handler 或 middleware 读取。
func (c *Context) Set(key string, value httpx.ContextValue) { c.values[key] = value }

// Get 读取一项上下文值，并在内部存的是裸值时自动包装为 ContextValue。
func (c *Context) Get(key string) (httpx.ContextValue, bool) {
	v, ok := c.values[key]
	if !ok {
		return httpx.ContextValue{}, false
	}
	if typed, ok := v.(httpx.ContextValue); ok {
		return typed, true
	}
	return httpx.ValueOf(v), true
}

// Required 获取指定 key 的值；当 key 不存在时返回 error（不再 panic）。
func (c *Context) Required(key string) (httpx.ContextValue, error) {
	if key == "" {
		return httpx.ContextValue{}, errors.NewCode(errors.InvalidInput, "key cannot be empty")
	}
	if v, ok := c.values[key]; ok {
		if typed, ok := v.(httpx.ContextValue); ok {
			return typed, nil
		}
		return httpx.ValueOf(v), nil
	}
	return httpx.ContextValue{}, errors.NewCode(errors.Internal, "missing required context value").
		WithContext("key", key)
}

// Abort 标记当前请求为已终止，阻止后续 handler 执行。
func (c *Context) Abort() { c.aborted = true }

// AbortWithStatus 设置状态码并标记当前请求已终止。
func (c *Context) AbortWithStatus(code int) { c.SetStatus(code); c.Abort() }

// AbortWithStatusJSON 写入 JSON 响应后立刻中断后续处理链。
func (c *Context) AbortWithStatusJSON(code int, jsonObj httpx.JSONBody) {
	_ = c.JSON(code, jsonObj)
	c.Abort()
}

func (c *Context) IsAborted() bool { return c.aborted }

// SaveUploadedFile 把上传文件内容复制到目标路径。
func (c *Context) SaveUploadedFile(file httpx.IUploadedFile, dst string) error {
	if dst == "" {
		return errors.NewCode(errors.InvalidInput, "destination path cannot be empty")
	}
	if file == nil {
		return errors.NewCode(errors.InvalidInput, "file cannot be nil")
	}

	src, err := file.Open()
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to open uploaded file")
	}
	defer func() { _ = src.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.Wrap(err, errors.Internal, "failed to create destination file")
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, src); err != nil {
		return errors.Wrap(err, errors.Internal, "failed to save uploaded file")
	}
	return nil
}

// FormFile 读取 multipart 请求中的指定文件字段。
func (c *Context) FormFile(name string) (httpx.IUploadedFile, error) {
	if name == "" {
		return nil, errors.NewCode(errors.InvalidInput, "file field name cannot be empty")
	}
	f, header, err := c.request.FormFile(name)
	if err != nil {
		return nil, errors.Wrap(err, errors.InvalidInput, "failed to read form file")
	}
	_ = f.Close()
	return uploadedFile{header: header}, nil
}

// ClientIP 基于代理信任配置解析客户端真实 IP。
func (c *Context) ClientIP() string {
	return request.ResolveClientIP(c.request, c.trustedProxyChecker)
}

// UserAgent 返回请求头中的 User-Agent。
func (c *Context) UserAgent() string { return c.request.UserAgent() }

// Request 返回底层 `*http.Request`，便于适配层向下游透传。
func (c *Context) Request() *http.Request { return c.request }

// ResponseWriter 返回底层 net/http ResponseWriter。
func (c *Context) ResponseWriter() http.ResponseWriter { return c.writer }

// SetParam 为当前上下文补充一项路由参数。
func (c *Context) SetParam(key, value string) { c.params[key] = value }

type uploadedFile struct {
	header *multipart.FileHeader
}

func (f uploadedFile) Filename() string {
	if f.header == nil {
		return ""
	}
	return f.header.Filename
}

func (f uploadedFile) Size() int64 {
	if f.header == nil {
		return 0
	}
	return f.header.Size
}

// Header 返回上传文件 MIME 头。
func (f uploadedFile) Header() textproto.MIMEHeader {
	if f.header == nil {
		return nil
	}
	return f.header.Header
}

// Open 打开上传文件内容。
func (f uploadedFile) Open() (io.ReadCloser, error) {
	if f.header == nil {
		return nil, errors.NewCode(errors.InvalidInput, "uploaded file header is nil")
	}
	return f.header.Open()
}
