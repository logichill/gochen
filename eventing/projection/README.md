# Projection Management - 投影管理

投影管理器负责将事件流转换为读模型，实现 CQRS 模式的查询端。

## 概述

投影（Projection）是 CQRS 模式的核心组件：
- **读写分离** - 写模型（事件）和读模型（投影）分离
- **优化查询** - 针对查询场景优化的数据模型
- **最终一致性** - 异步更新，保证最终一致
- **多视图** - 同一事件流可生成多个不同视图

## 核心接口（当前实现）

### IProjection - 投影接口

```go
// IProjection 投影接口
type IProjection interface {
    // 获取投影名称
    GetName() string

    // 处理单个事件，更新读模型
    Handle(ctx context.Context, event eventing.IEvent) error

    // 声明支持的事件类型（用于事件总线订阅）
    GetSupportedEventTypes() []string

    // 重建投影（通常基于事件存储中的历史事件）
    Rebuild(ctx context.Context, events []eventing.Event) error

    // 获取当前投影状态（监控/观测用）
    GetStatus() ProjectionStatus
}
```

### ProjectionManager - 投影管理器

```go
// ProjectionManager 负责：
// - 管理多个 IProjection 实例
// - 通过 event bus 订阅事件并分发给对应投影
// - 维护每个投影的状态（Processed/Failed/LastEvent 等）
// - 可选：基于 CheckpointStore 进行检查点管理
type ProjectionManager struct {
    // ...
}

// NewProjectionManager 创建默认配置的投影管理器
func NewProjectionManager(
    eventStore store.IEventStore,
    eventBus   bus.IEventBus,
) *ProjectionManager

// WithCheckpointStore 启用检查点存储（可选）
func (pm *ProjectionManager) WithCheckpointStore(store ICheckpointStore) *ProjectionManager

// RegisterProjection 注册投影并自动在 event bus 上订阅其声明的事件类型
func (pm *ProjectionManager) RegisterProjection(projection IProjection) error

// UnregisterProjection 取消注册并取消订阅
func (pm *ProjectionManager) UnregisterProjection(name string) error

// StartProjection/StopProjection 控制单个投影的运行状态
func (pm *ProjectionManager) StartProjection(name string) error
func (pm *ProjectionManager) StopProjection(name string) error
```

## 使用示例

### 1. 定义投影

```go
package projection

import (
    "context"
    "gochen/eventing"
)

// UserViewProjection 用户视图投影
type UserViewProjection struct {
    db IDatabase
}

func NewUserViewProjection(db IDatabase) *UserViewProjection {
    return &UserViewProjection{db: db}
}

func (p *UserViewProjection) GetName() string {
    return "user_view"
}

func (p *UserViewProjection) Handle(ctx context.Context, event eventing.IEvent) error {
    switch e := event.(type) {
    case *UserCreated:
        return p.handleUserCreated(ctx, e)
    case *UserUpdated:
        return p.handleUserUpdated(ctx, e)
    case *UserDeleted:
        return p.handleUserDeleted(ctx, e)
    default:
        return nil // 忽略不相关事件
    }
}

func (p *UserViewProjection) handleUserCreated(ctx context.Context, e *UserCreated) error {
    view := &UserView{
        ID:        e.UserID,
        Name:      e.Name,
        Email:     e.Email,
        CreatedAt: e.GetTimestamp(),
    }
    
    return p.db.Insert(ctx, "user_views", view)
}

func (p *UserViewProjection) handleUserUpdated(ctx context.Context, e *UserUpdated) error {
    return p.db.Update(ctx, "user_views", 
        map[string]any{
            "name":       e.Name,
            "email":      e.Email,
            "updated_at": e.GetTimestamp(),
        },
        "id = ?", e.UserID)
}

func (p *UserViewProjection) handleUserDeleted(ctx context.Context, e *UserDeleted) error {
    return p.db.Delete(ctx, "user_views", "id = ?", e.UserID)
}

func (p *UserViewProjection) Reset(ctx context.Context) error {
    // 清空视图表
    return p.db.Exec(ctx, "TRUNCATE TABLE user_views")
}
```

### 2. 注册和启动投影（基于 ProjectionManager）

```go
// 创建投影管理器（集成事件总线和事件存储）
eventStore := store.NewMemoryEventStore()
eventBus   := bus.NewInMemoryEventBus()

projectionManager := projection.NewProjectionManager(eventStore, eventBus)

// 注册投影（会根据 GetSupportedEventTypes 自动在 event bus 上订阅）
if err := projectionManager.RegisterProjection(NewUserViewProjection(db)); err != nil {
    log.Fatal(err)
}

// 启动指定投影
if err := projectionManager.StartProjection("user_view"); err != nil {
    log.Fatal(err)
}
defer projectionManager.StopProjection("user_view")
```

### 3. 读模型查询

