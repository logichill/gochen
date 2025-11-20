# Gochen 示例集（聚焦三类能力）

该目录现在按三种典型使用模式提供示例，便于在真实项目中“清理/迁移”与增量演进：

### 1️⃣ 普通 CRUD (`domain/crud`)

**适用场景**: 简单的业务系统、传统 Web 应用、快速原型开发

**核心接口**:
- `IRepository[T, ID]` - 通用仓储接口
- `IValidator` - 数据验证接口
- `IHttpContext` - HTTP 上下文抽象

**功能特性**:
- ✅ 标准 CRUD 操作（Create/Read/Update/Delete）
- ✅ 自动数据验证
- ✅ RESTful API 自动生成
- ✅ 分页、排序、过滤支持
- ✅ 批量操作支持

**运行示例**:
```bash
go run ./examples/domain/crud
```

---

### 2️⃣ 审计日志 (`domain/audited`)

**适用场景**: 需要审计追踪的企业系统、合规性要求高的应用

**核心接口**:
- `IAuditedRepository[T, ID]` - 审计仓储接口
- `IAuditable` - 审计实体接口
- `ISoftDeletable` - 软删除接口

**功能特性**:
- ✅ 自动记录创建/更新/删除操作
- ✅ 记录操作人和操作时间
- ✅ 软删除支持（逻辑删除）
- ✅ 审计日志查询
- ✅ 数据恢复功能

**运行示例**:
```bash
go run ./examples/domain/audited
```

---

### 3️⃣ 事件溯源 (`domain/eventsourced`)

**适用场景**: 复杂业务系统、金融系统、需要完整历史追溯的应用

**核心接口**:
- `IEventStore` - 事件存储接口
- `IEventBus` - 事件总线接口
- `IEventSourcedRepository[T, ID]` - 事件溯源仓储
- `IProjection` - 投影接口

**功能特性**:
- ✅ 完整的事件溯源支持
- ✅ 事件重放和时间旅行
- ✅ 快照优化（提升加载性能）
- ✅ CQRS 读写分离
- ✅ Outbox 模式（确保事件发布）
- ✅ 投影自动更新

**运行示例**:
```bash
go run ./examples/domain/eventsourced
```

---

## 📚 分组索引（按能力）

- Domain 建模模式
  - domain/crud
  - domain/audited
  - domain/eventsourced

- 命令（Command/CQRS）
  - patterns/command/service

- Outbox（可靠发布）
  - infra/outbox/es_mock（内存 EventStore + mock Outbox）
  - infra/outbox/sql（SQLite + SQLEventStore + SimpleSQLOutboxRepository + OutboxAwareRepository）

- 投影（Projection）
  - infra/projection/basic（内存检查点，演示启动/恢复/追赶）
  - infra/projection/idempotent（基于 event.ID 的幂等写模板）
  - infra/projection/sql_checkpoint（基于 SQL 检查点的持久化恢复）

- 快照（Snapshot）
  - infra/snapshot/basic（SQLite 持久化快照 + 内存 EventStore，对比有/无快照的加载路径）

- SQL Builder / Dialect
  - infra/sqlbuilder/basic（基于 SQLite + ISql 的最小示例：创建表、插入数据、条件查询、SetExpr 更新）

- Saga/流程编排
  - patterns/saga/basic（最小骨架：启动、扣款、加款/补偿）

- Tracing / 观测
  - patterns/tracing/basic（TracingMiddleware 贯通 corr/caus/trace ID）

## 🔄 渐进式演进路径

Gochen Shared 支持从简单到复杂的平滑演进：

```
CRUD 模式 → 审计日志模式 → 事件溯源模式
   ↓              ↓                ↓
简单快速      合规追踪          完整溯源
IRepository  IAuditedRepository  IEventSourcedRepository
```

**演进建议**:
1. **初期**: 使用 CRUD 模式快速开发
2. **中期**: 关键业务添加审计日志
3. **成熟**: 核心领域采用事件溯源

---

## ⚠️ 重要变更说明

### 命名规范更新（v1.0）

