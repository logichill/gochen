# Gochen Shared Library

企业级 DDD + Event Sourcing + CQRS 共享库，提供可跨多个服务复用的核心组件。

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-yellow)](./LICENSE)

## 特性概览

### 核心能力 ✅

✅ **渐进式架构** - 支持按 CRUD → Audited → Event Sourcing 的路径逐步演进（阶段间可能需要适度重构和数据迁移）  
✅ **完整的事件溯源** - 事件存储、快照、投影、Outbox 模式齐全  
✅ **SOLID 原则** - 接口隔离、依赖倒置、开闭原则严格遵循  
✅ **泛型支持** - 充分利用 Go 1.21+ 泛型，类型安全且灵活  
✅ **并发安全** - 核心组件使用 RWMutex 保护，线程安全  
✅ **框架无关** - HTTP、数据库、消息传输均可替换底层实现  
✅ **高性能** - LRU 缓存、批量操作、快照优化、游标分页  
✅ **企业级规范** - 统一命名规范（I 前缀接口），完整文档

### 高级功能 🎉 NEW!

✅ **命令总线** - CQRS 命令处理、中间件管道、幂等性、验证  
✅ **Saga 模式** - 跨聚合长事务、自动补偿、状态持久化  
✅ **投影检查点** - 进程恢复、增量追赶、幂等写入保护  
✅ **远程桥接** - 分布式命令/事件通信、HTTP/gRPC 支持  
✅ **全链路追踪** - Correlation/Causation ID 自动传播  
✅ **Outbox 增强** - 并行发布、死信队列、批量操作（5x 性能）  
✅ **多租户支持** - 租户隔离、上下文传递、装饰器模式

---

## 目录结构

