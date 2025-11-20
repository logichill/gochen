# RESTful API 路由构建器

这是一个强大的 RESTful API 路由构建器，旨在消除业务系统中的模板代码，提供健壮、可配置、可扩展的 API 构建能力。

## 特性

- ✅ **零模板代码**：自动生成完整的 CRUD API 端点
- ✅ **类型安全**：基于 Go 泛型的类型安全实现
- ✅ **配置驱动**：灵活的配置选项满足不同业务需求
- ✅ **中间件支持**：完整的中间件生态
- ✅ **批量操作**：支持高性��的批量 CRUD 操作
- ✅ **高级查询**：分页、排序、过滤、字段选择
- ✅ **错误处理**：统一的错误处理机制
- ✅ **CORS 支持**：内置 CORS 配置
- ✅ **验证集成**：可插拔的验证器支持

## 快速开始

### 基本使用

```go
// 定义实体
type User struct {
    ID    int64  `json:"id"`
    Name  string `json:"name" validate:"required"`
    Email string `json:"email" validate:"email"`
}

func (u *User) GetID() int64 { return u.ID }
func (u *User) SetID(id int64) { u.ID = id }
func (u *User) Validate() error { return nil }

// 创建服务和路由
userService := app.NewApplication(userRepository, validator, nil)

// 自动注册所有 CRUD API
err := restful.RegisterRESTfulAPI(router, userService, validator)
```

### 高级配置

```go
restful.NewRestfulBuilder(userService, validator).
    Route(func(config *restful.RouteConfig) {
        config.EnableBatch = true
        config.MaxPageSize = 500
        config.DefaultPageSize = 20
        config.BasePath = "/api/v1/users"
        config.ErrorHandler = customErrorHandler
        config.ResponseWrapper = customResponseWrapper
    }).
    Service(func(config *app.ServiceConfig) {
        config.AutoValidate = true
        config.SoftDelete = true
        config.EnableAudit = true
        config.MaxBatchSize = 100
    }).
    Middleware(authMiddleware, loggingMiddleware).
    Build(router)
```

## 便捷选项

```go
// 使用预定义选项
restful.RegisterRESTfulAPI(router, userService, validator,
    restful.WithBatchOperations(500),
    restful.WithPagination(20, 200),
    restful.WithCORS([]string{"*"}),
    restful.WithAuth(authMiddleware),
    restful.WithLogging(loggingMiddleware),
)
```

## 生成的 API 端点

### 基础 CRUD
- `GET /users` - 获取用户列表
- `GET /users/:id` - 获取单个用户
- `POST /users` - 创建用户
- `PUT /users/:id` - 更新用户
- `DELETE /users/:id` - 删除用户

### 批量操作（可选）
- `POST /users/batch` - 批量创建用户
- `PUT /users/batch` - 批量更新用户
- `DELETE /users/batch` - 批量删除用户

### 查询参数（Filters & Sorts）

当仓储实现了 `IQueryableRepository`（实体使用 `CrudBase`，读模型使用 `CrudViewBase`）时，REST 构建器将自动使用统一查询：

- 分页：`page`、`size`（或 `page_size`）
- 排序：`sorts=field:asc|desc`（当前服务层会转换为单字段排序）
- 过滤（字段名 + 操作符后缀）：
  - 精确：`field=value`
  - 模糊：`field_like=value`
  - 比较：`field_gt/gte/lt/lte=value`
  - 不等：`field_ne=value`
  - 集合：`field_in=a,b,c`、`field_not_in=a,b,c`
- 字段选择：`fields=id,name,...`

### 示例请求

```bash
# 基础查询
GET /users

# 分页查询
GET /users?page=2&size=20

# 排序查询
GET /users?sort=name&order=desc

# 过滤查询
GET /users?name=John&status=active

# 复杂查询（Filters 与 Sorts）
GET /users?page=1&size=10&sorts=created_at:desc&status=active&name_like=John&fields=id,name,email

## 管理端示例

### 商品管理（/api/v1/products/admin）

```
GET /api/v1/products/admin?status=on_sale&points_gte=100&points_lte=500&sorts=sort_order:desc&page=1&size=20
```

### 订单管理（/api/v1/orders/admin）

```
GET /api/v1/orders/admin?status=completed&user_id=10001&total_points_gte=100&sorts=created_at:desc&page=1&size=20
```

# 批量创建
POST /users/batch
[
  {"name": "User 1", "email": "user1@example.com"},
  {"name": "User 2", "email": "user2@example.com"}
]
```

## 配置选项

### RouteConfig

```go
type RouteConfig struct {
    BasePath         string                    // 基础路径
    EnableBatch      bool                      // 启用批量操作
    EnablePagination bool                      // 启用分页
    Validator        func(any) error   // 自定义验证器
    ErrorHandler     func(error) IResponse     // 错误处理器
    MaxPageSize      int                       // 最大分页大小
    DefaultPageSize  int                       // 默认分页大小
    MaxBodySize      int64                     // 请求体大小限制
    AllowedMethods   []string                  // 允许的 HTTP 方法
    CORS             *CORSConfig               // CORS 配置
    Middlewares      []Middleware              // 中间件
    ResponseWrapper  func(any) any // 响应包装器
}
```

> **提示**：自定义 `ErrorHandler` 必须返回非空的 `IResponse`，否则路由器会回退到默认错误处理逻辑。

### ServiceConfig

