# HTTP Abstraction Layer - HTTP 抽象层

HTTP 抽象层提供框架无关的 HTTP 接口，支持 Gin、Fiber、Echo 等多种 Web 框架。

## 概述

HTTP 抽象层通过定义统一的接口，实现了与具体 Web 框架的解耦，具有以下优势：
- **框架无关** - 业务逻辑不依赖具体 Web 框架
- **易于测试** - 可以 Mock HTTP 上下文进行单元测试
- **灵活切换** - 可以轻松更换底层 Web 框架
- **统一API** - 提供一致的 HTTP 操作接口

## 核心接口

### IHttpContext

HTTP 上下文接口，封装请求和响应操作。

```go
// IHttpContext HTTP 上下文接口
type IHttpContext interface {
    // Request 获取 HTTP 请求
    Request() IHttpRequest
    
    // Response 获取 HTTP 响应
    Response() IHttpResponse
    
    // Param 获取路径参数
    Param(key string) string
    
    // Query 获取查询参数
    Query(key string) string
    
    // Header 获取请求头
    Header(key string) string
    
    // SetHeader 设置响应头
    SetHeader(key, value string)
    
    // Bind 绑定请求体到结构体
    Bind(v any) error
    
    // JSON 返回 JSON 响应
    JSON(code int, data any) error
    
    // String 返回字符串响应
    String(code int, format string, values ...any) error
    
    // GetContext 获取 context.Context
    GetContext() context.Context
    
    // SetContext 设置 context.Context
    SetContext(ctx context.Context)
}
```

### IHttpRequest

HTTP 请求接口。

```go
// IHttpRequest HTTP 请求接口
type IHttpRequest interface {
    // Method 获取请求方法
    Method() string
    
    // Path 获取请求路径
    Path() string
    
    // URL 获取完整 URL
    URL() string
    
    // Header 获取请求头
    Header(key string) string
    
    // Query 获取查询参数
    Query(key string) string
    
    // Param 获取路径参数
    Param(key string) string
    
    // Body 获取请求体
    Body() ([]byte, error)
    
    // BindJSON 绑定 JSON 请求体
    BindJSON(v any) error
}
```

### IHttpResponse

HTTP 响应接口。

```go
// IHttpResponse HTTP 响应接口
type IHttpResponse interface {
    // Status 设置响应状态码
    Status(code int) IHttpResponse
    
    // Header 设置响应头
    Header(key, value string) IHttpResponse
    
    // JSON 返回 JSON 响应
    JSON(data any) error
    
    // String 返回字符串响应
    String(text string) error
    
    // Bytes 返回字节响应
    Bytes(data []byte) error
}
```

### IHttpServer

HTTP 服务器接口。

```go
// IHttpServer HTTP 服务器接口
type IHttpServer interface {
    // Start 启动服务器
    Start(addr string) error
    
    // Stop 停止服务器
    Stop(ctx context.Context) error
    
    // GET 注册 GET 路由
    GET(path string, handler func(IHttpContext) error)
    
    // POST 注册 POST 路由
    POST(path string, handler func(IHttpContext) error)
    
    // PUT 注册 PUT 路由
    PUT(path string, handler func(IHttpContext) error)
    
    // DELETE 注册 DELETE 路由
    DELETE(path string, handler func(IHttpContext) error)
    
    // Group 创建路由组
    Group(prefix string) IRouter
    
    // Use 注册全局中间件
    Use(middleware ...func(IHttpContext) error)
}
```

## 使用示例

### 基础用法

```go
package main

import (
    httpx "gochen/http"
    basic "gochen/http/basic"
)

func main() {
    // 创建 HTTP 服务器
    server := basic.NewHTTPServer(&httpx.WebConfig{
        Host: "0.0.0.0",
        Port: 8080,
    })

    // 注册路由
    server.GET("/hello", func(ctx httpx.IHttpContext) error {
        return ctx.JSON(200, map[string]string{
            "message": "Hello, World!",
        })
    })

    server.POST("/users", func(ctx httpx.IHttpContext) error {
        var user User
        if err := ctx.BindJSON(&user); err != nil {
            return ctx.JSON(400, map[string]string{
                "error": "invalid request",
            })
        }

        // 处理业务逻辑
        // ...

        return ctx.JSON(201, user)
    })

    // 启动服务器
    if err := server.Start(":8080"); err != nil {
        panic(err)
    }
}
```

### 路由组

```go
// 创建 API v1 路由组
v1 := server.Group("/api/v1")

// 注册路由
v1.GET("/users", listUsers)
v1.GET("/users/:id", getUser)
v1.POST("/users", createUser)
v1.PUT("/users/:id", updateUser)
v1.DELETE("/users/:id", deleteUser)

// 处理函数
func getUser(ctx httpx.IHttpContext) error {
    id := ctx.Param("id")
    
    // 查询用户
    user, err := userService.GetByID(ctx.GetContext(), id)
    if err != nil {
        return ctx.JSON(404, map[string]string{
            "error": "user not found",
        })
    }
    
    return ctx.JSON(200, user)
}
```

### 中间件