```
domain/                      # 领域层抽象
├── entity/                  # 实体和聚合根
│   ├── entity.go                  # IEntity, IAuditable, ISoftDeletable
│   ├── aggregate.go               # Aggregate（CRUD+事件） + IAggregate
│   ├── aggregate_eventsourced.go  # EventSourcedAggregate（事件溯源）
│   └── aggregate_errors.go        # 聚合根错误定义
├── repository/              # 仓储接口
│   ├── repo.go              # IRepository 基础仓储接口
│   ├── audited.go           # IAuditedRepository 审计仓储
│   ├── eventsourced.go      # IEventSourcedRepository 事件溯源仓储
│   ├── batch.go             # IBatchOperations 批量操作
│   └── transactional.go     # ITransactional 事务管理
└── service/                 # 服务层接口
    └── service.go           # ICRUDService 业务服务抽象

eventing/                   # 事件系统（事件模型/存储/投影/出箱）
├── event.go                # 事件模型（嵌入 messaging.Message）
├── errors.go               # 错误与并发冲突语义
├── tracing_store.go        # TracingEventStore 装饰器 🎉 NEW!
├── tenant_store.go         # TenantAwareEventStore 装饰器 🎉 NEW!
├── registry/               # 事件类型注册（Schema 版本 + 反序列化）
├── bus/                    # 事件总线
│   └── eventbus.go         # IEventBus 接口及实现
├── store/                  # 存储抽象
│   ├── eventstore.go       # IEventStore / 扩展接口定义
│   ├── helpers.go          # AggregateExists/GetCurrentVersion 等辅助函数
│   ├── memory_store.go     # 内存实现（测试/示例）
│   ├── cached/             # 缓存/指标装饰器
│   ├── snapshot/           # 快照存储与管理器
│   └── sql/                # SQL 实现（追加/查询/游标）
├── outbox/                 # Outbox 模式（仓储 + 发布器）
│   ├── outbox.go                   # OutboxEntry + IOutboxRepository/IOutboxPublisher 接口
│   ├── sql_repository.go           # 基于 IDatabase/ISql 的 SQL 仓储实现
│   ├── publisher.go                # Outbox 发布器
│   ├── publisher_parallel.go       # 并行发布器 🎉 NEW!
│   ├── dlq.go                      # 死信队列 🎉 NEW!
│   ├── batch.go                    # 批量操作 🎉 NEW!
│   ├── cleanup.go                  # 清理策略 🎉 NEW!
│   └── metrics.go                  # 监控指标 🎉 NEW!
├── projection/             # CQRS 投影管理
│   ├── projection.go       # IProjection 接口（旧版投影管理器，已不推荐）
│   ├── manager.go          # ProjectionManager 投影管理器（支持检查点）
│   ├── checkpoint.go               # ICheckpointStore 接口 🎉 NEW!
│   ├── checkpoint_sql.go           # SQL 实现 🎉 NEW!
│   ├── checkpoint_memory.go        # 内存实现 🎉 NEW!
│   └── tenant.go                   # TenantAwareProjector 🎉 NEW!
├── integration/            # 集成事件（跨上下文通信）
└── upgrader/               # 事件升级器

messaging/                  # 消息系统
├── message.go              # IMessage 接口和实现
├── handler.go              # IMessageHandler 处理器接口
├── bus.go                  # IMessageBus 消息总线实现
├── transport.go            # ITransport 传输层接口
├── bridge/                 # 远程桥接（基于 HTTP 的命令/事件转发）
├── command/                # 命令总线 🎉 NEW!
│   ├── command.go                 # Command 实现（嵌入 Message）
│   ├── handler.go                 # CommandHandler 适配器
│   ├── bus.go                     # CommandBus 包装器
│   ├── errors.go                  # 命令错误
│   └── middleware/                # 标准中间件
│       ├── validation.go          # 验证中间件
│       ├── idempotency.go         # 幂等性中间件
│       ├── aggregate_lock.go      # 聚合锁中间件
│       ├── tracing.go             # 追踪中间件 🎉 NEW!
│       └── tenant.go              # 租户中间件 🎉 NEW!
└── transport/              # 传输实现
    ├── memory/             # 内存传输（异步队列）
    ├── natsjetstream/      # NATS JetStream 传输
    ├── redisstreams/       # Redis Streams 传输
    └── sync/               # 同步传输（同步执行）

app/                        # 应用层
├── application/            # Application 应用服务（通用应用服务层）
└── api/                    # RESTful API 构建器
    ├── builder.go          # RestfulBuilder
    ├── router.go           # IRouter 路由接口
    └── config.go           # 路由和服务配置

http/                       # HTTP 抽象层
├── context.go              # IHttpContext 接口
├── request.go              # IHttpRequest 接口
├── response.go             # IHttpResponse 接口
├── server.go               # IHttpServer 接口
├── tracing.go              # Correlation/Causation ID 管理 🎉 NEW!
├── tenant.go               # 租户上下文管理 🎉 NEW!
└── basic/                  # 基础实现
    ├── context.go          # 基础 HTTP 上下文
    ├── request.go          # 基础 HTTP 请求
    └── response.go         # 基础 HTTP 响应

data/                       # 数据与存储抽象
├── db/                     # 数据库接口与实现（原 storage/database）
│   ├── basic/              # 基础 DB 实现（IDatabase）
│   ├── dialect/            # 数据库方言（DeleteLimit/Upsert 等）
│   └── sql/                # SQL Builder + ISql 抽象
├── file/                   # 文件存储
└── orm/                    # ORM 兼容抽象（接口与元信息）

cache/                      # 缓存系统
codegen/                    # 编码/ID 生成（原 idgen）
errors/                     # 错误处理
logging/                    # 日志系统
validation/                 # 验证工具
di/                         # 依赖注入
patterns/                   # 设计模式与流程编排
├── retry/                  # 重试模式
├── saga/                   # Saga 模式 🎉 NEW!
└── workflow/               # 工作流/流程管理
examples/                   # 示例代码
```

---

## 架构设计

### 核心理念

Gochen Shared 基于以下核心理念构建：

1. **渐进式复杂度** - 从简单 CRUD 到事件溯源，支持平滑迁移
2. **领域驱动设计** - 以业务领域为中心，实体、聚合、仓储清晰定义
3. **事件溯源与 CQRS** - 完整的事件存储、读写分离、投影管理
4. **框架无关性** - 核心逻辑不依赖具体框架，易于替换和测试
5. **类型安全** - 充分利用泛型，编译时检查，减少运行时错误