本框架已全面采用企业级命名规范，所有公共接口使用 **I 前缀**：

| 旧名称 | 新名称 | 说明 |
|--------|--------|------|
| `Repository` | `IRepository` | 通用仓储接口 |
| `EventStore` | `IEventStore` | 事件存储接口 |
| `EventBus` | `IEventBus` | 事件总线接口 |
| `AuditedRepository` | `IAuditedRepository` | 审计仓储接口 |
| `BatchOperations` | `IBatchOperations` | 批量操作接口 |
| `Transactional` | `ITransactional` | 事务管理接口 |

### 方法命名更新

所有缩写统一使用大写：

| 旧名称 | 新名称 | 说明 |
|--------|--------|------|
| `GetById()` | `GetByID()` | 根据ID查询 |
| `FindById()` | `FindByID()` | 查找实体 |
| `DeleteById()` | `DeleteByID()` | 删除实体 |

> 📖 **详细说明**: 查看 [命名规范文档](../NAMING_CONVENTIONS.md)

## 🚀 快速开始（以 CRUD 为例）

### 步骤 1: 定义实体

```go
package main

import (
    "errors"
    "gochen/domain/entity"
)

// User 用户实体
type User struct {
    ID    int64  `json:"id"`
    Name  string `json:"name" validate:"required,min=2,max=50"`
    Email string `json:"email" validate:"required,email"`
}

// 实现 entity.IEntity 接口
func (u *User) GetID() int64 { 
    return u.ID 
}

func (u *User) SetID(id int64) { 
    u.ID = id 
}

func (u *User) Validate() error {
    if u.Name == "" {
        return errors.New("用户名不能为空")
    }
    if u.Email == "" {
        return errors.New("邮箱不能为空")
    }
    return nil
}

// 确保实现了接口
var _ entity.IEntity[int64] = (*User)(nil)
```

### 步骤 2: 创建仓储和服务

```go
package main

import (
    "gochen/app"
    "gochen/domain/repository"
    "gochen/validation"
)

func main() {
    // 1. 创建仓储（实际项目中通常是数据库实现）
    // 注意：仓储类型是 repository.IRepository[*User, int64]
    userRepo := NewMemoryUserRepository()

    // 2. 创建验证器
    validator := validation.NewValidator()

    // 3. 创建应用服务
    userService := app.NewApplication[*User, int64](
        userRepo,
        validator,
        &app.ServiceConfig{
            AutoValidate:   true,  // 自动验证实体
            AutoTimestamp:  true,  // 自动设置时间戳
            EnableAudit:    true,  // 启用审计日志
            MaxBatchSize:   100,   // 最大批量操作数量
            SoftDelete:     false, // 是否启用软删除
        },
    )

    // 4. 使用服务
    ctx := context.Background()
    
    // 创建用户
    user := &User{Name: "张三", Email: "zhangsan@example.com"}
    if err := userService.Create(ctx, user); err != nil {
        log.Fatal(err)
    }
    
    // 查询用户
    found, err := userService.GetByID(ctx, user.ID)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("找到用户: %+v\n", found)
}
```

### 步骤 3: 注册 RESTful API

```go
package main

import (
    "gochen/app/api"
    "gochen/httpx"
    "github.com/gin-gonic/gin"
)

func main() {
    // 创建服务（步骤2）
    userService := createUserService()
    validator := validation.NewValidator()

    // 创建 HTTP 路由器
    router := gin.Default()

    // 方式 1: 快速注册（使用默认配置）
    api.RegisterRESTfulAPI(router, "/api/v1/users", userService, validator)

    // 方式 2: 使用构建器进行高级配置
    api.NewRestfulBuilder(userService, validator).
        BasePath("/api/v1/users").
        Route(func(config *api.RouteConfig) {
            config.EnableBatch = true      // 启用批量操作
            config.EnablePagination = true // 启用分页
            config.MaxPageSize = 500       // 最大分页大小
            config.DefaultPageSize = 20    // 默认分页大小
        }).
        Service(func(config *app.ServiceConfig) {
            config.AutoValidate = true  // 自动验证
            config.SoftDelete = true    // 软删除
            config.EnableCache = true   // 启用缓存
            config.CacheTTL = 300      // 缓存5分钟
        }).
        Middleware(
            loggingMiddleware,  // 日志中间件
            authMiddleware,     // 认证中间件
            rateLimitMiddleware, // 限流中间件
        ).
        Build(router)

    // 启动服务器
    router.Run(":8080")
}
```