```go
// UserViewRepository 用户视图仓储
type UserViewRepository struct {
    db IDatabase
}

func (r *UserViewRepository) GetByID(ctx context.Context, id int64) (*UserView, error) {
    var view UserView
    err := r.db.QueryRow(ctx, 
        "SELECT * FROM user_views WHERE id = ?", id).Scan(&view)
    return &view, err
}

func (r *UserViewRepository) List(ctx context.Context, opts *QueryOptions) ([]*UserView, error) {
    query := "SELECT * FROM user_views WHERE 1=1"
    args := []any{}
    
    // 应用过滤
    if opts.Status != "" {
        query += " AND status = ?"
        args = append(args, opts.Status)
    }
    
    // 应用排序
    query += " ORDER BY created_at DESC"
    
    // 应用分页
    query += " LIMIT ? OFFSET ?"
    args = append(args, opts.Size, (opts.Page-1)*opts.Size)
    
    rows, err := r.db.Query(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    var views []*UserView
    for rows.Next() {
        var view UserView
        if err := rows.Scan(&view); err != nil {
            return nil, err
        }
        views = append(views, &view)
    }
    
    return views, nil
}
```

## 高级特性

### 1. 复杂聚合投影

```go
// OrderStatisticsProjection 订单统计投影
type OrderStatisticsProjection struct {
    db IDatabase
}

func (p *OrderStatisticsProjection) Handle(ctx context.Context, event eventing.IEvent) error {
    switch e := event.(type) {
    case *OrderCreated:
        return p.incrementDailyOrders(ctx, e.GetTimestamp())
    case *OrderCompleted:
        return p.updateRevenue(ctx, e.GetTimestamp(), e.Total)
    case *OrderCancelled:
        return p.incrementCancelledOrders(ctx, e.GetTimestamp())
    }
    return nil
}

func (p *OrderStatisticsProjection) incrementDailyOrders(ctx context.Context, timestamp time.Time) error {
    date := timestamp.Format("2006-01-02")
    
    return p.db.Exec(ctx, `
        INSERT INTO order_statistics (date, total_orders, total_revenue, cancelled_orders)
        VALUES (?, 1, 0, 0)
        ON DUPLICATE KEY UPDATE total_orders = total_orders + 1
    `, date)
}

func (p *OrderStatisticsProjection) updateRevenue(ctx context.Context, timestamp time.Time, amount int64) error {
    date := timestamp.Format("2006-01-02")
    
    return p.db.Exec(ctx, `
        UPDATE order_statistics 
        SET total_revenue = total_revenue + ? 
        WHERE date = ?
    `, amount, date)
}
```

### 2. 多表投影

```go
// ProductCatalogProjection 产品目录投影（包含多个表）
type ProductCatalogProjection struct {
    db IDatabase
}

func (p *ProductCatalogProjection) Handle(ctx context.Context, event eventing.IEvent) error {
    switch e := event.(type) {
    case *ProductCreated:
        return p.handleProductCreated(ctx, e)
    case *ProductPriceChanged:
        return p.handlePriceChanged(ctx, e)
    case *ProductImageAdded:
        return p.handleImageAdded(ctx, e)
    }
    return nil
}

func (p *ProductCatalogProjection) handleProductCreated(ctx context.Context, e *ProductCreated) error {
    // 1. 插入产品主表
    if err := p.db.Insert(ctx, "products", &Product{
        ID:          e.ProductID,
        Name:        e.Name,
        Description: e.Description,
        Price:       e.Price,
    }); err != nil {
        return err
    }
    
    // 2. 插入分类关联表
    if err := p.db.Insert(ctx, "product_categories", &ProductCategory{
        ProductID:  e.ProductID,
        CategoryID: e.CategoryID,
    }); err != nil {
        return err
    }
    
    return nil
}

func (p *ProductCatalogProjection) handleImageAdded(ctx context.Context, e *ProductImageAdded) error {
    // 插入产品图片表
    return p.db.Insert(ctx, "product_images", &ProductImage{
        ProductID: e.ProductID,
        URL:       e.ImageURL,
        IsPrimary: e.IsPrimary,
    })
}
```

### 3. 投影状态追踪

```go
// ProjectionState 投影状态
type ProjectionState struct {
    Name            string    `db:"name"`
    LastEventID     string    `db:"last_event_id"`
    LastEventTime   time.Time `db:"last_event_time"`
    LastProcessedAt time.Time `db:"last_processed_at"`
    ErrorCount      int       `db:"error_count"`
    LastError       string    `db:"last_error"`
}

// 保存投影状态
func (p *BaseProjection) saveState(ctx context.Context, event eventing.IEvent) error {
    state := &ProjectionState{
        Name:            p.GetName(),
        LastEventID:     event.GetID(),
        LastEventTime:   event.GetTimestamp(),
        LastProcessedAt: time.Now(),
    }
    
    return p.db.Upsert(ctx, "projection_states", state, "name")
}

// 恢复投影（从上次位置继续）
func (m *ProjectionManager) Resume(ctx context.Context, projectionName string) error {
    // 加载投影状态
    var state ProjectionState
    err := m.db.QueryRow(ctx, 
        "SELECT * FROM projection_states WHERE name = ?", 
        projectionName).Scan(&state)
    
    if err != nil {
        return err
    }
    
    // 从上次位置继续处理事件
    events, err := m.eventStore.LoadEventsAfter(ctx, state.LastEventTime)
    if err != nil {
        return err
    }
    
    for _, event := range events {
        if err := projection.Handle(ctx, event); err != nil {
            return err
        }
    }
    
    return nil
}
```

