# Event Bus - 事件总线

事件总线提供进程内事件发布和订阅机制，支持异步事件处理。

## 概述

事件总线（EventBus）是发布-订阅模式的实现，用于：
- **解耦组件** - 发布者和订阅者松耦合
- **异步处理** - 事件异步分发和处理
- **多订阅者** - 一个事件可被多个订阅者处理
- **类型安全** - 基于事件类型的路由

## 核心接口

### IEventBus - 事件总线接口

```go
// IEventBus 事件总线接口
type IEventBus interface {
    // Publish 发布单个事件
    Publish(ctx context.Context, event IEvent) error
    
    // PublishAll 发布多个事件
    PublishAll(ctx context.Context, events []IEvent) error
    
    // Subscribe 订阅事件类型
    Subscribe(eventType string, handler IEventHandler) error
    
    // Unsubscribe 取消订阅
    Unsubscribe(eventType string, handler IEventHandler) error
}
```

### IEventHandler - 事件处理器接口

```go
// IEventHandler 事件处理器接口
type IEventHandler interface {
    // Handle 处理事件
    Handle(ctx context.Context, event IEvent) error
}
```

## 使用示例

### 1. 基础用法

```go
package main

import (
    "context"
    "gochen/eventing"
    "gochen/eventing/bus"
)

func main() {
    ctx := context.Background()
    
    // 创建事件总线
    eventBus := bus.NewEventBus()
    
    // 订阅事件
    eventBus.Subscribe("UserCreated", &UserCreatedHandler{})
    eventBus.Subscribe("UserCreated", &EmailNotificationHandler{})
    
    // 发布事件
    event := &UserCreated{
        EventBase: eventing.NewEventBase(1, "UserCreated", 1),
        UserID:    1,
        Name:      "张三",
        Email:     "zhangsan@example.com",
    }
    
    if err := eventBus.Publish(ctx, event); err != nil {
        log.Fatal(err)
    }
}
```

### 2. 实现事件处理器

```go
// UserCreatedHandler 用户创建事件处理器
type UserCreatedHandler struct {
    userService *UserService
}

func (h *UserCreatedHandler) Handle(ctx context.Context, event eventing.IEvent) error {
    e, ok := event.(*UserCreated)
    if !ok {
        return errors.New("invalid event type")
    }
    
    log.Printf("User created: ID=%d, Name=%s", e.UserID, e.Name)
    
    // 执行业务逻辑
    return h.userService.OnUserCreated(ctx, e.UserID)
}

// EmailNotificationHandler 邮件通知处理器
type EmailNotificationHandler struct {
    emailService *EmailService
}

func (h *EmailNotificationHandler) Handle(ctx context.Context, event eventing.IEvent) error {
    e, ok := event.(*UserCreated)
    if !ok {
        return errors.New("invalid event type")
    }
    
    // 发送欢迎邮件
    return h.emailService.SendWelcomeEmail(e.Email, e.Name)
}
```

### 3. 批量发布

```go
// 批量发布多个事件
events := []eventing.IEvent{
    &UserCreated{...},
    &UserEmailChanged{...},
    &UserActivated{...},
}

if err := eventBus.PublishAll(ctx, events); err != nil {
    log.Fatal(err)
}
```

### 4. 函数式订阅

```go
// 使用匿名函数订阅
eventBus.Subscribe("UserCreated", eventing.HandlerFunc(func(ctx context.Context, event eventing.IEvent) error {
    e := event.(*UserCreated)
    log.Printf("New user: %s", e.Name)
    return nil
}))
```

## 高级特性

### 1. 异步处理

```go
// 创建异步事件总线
asyncBus := bus.NewAsyncEventBus(&bus.AsyncConfig{
    WorkerCount: 10,      // 工作协程数
    BufferSize:  1000,    // 事件缓冲区大小
    ErrorHandler: func(err error) {
        log.Printf("Error handling event: %v", err)
    },
})

// 启动
if err := asyncBus.Start(ctx); err != nil {
    log.Fatal(err)
}
defer asyncBus.Stop()

// 发布事件（非阻塞）
asyncBus.Publish(ctx, event)
```

### 2. 错误处理

```go
// 实现错误恢复
type ResilientHandler struct {
    handler  eventing.IEventHandler
    maxRetries int
}

func (h *ResilientHandler) Handle(ctx context.Context, event eventing.IEvent) error {
    var err error
    for i := 0; i < h.maxRetries; i++ {
        err = h.handler.Handle(ctx, event)
        if err == nil {
            return nil
        }
        
        log.Printf("Retry %d/%d: %v", i+1, h.maxRetries, err)
        time.Sleep(time.Second * time.Duration(i+1))
    }
    
    return fmt.Errorf("failed after %d retries: %w", h.maxRetries, err)
}
```

### 3. 中间件

```go
// 日志中间件
func LoggingMiddleware(next eventing.IEventHandler) eventing.IEventHandler {
    return eventing.HandlerFunc(func(ctx context.Context, event eventing.IEvent) error {
        start := time.Now()
        log.Printf("Handling event: %s", event.GetType())
        
        err := next.Handle(ctx, event)
        
        duration := time.Since(start)
        if err != nil {
            log.Printf("Error handling %s (took %v): %v", event.GetType(), duration, err)
        } else {
            log.Printf("Successfully handled %s (took %v)", event.GetType(), duration)
        }
        
        return err
    })
}

// 使用中间件
handler := &UserCreatedHandler{}
wrappedHandler := LoggingMiddleware(handler)
eventBus.Subscribe("UserCreated", wrappedHandler)
```