**自动生成的 API 端点**:
```
GET    /api/v1/users          # 获取用户列表（支持分页/过滤/排序）
GET    /api/v1/users/:id      # 获取单个用户
POST   /api/v1/users          # 创建用户
PUT    /api/v1/users/:id      # 更新用户
DELETE /api/v1/users/:id      # 删除用户
POST   /api/v1/users/batch    # 批量创建
PUT    /api/v1/users/batch    # 批量更新
DELETE /api/v1/users/batch    # 批量删除
```

---

---

## 📖 RESTful API 端点说明

注册后自动生成的 RESTful API 端点：

### 基础 CRUD 操作

| HTTP 方法 | 路径 | 描述 | 请求体 | 响应 |
|-----------|------|------|--------|------|
| GET | `/resource` | 获取资源列表 | - | `{ data: [], total: 100, page: 1 }` |
| GET | `/resource/:id` | 获取单个资源 | - | `{ data: {...} }` |
| POST | `/resource` | 创建新资源 | `{ name: "..." }` | `{ data: {...}, id: 1 }` |
| PUT | `/resource/:id` | 更新资源 | `{ name: "..." }` | `{ data: {...} }` |
| PATCH | `/resource/:id` | 部分更新 | `{ name: "..." }` | `{ data: {...} }` |
| DELETE | `/resource/:id` | 删除资源 | - | `{ success: true }` |

### 批量操作（需启用）

| HTTP 方法 | 路径 | 描述 | 请求体 | 响应 |
|-----------|------|------|--------|------|
| POST | `/resource/batch` | 批量创建 | `[{...}, {...}]` | `{ created: 10, failed: 0 }` |
| PUT | `/resource/batch` | 批量更新 | `[{id:1, ...}, {...}]` | `{ updated: 10, failed: 0 }` |
| DELETE | `/resource/batch` | 批量删除 | `[1, 2, 3]` | `{ deleted: 3 }` |

### 查询参数
- **分页参数：**
  - `page` - 页码（默认：1）
  - `size` - 每页大小（默认：10）

- **排序参数：**
  - `sort` - 排序字段
  - `order` - 排序方向（asc/desc）

- **过滤参数：**
  - 任何非保留参数都将作为过滤条件

- **字段选择：**
  - `fields` - 选择返回的字段，逗号分隔

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

# 复杂查询
GET /users?page=1&size=10&sort=createdAt&order=desc&status=active&fields=id,name,email

