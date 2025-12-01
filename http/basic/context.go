package basic

import (
	"encoding/json"
	"net/http"
	"net/url"

	"gochen/errors"
	httpx "gochen/http"
)

type HttpContext struct {
	request *http.Request
	writer  http.ResponseWriter
	params  map[string]string
	query   map[string]string
	headers map[string]string
	reqCtx  httpx.IRequestContext
	status  int
	aborted bool
	values  map[string]any
}

func NewBaseHttpContext(w http.ResponseWriter, r *http.Request) *HttpContext {
	return &HttpContext{
		request: r,
		writer:  w,
		params:  make(map[string]string),
		query:   make(map[string]string),
		headers: make(map[string]string),
		reqCtx:  NewRequestContext(r.Context()),
		status:  http.StatusOK,
		values:  make(map[string]any),
	}
}

// implement httpx.IHttpContext
func (c *HttpContext) GetMethod() string           { return c.request.Method }
func (c *HttpContext) GetPath() string             { return c.request.URL.Path }
func (c *HttpContext) GetQuery(key string) string  { return c.request.URL.Query().Get(key) }
func (c *HttpContext) GetParam(key string) string  { return c.params[key] }
func (c *HttpContext) GetHeader(key string) string { return c.request.Header.Get(key) }
func (c *HttpContext) GetBody() ([]byte, error) {
	defer c.request.Body.Close()
	buf := make([]byte, c.request.ContentLength)
	_, err := c.request.Body.Read(buf)
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeInternal, "failed to read request body")
	}
	return buf, nil
}
func (c *HttpContext) BindJSON(obj any) error {
	body, err := c.GetBody()
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, obj); err != nil {
		return errors.WrapError(err, errors.ErrCodeInvalidInput, "failed to parse JSON")
	}
	return nil
}
func (c *HttpContext) ShouldBindJSON(obj any) error { return c.BindJSON(obj) }
func (c *HttpContext) BindQuery(obj any) error      { return nil }
func (c *HttpContext) SetStatus(code int)           { c.status = code }
func (c *HttpContext) SetHeader(key, value string)  { c.writer.Header().Set(key, value) }
func (c *HttpContext) JSON(code int, obj any) error {
	c.SetHeader("Content-Type", "application/json")
	c.SetStatus(code)
	data, err := json.Marshal(obj)
	if err != nil {
		return errors.WrapError(err, errors.ErrCodeInternal, "failed to serialize JSON")
	}
	c.writer.WriteHeader(c.status)
	_, err = c.writer.Write(data)
	if err == nil {
		c.values["response_written"] = true
	}
	return err
}
func (c *HttpContext) String(code int, text string) error {
	c.SetHeader("Content-Type", "text/plain")
	c.SetStatus(code)
	c.writer.WriteHeader(c.status)
	_, err := c.writer.Write([]byte(text))
	if err == nil {
		c.values["response_written"] = true
	}
	return err
}
func (c *HttpContext) Data(code int, contentType string, data []byte) error {
	c.SetHeader("Content-Type", contentType)
	c.SetStatus(code)
	c.writer.WriteHeader(c.status)
	_, err := c.writer.Write(data)
	if err == nil {
		c.values["response_written"] = true
	}
	return err
}
func (c *HttpContext) GetContext() httpx.IRequestContext    { return c.reqCtx }
func (c *HttpContext) SetContext(ctx httpx.IRequestContext) { c.reqCtx = ctx }
func (c *HttpContext) GetQueryParams() url.Values           { return c.request.URL.Query() }
func (c *HttpContext) Set(key string, value any)            { c.values[key] = value }
func (c *HttpContext) Get(key string) (any, bool)           { v, ok := c.values[key]; return v, ok }
func (c *HttpContext) MustGet(key string) any {
	if v, ok := c.values[key]; ok {
		return v
	}
	panic("Key \"" + key + "\" does not exist")
}
func (c *HttpContext) Abort()                   { c.aborted = true }
func (c *HttpContext) AbortWithStatus(code int) { c.SetStatus(code); c.Abort() }
func (c *HttpContext) AbortWithStatusJSON(code int, jsonObj any) {
	_ = c.JSON(code, jsonObj)
	c.Abort()
}
func (c *HttpContext) IsAborted() bool                             { return c.aborted }
func (c *HttpContext) SaveUploadedFile(file any, dst string) error { return nil }
func (c *HttpContext) FormFile(name string) (any, error)           { return nil, nil }
func (c *HttpContext) ClientIP() string                            { return c.request.RemoteAddr }
func (c *HttpContext) UserAgent() string                           { return c.request.UserAgent() }
func (c *HttpContext) GetRequest() *http.Request                   { return c.request }
func (c *HttpContext) GetRaw() any {
	return map[string]any{"request": c.request, "response": c.writer}
}
func (c *HttpContext) SetParam(key, value string) { c.params[key] = value }
