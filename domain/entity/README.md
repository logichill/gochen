# Domain Entity - 领域实体

领域实体是 DDD 架构的核心，表示业务领域中的概念和对象。

## 概述

实体（Entity）是具有唯一标识的领域对象，其生命周期中可以发生状态变化，但身份保持不变。

**核心特征**:
- **唯一标识** - 每个实体有唯一 ID
- **可变状态** - 实体状态可以改变
- **生命周期** - 从创建到删除的完整生命周期
- **业务规则** - 封装业务逻辑和验证规则

## 核心接口

### IEntity - 基础实体接口

```go
// IEntity 基础实体接口
type IEntity[ID comparable] interface {
    // GetID 获取实体ID
    GetID() ID
    
    // SetID 设置实体ID
    SetID(id ID)
    
    // Validate 验证实体是否有效
    Validate() error
}
```

### IAuditable - 可审计实体接口

```go
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
```

### ISoftDeletable - 软删除接口

```go
// ISoftDeletable 软删除实体接口
type ISoftDeletable interface {
    GetDeletedAt() *time.Time
    SetDeletedAt(t *time.Time)
    IsDeleted() bool
}
```

## 实体类型

### 1. 简单实体

适用于配置表、字典表等简单数据。

```go
// Category 分类实体
type Category struct {
    ID   int64  `json:"id"`
    Name string `json:"name" validate:"required,min=2,max=50"`
}

func (c *Category) GetID() int64 { 
    return c.ID 
}

func (c *Category) SetID(id int64) { 
    c.ID = id 
}

func (c *Category) Validate() error {
    if c.Name == "" {
        return errors.New("name is required")
    }
    return nil
}

// 确保实现了接口
var _ entity.IEntity[int64] = (*Category)(nil)
```

### 2. 审计实体

适用于需要追踪创建/修改历史的实体。

```go
// User 用户实体（带审计字段）
type User struct {
    ID        int64     `json:"id"`
    Name      string    `json:"name" validate:"required"`
    Email     string    `json:"email" validate:"required,email"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    CreatedBy string    `json:"created_by"`
    UpdatedBy string    `json:"updated_by"`
}

func (u *User) GetID() int64 { return u.ID }
func (u *User) SetID(id int64) { u.ID = id }

// 实现 IAuditable
func (u *User) GetCreatedAt() time.Time { return u.CreatedAt }
func (u *User) SetCreatedAt(t time.Time) { u.CreatedAt = t }
func (u *User) GetUpdatedAt() time.Time { return u.UpdatedAt }
func (u *User) SetUpdatedAt(t time.Time) { u.UpdatedAt = t }
func (u *User) GetCreatedBy() string { return u.CreatedBy }
func (u *User) SetCreatedBy(by string) { u.CreatedBy = by }
func (u *User) GetUpdatedBy() string { return u.UpdatedBy }
func (u *User) SetUpdatedBy(by string) { u.UpdatedBy = by }

func (u *User) Validate() error {
    if u.Name == "" {
        return errors.New("name is required")
    }
    if u.Email == "" {
        return errors.New("email is required")
    }
    return nil
}

