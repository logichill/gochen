# Application Layer - 应用服务层 & 事件溯源模板

应用服务层位于领域层与基础设施层之上，协调领域对象完成业务用例，并基于事件与消息提供应用级模板能力（包括事件溯源）。

## 概述

应用服务层（Application Layer）是 DDD 架构中的关键层，负责：
- 协调领域对象执行业务用例
- 管理事务边界
- 处理应用级的验证和授权
- 转换领域模型为 DTO

## 核心组件

### Application Service（`app/application`）

通用应用服务，支持 CRUD 操作。

```go
// Application 通用应用服务
type Application[T entity.IEntity[ID], ID comparable] struct {
    repo      repository.IRepository[T, ID]
    validator validation.IValidator
    config    *ServiceConfig
}

// 创建应用服务
func NewApplication[T entity.IEntity[ID], ID comparable](
    repo repository.IRepository[T, ID],
    validator validation.IValidator,
    config *ServiceConfig,
) *Application[T, ID]
```

### 服务配置（`ServiceConfig`）

```go
type ServiceConfig struct {
    // 验证配置
    AutoValidate   bool  // 自动验证实体
    StrictValidate bool  // 严格验证模式
    
    // 时间戳配置
    AutoTimestamp  bool  // 自动设置时间戳
    
    // 软删除配置
    SoftDelete     bool  // 启用软删除
    
    // 审计配置
    EnableAudit    bool  // 启用审计日志
    AuditFields    bool  // 自动设置审计字段
    
    // 批量操作配置
    MaxBatchSize   int   // 最大批量操作数量
    BatchTimeout   int   // 批量操作超时（秒）
    
    // 缓存配置
    EnableCache    bool   // 启用缓存
    CacheTTL       int    // 缓存生存时间（秒）
    CacheKeyPrefix string // 缓存键前缀
    
    // 并发控制
    OptimisticLock bool  // 乐观锁
    MaxConcurrency int   // 最大并发数
    
    // 事务管理
    Transactional  bool   // 启用事务
    IsolationLevel string // 事务隔离级别
}
```

## 核心方法（Application Service）

### CRUD 操作

```go
// Create 创建实体
func (s *Application[T, ID]) Create(ctx context.Context, entity T) error

// GetByID 根据 ID 获取实体
func (s *Application[T, ID]) GetByID(ctx context.Context, id ID) (T, error)

// Update 更新实体
func (s *Application[T, ID]) Update(ctx context.Context, entity T) error

// Delete 删除实体
func (s *Application[T, ID]) Delete(ctx context.Context, id ID) error

// List 查询实体列表（支持分页、过滤、排序）
func (s *Application[T, ID]) List(ctx context.Context, opts *QueryOptions) ([]T, int64, error)
```

### 批量操作

```go
// CreateBatch 批量创建
func (s *Application[T, ID]) CreateBatch(ctx context.Context, entities []T) error

// UpdateBatch 批量更新
func (s *Application[T, ID]) UpdateBatch(ctx context.Context, entities []T) error

// DeleteBatch 批量删除
func (s *Application[T, ID]) DeleteBatch(ctx context.Context, ids []ID) error
```

## 使用示例（Application Service）

### 基础用法

```go
package main

import (
    "context"
    application "gochen/app/application"
    "gochen/domain/entity"
    "gochen/validation"
)

// 定义实体
type Product struct {
    ID    int64  `json:"id"`
    Name  string `json:"name" validate:"required"`
    Price int64  `json:"price" validate:"required,gt=0"`
}

func (p *Product) GetID() int64 { return p.ID }
func (p *Product) SetID(id int64) { p.ID = id }
func (p *Product) Validate() error { return nil }

func main() {
    ctx := context.Background()
    
    // 创建仓储
    productRepo := NewProductRepository()

    // 创建应用服务
    productService := application.NewApplication[*Product, int64](
        productRepo,
        validation.NewValidator(),
        &application.ServiceConfig{
            AutoValidate: true,
            EnableCache:  true,
            CacheTTL:     300,
        },
    )
    
    // 创建产品
    product := &Product{Name: "iPhone", Price: 5999}
    if err := productService.Create(ctx, product); err != nil {
        log.Fatal(err)
    }
    
    // 查询产品
    found, err := productService.GetByID(ctx, product.ID)
    if err != nil {
        log.Fatal(err)
    }
    
    // 更新产品
    found.Price = 5499
    if err := productService.Update(ctx, found); err != nil {
        log.Fatal(err)
    }
}
```

### 高级用法 - 事务

```go
// 在事务中执行多个操作
func transferMoney(ctx context.Context, service *Application[*Account, int64], 
                  fromID, toID int64, amount int64) error {
    // 开始事务
    txRepo := service.Repository().(repository.ITransactional)
    ctx, err := txRepo.BeginTx(ctx)
    if err != nil {
        return err
    }
    defer txRepo.Rollback(ctx)
    
    // 扣款
    from, err := service.GetByID(ctx, fromID)
    if err != nil {
        return err
    }
    from.Balance -= amount
    if err := service.Update(ctx, from); err != nil {
        return err
    }
    
    // 加款
    to, err := service.GetByID(ctx, toID)
    if err != nil {
        return err
    }
    to.Balance += amount
    if err := service.Update(ctx, to); err != nil {
        return err
    }
    
    // 提交事务
    return txRepo.Commit(ctx)
}
```