# 批量创建
POST /users/batch
[
  {"name": "User 1", "email": "user1@example.com"},
  {"name": "User 2", "email": "user2@example.com"}
]
```

---

## 🔧 配置选项

### 路由配置 (RouteConfig)
```go
config := &RouteConfig{
    BasePath:         "/api/v1",     // 基础路径
    EnableBatch:      true,          // 启用批量操作
    EnablePagination: true,          // 启用分页
    MaxPageSize:      500,           // 最大分页大小
    DefaultPageSize:  20,            // 默认分页大小
    MaxBodySize:      10 << 20,      // 10MB
    ErrorHandler:     customErrorHandler,   // 自定义错误处理器
    ResponseWrapper:  customResponseWrapper, // 自定义响应包装器
    CORS: &CORSConfig{
        AllowOrigins:     []string{"*"},
        AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
        AllowHeaders:     []string{"*"},
        AllowCredentials: false,
        MaxAge:           86400,
    },
}
```

### 服务配置 (ServiceConfig)
```go
config := &ServiceConfig{
    AutoValidate:    true,    // 自动验证
    AutoTimestamp:   true,    // 自动时间戳
    SoftDelete:      true,    // 软删除
    AuditFields:     true,    // 审计字段
    MaxBatchSize:    100,     // 最大批量大���
    EnableCache:     true,    // 启用缓存
    CacheTTL:        300,     // 5分钟
    EnableAudit:     true,    // 启用审计
    OptimisticLock:  true,    // 乐观锁
    Transactional:   true,    // 事务管理
}
```

---

## 🛡️ 中间件

### 内置中间件
- **认证中间件** - 验证 API 令牌
- **授权中间件** - 基于角色的访问控制
- **限流中间件** - 防止滥用
- **日志中间件** - 记录请求和响应
- **监控中间件** - 性能统计
- **CORS 中间件** - 跨域支持
- **健康检查** - 服务状态监控

### 自定义中间件
```go
func customMiddleware(ctx core.IHttpContext, next func() error) error {
    // 前置逻辑
    log.Println("处理请求前")

    // 执行处理
    err := next()

    // 后置逻辑
    log.Println("处理请求后")

    return err
}
```

---

## 📊 最佳实践

### 1. 实体设计
- 确保实体实现 `IEntity` 接口
- 在 `Validate()` 方法中实现验证逻辑
- 使用有意义的字段名和 JSON 标签
- 保持实体的简洁和内聚

### 2. 服务配置
- 根据业务需求启用适当的配置
- 生产环境启用缓存和审计
- 合理设置分页大小限制
- 启用事务和乐观锁

### 3. 中间件使用
- 认证中间件放在最前面
- 日志中间件用于记录请求
- 限流中间件保护服务
- 性能监控中间件用于分析

### 4. 错误处理
- 使用统一的错误类型
- 返回有意义的错误信息
- 避免在生产环境暴露内部错误
- 实现自定义错误处理器

### 5. 性能优化
- 启用缓存减少数据库查询
- 使用批量操作提高吞吐量
- 合理设置分页大小
- 避免 N+1 查询问题

---

## 🔍 测试示例

### 使用 curl 测试
```bash
# 创建用户
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"name":"张三","email":"zhangsan@example.com"}'

# 获取用户列表
curl http://localhost:8080/users

# 获取单个用户
curl http://localhost:8080/users/1

# 更新用户
curl -X PUT http://localhost:8080/users/1 \
  -H "Content-Type: application/json" \
  -d '{"name":"张三","email":"zhangsan@example.com"}'

# 删除用户
curl -X DELETE http://localhost:8080/users/1

# 批量创建
curl -X POST http://localhost:8080/users/batch \
  -H "Content-Type: application/json" \
  -d '[{"name":"用户1","email":"user1@example.com"},{"name":"用户2","email":"user2@example.com"}]'
```

---

---

## 📚 核心接口参考

### 实体接口

```go
// IEntity 基础实体接口
type IEntity[ID comparable] interface {
    GetID() ID
    SetID(id ID)
    Validate() error
}

// IAuditable 可审计实体接口
type IAuditable interface {
    GetCreatedAt() time.Time
    SetCreatedAt(t time.Time)
    GetUpdatedAt() time.Time
    SetUpdatedAt(t time.Time)
    GetCreatedBy() string
    SetCreatedBy(by string)
    GetUpdatedBy() string
    SetUpdatedBy(by string)
}

// ISoftDeletable 软删除实体接口
type ISoftDeletable interface {
    GetDeletedAt() *time.Time
    SetDeletedAt(t *time.Time)
    IsDeleted() bool
}
```

### 仓储接口

```go
// IRepository 通用仓储接口
type IRepository[T IEntity[ID], ID comparable] interface {
    GetByID(ctx context.Context, id ID) (T, error)
    Save(ctx context.Context, entity T) error
    Delete(ctx context.Context, id ID) error
    List(ctx context.Context, opts *QueryOptions) ([]T, error)
    Count(ctx context.Context, filters map[string]any) (int64, error)
}

// IBatchOperations 批量操作接口
type IBatchOperations[T IEntity[ID], ID comparable] interface {
    CreateAll(ctx context.Context, entities []T) error
    UpdateBatch(ctx context.Context, entities []T) error
    DeleteBatch(ctx context.Context, ids []ID) error
}