### 分层架构

```
┌─────────────────────────────────────────────────────────────┐
│                     应用层 (Application)                    │
│               使用 shared 构建具体业务逻辑                  │
│   ┌──────────────┬──────────────┬──────────────────────┐   │
│   │  HTTP Routes │   Services   │  Command Handlers    │   │
│   └──────────────┴──────────────┴──────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                            ↓ 依赖（接口）
┌─────────────────────────────────────────────────────────────┐
│                     领域层 (Domain)                         │
│                      纯业务逻辑，无框架依赖                  │
│   ┌────────────┬──────────────┬─────────────────────────┐  │
│   │  Entity    │  Repository  │  Service                │  │
│   │  Aggregate │  Interface   │  Interface              │  │
│   │  Event     │              │                         │  │
│   └────────────┴──────────────┴─────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                            ↓ 依赖（实现）
┌─────────────────────────────────────────────────────────────┐
│                  基础设施层 (Infrastructure)                │
│              提供技术能力，实现领域层接口                    │
│   ┌───────────┬──────────┬─────────┬──────────┬──────────┐ │
│   │ Eventing  │ Messaging│ Storage │  HTTP    │  Cache   │ │
│   │ (事件系统) │ (消息系统)│ (存储)  │ (Web)    │ (缓存)   │ │
│   │ - Store   │ - Bus    │ - DB    │ - Server │ - LRU    │ │
│   │ - Outbox  │ - Worker │ - Repo  │ - Router │ - TTL    │ │
│   └───────────┴──────────┴─────────┴──────────┴──────────┘ │
└─────────────────────────────────────────────────────────────┘
```

**架构优势**:
- ✅ **依赖方向清晰** - 高层不依赖低层实现，遵循 DIP 原则
- ✅ **易于测试** - 领域层可独立测试，无需启动完整应用
- ✅ **技术栈灵活** - 基础设施层可替换（如 GORM → SQLX，Gin → Echo）
- ✅ **业务逻辑纯粹** - 领域层专注业务规则，不涉及技术细节

### 渐进式演进路径

Gochen Shared 支持三个递进的复杂度级别，允许根据业务需求灵活选择：

```
第一阶段：简单 CRUD（配置表、字典表）
└─ IRepository[T, ID] + Application[T, ID]
   - 适用场景：分类管理、标签系统、配置项
   - 特点：轻量级，快速开发
   
第二阶段：审计追踪（订单、用户管理）
└─ IAuditedRepository[T, ID] + Application[T, ID]
   - 适用场景：订单系统、用户管理、内容管理
   - 新增能力：创建人/时间、修改人/时间、软删除/恢复
   
第三阶段：事件溯源（金融交易、积分系统）
└─ IEventSourcedRepository[A, ID] + EventSourcedService[A, ID]
   - 适用场景：积分系统、金融账户、审计日志
   - 新增能力：完整事件历史、时间旅行、事件重放
```

**演进示例（阶段 1→2 基本无侵入，2→3 需要有意识重构和数据迁移）**:

```go
// 阶段 1: 从简单 CRUD 开始
type Category struct {
    entity.Entity
    Name string
}

// 阶段 2: 后续需求变化，升级到审计模式（实体结构基本无需重构）
// - Entity 已包含审计字段
// - 切换到 IAuditedRepository 即可

// 阶段 3: 业务关键，升级到事件溯源（需要重构聚合模型与持久化方式）
type Category struct {
    *entity.EventSourcedAggregate[int64]
    Name string
}
// - 重构为事件溯源聚合（命令处理/事件模型/投影均需调整）
// - 历史数据需要通过批处理/迁移工具转换为事件流
```

### 可插拔技术栈

框架采用适配器模式，支持替换底层实现：

