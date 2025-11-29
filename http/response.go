package http

// IResponse 表示统一的 HTTP 响应对象
type IResponse interface{ Send(ctx IHttpContext) error }
