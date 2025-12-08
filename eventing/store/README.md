# Event Store - 事件存储

事件存储是事件溯源架构的核心组件，负责持久化和检索领域事件。

## 概述

事件存储（EventStore）提供：
- **事件持久化** - 将领域事件永久保存
- **事件检索** - 按聚合ID加载历史事件
- **事件流** - 支持事件流式读取和订阅
- **并发控制** - 乐观锁保证数据一致性
- **快照优化** - 减少事件重放开销

## 核心接口

### IEventStore - 事件存储核心接口

```go
// IEventStore 定义事件存储的核心接口（最小化设计）
// ID 为聚合根 ID 类型，常见为 int64
type IEventStore[ID comparable] interface {
    // AppendEvents 追加事件到指定聚合的事件流
    //
    // 参数：
    //   - ctx: 上下文
    //   - aggregateID: 聚合根ID
    //   - events: 待追加的事件列表
    //   - expectedVersion: 期望的当前版本，用于乐观锁控制
    //
    // 返回：
    //   - error: 版本冲突返回 ConcurrencyError
    AppendEvents(ctx context.Context, aggregateID ID,
                 events []IEvent, expectedVersion uint64) error
    
    // LoadEvents 加载聚合的事件历史
    //
    // 参数：
    //   - ctx: 上下文
    //   - aggregateID: 聚合根ID
    //   - afterVersion: 起始版本（0表示从头开始）
    //
    // 返回：
    //   - []IEvent: 事件列表
    //   - error: 错误
    LoadEvents(ctx context.Context, aggregateID ID,
               afterVersion uint64) ([]IEvent, error)
    
    // StreamEvents 拉取指定时间后的事件列表（按时间升序）
    //
    // 说明：
    //   - 为兼顾向后兼容，基础接口仅提供时间窗口拉取；
    //   - 需要分页/类型过滤/游标时，推荐实现并使用 IEventStoreExtended.GetEventStreamWithCursor。
    StreamEvents(ctx context.Context, fromTime time.Time) ([]eventing.Event, error)
}
```

### IAggregateInspector - 聚合检查器接口

```go
// IAggregateInspector 聚合检查器接口
type IAggregateInspector[ID comparable] interface {
    // HasAggregate 检查聚合是否存在
    HasAggregate(ctx context.Context, aggregateID ID) (bool, error)
    
    // GetAggregateVersion 获取聚合当前版本
    GetAggregateVersion(ctx context.Context, aggregateID ID) (uint64, error)
}
```

### ITypedEventStore - 类型化事件存储接口

```go
// ITypedEventStore 类型化事件存储接口
type ITypedEventStore[ID comparable] interface {
    IEventStore[ID]
    
    // LoadEventsByType 按聚合类型加载事件
    LoadEventsByType(ctx context.Context, aggregateType string,
                     aggregateID ID, afterVersion uint64) ([]IEvent, error)
}
```

## 使用示例

### 1. 保存事件

```go
package main

import (
    "context"
    "gochen/eventing"
    "gochen/eventing/store"
)

func main() {
    ctx := context.Background()
    
    // 创建事件存储
    eventStore := store.NewSQLEventStore(db)
    
    // 定义事件
    events := []eventing.IEvent{
        &UserCreated{
            EventBase: eventing.NewEventBase(1, "UserCreated", 1),
            Name:      "张三",
            Email:     "zhangsan@example.com",
        },
    }
    
    // 保存事件（expectedVersion=0 表示新聚合）
    err := eventStore.AppendEvents(ctx, 1, events, 0)
    if err != nil {
        log.Fatal(err)
    }
}
```

### 2. 加载事件

```go
// 加载聚合的所有事件
events, err := eventStore.LoadEvents(ctx, aggregateID, 0)
if err != nil {
    log.Fatal(err)
}

// 重放事件重建聚合状态
aggregate := &User{ID: aggregateID}
for _, event := range events {
    event.Apply(aggregate)
}
```

### 3. 乐观锁控制

```go
// 加载聚合
events, err := eventStore.LoadEvents(ctx, aggregateID, 0)
currentVersion := uint64(len(events))

// 业务逻辑产生新事件
newEvents := []eventing.IEvent{
    &UserEmailChanged{
        EventBase: eventing.NewEventBase(aggregateID, "UserEmailChanged", 1),
        NewEmail:  "newemail@example.com",
    },
}

// 保存时检查版本
err = eventStore.AppendEvents(ctx, aggregateID, newEvents, currentVersion)
if err != nil {
    if errors.Is(err, store.ErrConcurrencyConflict) {
        // 版本冲突，需要重试
        log.Println("Concurrency conflict, retrying...")
    }
    log.Fatal(err)
}
```