| 组件 | 默认实现 | 可选实现 | 扩展方式 |
|------|---------|---------|---------|
| **Web 框架** | Gin | Fiber, Echo | 实现 `IHttpServer` 接口 |
| **ORM** | GORM | SQLX, Ent | 实现 `IDatabase` 接口 |
| **事件存储** | SQL | MongoDB, EventStoreDB | 实现 `IEventStore` 接口 |
| **消息传输** | Memory | Redis Streams、NATS JetStream | 实现 `ITransport` 接口 |
| **缓存** | 内存 LRU | Redis, Memcached | 实现缓存接口 |

---

## 核心概念

### 1. 实体和聚合根

#### 基础实体接口

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

#### 实体实现示例

```go
// User 用户实体
type User struct {
    ID    int64  `json:"id"`
    Name  string `json:"name" validate:"required"`
    Email string `json:"email" validate:"required,email"`
}

func (u *User) GetID() int64 { return u.ID }
func (u *User) SetID(id int64) { u.ID = id }
func (u *User) Validate() error {
    if u.Name == "" {
        return errors.New("name is required")
    }
    return nil
}

// 确保实现了接口
var _ entity.IEntity[int64] = (*User)(nil)
```

### 2. 仓储模式

#### 仓储接口层次

```go
// IRepository 通用仓储接口（CRUD 基础）
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

// IAuditedRepository 审计仓储接口
type IAuditedRepository[T entity.IAuditable, ID comparable] interface {
    IRepository[T, ID]
    // 自动记录创建/更新/删除的操作人和时间
}

// IEventSourcedRepository 事件溯源仓储接口
type IEventSourcedRepository[T IEventSourcedAggregate[ID], ID comparable] interface {
    Load(ctx context.Context, id ID) (T, error)
    Save(ctx context.Context, aggregate T) error
    Exists(ctx context.Context, id ID) (bool, error)
}
```

### 3. 事件溯源

#### 事件存储接口

```go
// IEventStore 事件存储核心接口
type IEventStore interface {
    // AppendEvents 追加事件到指定聚合的事件流
    AppendEvents(ctx context.Context, aggregateID int64, 
                 events []IEvent, expectedVersion uint64) error
    
    // LoadEvents 加载聚合的事件历史
    LoadEvents(ctx context.Context, aggregateID int64, 
               afterVersion uint64) ([]IEvent, error)
    
    // StreamEvents 拉取指定时间之后的事件列表（按时间升序）
    // 如需基于游标/类型过滤/limit 的流式消费，请优先实现 IEventStoreExtended.GetEventStreamWithCursor。
    StreamEvents(ctx context.Context, fromTime time.Time) ([]eventing.Event, error)
}

// IAggregateInspector 聚合检查器接口
type IAggregateInspector interface {
    HasAggregate(ctx context.Context, aggregateID int64) (bool, error)
    GetAggregateVersion(ctx context.Context, aggregateID int64) (uint64, error)
}

// ITypedEventStore 类型化事件存储接口
type ITypedEventStore interface {
    IEventStore
    LoadEventsByType(ctx context.Context, aggregateType string, 
                     aggregateID int64, afterVersion uint64) ([]IEvent, error)
}
```

#### 事件定义示例

```go
// UserCreated 用户创建事件
type UserCreated struct {
    eventing.EventBase
    UserID int64
    Name   string
    Email  string
}

// Apply 应用事件到聚合
func (e *UserCreated) Apply(aggregate entity.IAggregate) error {
    user := aggregate.(*User)
    user.ID = e.UserID
    user.Name = e.Name
    user.Email = e.Email
    return nil
}
```

### 4. CQRS 读写分离

#### 投影接口

```go
// IProjection 投影接口
type IProjection interface {
    // Handle 处理事件，更新读模型
    Handle(ctx context.Context, event IEvent) error
    
    // GetName 获取投影名称
    GetName() string
    
    // Reset 重置投影
    Reset(ctx context.Context) error
}

// ProjectionManager 投影管理器接口
type ProjectionManager interface {
    // Register 注册投影
    Register(projection IProjection) error
    
    // Start 启动投影更新
    Start(ctx context.Context) error
    
    // Stop 停止投影更新
    Stop() error
}
```

#### 投影实现示例

