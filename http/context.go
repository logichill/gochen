package http

// IResponseWriter 响应写入接口 - 只负责写入响应
type IResponseWriter interface {
	// 状态和头部
	SetStatus(code int)
	SetHeader(key, value string)

	// 响应内容
	JSON(code int, obj any) error
	String(code int, text string) error
	Data(code int, contentType string, data []byte) error
}

// IContextStorage 上下文存储接口 - 只负责键值存储
type IContextStorage interface {
	Set(key string, value any)
	Get(key string) (any, bool)
	MustGet(key string) any
}

// IFlowControl 流程控制接口 - 只负责请求流程控制
type IFlowControl interface {
	Abort()
	AbortWithStatus(code int)
	AbortWithStatusJSON(code int, jsonObj any)
	IsAborted() bool
}

// IFileHandler 文件处理接口 - 只负责文件操作
type IFileHandler interface {
	FormFile(name string) (any, error)
	SaveUploadedFile(file any, dst string) error
}

// IHttpContext 组合接口 - 通过组合而非继承
type IHttpContext interface {
	IRequestReader
	IRequestBinder
	IResponseWriter
	IContextStorage
	IFlowControl

	// 上下文相关（保留，因为这是核心功能）
	GetContext() IRequestContext
	SetContext(ctx IRequestContext)

	// 原始对象访问（用于特殊情况）
	GetRaw() any
}

// IHttpContextLite 轻量级接口 - 只包含最常用的方法
type IHttpContextLite interface {
	// 最常用的请求方法
	GetParam(key string) string
	GetQuery(key string) string
	BindJSON(obj any) error

	// 最常用的响应方法
	JSON(code int, obj any) error
	String(code int, text string) error

	// 基础流程控制
	Abort()
	IsAborted() bool
}

// HttpHandler 处理器函数类型
type HttpHandler func(ctx IHttpContext) error

// HttpHandlerLite 轻量级处理器函数类型
type HttpHandlerLite func(ctx IHttpContextLite) error
