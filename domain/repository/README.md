# Domain Repository - 领域仓储

仓储模式提供了类似集合的接口来操作领域对象，封装了数据访问逻辑。

## 概述

仓储（Repository）是 DDD 中的关键模式，负责：
- 封装数据访问逻辑
- 提供类似集合的接口
- 维护领域对象的完整性
- 解耦领域层和数据访问层

## 核心接口

### IRepository - 基础仓储接口

```go
// IRepository 通用仓储接口
type IRepository[T entity.IEntity[ID], ID comparable] interface {
    // GetByID 根据ID获取实体
    GetByID(ctx context.Context, id ID) (T, error)
    
    // Save 保存实体（创建或更新）
    Save(ctx context.Context, entity T) error
    
    // Delete 删除实体
    Delete(ctx context.Context, id ID) error
    
    // List 查询实体列表
    List(ctx context.Context, opts *QueryOptions) ([]T, error)
    
    // Count 统计数量
    Count(ctx context.Context, filters map[string]interface{}) (int64, error)
}
```

### IBatchOperations - 批量操作接口

```go
// IBatchOperations 定义批量操作接口
type IBatchOperations[T entity.IEntity[ID], ID comparable] interface {
    // CreateAll 批量创建实体
    CreateAll(ctx context.Context, entities []T) error
    
    // UpdateBatch 批量更新实体
    UpdateBatch(ctx context.Context, entities []T) error
    
    // DeleteBatch 批量删除实体
    DeleteBatch(ctx context.Context, ids []ID) error
}
```

### ITransactional - 事务管理接口

```go
// ITransactional 定义支持事务的仓储接口
type ITransactional interface {
    // BeginTx 开始一个新事务
    BeginTx(ctx context.Context) (context.Context, error)
    
    // Commit 提交当前事务
    Commit(ctx context.Context) error
    
    // Rollback 回滚当前事务
    Rollback(ctx context.Context) error
}
```

### IAuditedRepository - 审计仓储接口

```go
// IAuditedRepository 审计仓储接口
type IAuditedRepository[T entity.IAuditable, ID comparable] interface {
    IRepository[T, ID]
    
    // 自动记录创建/更新/删除的操作人和时间
}
```

### IEventSourcedRepository - 事件溯源仓储接口

```go
// IEventSourcedRepository 事件溯源仓储接口
type IEventSourcedRepository[T entity.IEventSourcedAggregate[ID], ID comparable] interface {
    // Load 加载聚合根（通过事件重放）
    Load(ctx context.Context, id ID) (T, error)
    
    // Save 保存聚合根（保存新事件）
    Save(ctx context.Context, aggregate T) error
    
    // Exists 检查聚合根是否存在
    Exists(ctx context.Context, id ID) (bool, error)
}
```

## 查询选项

### QueryOptions

```go
// QueryOptions 查询选项
type QueryOptions struct {
    // 分页
    Pagination *PaginationOption
    
    // 排序
    Sort *SortOption
    
    // 过滤条件
    Filters map[string]interface{}
    
    // 字段选择
    Fields []string
    
    // 关联加载
    Preloads []string
}

// PaginationOption 分页选项
type PaginationOption struct {
    Page int // 页码（从1开始）
    Size int // 每页大小
}

// SortOption 排序选项
type SortOption struct {
    Field string // 排序字段
    Order string // 排序方向（asc/desc）
}
```

## 使用示例

### 1. 简单 CRUD 仓储

