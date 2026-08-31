package httpx

// IResponseWriter 响应写入接口 - 只负责写入响应。
type IResponseWriter interface {
	// 状态和头部
	SetStatus(code int)
	SetHeader(key, value string)

	// 响应内容
	JSON(code int, body JSONBody) error
	String(code int, text string) error
	Data(code int, contentType string, data []byte) error
}

// IContextStorage 上下文存储接口 - 只负责键值存储。
type IContextStorage interface {
	Set(key string, value ContextValue)
	Get(key string) (ContextValue, bool)
	Required(key string) (ContextValue, error)
}

// IFlowControl 流程控制接口 - 只负责请求流程控制。
type IFlowControl interface {
	Abort()
	AbortWithStatus(code int)
	AbortWithStatusJSON(code int, jsonObj JSONBody)
	IsAborted() bool
}

// IFileHandler 文件处理接口 - 只负责文件操作。
//
// 约定：
//   - 公共接口统一使用 IUploadedFile，避免把具体 Web 框架文件对象直接暴露到核心契约；
//   - 具体适配器负责把底层文件对象转换成 IUploadedFile，并在实现内处理保存细节；
//   - 调用方应通过 IUploadedFile 提供的稳定字段与能力访问上传文件，而不是假定具体框架类型。
type IFileHandler interface {
	FormFile(name string) (IUploadedFile, error)
	SaveUploadedFile(file IUploadedFile, dst string) error
}

// IContext 组合接口 - 通过组合而非继承。
type IContext interface {
	IRequestReader
	IRequestBinder
	IResponseWriter
	IContextStorage
	IFlowControl

	// 上下文相关（保留，因为这是核心功能）
	RequestContext() IRequestContext
	SetContext(ctx IRequestContext)
}

// Handler 处理器函数类型。
type Handler func(ctx IContext) error