```go
type ServiceConfig struct {
    AutoValidate    bool  // 自动验证
    AutoTimestamp   bool  // 自动时间戳
    SoftDelete      bool  // 软删除
    AuditFields     bool  // 审计字段
    MaxBatchSize    int   // 最大批量操作数量
    EnableCache     bool  // 启用缓存
    CacheTTL        int   // 缓存过期时间
    EnableAudit     bool  // 启用审计日志
    OptimisticLock  bool  // 乐观锁
    Transactional   bool  // 事务管理
}
```

## 中间件

中间件是处理 HTTP 请求的函数，可以在请求到达处理器之前或响应返回之前执行操作。

```go
func authMiddleware(c core.IHttpContext, next core.HandlerFunc) error {
    token := c.GetHeader("Authorization")
    if !validateToken(token) {
        return c.JSON(401, map[string]string{"error": "unauthorized"})
    }
    return next(c)
}

func loggingMiddleware(c core.IHttpContext, next core.HandlerFunc) error {
    start := time.Now()
    err := next(c)
    duration := time.Since(start)

    log.Printf("%s %s - %v", c.GetMethod(), c.GetPath(), duration)
    return err
}
```

## 错误处理

内置了完整的错误处理体系，支持不同类型的错误：

```go
// 验证错误
return errors.NewValidationError("invalid input")

// 未找到错误
return errors.NewNotFoundError("user not found")

// 冲突错误
return errors.NewConflictError("user already exists")

// 内部错误
return errors.NewInternalError(err, "database error")
```

## 自定义验证

```go
func customUserValidator(user any) error {
    if u, ok := user.(*User); ok {
        if len(u.Name) < 2 {
            return errors.NewValidationError("用户名至少需要2个字符")
        }
        if !isValidEmail(u.Email) {
            return errors.NewValidationError("邮箱格式无效")
        }
    }
    return nil
}

// 应用自定义验证
restful.NewRestfulBuilder(userService, validator).
    Route(func(config *restful.RouteConfig) {
        config.Validator = customUserValidator
    }).
    Build(router)
```

## 批量操作结果

批量操作返回详细的结果信息：

```json
{
  "total": 100,
  "success": 95,
  "failed": 5,
  "success_ids": [1, 2, 3, 4, 5],
  "failed_ids": [6, 7, 8, 9, 10],
  "errors": [
    "用户 6: 邮箱格式无效",
    "用户 7: 用户名已存在"
  ]
}
```

## 性能优化

### 批量操作优化

```go
// 配置批量操作参数
restful.NewRestfulBuilder(userService, validator).
    Service(func(config *app.ServiceConfig) {
        config.MaxBatchSize = 1000    // 最大批量大小
        config.Transactional = true   // 启用事务
    }).
    Build(router)
```

### 缓存支持

```go
// 启用缓存
restful.NewRestfulBuilder(userService, validator).
    Service(func(config *app.ServiceConfig) {
        config.EnableCache = true
        config.CacheTTL = 300        // 5分钟缓存
    }).
    Build(router)
```

## 架构设计

本构建器采用分层架构设计：

```
┌─────────────────────────────────────┐
│           HTTP Layer                │
│  ┌─────────────────────────────┐    │
│  │      RouteBuilder           │    │
│  │  ┌─────────────────────┐    │    │
│  │  │   Config Manager    │    │    │
│  │  │  ┌────────────────┐ │    │    │
│  │  │  │  Middleware    │ │    │    │
│  │  │  │    Chain       │ │    │    │
│  │  │  └────────────────┘ │    │    │
│  │  └─────────────────────┘    │    │
│  └─────────────────────────────┘    │
└─────────────────────────────────────┘
┌─────────────────────────────────────┐
│         Application Layer           │
│  ┌─────────────────────────────┐    │
│  │     AppService              │    │
│  │  ┌─────────────────────┐    │    │
│  │  │  Lifecycle Hooks    │    │    │
│  │  │  Validation Logic   │    │    │
│  │  │  Batch Operations   │    │    │
│  │  └─────────────────────┘    │    │
│  └─────────────────────────────┘    │
└─────────────────────────────────────┘
┌─────────────────────────────────────┐
│           Domain Layer              │
│  ��─────────────────────────────┐    │
│  │   CRUDService               │    │
│  │   AuditedService            │    │
│  │   EventSourcedService       │    │
│  └─────────────────────────────┘    │
└─────────────────────────────────────┘
┌─────────────────────────────────────┐
│        Infrastructure Layer          │
│  ┌─────────────────────────────┐    │
│  │    Repository               │    │
│  │    Database                 │    │
│  │    Cache                    │    │
│  └─────────────────────────────┘    │
└─────────────────────────────────────┘
```

## 最佳实践

1. **统一配置管理**：使用构建器模式统一管理 API 配置
2. **中间件复用**：创建可复用的中间件组件
3. **错误处理标准化**：实现统一的错误处理策略
4. **验证规则集中化**：将验证逻辑集中在服务层
5. **性能监控**：添加日志和性能监控中间件
6. **安全防护**：使用认证和授权中间件保护 API
7. **测试覆盖**：确保完整的单元测试和集成测试

## 迁移指南

详细的迁移指南请参考：[迁移指南](../../../docs/refactor/migration-guide.md)

## 示例项目

完整的使用示例请参考：[example.go](example.go)

## 贡献

欢迎提交 Issue 和 Pull Request 来改进这个项目。

## 许可证

MIT License
