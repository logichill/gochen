# Gochen Shared - 命名规范文档

**版本**: 1.0  
**日期**: 2025-11-10  
**状态**: 正式发布

---

## 目录

1. [接口命名规范](#接口命名规范)
2. [类型命名规范](#类型命名规范)
3. [方法命名规范](#方法命名规范)
4. [变量命名规范](#变量命名规范)
5. [包命名规范](#包命名规范)
6. [缩写和首字母缩略词](#缩写和首字母缩略词)
7. [注释和文档规范](#注释和文档规范)
8. [代码示例](#代码示例)

---

## 接口命名规范

### 规则 1: 所有公共接口使用 I 前缀

**正确**:
```go
type IRepository interface {}
type IEventStore interface {}
type IMessageBus interface {}
type IValidator interface {}
```

**错误**:
```go
type Repository interface {}    // ❌ 缺少 I 前缀
type EventStore interface {}    // ❌ 缺少 I 前缀
```

**理由**:
- 一眼识别接口类型
- 避免与实现类型命名冲突
- 符合企业级项目规范
- IDE 自动补全更友好

### 规则 2: 接口名应准确描述其职责

**正确**:
```go
type IEventStore interface {}          // 清晰：事件存储
type IAggregateInspector interface {} // 清晰：聚合检查器
type IBatchOperations interface {}    // 清晰：批量操作
```

**错误**:
```go
type IHandler interface {}   // ❌ 太泛化
type IManager interface {}   // ❌ 职责不明确
type IService interface {}   // ❌ 含义模糊
```

### 规则 3: 接口方法应使用动词开头

**正确**:
```go
type IRepository interface {
    GetByID(id int64) (Entity, error)
    Save(entity Entity) error
    Delete(id int64) error
}
```

**错误**:
```go
type IRepository interface {
    ByID(id int64) (Entity, error)     // ❌ 缺少动词
    Entity(e Entity) error              // ❌ 使用名词
}
```

---

## 类型命名规范

### 规则 4: 结构体使用名词，体现其本质

**正确**:
```go
type User struct {}
type Order struct {}
type SQLEventStore struct {}    // 实现类型，无 I 前缀
type MemoryCache struct {}
```

**错误**:
```go
type UserImpl struct {}         // ❌ 不要使用 Impl 后缀
type GetUser struct {}          // ❌ 不要使用动词
type IUserStruct struct {}      // ❌ 结构体不使用 I 前缀
```

### 规则 5: 类型别名应清晰表达含义

**正确**:
```go
type UserID int64
type Email string
type EventHandler func(context.Context, Event) error
```

**错误**:
```go
type UID int64              // ❌ 过度缩写
type E string               // ❌ 单字母别名
type Func func()            // ❌ 太泛化
```

---

## 方法命名规范

### 规则 6: CRUD 操作使用标准动词

| 操作 | 方法名模式 | 示例 |
|------|-----------|------|
| 创建 | `Create*`, `Add*`, `New*` | `CreateUser`, `AddItem` |
| 读取 | `Get*`, `Find*`, `Load*`, `List*` | `GetByID`, `FindAll` |
| 更新 | `Update*`, `Modify*`, `Set*` | `UpdateStatus`, `SetName` |
| 删除 | `Delete*`, `Remove*` | `DeleteByID`, `RemoveAll` |

**正确**:
```go
func (r *Repository) GetByID(id int64) (*User, error)
func (r *Repository) FindByEmail(email string) (*User, error)
func (r *Repository) Save(user *User) error
func (r *Repository) Delete(id int64) error
```

**错误**:
```go
func (r *Repository) GetById(id int64)      // ❌ 小写 Id
func (r *Repository) Fetch(id int64)        // ❌ 使用 Fetch 而非 Get
func (r *Repository) Remove(id int64)       // ⚠️ 可接受，但 Delete 更标准
```

### 规则 7: 布尔值方法使用 Is/Has/Can/Should 前缀

**正确**:
```go
func (u *User) IsActive() bool
func (u *User) HasPermission(p Permission) bool
func (u *User) CanAccess(resource string) bool
func (u *User) ShouldNotify() bool
```

**错误**:
```go
func (u *User) Active() bool           // ❌ 缺少 Is
func (u *User) Permission() bool       // ❌ 返回类型不清晰
func (u *User) CheckActive() bool      // ⚠️ Check 可接受但 Is 更简洁
```

### 规则 8: Getter 和 Setter 使用标准模式

**正确**:
```go
// Getter: 直接返回字段名（无 Get 前缀）
func (u *User) Name() string { return u.name }
func (u *User) Email() string { return u.email }

// Setter: 使用 Set 前缀
func (u *User) SetName(name string) { u.name = name }
func (u *User) SetEmail(email string) { u.email = email }

// 复杂 Getter 使用 Get 前缀
func (r *Repository) GetByID(id int64) (*User, error)
```

**错误**:
```go
func (u *User) GetName() string    // ⚠️ 简单 Getter 不需要 Get
func (u *User) name() string       // ❌ 小写，未导出
```

---

## 变量命名规范

### 规则 9: 变量名应简短但有意义

**正确**:
```go
var user *User
var users []*User
var userID int64
var ctx context.Context
var err error

// 循环变量
for i, user := range users {}
for id, name := range userMap {}
```

**错误**:
```go
var u *User                    // ❌ 过度缩写（除非在小作用域）
var userVariable *User         // ❌ 冗余后缀
var theUser *User              // ❌ 冗余前缀
```

### 规则 10: 常量使用驼峰命名或全大写

**正确**:
```go
// 导出常量：大驼峰
const MaxRetries = 5
const DefaultTimeout = 30 * time.Second

// 未导出常量：小驼峰
const maxConnections = 100
const defaultPort = 8080

// 枚举常量：大驼峰 + 类型前缀
const (
    StatusPending   Status = "pending"
    StatusCompleted Status = "completed"
    StatusFailed    Status = "failed"
)
```

**错误**:
```go
const MAX_RETRIES = 5          // ❌ Go 不使用蛇形命名
const max_connections = 100    // ❌ 不要用下划线
```

---

## 包命名规范

### 规则 11: 包名使用小写单数名词

**正确**:
```go
package user
package order
package repository
package eventing
```

**错误**:
```go
package users              // ❌ 不要用复数
package userRepository     // ❌ 不要用驼峰
package user_service       // ❌ 不要用下划线
```

### 规则 12: 包名应简短且有意义

**正确**:
```go
import "gochen/eventing"
import "gochen/cache"
import "gochen/validation"
```

**错误**:
```go
import "gochen/evt"     // ❌ 过度缩写
import "gochen/util"    // ❌ 太泛化
```

---

## 缩写和首字母缩略词

### 规则 13: 常见缩写统一使用大写

| 缩写 | 正确形式 | 错误形式 |
|------|---------|---------|
| ID | `UserID`, `GetByID` | `UserId`, `GetById` |
| HTTP | `HTTPServer`, `HTTPContext` | `HttpServer`, `HttpContext` |
| URL | `ParseURL`, `BaseURL` | `ParseUrl`, `BaseUrl` |
| JSON | `ToJSON`, `JSONData` | `ToJson`, `JsonData` |
| XML | `ToXML`, `XMLParser` | `ToXml`, `XmlParser` |
| API | `APIKey`, `RestAPI` | `ApiKey`, `RestApi` |
| UUID | `GenerateUUID` | `GenerateUuid` |
| SQL | `SQLQuery`, `SQLStore` | `SqlQuery`, `SqlStore` |
| DB | `DBConnection` | `DbConnection` |
| RPC | `RPCClient` | `RpcClient` |

**示例**:
```go
// ✅ 正确
type UserID int64
func GetByID(id int64) (*User, error)
func ParseURL(rawURL string) (*URL, error)
func ToJSON(v interface{}) ([]byte, error)

// ❌ 错误
type UserId int64
func GetById(id int64) (*User, error)
func ParseUrl(rawUrl string) (*URL, error)
func ToJson(v interface{}) ([]byte, error)
```

### 规则 14: 多个缩写连用时保持一致

**正确**:
```go
type HTTPAPI interface {}
type SQLDB interface {}
type JSONAPI interface {}
```

**错误**:
```go
type HTTPApi interface {}    // ❌ 混用大小写
type SqlDB interface {}      // ❌ 不一致
```

---

## 注释和文档规范

### 规则 15: 所有导出类型必须有 GoDoc 注释

**正确**:
```go
// IEventStore 定义事件存储的核心接口。
//
// 事件存储负责持久化和检索领域事件，是事件溯源架构的核心组件。
// 该接口遵循最小化设计原则，仅包含必需的方法。
//
// 使用示例：
//   store := NewSQLEventStore(db)
//   err := store.AppendEvents(ctx, aggregateID, events, 0)
type IEventStore interface {
    // AppendEvents 追加事件到指定聚合的事件流。
    //
    // 参数：
    //   - ctx: 上下文，用于超时控制和取消
    //   - aggregateID: 聚合根ID
    //   - events: 待追加的事件列表
    //   - expectedVersion: 期望的当前版本，用于乐观锁控制
    //
    // 返回：
    //   - error: 版本冲突返回 ConcurrencyError，其他错误返回 EventStoreError
    AppendEvents(ctx context.Context, aggregateID int64, events []IEvent, expectedVersion uint64) error
}
```

**错误**:
```go
// EventStore interface
type IEventStore interface {}  // ❌ 注释太简单

type IEventStore interface {}  // ❌ 完全没有注释

// This is event store
type IEventStore interface {}  // ❌ 英文注释（应使用中文）
```

### 规则 16: 注释格式规范

**正确的注释格式**:
```go
// Package eventing 提供事件溯源和 CQRS 模式的核心组件。
//
// 该包实现了完整的事件驱动架构支持，包括：
//   - 事件存储和检索
//   - 事件总线和订阅
//   - 快照管理
//   - 投影更新
package eventing

// User 表示系统中的用户实体。
type User struct {
    ID   int64
    Name string
}

// GetByID 根据用户ID查询用户信息。
//
// 如果用户不存在，返回 ErrNotFound 错误。
func (r *Repository) GetByID(ctx context.Context, id int64) (*User, error) {
    // 实现...
}
```

**注释规则**:
1. 注释应以类型名或方法名开头
2. 第一句应是完整的句子，以句号结尾
3. 详细说明可以分段，用空行分隔
4. 参数和返回值应单独说明
5. 使用中文注释（项目约定）

---

## 代码示例

### 完整示例 1: 仓储接口

```go
package repository

import "context"

// IRepository 定义通用仓储接口。
//
// 仓储模式封装了数据访问逻辑，提供类似集合的接口来操作领域对象。
// 该接口定义了标准的 CRUD 操作。
//
// 泛型参数：
//   - T: 实体类型，必须实现 IEntity 接口
//   - ID: 实体ID类型，必须是可比较类型
//
// 使用示例：
//   repo := NewUserRepository(db)
//   user, err := repo.GetByID(ctx, 123)
//   if err != nil {
//       return err
//   }
type IRepository[T IEntity[ID], ID comparable] interface {
    // GetByID 根据ID查询实体。
    //
    // 参数：
    //   - ctx: 上下文
    //   - id: 实体ID
    //
    // 返回：
    //   - T: 实体对象
    //   - error: 如果实体不存在返回 ErrNotFound，其他错误返回具体错误
    GetByID(ctx context.Context, id ID) (T, error)

    // Save 保存实体（创建或更新）。
    //
    // 如果实体不存在则创建，存在则更新。
    //
    // 参数：
    //   - ctx: 上下文
    //   - entity: 待保存的实体
    //
    // 返回：
    //   - error: 保存失败时返回错误
    Save(ctx context.Context, entity T) error

    // Delete 删除指定ID的实体。
    //
    // 参数：
    //   - ctx: 上下文
    //   - id: 待删除实体的ID
    //
    // 返回：
    //   - error: 删除失败时返回错误，如果实体不存在不返回错误
    Delete(ctx context.Context, id ID) error
}
```

### 完整示例 2: 服务实现

```go
package service

import (
    "context"
    "fmt"
)

// UserService 用户服务实现。
type UserService struct {
    repo IUserRepository
    logger ILogger
}

// NewUserService 创建用户服务实例。
//
// 参数：
//   - repo: 用户仓储
//   - logger: 日志记录器
//
// 返回：
//   - *UserService: 服务实例
func NewUserService(repo IUserRepository, logger ILogger) *UserService {
    return &UserService{
        repo:   repo,
        logger: logger,
    }
}

// GetUserByID 获取用户信息。
//
// 该方法会记录查询日志并处理错误。
//
// 参数：
//   - ctx: 上下文
//   - userID: 用户ID
//
// 返回：
//   - *User: 用户对象
//   - error: 查询失败时返回错误
func (s *UserService) GetUserByID(ctx context.Context, userID int64) (*User, error) {
    s.logger.Info(ctx, "查询用户", "user_id", userID)
    
    user, err := s.repo.GetByID(ctx, userID)
    if err != nil {
        s.logger.Error(ctx, "查询用户失败", "user_id", userID, "error", err)
        return nil, fmt.Errorf("获取用户失败: %w", err)
    }
    
    return user, nil
}
```

---

## 特殊情况处理

### 1. 包内私有接口

包内私有接口（小写开头）不需要 I 前缀：

```go
// ✅ 正确：私有接口不用 I 前缀
type configurableService interface {
    setConfig(cfg Config)
}

type validatorAware interface {
    setValidator(v IValidator)
}
```

### 2. 测试 Mock 类型

测试 Mock 类型使用 Mock 前缀：

```go
// ✅ 正确：测试 Mock
type MockEventStore struct {
    events []Event
}

type MockRepository struct {
    data map[int64]Entity
}
```

### 3. 内部实现类型

内部实现类型可以更灵活，但仍需保持一致性：

```go
// ✅ 正确：内部实现
type sqlEventStore struct {
    db IDatabase
}

type memoryCache struct {
    data sync.Map
}
```

---

## 检查清单

在提交代码前，请检查：

- [ ] 所有公共接口都有 I 前缀
- [ ] 所有缩写都使用正确的大小写（ID/HTTP/URL 等）
- [ ] 所有导出类型都有完整的 GoDoc 注释
- [ ] 方法命名使用标准动词（Get/Set/Has/Is等）
- [ ] 布尔值方法使用 Is/Has/Can/Should 前缀
- [ ] 包名是小写单数形式
- [ ] 没有使用蛇形命名或全大写常量名（非Go风格）
- [ ] 注释使用中文且格式正确

---

## 工具支持

### Linter 配置（golangci-lint）

推荐在 `.golangci.yml` 中添加：

```yaml
linters-settings:
  revive:
    rules:
      - name: exported
        severity: warning
        disabled: false
        arguments:
          - "checkPrivateReceivers"
          - "sayRepetitiveInsteadOfStutters"
      - name: package-comments
        severity: warning
      - name: var-naming
        severity: warning
        arguments:
          - ["ID", "HTTP", "URL", "JSON", "XML", "API", "UUID", "SQL", "DB", "RPC"]

  stylecheck:
    checks: ["all"]
```

### IDE 配置

**VS Code** (`settings.json`):
```json
{
  "go.lintTool": "golangci-lint",
  "go.lintOnSave": "package",
  "go.formatTool": "goimports"
}
```

**GoLand/IntelliJ**:
- Settings → Go → Golangci-lint → Enable
- Settings → Editor → Inspections → Go → Naming conventions → Enable

---

## 参考资料

- [Effective Go](https://golang.org/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
- [Google Go Style Guide](https://google.github.io/styleguide/go/)

---

## 版本历史

| 版本 | 日期 | 变更 |
|------|------|------|
| 1.0 | 2025-11-10 | 初始版本，建立核心规范 |

---

## 联系方式

如有疑问或建议，请联系项目维护者。

---

**最后更新**: 2025-11-10  
**文档状态**: 正式发布  
**适用项目**: Gochen Shared v1.0+