### 4. 事件过滤

```go
// 基于条件的事件处理
type ConditionalHandler struct {
    condition func(eventing.IEvent) bool
    handler   eventing.IEventHandler
}

func (h *ConditionalHandler) Handle(ctx context.Context, event eventing.IEvent) error {
    if !h.condition(event) {
        return nil // 跳过处理
    }
    
    return h.handler.Handle(ctx, event)
}

// 只处理特定用户的事件
handler := &ConditionalHandler{
    condition: func(event eventing.IEvent) bool {
        e, ok := event.(*UserCreated)
        return ok && e.UserID > 1000 // 只处理 ID > 1000 的用户
    },
    handler: &UserCreatedHandler{},
}

eventBus.Subscribe("UserCreated", handler)
```

## 最佳实践

### 1. 保持处理器简单

```go
// ✅ 正确：处理器只做一件事
type UserCreatedHandler struct {
    userService *UserService
}

func (h *UserCreatedHandler) Handle(ctx context.Context, event eventing.IEvent) error {
    e := event.(*UserCreated)
    return h.userService.CreateUserProfile(ctx, e.UserID)
}

// ❌ 错误：处理器做太多事情
type UserCreatedHandler struct {
    userService  *UserService
    emailService *EmailService
    smsService   *SMSService
    auditService *AuditService
}

func (h *UserCreatedHandler) Handle(ctx context.Context, event eventing.IEvent) error {
    // 不要在一个处理器中做太多事情
    h.userService.CreateProfile(...)
    h.emailService.SendWelcome(...)
    h.smsService.SendNotification(...)
    h.auditService.Log(...)
    return nil
}
```

### 2. 处理器幂等性

```go
// 确保处理器幂等
type UserCreatedHandler struct {
    processed map[string]bool
    mu        sync.Mutex
}

func (h *UserCreatedHandler) Handle(ctx context.Context, event eventing.IEvent) error {
    h.mu.Lock()
    defer h.mu.Unlock()
    
    eventID := event.GetID()
    if h.processed[eventID] {
        return nil // 已处理，跳过
    }
    
    // 处理事件
    if err := h.process(ctx, event); err != nil {
        return err
    }
    
    // 标记已处理
    h.processed[eventID] = true
    return nil
}
```

### 3. 错误处理策略

```go
// 区分可重试和不可重试错误
var (
    ErrRetryable    = errors.New("retryable error")
    ErrNonRetryable = errors.New("non-retryable error")
)

func (h *Handler) Handle(ctx context.Context, event eventing.IEvent) error {
    err := h.process(ctx, event)
    if err != nil {
        // 网络错误可重试
        if isNetworkError(err) {
            return errors.Join(ErrRetryable, err)
        }
        
        // 验证错误不可重试
        if isValidationError(err) {
            return errors.Join(ErrNonRetryable, err)
        }
    }
    
    return err
}
```

## 与其他组件集成

### 1. 与事件存储集成

```go
// 保存事件后自动发布
func (r *EventSourcedRepository) Save(ctx context.Context, aggregate *Aggregate) error {
    events := aggregate.GetUncommittedEvents()
    
    // 保存到事件存储
    if err := r.eventStore.AppendEvents(ctx, aggregate.GetID(), events, aggregate.GetVersion()); err != nil {
        return err
    }
    
    // 发布到事件总线
    if err := r.eventBus.PublishAll(ctx, events); err != nil {
        log.Printf("Failed to publish events: %v", err)
        // 注意：事件已保存，发布失败不应回滚
    }
    
    aggregate.MarkEventsAsCommitted()
    return nil
}
```

### 2. 与投影管理器集成

```go
// 投影管理器订阅事件
projectionManager := projection.NewManager(eventBus)

// 注册投影
projectionManager.Register(&UserViewProjection{db})
projectionManager.Register(&OrderStatisticsProjection{db})

// 启动投影更新
if err := projectionManager.Start(ctx); err != nil {
    log.Fatal(err)
}
```

## 性能优化

### 1. 批量处理

```go
// 批量处理事件以提升性能
type BatchHandler struct {
    handler    eventing.IEventHandler
    batchSize  int
    buffer     []eventing.IEvent
    mu         sync.Mutex
}

func (h *BatchHandler) Handle(ctx context.Context, event eventing.IEvent) error {
    h.mu.Lock()
    h.buffer = append(h.buffer, event)
    
    if len(h.buffer) >= h.batchSize {
        batch := h.buffer
        h.buffer = nil
        h.mu.Unlock()
        
        return h.processBatch(ctx, batch)
    }
    
    h.mu.Unlock()
    return nil
}
```

### 2. 并发处理

```go
// 并发处理事件
type ConcurrentHandler struct {
    handler     eventing.IEventHandler
    concurrency int
}

func (h *ConcurrentHandler) Handle(ctx context.Context, event eventing.IEvent) error {
    sem := make(chan struct{}, h.concurrency)
    
    sem <- struct{}{}
    go func() {
        defer func() { <-sem }()
        h.handler.Handle(ctx, event)
    }()
    
    return nil
}
```

## 相关文档

- [事件存储](../store/README.md) - 事件持久化
- [Outbox 模式](../outbox/README.md) - 可靠事件发布
- [投影管理](../projection/README.md) - 读模型更新

## 许可证

MIT License