### 4. 增量读取事件

```go
// 推荐：若实现了 IEventStoreExtended[int64]，使用游标 + limit 方式读取
if extended, ok := eventStore.(store.IEventStoreExtended[int64]); ok {
    stream, err := extended.GetEventStreamWithCursor(ctx, &store.StreamOptions{
        FromTime: time.Now().Add(-24 * time.Hour),              // 最近24小时
        Types:    []string{"UserCreated", "UserUpdated"},       // 可选类型过滤
        Limit:    500,                                          // 单批限制，避免一次性取太多
    })
    if err != nil {
        log.Fatal(err)
    }
    for _, event := range stream.Events {
        log.Printf("Event: %s at %s", event.GetType(), event.GetTimestamp())
    }
}

// 回退：仅有基础 IEventStore[int64] 时，先取时间窗口，再用 FilterEventsWithOptions 过滤
events, err := eventStore.StreamEvents(ctx, time.Now().Add(-24*time.Hour))
if err != nil {
    log.Fatal(err)
}
filtered := store.FilterEventsWithOptions(events, &store.StreamOptions{
    Types: []string{"UserCreated", "UserUpdated"},
    Limit: 500,
})
for _, event := range filtered.Events {
    log.Printf("Event: %s at %s", event.GetType(), event.GetTimestamp())
}
```

## 高级特性

### 聚合级 helper 与泛型支持

除了 `int64` 聚合 ID 的便捷函数外，store 包还提供了针对泛型 `ID` 的聚合级 helper：

```go
// 泛型版本：适用于任意 ID 形态的 IEventStore[ID]
exists, err := store.AggregateExistsGeneric[string](ctx, stringEventStore, "ACC-1001")
version, err := store.GetCurrentVersionGeneric[string](ctx, stringEventStore, "ACC-1001")
```

使用原则：
- 事件存储实现若同时实现了 `IAggregateInspector[ID]`，helper 会优先调用 `HasAggregate` / `GetAggregateVersion`，避免全量回放；
- 否则退回到 `LoadEvents(..., 0)` 的实现。

针对 `int64` 的便捷函数（`AggregateExists`/`GetCurrentVersion` 等）会在内部延续当前行为，后续可以根据需要迁移到泛型版本。

### 游标与类型过滤（推荐）

为避免在事件表上做全表扫描，可以优先使用 `IEventStoreExtended[int64].GetEventStreamWithCursor`：

```go
stream, err := eventStore.(store.IEventStoreExtended[int64]).GetEventStreamWithCursor(ctx, &store.StreamOptions{
    After:    lastCursor,                // 上次处理的事件 ID（可为空）
    Types:    []string{"UserUpdated"},   // 可选：按事件类型过滤
    FromTime: lastEventTime,             // 可选：时间窗口
    Limit:    500,                       // 限制单次批量
})
if err != nil {
    // 处理错误
}
for _, evt := range stream.Events {
    // 处理事件
}
nextCursor := stream.NextCursor
```

常用实现支持情况：
- SQL/Memory/Cached/Metrics 事件存储均实现 `IEventStoreExtended[int64]`
- 若当前实例不支持，可回退到 `StreamEvents` 并使用 `store.FilterEventsWithOptions` 过滤

### SQL 索引建议

为提升游标与类型过滤性能，建议在事件表上添加组合索引：
- `(timestamp, id)`：支持时间窗口 + 游标的顺序扫描
- `(type, timestamp)`：按事件类型过滤时减少回表
- 常见 schema（`event_store`）：可新增示例索引
```sql
CREATE INDEX idx_event_store_timestamp_id ON event_store (timestamp, id);
CREATE INDEX idx_event_store_type_timestamp ON event_store (type, timestamp);
```

### 1. 快照支持

快照可以减少事件重放开销：

```go
// 使用快照存储
snapshotStore := snapshot.NewSnapshotStore(db)

// 保存快照（每100个事件）
if len(events) % 100 == 0 {
    snapshot := &Snapshot{
        AggregateID: aggregateID,
        Version:     currentVersion,
        State:       serializeAggregate(aggregate),
    }
    snapshotStore.SaveSnapshot(ctx, snapshot)
}

// 加载时先检查快照
snapshot, err := snapshotStore.LoadSnapshot(ctx, aggregateID)
if err == nil {
    // 从快照恢复
    aggregate = deserializeAggregate(snapshot.State)
    // 只需重放快照之后的事件
    events, _ := eventStore.LoadEvents(ctx, aggregateID, snapshot.Version)
} else {
    // 从头重放所有事件
    events, _ := eventStore.LoadEvents(ctx, aggregateID, 0)
}
```