```go
// UserViewProjection 用户视图投影
type UserViewProjection struct {
    db IDatabase
}

func (p *UserViewProjection) Handle(ctx context.Context, event IEvent) error {
    switch e := event.(type) {
    case *UserCreated:
        return p.handleUserCreated(ctx, e)
    case *UserUpdated:
        return p.handleUserUpdated(ctx, e)
    default:
        return nil
    }
}

func (p *UserViewProjection) handleUserCreated(ctx context.Context, e *UserCreated) error {
    view := &UserView{
        ID:    e.UserID,
        Name:  e.Name,
        Email: e.Email,
    }
    return p.db.Insert(ctx, "user_views", view)
}
```

### 5. Outbox 模式

Outbox 模式确保事件可靠发布，避免分布式事务问题。

```go
// IOutboxRepository Outbox 仓储接口
type IOutboxRepository interface {
    // Save 保存待发布事件
    Save(ctx context.Context, entry *OutboxEntry) error
    
    // GetPending 获取待发布事件
    GetPending(ctx context.Context, limit int) ([]*OutboxEntry, error)
    
    // MarkPublished 标记事件已发布
    MarkPublished(ctx context.Context, id int64) error
    
    // MarkFailed 标记事件发布失败
    MarkFailed(ctx context.Context, id int64, err error) error
}

// IOutboxPublisher Outbox 发布器接口
type IOutboxPublisher interface {
    // Start 启动后台发布任务
    Start(ctx context.Context) error
    
    // Stop 停止发布
    Stop() error
    
    // PublishPending 立即发布待发布事件
    PublishPending(ctx context.Context) error
}
```

---

## 快速开始

### 1. 简单 CRUD 示例

```go
package main

import (
    "context"
    application "gochen/app/application"
    "gochen/app/api"
    "gochen/domain/entity"
    "gochen/validation"
    "github.com/gin-gonic/gin"
)

// 1. 定义实体
type Product struct {
    ID    int64  `json:"id"`
    Name  string `json:"name" validate:"required"`
    Price int64  `json:"price" validate:"required,gt=0"`
}

func (p *Product) GetID() int64 { return p.ID }
func (p *Product) SetID(id int64) { p.ID = id }
func (p *Product) Validate() error { return nil }

func main() {
    // 2. 创建仓储（实际项目使用数据库实现）
    productRepo := NewProductRepository()
    
    // 3. 创建应用服务
    productService := application.NewApplication[*Product, int64](
        productRepo,
        validation.NewValidator(),
        &application.ServiceConfig{
            AutoValidate: true,
            EnableCache:  true,
        },
    )
    
    // 4. 创建 HTTP 服务器并注册 API
    router := gin.Default()
    api.RegisterRESTfulAPI(router, "/api/v1/products", productService, validation.NewValidator())
    
    // 5. 启动服务器
    router.Run(":8080")
}
```

### 2. 事件溯源示例

```go
package main

import (
    "context"
    "gochen/domain/entity"
    "gochen/domain/eventsourced"
    "gochen/eventing"
    "gochen/eventing/store"
)

// 1. 定义聚合根
type Account struct {
    *entity.EventSourcedAggregate[int64]
    Balance int64
}

// 2. 定义事件
type MoneyDeposited struct {
    eventing.EventBase
    Amount int64
}

// 3. 实现事件应用逻辑
func (e *MoneyDeposited) Apply(agg entity.IAggregate) error {
    account := agg.(*Account)
    account.Balance += e.Amount
    return nil
}

// 4. 定义命令
func (a *Account) Deposit(amount int64) error {
    if amount <= 0 {
        return errors.New("amount must be positive")
    }
    
    // 记录事件
    event := &MoneyDeposited{
        EventBase: eventing.NewEventBase(a.GetID(), "MoneyDeposited", 1),
        Amount:    amount,
    }
    a.RecordEvent(event)
    return nil
}

func main() {
    ctx := context.Background()
    
    // 5. 创建事件存储
    eventStore := store.NewSQLEventStore(db)
    
    // 6. 创建仓储
    accountRepo := eventsourced.NewRepository[*Account, int64](eventStore)
    
    // 7. 使用聚合
    account := &Account{
        EventSourcedAggregate: entity.NewEventSourcedAggregate[int64](1),
    }
    account.Deposit(100)
    
    // 8. 保存（自动保存事件）
    if err := accountRepo.Save(ctx, account); err != nil {
        log.Fatal(err)
    }
    
    // 9. 加载（自动重放事件）
    loadedAccount, err := accountRepo.Load(ctx, 1)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Balance: %d\n", loadedAccount.Balance) // 输出: Balance: 100
}
```