// 确保实现了接口
var _ entity.IEntity[int64] = (*User)(nil)
var _ entity.IAuditable = (*User)(nil)
```

### 3. 软删除实体

适用于需要逻辑删除的实体（数据保留）。

```go
// Order 订单实体（带软删除）
type Order struct {
    ID        int64      `json:"id"`
    UserID    int64      `json:"user_id"`
    Total     int64      `json:"total"`
    Status    string     `json:"status"`
    DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

func (o *Order) GetID() int64 { return o.ID }
func (o *Order) SetID(id int64) { o.ID = id }

// 实现 ISoftDeletable
func (o *Order) GetDeletedAt() *time.Time { return o.DeletedAt }
func (o *Order) SetDeletedAt(t *time.Time) { o.DeletedAt = t }
func (o *Order) IsDeleted() bool { return o.DeletedAt != nil }

func (o *Order) Validate() error {
    if o.UserID == 0 {
        return errors.New("user_id is required")
    }
    if o.Total < 0 {
        return errors.New("total cannot be negative")
    }
    return nil
}

var _ entity.IEntity[int64] = (*Order)(nil)
var _ entity.ISoftDeletable = (*Order)(nil)
```

## 聚合根

### IAggregate - 聚合根接口

聚合根是一组相关实体的根，定义事务一致性边界。

```go
// IAggregate 聚合根接口
type IAggregate interface {
    IEntity[int64]
    
    // GetVersion 获取聚合版本（用于乐观锁）
    GetVersion() uint64
    
    // SetVersion 设置聚合版本
    SetVersion(version uint64)
}
```

### 聚合根示例

```go
// OrderAggregate 订单聚合根
type OrderAggregate struct {
    entity.AggregateBase
    
    // 聚合状态
    ID     int64       `json:"id"`
    UserID int64       `json:"user_id"`
    Items  []OrderItem `json:"items"`
    Total  int64       `json:"total"`
    Status OrderStatus `json:"status"`
}

// OrderItem 订单项（值对象，属于聚合的一部分）
type OrderItem struct {
    ProductID int64  `json:"product_id"`
    Quantity  int    `json:"quantity"`
    Price     int64  `json:"price"`
}

// 业务方法
func (o *OrderAggregate) AddItem(productID int64, quantity int, price int64) error {
    // 验证
    if quantity <= 0 {
        return errors.New("quantity must be positive")
    }
    
    // 添加订单项
    o.Items = append(o.Items, OrderItem{
        ProductID: productID,
        Quantity:  quantity,
        Price:     price,
    })
    
    // 重新计算总价
    o.calculateTotal()
    
    return nil
}

func (o *OrderAggregate) RemoveItem(productID int64) error {
    for i, item := range o.Items {
        if item.ProductID == productID {
            o.Items = append(o.Items[:i], o.Items[i+1:]...)
            o.calculateTotal()
            return nil
        }
    }
    return errors.New("item not found")
}

func (o *OrderAggregate) calculateTotal() {
    total := int64(0)
    for _, item := range o.Items {
        total += item.Price * int64(item.Quantity)
    }
    o.Total = total
}

func (o *OrderAggregate) Validate() error {
    if o.UserID == 0 {
        return errors.New("user_id is required")
    }
    if len(o.Items) == 0 {
        return errors.New("order must have at least one item")
    }
    return nil
}
```

## 事件溯源聚合

### IEventSourcedAggregate - 事件溯源聚合接口

```go
// IEventSourcedAggregate 事件溯源聚合根接口
type IEventSourcedAggregate[ID comparable] interface {
    IAggregate
    
    // GetUncommittedEvents 获取未提交的事件
    GetUncommittedEvents() []IEvent
    
    // MarkEventsAsCommitted 标记事件已提交
    MarkEventsAsCommitted()
    
    // LoadFromHistory 从历史事件重建聚合
    LoadFromHistory(events []IEvent) error
}
```

### 事件溯源聚合示例

```go
// AccountAggregate 账户聚合根（事件溯源）
type AccountAggregate struct {
    *entity.EventSourcedAggregate[int64]
    
    // 聚合状态
    Balance int64
    Status  string
}

// 事件定义
type AccountCreated struct {
    eventing.EventBase
    AccountID int64
}

type MoneyDeposited struct {
    eventing.EventBase
    Amount int64
}

type MoneyWithdrawn struct {
    eventing.EventBase
    Amount int64
}

// 命令方法
func (a *AccountAggregate) Deposit(amount int64) error {
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

func (a *AccountAggregate) Withdraw(amount int64) error {
    if amount <= 0 {
        return errors.New("amount must be positive")
    }
    
    if a.Balance < amount {
        return errors.New("insufficient balance")
    }
    
    // 记录事件
    event := &MoneyWithdrawn{
        EventBase: eventing.NewEventBase(a.GetID(), "MoneyWithdrawn", 1),
        Amount:    amount,
    }
    
    a.RecordEvent(event)
    return nil
}

// 事件应用方法
func (e *MoneyDeposited) Apply(agg entity.IAggregate) error {
    account := agg.(*AccountAggregate)
    account.Balance += e.Amount
    return nil
}

func (e *MoneyWithdrawn) Apply(agg entity.IAggregate) error {
    account := agg.(*AccountAggregate)
    account.Balance -= e.Amount
    return nil
}
```

## 最佳实践

### 1. 封装业务规则

```go
// ✅ 正确：业务逻辑在实体内部
func (u *User) ChangeEmail(newEmail string) error {
    if !isValidEmail(newEmail) {
        return errors.New("invalid email format")
    }
    
    if u.Email == newEmail {
        return errors.New("email not changed")
    }
    
    u.Email = newEmail
    return nil
}

// ❌ 错误：业务逻辑在外部
// 不要在服务层直接修改实体字段
func (s *UserService) ChangeEmail(user *User, newEmail string) error {
    if !isValidEmail(newEmail) { // 业务规则泄露到服务层
        return errors.New("invalid email format")
    }
    user.Email = newEmail // 直接修改字段
    return nil
}
```

### 2. 使用值对象

```go
// Email 值对象（不可变）
type Email struct {
    value string
}

func NewEmail(value string) (*Email, error) {
    if !isValidEmail(value) {
        return nil, errors.New("invalid email")
    }
    return &Email{value: value}, nil
}

func (e *Email) String() string {
    return e.value
}

// 在实体中使用
type User struct {
    ID    int64
    Name  string
    Email *Email // 使用值对象而非字符串
}
```

### 3. 保持实体纯粹

```go
// ✅ 正确：实体只包含业务逻辑，不依赖外部服务
type Order struct {
    ID     int64
    Items  []OrderItem
    Total  int64
}

func (o *Order) CalculateTotal() {
    total := int64(0)
    for _, item := range o.Items {
        total += item.Price * int64(item.Quantity)
    }
    o.Total = total
}

// ❌ 错误：实体依赖外部服务
type Order struct {
    ID          int64
    priceService IPriceService // 不要注入服务
}

func (o *Order) CalculateTotal() {
    // 不要在实体中调用外部服务
    total := o.priceService.Calculate(o.Items)
    o.Total = total
}
```

## 相关文档

- [仓储模式](../repository/README.md) - 实体持久化
- [应用服务层](../../app/README.md) - 实体使用示例
- [事件溯源](../../eventing/store/README.md) - 事件溯源聚合

## 许可证

MIT License