// ITransactional 事务管理接口
type ITransactional interface {
    BeginTx(ctx context.Context) (context.Context, error)
    Commit(ctx context.Context) error
    Rollback(ctx context.Context) error
}
```

### 事件溯源接口

```go
// IEventStore 事件存储核心接口
type IEventStore interface {
    AppendEvents(ctx context.Context, aggregateID int64, 
                 events []IEvent, expectedVersion uint64) error
    LoadEvents(ctx context.Context, aggregateID int64, 
               afterVersion uint64) ([]IEvent, error)
    StreamEvents(ctx context.Context, 
                 opts *StreamOptions) (<-chan IEvent, error)
}

// IEventBus 事件总线接口
type IEventBus interface {
    Publish(ctx context.Context, event IEvent) error
    PublishAll(ctx context.Context, events []IEvent) error
    Subscribe(eventType string, handler IEventHandler) error
}

// IEventSourcedRepository 事件溯源仓储接口
type IEventSourcedRepository[T IEventSourcedAggregate[ID], ID comparable] interface {
    Load(ctx context.Context, id ID) (T, error)
    Save(ctx context.Context, aggregate T) error
    Exists(ctx context.Context, id ID) (bool, error)
}
```

---

## 📖 相关文档

### 核心文档
- 📘 [最终评估报告](../FINAL_ASSESSMENT.md) - 项目评分和改进历程（9.3/10）
- 📗 [命名规范文档](../NAMING_CONVENTIONS.md) - 完整的代码规范标准（必读）
- 📙 [重构完成报告](../REFACTORING_COMPLETE.md) - 重构详情和统计数据
- 📕 [架构评估报告](../ARCHITECTURE_ASSESSMENT.md) - DDD 架构详细分析

### API 文档
- [RESTful API 构建器](../app/api/README.md) - 详细 API 配置说明
- [HTTP 抽象层](../httpx/README.md) - HTTP 上下文和路由
- [应用服务层](../app/README.md) - 应用服务接口

### 领域层文档
- [实体和聚合根](../domain/entity/README.md) - 实体设计指南
- [仓储模式](../domain/repository/README.md) - 仓储接口说明
- [领域服务](../domain/service/README.md) - 领域服务抽象

### 事件溯源文档
- [事件存储](../eventing/store/README.md) - EventStore 详细说明
- [事件总线](../eventing/bus/README.md) - EventBus 使用指南
- [Outbox 模式](../eventing/outbox/README.md) - 可靠事件发布
- [投影管理](../eventing/projection/README.md) - 读模型投影

### 测试文档
- [测试策略](../TESTING.md) - 单元测试和集成测试
- [Mock 框架](./internal/mocks/README.md) - 测试 Mock 实现

---

---

## 🎯 快速参考

### 常用命令

```bash
# 运行所有示例
go run ./examples/crud/main.go
go run ./examples/audited/main.go
go run ./examples/eventsourced/main.go

# 运行测试
go test ./... -v

# 运行测试并查看覆盖率
go test ./... -cover

# 生成测试覆盖率报告
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# 格式化代码
go fmt ./...

# 检查代码
go vet ./...