### 3. 命令总线示例 🎉 NEW!

```go
package main

import (
    "context"
    "gochen/messaging"
    "gochen/messaging/command"
    "gochen/messaging/command/middleware"
)

// 1. 定义命令
type CreateOrderCommand struct {
    OrderID   int64
    ProductID int64
    Quantity  int
}

// 2. 定义命令处理器
func HandleCreateOrder(ctx context.Context, cmd *command.Command) error {
    payload := cmd.Payload.(*CreateOrderCommand)
    
    // 处理业务逻辑
    order := &Order{
        ID:        payload.OrderID,
        ProductID: payload.ProductID,
        Quantity:  payload.Quantity,
    }
    
    // 保存订单...
    return orderRepo.Save(ctx, order)
}

func main() {
    // 3. 创建消息总线（同步 Transport 能提供更清晰的错误语义）
    messageBus := messaging.NewMessageBus(sync.NewSyncTransport())
    
    // 4. 创建命令总线并添加中间件
    commandBus := command.NewCommandBus(messageBus, nil)
    commandBus.Use(middleware.ValidationMiddleware())
    commandBus.Use(middleware.IdempotencyMiddleware(cache))
    commandBus.Use(middleware.TracingMiddleware())
    commandBus.Use(middleware.TenantMiddleware())
    
    // 5. 注册命令处理器
    commandBus.RegisterHandler("create-order", command.NewCommandHandler(HandleCreateOrder))
    
    // 6. 执行命令
    cmd := command.NewCommand(
        "cmd-123",
        "create-order",
        100,
        "Order",
        &CreateOrderCommand{
            OrderID:   100,
            ProductID: 1,
            Quantity:  5,
        },
    )
    
    err := commandBus.Dispatch(ctx, cmd)
    if err != nil {
        log.Fatal(err)
    }
}

// 自定义异步传输示例（伪代码）
// 业务侧可以在自己的仓库中实现基于 Redis Streams / NATS JetStream 等的 Transport
func newCustomAsyncTransport() messaging.Transport {
    // return mypkg.NewRedisStreamsTransport(...)
    // return mypkg.NewNATSJetStreamTransport(...)
    panic("implement in application repo")
}
```

### 4. Saga 模式示例 🎉 NEW!

```go
package main

import (
    "context"
    "gochen/patterns/saga"
    "gochen/messaging/command"
)

// 1. 定义 Saga
func CreateOrderSaga(orderID int64) saga.ISaga {
    return saga.NewSaga("order-saga", orderID).
        // Step 1: 预留库存
        AddStep(saga.NewSagaStep(
            "reserve-inventory",
            func(ctx context.Context) error {
                cmd := &ReserveInventoryCommand{OrderID: orderID}
                return commandBus.Dispatch(ctx, cmd)
            },
            func(ctx context.Context) error {
                // 补偿：释放库存
                cmd := &ReleaseInventoryCommand{OrderID: orderID}
                return commandBus.Dispatch(ctx, cmd)
            },
        )).
        // Step 2: 预扣款
        AddStep(saga.NewSagaStep(
            "charge-payment",
            func(ctx context.Context) error {
                cmd := &ChargePaymentCommand{OrderID: orderID}
                return commandBus.Dispatch(ctx, cmd)
            },
            func(ctx context.Context) error {
                // 补偿：退款
                cmd := &RefundPaymentCommand{OrderID: orderID}
                return commandBus.Dispatch(ctx, cmd)
            },
        )).
        // Step 3: 创建订单
        AddStep(saga.NewSagaStep(
            "create-order",
            func(ctx context.Context) error {
                cmd := &CreateOrderCommand{OrderID: orderID}
                return commandBus.Dispatch(ctx, cmd)
            },
            func(ctx context.Context) error {
                // 补偿：取消订单
                cmd := &CancelOrderCommand{OrderID: orderID}
                return commandBus.Dispatch(ctx, cmd)
            },
        ))
}

func main() {
    // 2. 创建 Saga 编排器
    stateStore := saga.NewMemoryStateStore()
    orchestrator := saga.NewOrchestrator(stateStore)
    
    // 3. 执行 Saga
    orderSaga := CreateOrderSaga(123)
    err := orchestrator.Execute(context.Background(), orderSaga)
    if err != nil {
        // Saga 失败，已自动执行补偿
        log.Printf("Saga failed: %v", err)
    }
}
```

