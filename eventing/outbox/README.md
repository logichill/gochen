# Outbox Pattern - 出箱模式

Outbox 模式确保事件可靠发布，解决分布式事务中的一致性问题。

## 概述

Outbox 模式通过以下方式保证事件可靠发布：
- **本地事务** - 业务数据和事件在同一事务中保存
- **异步发布** - 后台任务异步发布待发送事件
- **至少一次** - 确保事件至少被发布一次
- **重试机制** - 失败自动重试，指数退避

## 核心接口

### IOutboxRepository - Outbox 仓储接口

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
    
    // Delete 删除已发布事件（清理）
    Delete(ctx context.Context, before time.Time) error
}
```

### IOutboxPublisher - Outbox 发布器接口

```go
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

## 使用示例

### 1. 保存事件到 Outbox

```go
package main

import (
    "context"
    "gochen/eventing/outbox"
)

func createUser(ctx context.Context, user *User) error {
    // 开始事务
    tx, err := db.Begin(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback()
    
    // 1. 保存业务数据
    if err := tx.Insert(ctx, "users", user); err != nil {
        return err
    }
    
    // 2. 保存事件到 Outbox（同一事务）
    event := &UserCreated{
        UserID: user.ID,
        Name:   user.Name,
        Email:  user.Email,
    }
    
    entry := outbox.EventToOutboxEntry(event)
    if err := outboxRepo.Save(ctx, entry); err != nil {
        return err
    }
    
    // 3. 提交事务
    return tx.Commit()
}
```

### 2. 启动 Outbox Publisher

```go
// 创建 Outbox 发布器
publisher := outbox.NewPublisher(
    outboxRepo,
    eventBus,
    &outbox.PublisherConfig{
        PollInterval:    time.Second,     // 轮询间隔
        BatchSize:       100,              // 每次处理数量
        MaxRetries:      3,                // 最大重试次数
        RetryInterval:   time.Second * 5,  // 重试间隔
        CleanupInterval: time.Hour,        // 清理间隔
        CleanupAfter:    time.Hour * 24,   // 清理24小时前的事件
    },
)

// 启动发布器
if err := publisher.Start(ctx); err != nil {
    log.Fatal(err)
}
defer publisher.Stop()
```

### 3. 手动触发发布

```go
// 立即发布待发布事件（而非等待轮询）
if err := publisher.PublishPending(ctx); err != nil {
    log.Printf("Failed to publish pending events: %v", err)
}
```

## Outbox Entry 结构

```go
// OutboxEntry Outbox 表条目
type OutboxEntry struct {
    ID           int64      `db:"id"`
    EventID      string     `db:"event_id"`
    EventType    string     `db:"event_type"`
    EventData    []byte     `db:"event_data"`
    AggregateID  int64      `db:"aggregate_id"`
    CreatedAt    time.Time  `db:"created_at"`
    PublishedAt  *time.Time `db:"published_at"`
    Status       string     `db:"status"` // pending/published/failed
    RetryCount   int        `db:"retry_count"`
    LastError    string     `db:"last_error"`
    NextRetryAt  *time.Time `db:"next_retry_at"`
}
```

## 数据库表结构

```sql
CREATE TABLE outbox_events (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    event_id VARCHAR(255) NOT NULL UNIQUE,
    event_type VARCHAR(255) NOT NULL,
    event_data TEXT NOT NULL,
    aggregate_id BIGINT NOT NULL,
    created_at DATETIME NOT NULL,
    published_at DATETIME,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    retry_count INT NOT NULL DEFAULT 0,
    last_error TEXT,
    next_retry_at DATETIME,
    INDEX idx_status_retry (status, next_retry_at),
    INDEX idx_created_at (created_at)
);
```

## 高级特性

### 1. 重试策略

```go
// 指数退避重试
func (e *OutboxEntry) ShouldRetry(maxRetries int) bool {
    return e.RetryCount < maxRetries
}

func (e *OutboxEntry) CalculateNextRetryTime() time.Time {
    // 指数退避：1s, 2s, 4s, 8s, 16s...
    delay := time.Duration(1<<e.RetryCount) * time.Second
    if delay > time.Hour {
        delay = time.Hour // 最大延迟1小时
    }
    return time.Now().Add(delay)
}
```

### 2. 批量发布

```go
func (p *Publisher) publishBatch(ctx context.Context, entries []*OutboxEntry) error {
    events := make([]eventing.IEvent, 0, len(entries))
    
    // 转换为事件
    for _, entry := range entries {
        event, err := entry.ToEvent()
        if err != nil {
            log.Printf("Failed to deserialize event %s: %v", entry.EventID, err)
            p.repo.MarkFailed(ctx, entry.ID, err)
            continue
        }
        events = append(events, event)
    }
    
    // 批量发布
    if err := p.eventBus.PublishAll(ctx, events); err != nil {
        return err
    }
    
    // 标记已发布
    for _, entry := range entries {
        p.repo.MarkPublished(ctx, entry.ID)
    }
    
    return nil
}
```

### 3. 定期清理

```go
// 清理已发布的旧事件
func (p *Publisher) cleanup(ctx context.Context) error {
    cutoff := time.Now().Add(-p.config.CleanupAfter)
    return p.repo.Delete(ctx, cutoff)
}

// 在后台定期执行
go func() {
    ticker := time.NewTicker(p.config.CleanupInterval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            if err := p.cleanup(ctx); err != nil {
                log.Printf("Cleanup error: %v", err)
            }
        case <-ctx.Done():
            return
        }
    }
}()
```