### 4. 投影重建

```go
// 重建投影（从头重新处理所有事件）
func (m *ProjectionManager) Rebuild(ctx context.Context, projectionName string) error {
    projection, ok := m.projections[projectionName]
    if !ok {
        return fmt.Errorf("projection not found: %s", projectionName)
    }
    
    // 1. 重置投影
    if err := projection.Reset(ctx); err != nil {
        return fmt.Errorf("failed to reset projection: %w", err)
    }
    
    // 2. 重新处理所有事件（优先使用游标接口，避免一次性拉取过多数据）
    if extended, ok := m.eventStore.(store.IEventStoreExtended[int64]); ok {
        stream, err := extended.GetEventStreamWithCursor(ctx, &store.StreamOptions{Limit: 500})
        if err != nil {
            return err
        }
        for _, event := range stream.Events {
            if err := projection.Handle(ctx, event); err != nil {
                log.Printf("Error handling event in rebuild: %v", err)
            }
        }
    } else {
        events, err := m.eventStore.StreamEvents(ctx, time.Time{})
        if err != nil {
            return err
        }
        for _, event := range events {
            if err := projection.Handle(ctx, event); err != nil {
                log.Printf("Error handling event in rebuild: %v", err)
            }
        }
    }
    
    return nil
}
```

## 最佳实践

### 1. 投影幂等性

```go
// 确保投影处理幂等
func (p *UserViewProjection) handleUserCreated(ctx context.Context, e *UserCreated) error {
    // 使用 INSERT ... ON DUPLICATE KEY UPDATE 或 UPSERT
    return p.db.Exec(ctx, `
        INSERT INTO user_views (id, name, email, created_at)
        VALUES (?, ?, ?, ?)
        ON DUPLICATE KEY UPDATE 
            name = VALUES(name),
            email = VALUES(email)
    `, e.UserID, e.Name, e.Email, e.GetTimestamp())
}
```

### 2. 错误处理

```go
// 投影错误处理策略
func (p *BaseProjection) Handle(ctx context.Context, event eventing.IEvent) error {
    // 使用事务确保原子性
    return p.db.WithTransaction(ctx, func(tx ITransaction) error {
        // 处理事件
        if err := p.handleEvent(ctx, event); err != nil {
            // 记录错误
            p.logError(ctx, event, err)
            
            // 区分可恢复和不可恢复错误
            if isRecoverable(err) {
                return err // 重试
            }
            
            // 不可恢复错误：记录但继续处理
            log.Printf("Non-recoverable error in projection %s: %v", 
                p.GetName(), err)
            return nil
        }
        
        // 更新投影状态
        return p.saveState(ctx, event)
    })
}
```

### 3. 性能优化

```go
// 批量处理事件
type BatchedProjection struct {
    projection IProjection
    batchSize  int
    buffer     []eventing.IEvent
}

func (p *BatchedProjection) Handle(ctx context.Context, event eventing.IEvent) error {
    p.buffer = append(p.buffer, event)
    
    if len(p.buffer) >= p.batchSize {
        return p.flush(ctx)
    }
    
    return nil
}

func (p *BatchedProjection) flush(ctx context.Context) error {
    if len(p.buffer) == 0 {
        return nil
    }
    
    // 在事务中批量处理
    return p.db.WithTransaction(ctx, func(tx ITransaction) error {
        for _, event := range p.buffer {
            if err := p.projection.Handle(ctx, event); err != nil {
                return err
            }
        }
        p.buffer = nil
        return nil
    })
}
```

## 监控和运维

### 投影健康检查

```go
// 检查投影健康状态
func (m *ProjectionManager) HealthCheck(ctx context.Context) map[string]string {
    health := make(map[string]string)
    
    for name, projection := range m.projections {
        state, err := m.getProjectionState(ctx, name)
        if err != nil {
            health[name] = "error: " + err.Error()
            continue
        }
        
        // 检查延迟
        lag := time.Since(state.LastEventTime)
        if lag > time.Minute*5 {
            health[name] = fmt.Sprintf("lagging: %v behind", lag)
        } else {
            health[name] = "healthy"
        }
    }
    
    return health
}
```

## 相关文档

- [事件存储](../store/README.md) - 事件持久化
- [事件总线](../bus/README.md) - 事件发布订阅
- [领域仓储](../../domain/repository/README.md) - 仓储模式

## 许可证

MIT License