```go
package repo

import (
    "context"
    "gochen/domain/repository"
)

// UserRepository 用户仓储实现
type UserRepository struct {
    db IDatabase
}

func NewUserRepository(db IDatabase) repository.IRepository[*User, int64] {
    return &UserRepository{db: db}
}

func (r *UserRepository) GetByID(ctx context.Context, id int64) (*User, error) {
    var user User
    err := r.db.QueryRow(ctx, "SELECT * FROM users WHERE id = ?", id).Scan(&user)
    if err != nil {
        if err == sql.ErrNoRows {
            return nil, repository.ErrNotFound
        }
        return nil, err
    }
    return &user, nil
}

func (r *UserRepository) Save(ctx context.Context, user *User) error {
    if user.ID == 0 {
        // 创建
        result, err := r.db.Exec(ctx, 
            "INSERT INTO users (name, email) VALUES (?, ?)", 
            user.Name, user.Email)
        if err != nil {
            return err
        }
        id, _ := result.LastInsertId()
        user.ID = id
        return nil
    }
    
    // 更新
    _, err := r.db.Exec(ctx,
        "UPDATE users SET name = ?, email = ? WHERE id = ?",
        user.Name, user.Email, user.ID)
    return err
}

func (r *UserRepository) Delete(ctx context.Context, id int64) error {
    _, err := r.db.Exec(ctx, "DELETE FROM users WHERE id = ?", id)
    return err
}

func (r *UserRepository) List(ctx context.Context, opts *repository.QueryOptions) ([]*User, error) {
    query := "SELECT * FROM users WHERE 1=1"
    args := []interface{}{}
    
    // 应用过滤条件
    if opts != nil && opts.Filters != nil {
        if status, ok := opts.Filters["status"]; ok {
            query += " AND status = ?"
            args = append(args, status)
        }
    }
    
    // 应用排序
    if opts != nil && opts.Sort != nil {
        query += fmt.Sprintf(" ORDER BY %s %s", opts.Sort.Field, opts.Sort.Order)
    }
    
    // 应用分页
    if opts != nil && opts.Pagination != nil {
        offset := (opts.Pagination.Page - 1) * opts.Pagination.Size
        query += " LIMIT ? OFFSET ?"
        args = append(args, opts.Pagination.Size, offset)
    }
    
    rows, err := r.db.Query(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    var users []*User
    for rows.Next() {
        var user User
        if err := rows.Scan(&user); err != nil {
            return nil, err
        }
        users = append(users, &user)
    }
    
    return users, nil
}

func (r *UserRepository) Count(ctx context.Context, filters map[string]interface{}) (int64, error) {
    query := "SELECT COUNT(*) FROM users WHERE 1=1"
    args := []interface{}{}
    
    if status, ok := filters["status"]; ok {
        query += " AND status = ?"
        args = append(args, status)
    }
    
    var count int64
    err := r.db.QueryRow(ctx, query, args...).Scan(&count)
    return count, err
}
```

### 2. 批量操作

```go
func (r *UserRepository) CreateAll(ctx context.Context, users []*User) error {
    tx, err := r.db.Begin(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback()
    
    stmt, err := tx.Prepare(ctx, "INSERT INTO users (name, email) VALUES (?, ?)")
    if err != nil {
        return err
    }
    defer stmt.Close()
    
    for _, user := range users {
        result, err := stmt.Exec(ctx, user.Name, user.Email)
        if err != nil {
            return err
        }
        id, _ := result.LastInsertId()
        user.ID = id
    }
    
    return tx.Commit()
}
```

### 3. 事务操作

```go
// 实现 ITransactional 接口
func (r *UserRepository) BeginTx(ctx context.Context) (context.Context, error) {
    tx, err := r.db.BeginTx(ctx, nil)
    if err != nil {
        return nil, err
    }
    
    // 将事务存入上下文
    return context.WithValue(ctx, "tx", tx), nil
}

func (r *UserRepository) Commit(ctx context.Context) error {
    tx, ok := ctx.Value("tx").(ITx)
    if !ok {
        return errors.New("no transaction in context")
    }
    return tx.Commit()
}

func (r *UserRepository) Rollback(ctx context.Context) error {
    tx, ok := ctx.Value("tx").(ITx)
    if !ok {
        return errors.New("no transaction in context")
    }
    return tx.Rollback()
}

// 使用事务
func transferMoney(ctx context.Context, repo repository.IRepository[*Account, int64], 
                  fromID, toID int64, amount int64) error {
    txRepo := repo.(repository.ITransactional)
    
    // 开始事务
    ctx, err := txRepo.BeginTx(ctx)
    if err != nil {
        return err
    }
    defer txRepo.Rollback(ctx)
    
    // 扣款
    from, err := repo.GetByID(ctx, fromID)
    if err != nil {
        return err
    }
    from.Balance -= amount
    if err := repo.Save(ctx, from); err != nil {
        return err
    }
    
    // 加款
    to, err := repo.GetByID(ctx, toID)
    if err != nil {
        return err
    }
    to.Balance += amount
    if err := repo.Save(ctx, to); err != nil {
        return err
    }
    
    // 提交事务
    return txRepo.Commit(ctx)
}
```

### 4. 审计仓储

```go
// AuditedUserRepository 审计用户仓储
type AuditedUserRepository struct {
    *UserRepository
}

func (r *AuditedUserRepository) Save(ctx context.Context, user *User) error {
    // 获取当前用户
    currentUser := GetCurrentUserFromContext(ctx)
    
    // 设置审计字段
    now := time.Now()
    if user.ID == 0 {
        // 创建
        user.SetCreatedAt(now)
        user.SetCreatedBy(currentUser.Name)
    }
    user.SetUpdatedAt(now)
    user.SetUpdatedBy(currentUser.Name)
    
    // 调用基础仓储保存
    return r.UserRepository.Save(ctx, user)
}
```