```go
// 日志中间件
func loggingMiddleware(ctx httpx.IHttpContext, next func() error) error {
    start := time.Now()
    
    // 记录请求信息
    log.Printf("Request: %s %s", ctx.Request().Method(), ctx.Request().Path())
    
    // 执行处理
    err := next()
    
    // 记录响应信息
    duration := time.Since(start)
    log.Printf("Response: %d (took %v)", ctx.Response().Status, duration)
    
    return err
}

// 认证中间件
func authMiddleware(ctx httpx.IHttpContext, next func() error) error {
    token := ctx.Header("Authorization")
    if token == "" {
        return ctx.JSON(401, map[string]string{
            "error": "unauthorized",
        })
    }
    
    // 验证 token
    user, err := validateToken(token)
    if err != nil {
        return ctx.JSON(401, map[string]string{
            "error": "invalid token",
        })
    }
    
    // 将用户信息存入上下文
    newCtx := context.WithValue(ctx.GetContext(), "user", user)
    ctx.SetContext(newCtx)
    
    return next()
}

// 注册中间件
server.Use(loggingMiddleware)
server.Use(authMiddleware)
```

### 参数绑定

```go
func createUser(ctx httpx.IHttpContext) error {
    // 定义请求结构
    type CreateUserRequest struct {
        Name  string `json:"name" validate:"required"`
        Email string `json:"email" validate:"required,email"`
    }
    
    // 绑定请求体
    var req CreateUserRequest
    if err := ctx.Bind(&req); err != nil {
        return ctx.JSON(400, map[string]string{
            "error": "invalid request body",
        })
    }
    
    // 验证
    if err := validator.Validate(req); err != nil {
        return ctx.JSON(400, map[string]string{
            "error": err.Error(),
        })
    }
    
    // 创建用户
    user := &User{
        Name:  req.Name,
        Email: req.Email,
    }
    if err := userService.Create(ctx.GetContext(), user); err != nil {
        return ctx.JSON(500, map[string]string{
            "error": "failed to create user",
        })
    }
    
    return ctx.JSON(201, user)
}
```

### 查询参数

```go
func listUsers(ctx httpx.IHttpContext) error {
    // 获取分页参数
    page := ctx.Query("page")
    if page == "" {
        page = "1"
    }
    
    size := ctx.Query("size")
    if size == "" {
        size = "10"
    }
    
    // 获取过滤参数
    status := ctx.Query("status")
    
    // 查询用户
    users, total, err := userService.List(ctx.GetContext(), &QueryOptions{
        Page:   parseInt(page),
        Size:   parseInt(size),
        Filter: map[string]any{"status": status},
    })
    if err != nil {
        return ctx.JSON(500, map[string]string{
            "error": "failed to list users",
        })
    }
    
    return ctx.JSON(200, map[string]any{
        "data":  users,
        "total": total,
        "page":  page,
        "size":  size,
    })
}
```

## Gin 适配器示例

如果你的项目使用 Gin，可以通过适配器转换：

```go
package adapter

import (
    "gochen/http"
    "github.com/gin-gonic/gin"
)

// GinAdapter Gin 适配器
type GinAdapter struct {
    *gin.Context
}

func (a *GinAdapter) Param(key string) string {
    return a.Context.Param(key)
}

func (a *GinAdapter) Query(key string) string {
    return a.Context.Query(key)
}

func (a *GinAdapter) Bind(v any) error {
    return a.Context.ShouldBindJSON(v)
}

func (a *GinAdapter) JSON(code int, data any) error {
    a.Context.JSON(code, data)
    return nil
}

// WrapGinHandler 将 httpx 处理器包装为 Gin 处理器
func WrapGinHandler(handler func(httpx.IHttpContext) error) gin.HandlerFunc {
    return func(c *gin.Context) {
        ctx := &GinAdapter{Context: c}
        if err := handler(ctx); err != nil {
            c.JSON(500, gin.H{"error": err.Error()})
        }
    }
}
```

## 最佳实践

### 1. 统一响应格式

```go
type Response struct {
    Code    int         `json:"code"`
    Message string      `json:"message"`
    Data    any `json:"data,omitempty"`
}

func Success(ctx httpx.IHttpContext, data any) error {
    return ctx.JSON(200, Response{
        Code:    0,
        Message: "success",
        Data:    data,
    })
}

func Error(ctx httpx.IHttpContext, code int, message string) error {
    return ctx.JSON(code, Response{
        Code:    code,
        Message: message,
    })
}
```

### 2. 错误处理

```go
func handleError(ctx httpx.IHttpContext, err error) error {
    switch {
    case errors.Is(err, repository.ErrNotFound):
        return ctx.JSON(404, map[string]string{
            "error": "resource not found",
        })
    case errors.Is(err, validation.ErrInvalid):
        return ctx.JSON(400, map[string]string{
            "error": "invalid request",
        })
    default:
        log.Error("internal error", "error", err)
        return ctx.JSON(500, map[string]string{
            "error": "internal server error",
        })
    }
}
```

### 3. 上下文传递

```go
func getUserFromContext(ctx httpx.IHttpContext) (*User, error) {
    user, ok := ctx.GetContext().Value("user").(*User)
    if !ok {
        return nil, errors.New("user not found in context")
    }
    return user, nil
}

func requireAuth(ctx httpx.IHttpContext, next func() error) error {
    user, err := getUserFromContext(ctx)
    if err != nil {
        return ctx.JSON(401, map[string]string{
            "error": "unauthorized",
        })
    }
    
    // 继续处理
    return next()
}
```

## 相关文档

- [RESTful API 构建器](../app/api/README.md) - 自动生成 RESTful API
- [应用服务层](../app/README.md) - 应用服务接口
- [示例代码](../examples/README.md) - 完整使用示例

## 许可证

MIT License