### 4. 监控和指标

```go
// 发布器指标
type PublisherMetrics struct {
    TotalPublished   int64
    TotalFailed      int64
    PendingCount     int64
    LastPublishTime  time.Time
}

func (p *Publisher) GetMetrics(ctx context.Context) (*PublisherMetrics, error) {
    metrics := &PublisherMetrics{
        TotalPublished: p.publishedCount.Load(),
        TotalFailed:    p.failedCount.Load(),
    }
    
    // 查询待发布数量
    pending, err := p.repo.GetPending(ctx, 1)
    if err != nil {
        return nil, err
    }
    metrics.PendingCount = int64(len(pending))
    
    return metrics, nil
}
```

## 最佳实践

### 1. 事务边界

```go
// ✅ 正确：业务数据和 Outbox 在同一事务
func (s *OrderService) CreateOrder(ctx context.Context, order *Order) error {
    return s.db.WithTransaction(ctx, func(tx ITransaction) error {
        // 保存订单
        if err := tx.Insert(ctx, "orders", order); err != nil {
            return err
        }
        
        // 保存事件到 Outbox
        event := &OrderCreated{OrderID: order.ID}
        entry := outbox.EventToOutboxEntry(event)
        if err := s.outboxRepo.Save(ctx, entry); err != nil {
            return err
        }
        
        return nil
    })
}

// ❌ 错误：分开两个事务
func (s *OrderService) CreateOrder(ctx context.Context, order *Order) error {
    // 事务1：保存订单
    if err := s.orderRepo.Save(ctx, order); err != nil {
        return err
    }
    
    // 事务2：保存 Outbox（可能失败！）
    event := &OrderCreated{OrderID: order.ID}
    entry := outbox.EventToOutboxEntry(event)
    return s.outboxRepo.Save(ctx, entry)
}
```

### 2. 幂等性保证

```go
// 使用唯一的事件 ID 确保幂等
func EventToOutboxEntry(event eventing.IEvent) *OutboxEntry {
    return &OutboxEntry{
        EventID:     event.GetID(),      // 唯一ID
        EventType:   event.GetType(),
        EventData:   serializeEvent(event),
        AggregateID: event.GetAggregateID(),
        CreatedAt:   event.GetTimestamp(),
        Status:      "pending",
    }
}

// 订阅者检查重复事件
type IdempotentHandler struct {
    handler   eventing.IEventHandler
    processed map[string]bool
}

func (h *IdempotentHandler) Handle(ctx context.Context, event eventing.IEvent) error {
    if h.processed[event.GetID()] {
        return nil // 已处理，跳过
    }
    
    if err := h.handler.Handle(ctx, event); err != nil {
        return err
    }
    
    h.processed[event.GetID()] = true
    return nil
}
```

### 3. 错误处理

```go
// 区分临时错误和永久错误
func (p *Publisher) publishEvent(ctx context.Context, entry *OutboxEntry) error {
    event, err := entry.ToEvent()
    if err != nil {
        // 序列化错误（永久错误，不重试）
        return p.repo.MarkFailed(ctx, entry.ID, 
            fmt.Errorf("deserialization error: %w", err))
    }
    
    err = p.eventBus.Publish(ctx, event)
    if err != nil {
        // 网络错误等（临时错误，可重试）
        if isRetryable(err) && entry.ShouldRetry(p.config.MaxRetries) {
            entry.RetryCount++
            entry.NextRetryAt = entry.CalculateNextRetryTime()
            return p.repo.Save(ctx, entry)
        }
        
        // 超过重试次数
        return p.repo.MarkFailed(ctx, entry.ID, err)
    }
    
    // 成功
    return p.repo.MarkPublished(ctx, entry.ID)
}
```

## 故障恢复

### 应用重启后恢复

```go
// 应用启动时检查待发布事件
func (p *Publisher) Start(ctx context.Context) error {
    // 立即发布一次待发布事件
    if err := p.PublishPending(ctx); err != nil {
        log.Printf("Initial publish failed: %v", err)
    }
    
    // 启动后台轮询
    go p.pollLoop(ctx)
    
    return nil
}
```

### 处理死信

```go
// 定期检查多次失败的事件
func (p *Publisher) handleDeadLetters(ctx context.Context) error {
    deadLetters, err := p.repo.GetFailed(ctx, p.config.MaxRetries)
    if err != nil {
        return err
    }
    
    for _, entry := range deadLetters {
        log.Printf("Dead letter event: %s (retries: %d, last error: %s)", 
            entry.EventID, entry.RetryCount, entry.LastError)
        
        // 发送告警
        p.alertService.SendAlert(fmt.Sprintf("Dead letter event: %s", entry.EventID))
        
        // 可选：移动到专门的死信队列表
        p.deadLetterRepo.Save(ctx, entry)
    }
    
    return nil
}
```

## 相关文档

- [事件存储](../store/README.md) - 事件持久化
- [事件总线](../bus/README.md) - 事件发布订阅
- [领域仓储](../../domain/repository/README.md) - 仓储模式

## 许可证

MIT License