### 2. 游标分页查询

```go
// 使用游标查询事件流
cursor := ""
for {
    result, err := eventStore.GetEventStreamWithCursor(ctx, &store.StreamOptions{
        Cursor: cursor,
        Limit:  100,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // 处理事件
    for _, event := range result.Events {
        processEvent(event)
    }
    
    // 检查是否还有更多
    if !result.HasMore {
        break
    }
    cursor = result.NextCursor
}
```

### 3. 事件过滤

```go
// 按时间范围过滤
opts := &store.StreamOptions{
    FromTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
    ToTime:   time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC),
}

// 按事件类型过滤
opts := &store.StreamOptions{
    Types: []string{"UserCreated", "UserUpdated", "UserDeleted"},
}

// 按聚合类型过滤
opts := &store.StreamOptions{
    AggregateTypes: []string{"User", "Order"},
}

// 组合过滤
opts := &store.StreamOptions{
    FromTime:       time.Now().Add(-7 * 24 * time.Hour),
    Types:          []string{"OrderCreated"},
    AggregateTypes: []string{"Order"},
    Limit:          1000,
}
```

## SQL 存储实现

### 表结构

```sql
CREATE TABLE events (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    aggregate_id BIGINT NOT NULL,
    aggregate_type VARCHAR(255) NOT NULL,
    event_type VARCHAR(255) NOT NULL,
    event_data TEXT NOT NULL,
    version BIGINT NOT NULL,
    timestamp DATETIME NOT NULL,
    metadata TEXT,
    INDEX idx_aggregate (aggregate_id, version),
    INDEX idx_type (event_type),
    INDEX idx_timestamp (timestamp),
    UNIQUE KEY uk_aggregate_version (aggregate_id, version)
);
```

### 并发控制

SQL 存储使用唯一索引实现乐观锁：

```go
func (s *SQLEventStore) AppendEvents(ctx context.Context, aggregateID int64, 
                                     events []IEvent, expectedVersion uint64) error {
    tx, err := s.db.Begin(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback()
    
    // 检查当前版本
    var currentVersion uint64
    err = tx.QueryRow(ctx, 
        "SELECT COALESCE(MAX(version), 0) FROM events WHERE aggregate_id = ?", 
        aggregateID).Scan(&currentVersion)
    if err != nil {
        return err
    }
    
    // 版本检查
    if currentVersion != expectedVersion {
        return ErrConcurrencyConflict
    }
    
    // 插入事件
    for i, event := range events {
        version := expectedVersion + uint64(i) + 1
        _, err = tx.Exec(ctx,
            "INSERT INTO events (aggregate_id, event_type, event_data, version, timestamp) VALUES (?, ?, ?, ?, ?)",
            aggregateID, event.GetType(), serializeEvent(event), version, event.GetTimestamp())
        if err != nil {
            return err
        }
    }
    
    return tx.Commit()
}
```

## 最佳实践

### 1. 事件命名

```go
// ✅ 正确：使用过去式，描述已发生的事实
type UserCreated struct { ... }
type OrderPlaced struct { ... }
type MoneyWithdrawn struct { ... }

// ❌ 错误：使用现在式或命令式
type CreateUser struct { ... }
type PlaceOrder struct { ... }
type WithdrawMoney struct { ... }
```

### 2. 事件不可变

```go
// ✅ 正确：事件字段不可变
type UserCreated struct {
    eventing.EventBase
    UserID int64  // 只读
    Name   string // 只读
}

// ❌ 错误：事件字段可修改
type UserCreated struct {
    eventing.EventBase
    UserID *int64  // 可能被修改
    Name   *string // 可能被修改
}
```

### 3. 版本控制

```go
// 为事件添加版本号
type UserCreatedV1 struct {
    eventing.EventBase
    UserID int64
    Name   string
}

type UserCreatedV2 struct {
    eventing.EventBase
    UserID int64
    Name   string
    Email  string // 新增字段
}

// 使用升级器处理旧版本事件
func UpgradeUserCreatedV1ToV2(v1 *UserCreatedV1) *UserCreatedV2 {
    return &UserCreatedV2{
        EventBase: v1.EventBase,
        UserID:    v1.UserID,
        Name:      v1.Name,
        Email:     "", // 默认值
    }
}
```

## 相关文档

- [事件总线](../bus/README.md) - 事件发布和订阅
- [Outbox 模式](../outbox/README.md) - 可靠事件发布
- [投影管理](../projection/README.md) - 读模型更新
- [领域实体](../../domain/entity/README.md) - 事件溯源聚合

## 许可证

MIT License