### 5. 多租户隔离示例 🎉 NEW!

```go
package main

import (
    "context"
    "gochen/http"
    "gochen/eventing"
    "gochen/eventing/projection"
)

func main() {
    // 1. HTTP 层 - 自动提取租户 ID
    mux := http.NewServeMux()
    
    mux.HandleFunc("/api/orders", func(w http.ResponseWriter, r *http.Request) {
        // 提取租户 ID（从 Header: X-Tenant-ID）
        tenantID := http.ExtractTenantIDFromRequest(r)
        ctx := http.WithTenantID(r.Context(), tenantID)
        
        // 处理请求...
        handleCreateOrder(ctx, w, r)
    })
    
    // 使用中间件自动处理
    http.ListenAndServe(":8080", http.TenantMiddleware(mux))
    
    // 2. 事件存储 - 自动隔离
    baseStore := store.NewSQLEventStore(db)
    tenantStore := eventing.NewTenantAwareEventStore(baseStore)
    
    // 保存事件时自动注入租户 ID
    ctx := http.WithTenantID(ctx, "tenant-A")
    err := tenantStore.AppendEvents(ctx, aggregateID, events, 0)
    // 事件自动包含 metadata["tenant_id"] = "tenant-A"
    
    // 加载事件时自动过滤
    events, err := tenantStore.LoadEvents(ctx, aggregateID, 0)
    // 只返回 tenant-A 的事件
    
    // 3. 投影 - 自动过滤
    baseProjector := &OrderProjector{}
    tenantProjector := projection.NewTenantAwareProjector(baseProjector)
    
    // 只处理当前租户的事件
    err = tenantProjector.Handle(ctx, event)
}
```

### 6. 全链路追踪示例 🎉 NEW!

```go
package main

import (
    "context"
    "gochen/http"
    "gochen/eventing"
)

func main() {
    // 1. HTTP 层 - 生成或提取 Correlation ID
    mux := http.NewServeMux()
    
    mux.HandleFunc("/api/orders", func(w http.ResponseWriter, r *http.Request) {
        // 自动提取或生成 correlation_id
        correlationID := http.GetOrGenerateCorrelationID(r)
        ctx := http.WithCorrelationID(r.Context(), correlationID)
        ctx = http.WithCausationID(ctx, r.Header.Get("X-Request-ID"))
        
        // 处理请求...
        handleCreateOrder(ctx, w, r)
    })
    
    // 2. 命令层 - 自动注入追踪 ID
    cmd := command.NewCommand("cmd-123", "create-order", 100, "Order", payload)
    http.InjectTraceContext(ctx, cmd.Metadata)
    // cmd.Metadata["correlation_id"] = "cor-xxx"
    // cmd.Metadata["causation_id"] = "req-xxx"
    
    // 3. 事件层 - 自动传播追踪 ID
    baseStore := store.NewSQLEventStore(db)
    tracingStore := eventing.NewTracingEventStore(baseStore)
    
    err := tracingStore.AppendEvents(ctx, aggregateID, events, 0)
    // 所有事件自动包含 correlation_id 和 causation_id
    
    // 完整追踪链：
    // HTTP Request (correlation_id) 
    //   → Command (correlation_id + causation_id)
    //     → Event (correlation_id + causation_id)
    //       → Projection (可追溯完整链路)
}
```