### 5. 事件溯源仓储

```go
// AccountRepository 账户事件溯源仓储
type AccountRepository struct {
    eventStore eventing.IEventStore
}

func NewAccountRepository(eventStore eventing.IEventStore) repository.IEventSourcedRepository[*Account, int64] {
    return &AccountRepository{eventStore: eventStore}
}

func (r *AccountRepository) Load(ctx context.Context, id int64) (*Account, error) {
    // 加载历史事件
    events, err := r.eventStore.LoadEvents(ctx, id, 0)
    if err != nil {
        return nil, err
    }
    
    if len(events) == 0 {
        return nil, repository.ErrNotFound
    }
    
    // 创建聚合
    account := &Account{
        EventSourcedAggregate: entity.NewEventSourcedAggregate[int64](id),
    }
    
    // 重放事件
    if err := account.LoadFromHistory(events); err != nil {
        return nil, err
    }
    
    return account, nil
}

func (r *AccountRepository) Save(ctx context.Context, account *Account) error {
    // 获取未提交的事件
    events := account.GetUncommittedEvents()
    if len(events) == 0 {
        return nil // 没有变化
    }
    
    // 保存事件
    err := r.eventStore.AppendEvents(ctx, account.GetID(), events, account.GetVersion())
    if err != nil {
        return err
    }
    
    // 标记事件已提交
    account.MarkEventsAsCommitted()
    
    return nil
}

func (r *AccountRepository) Exists(ctx context.Context, id int64) (bool, error) {
    inspector, ok := r.eventStore.(eventing.IAggregateInspector)
    if !ok {
        // Fallback: 尝试加载事件
        events, err := r.eventStore.LoadEvents(ctx, id, 0)
        if err != nil {
            return false, err
        }
        return len(events) > 0, nil
    }
    
    return inspector.HasAggregate(ctx, id)
}
```

## 最佳实践

### 1. 使用接口而非具体实现

```go
// ✅ 正确：依赖接口
type UserService struct {
    repo repository.IRepository[*User, int64]
}

func NewUserService(repo repository.IRepository[*User, int64]) *UserService {
    return &UserService{repo: repo}
}

// ❌ 错误：依赖具体实现
type UserService struct {
    repo *UserRepository // 不要依赖具体实现
}
```

### 2. 仓储只返回聚合根

```go
// ✅ 正确：仓储返回完整聚合根
func (r *OrderRepository) GetByID(ctx context.Context, id int64) (*Order, error) {
    // 加载订单及其订单项
    order := &Order{}
    if err := r.db.Preload("Items").First(order, id).Error; err != nil {
        return nil, err
    }
    return order, nil
}

// ❌ 错误：返回不完整的聚合
func (r *OrderRepository) GetByID(ctx context.Context, id int64) (*Order, error) {
    // 只加载订单，不加载订单项
    order := &Order{}
    if err := r.db.First(order, id).Error; err != nil {
        return nil, err
    }
    return order, nil // 订单项缺失！
}
```

### 3. 错误处理

```go
var (
    ErrNotFound = errors.New("entity not found")
    ErrConflict = errors.New("entity already exists")
    ErrVersion  = errors.New("version mismatch")
)

func (r *UserRepository) GetByID(ctx context.Context, id int64) (*User, error) {
    var user User
    err := r.db.QueryRow(ctx, "SELECT * FROM users WHERE id = ?", id).Scan(&user)
    if err != nil {
        if err == sql.ErrNoRows {
            return nil, ErrNotFound
        }
        return nil, fmt.Errorf("failed to get user: %w", err)
    }
    return &user, nil
}
```

### 4. 规范命名

```go
// ✅ 正确的方法命名
GetByID(id int64) (*User, error)
FindByEmail(email string) (*User, error)
ListByStatus(status string) ([]*User, error)
DeleteByID(id int64) error

// ❌ 错误的命名
GetById(id int64)           // 小写 Id
Get(id int64)              // 不够明确
FindUser(email string)     // 冗余的 User
```

## 相关文档

- [实体和聚合根](../entity/README.md) - 领域实体
- [应用服务层](../../app/README.md) - 仓储使用示例
- [事件溯源](../../eventing/store/README.md) - 事件溯源仓储

## 许可证

MIT License