# 整理依赖
go mod tidy
```

### 命名规范检查清单

在编写代码时，请确保遵循以下规范：

- [ ] 所有公共接口使用 **I 前缀**（如 `IRepository`）
- [ ] 缩写统一使用**大写**（如 `GetByID` 而非 `GetById`）
- [ ] HTTP/URL/JSON/XML/API 等缩写全部大写
- [ ] 所有导出类型有**完整的 GoDoc 注释**
- [ ] 方法命名使用标准动词（Get/Set/Has/Is/Create/Update/Delete）
- [ ] 包名使用**小写单数**形式

> 📖 详细规范请查看 [命名规范文档](../NAMING_CONVENTIONS.md)

---

## ❓ 常见问题

### Q1: 如何从旧版本迁移？

旧代码使用无前缀接口：
```go
type EventStore interface { ... }
repo := NewRepository[User, int64](...)
```

新代码使用 I 前缀：
```go
type IEventStore interface { ... }
repo := NewRepository[User, int64](...)  // 实现类不变
```

**迁移步骤**:
1. 全局搜索替换接口名称（如 `EventStore` → `IEventStore`）
2. 更新方法调用（如 `GetById` → `GetByID`）
3. 运行测试确保没有遗漏

### Q2: 为什么接口要用 I 前缀？

**优势**:
- ✅ 一眼识别接口类型
- ✅ 避免命名冲突（接口和实现可同名）
- ✅ IDE 自动补全更友好
- ✅ 符合企业级 Go 项目规范
- ✅ 降低团队学习曲线

**对比**:
```go
// 标准库风格（适合小型项目）
type Reader interface { ... }
type Writer interface { ... }

// 企业级风格（适合大型项目）- Gochen Shared 采用
type IReader interface { ... }
type IWriter interface { ... }
```

### Q3: CRUD/Audited/EventSourced 如何选择？

| 场景 | 推荐模式 | 理由 |
|------|----------|------|
| 简单业务系统 | CRUD | 快速开发，满足基本需求 |
| 企业管理系统 | Audited | 需要审计追踪和合规性 |
| 金融/医疗系统 | EventSourced | 需要完整历史和时间旅行 |
| 初创产品 | CRUD → Audited | 先快速上线，后续添加审计 |
| 成熟产品 | Audited → EventSourced | 核心业务逐步演进为事件溯源 |

### Q4: 如何处理复杂查询？

```go
// 使用 QueryOptions
opts := &repository.QueryOptions{
    Filters: map[string]any{
        "status": "active",
        "age_gt": 18,  // 大于18岁
    },
    Sort: &repository.SortOption{
        Field: "created_at",
        Order: "desc",
    },
    Pagination: &repository.PaginationOption{
        Page: 1,
        Size: 20,
    },
}

users, err := userRepo.List(ctx, opts)
```

### Q5: 如何实现事务？

```go
// 仓储实现 ITransactional 接口
txRepo, ok := userRepo.(repository.ITransactional)
if !ok {
    return errors.New("仓储不支持事务")
}

// 开始事务
ctx, err := txRepo.BeginTx(ctx)
if err != nil {
    return err
}
defer txRepo.Rollback(ctx)  // 确保异常时回滚

// 执行操作
if err := userRepo.Save(ctx, user1); err != nil {
    return err
}
if err := userRepo.Save(ctx, user2); err != nil {
    return err
}

// 提交事务
return txRepo.Commit(ctx)
```

---

## 🤝 贡献指南

欢迎提交 Issue 和 Pull Request 来改进示例！

### 添加新示例

1. 在对应目录创建新的 `main.go`
2. 添加详细的注释说明
3. 遵循命名规范（I 前缀、大写缩写）
4. 更新本 README 文档
5. 确保示例可独立运行
6. 添加测试用例

### 改进现有示例

1. 检查代码质量和可读性
2. 补充注释和说明
3. 优化性能和错误处理
4. 更新文档
5. 运行测试验证

### 代码审查清单

提交前请检查：
- [ ] 遵循命名规范
- [ ] 添加完整注释
- [ ] 通过所有测试
- [ ] 格式化代码（`go fmt`）
- [ ] 检查代码（`go vet`）
- [ ] 更新相关文档

---

## 📊 项目状态

**版本**: v1.0  
**评分**: 9.3/10 🏆  
**测试覆盖率**: 65%+ (核心模块 80%+)  
**状态**: 🟢 生产就绪

**最近更新**（2025-11-10）:
- ✅ 接口命名统一化（14个接口，100% I前缀）
- ✅ 测试覆盖率大幅提升（SQL 80.3%, Outbox 75.9%）
- ✅ 完整的命名规范文档
- ✅ 详尽的评估和重构报告

---

## 📄 许可证

MIT License

---

**感谢使用 Gochen Shared！**  
如有问题请查看文档或提交 Issue。