---

## 命名规范

Gochen Shared 遵循企业级 Go 项目命名规范：

### 接口命名

**所有公共接口使用 I 前缀**：

```go
// ✅ 正确
type IRepository interface { ... }
type IEventStore interface { ... }
type IMessageBus interface { ... }

// ❌ 错误
type Repository interface { ... }
type EventStore interface { ... }
```

### 方法命名

**ID/HTTP/URL 等缩写统一使用大写**：

```go
// ✅ 正确
func GetByID(id int64) (*User, error)
func ParseURL(rawURL string) (*URL, error)
func ToJSON(v any) ([]byte, error)

// ❌ 错误
func GetById(id int64) (*User, error)
func ParseUrl(rawUrl string) (*URL, error)
func ToJson(v any) ([]byte, error)
```

> 📖 详细规范请查看 [命名规范文档](./NAMING.md)

---

## 文档

### 核心文档
- [命名规范](./NAMING.md) - 完整的代码规范标准
- [示例代码](./examples/README.md) - CRUD/Audited/EventSourced 示例

### API 文档
- [RESTful API 构建器](./app/api/README.md) - API 配置说明
- [HTTP 抽象层](./http/README.md) - HTTP 上下文和路由
- [应用服务层](./app/README.md) - 应用服务接口

### 领域层文档
- [实体和聚合根](./domain/entity/README.md) - 实体设计指南
- [仓储模式](./domain/repository/README.md) - 仓储接口说明

### 事件溯源文档
- [事件存储](./eventing/store/README.md) - EventStore 详细说明
- [事件总线](./eventing/bus/README.md) - EventBus 使用指南
- [Outbox 模式](./eventing/outbox/README.md) - 可靠事件发布
- [投影管理](./eventing/projection/README.md) - 读模型投影

### 事件溯源 + Outbox 原子链路（可选）

为避免“状态已落库但事件未发布”的不一致，可启用 Outbox 装饰器仓储，在同一事务中写入事件与 Outbox 表，并由 Publisher 异步发布：

```go
// 基础 ES 仓储
base, _ := eventsourced.NewEventSourcedRepository[*Account](eventsourced.EventSourcedRepositoryOptions[*Account]{
    AggregateType: "account",
    Factory:       NewAccount,
    EventStore:    sqlEventStore, // SQL 实现，支持 AppendEventsWithDB
})

// Outbox 仓储（与 SQL EventStore 共用数据库连接）
obRepo := outbox.NewSimpleSQLOutboxRepository(db, sqlEventStore, logging.GetLogger())

// 包装为 OutboxAwareRepository
repo, _ := eventsourced.NewOutboxAwareRepository(base, obRepo)

// 保存：同事务写入事件与 Outbox；发布由 Publisher 异步完成
_ = repo.Save(ctx, aggregate)
```

说明：
- 未启用 Outbox 时仍可使用基础仓储，保持向后兼容；
- 启用 Outbox 后 Save 不再直接发布事件，推荐运行 outbox.Publisher 处理发布与重试；
- 快照策略仍按基础仓储逻辑执行（允许失败告警）。

---

## 安装

```bash
# 使用 go get 安装
go get gochen

# 或者在 go.mod 中添加
require gochen v1.0.0
```

## 依赖要求

- Go 1.21+ （泛型支持）
- 可选：GORM（数据库ORM）
- 可选：Gin（HTTP框架）

---

## 贡献

欢迎提交 Issue 和 Pull Request！

### 贡献指南
1. Fork 项目
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

### 代码规范
- 遵循命名规范（I 前缀接口，大写缩写）
- 添加完整的 GoDoc 注释
- 编写单元测试
- 运行 `go fmt` 和 `go vet`

---

## 许可证

MIT License - 查看 [LICENSE](./LICENSE) 文件了解详情

---

## 联系方式

- **项目主页**: [GitHub Repository]
- **问题反馈**: [GitHub Issues]
- **邮箱**: your-email@example.com

---

**感谢使用 Gochen Shared！**