### 高级用法 - 自定义验证

```go
// 自定义验证逻辑
type ProductService struct {
    *app.Application[*Product, int64]
}

func (s *ProductService) Create(ctx context.Context, product *Product) error {
    // 自定义验证
    if product.Price > 100000 {
        return errors.New("price too high, requires approval")
    }
    
    // 调用基础服务
    return s.Application.Create(ctx, product)
}
```

## 配置场景（Application Service）

### 场景 1: 高性能读取服务

```go
config := &application.ServiceConfig{
    EnableCache:    true,
    CacheTTL:       600,      // 10分钟缓存
    SoftDelete:     false,    // 不需要软删除
    EnableAudit:    false,    // 不需要审计
    Transactional:  false,    // 只读不需要事务
}
```

### 场景 2: 金融交易服务

```go
config := &application.ServiceConfig{
    AutoValidate:    true,
    StrictValidate:  true,     // 严格验证
    EnableAudit:     true,     // 必须审计
    Transactional:   true,     // 必须事务
    OptimisticLock:  true,     // 防止并发冲突
    SoftDelete:      true,     // 不能物理删除
    IsolationLevel:  "SERIALIZABLE", // 最高隔离级别
}
```

### 场景 3: 内容管理系统

```go
config := &application.ServiceConfig{
    AutoValidate:    true,
    AutoTimestamp:   true,
    SoftDelete:      true,     // 支持内容恢复
    EnableAudit:     true,     // 记录编辑历史
    EnableCache:     true,
    CacheTTL:        300,
    MaxBatchSize:    50,       // 批量导入内容
}
```

## 最佳实践（Application Service）

### 1. 验证策略

```go
// 在服务层进行业务验证
func (s *ProductService) Create(ctx context.Context, product *Product) error {
    // 1. 基础验证（字段格式）
    if err := s.validator.Validate(product); err != nil {
        return err
    }
    
    // 2. 业务规则验证
    if product.Price < 0 {
        return errors.New("price cannot be negative")
    }
    
    // 3. 数据一致性验证
    exists, err := s.CheckDuplicateName(ctx, product.Name)
    if err != nil {
        return err
    }
    if exists {
        return errors.New("product name already exists")
    }
    
    return s.Application.Create(ctx, product)
}
```

### 2. 错误处理

```go
// 统一错误处理
func (s *ProductService) GetByID(ctx context.Context, id int64) (*Product, error) {
    product, err := s.Application.GetByID(ctx, id)
    if err != nil {
        // 记录日志
        s.logger.Error(ctx, "failed to get product", "id", id, "error", err)
        
        // 转换为用户友好的错误
        if errors.Is(err, repository.ErrNotFound) {
            return nil, fmt.Errorf("product %d not found", id)
        }
        
        return nil, fmt.Errorf("failed to get product: %w", err)
    }
    
    return product, nil
}
```

### 3. 分页查询

```go
// 分页查询
func (s *ProductService) ListProducts(ctx context.Context, page, size int) ([]*Product, int64, error) {
    opts := &app.QueryOptions{
        Pagination: &app.PaginationOption{
            Page: page,
            Size: size,
        },
        Sort: &app.SortOption{
            Field: "created_at",
            Order: "desc",
        },
    }
    
    return s.Application.List(ctx, opts)
}
```

## 事件溯源应用模板（`app/eventsourced`）

在应用层中，`app/eventsourced` 提供了基于领域事件与事件存储的事件溯源模板，实现：

- 基于 `domain/eventsourced.IEventSourcedAggregate` 的通用仓储实现 `EventSourcedRepository`（依赖 `eventing/store.IEventStore[int64]`、可选快照与事件总线）；
- Outbox 装饰器 `OutboxAwareRepository`，与 `eventing/outbox` 协作实现“写事件 + 写 Outbox”原子操作；
- 历史视图与分页查询（`GetEventHistory*`）；
- 泛型事件处理器与投影模板（`EventSourcedTypedHandler`、`EventSourcedProjection`）；
- 与 `domain/eventsourced.EventSourcedService` 协同的命令适配器 `AsCommandMessageHandler`，便于挂到 `messaging/command.CommandBus` 上。

典型使用路径：

1. 在领域层基于 `domain/eventsourced` 定义聚合与领域事件；
2. 在应用层通过 `app/eventsourced.NewEventSourcedRepository` 组合 EventStore / SnapshotManager / EventBus；
3. 使用 `domain/eventsourced.EventSourcedService` 作为命令执行模板，并通过 `AsCommandMessageHandler` 接入命令总线；
4. 使用 `EventSourcedProjection` 与 `eventing/projection.ProjectionManager` 构建 CQRS 读模型。

具体 API 说明与完整示例请参考下方相关文档。

## 相关文档

- [RESTful API 构建器](./api/README.md) - 自动生成 RESTful API
- 事件溯源与 CQRS：
  - `docs/framework-design.md` 第 2.3 节（`domain/eventsourced` + `app/eventsourced`）
  - `docs/ddd-eventsourcing-quick-reference.md`
- 领域层：`domain/` 目录（聚合、仓储与服务抽象）
- 示例代码：`examples/` 目录（包括 CRUD/Audited/EventSourced 等完整使用示例）

## 许可证

MIT License
