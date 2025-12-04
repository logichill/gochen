# 公共模拟组件

这个包提供了示例中使用的公共模拟组件，避免在每个示例中重复定义相同的代码。

## 组件列表

### 1. 路由器 (Router)

#### MockRouter
基础的模拟路由器，适用于简单的 CRUD 示例。

```go
router := mocks.NewMockRouter()
```

#### MockAdvancedRouter
支持中间件的模拟路由器，适用于高级配置示例。

```go
router := mocks.NewMockAdvancedRouter()
```

#### MockRouterWithMiddleware
完整功能的模拟路由器，支持中间件链，适用于中间件示例。

```go
router := mocks.NewMockRouterWithMiddleware()
```

### 2. HTTP 上下文 (HttpContext)

#### MockHttpContext
模拟 HTTP 请求上下文，包含所有必需的接口方法。

```go
ctx := mocks.NewMockHttpContext("GET", "/api/users")
ctx.Headers["Authorization"] = "Bearer token"
```

#### MockHttpContextWithHeaders
带请求头功能的 HTTP 上下文模拟。

```go
ctx := mocks.NewMockHttpContextWithHeaders("GET", "/api/users")
ctx.Header("Authorization", "Bearer token")
```

### 3. 仓储 (Repository)

#### GenericMockRepository[T]
通用的模拟仓储实现，支持任意实体类型。

```go
type User struct {
    ID   int64  `json:"id"`
    Name string `json:"name"`
}

func (u *User) GetID() int64 { return u.ID }
func (u *User) SetID(id int64) { u.ID = id }

// 创建仓储
repo := mocks.NewGenericMockRepository[*User]()

// 使用
user := &User{Name: "张三"}
err := repo.Create(ctx, user)
```

支持的方法：
- Create - 创建实体
- GetByID - 根据 ID 查询
- Update - 更新实体
- Delete - 删除实体
- List - 查询列表（支持分页）
- Count - 统计数量
- Exists - 检查是否存在
- BatchCreate - 批量创建
- BatchUpdate - 批量更新
- BatchDelete - 批量删除

### 4. 验证器 (Validator)

#### GenericValidator[T]
通用验证器，支持自定义验证逻辑。

```go
validator := mocks.NewGenericValidator[*User](func(user *User) error {
    if user.Name == "" {
        return service.NewValidationError("用户名不能为空")
    }
    return nil
})
```

#### SimpleEntityValidator
简单的实体验证器。

```go
validator := mocks.NewSimpleEntityValidator(func(entity any) error {
    // 自定义验证逻辑
    return nil
})
```

## 使用示例

完整的示例代码：

```go
package main

import (
    "context"
    "log"

    "gochen/domain/crud"
    "gochen/domain/entity"
    "gochen/examples/internal/mocks"
    restful "gochen/app/restful"
)

// 定义实体
type User struct {
    ID   int64  `json:"id"`
    Name string `json:"name"`
}

// 实现 IEntity 接口
func (u *User) GetID() int64 { return u.ID }
func (u *User) SetID(id int64) { u.ID = id }

func main() {
    // 创建仓储
    repo := mocks.NewGenericMockRepository[*User]()

    // 创建验证器
    validator := mocks.NewGenericValidator[*User](func(user *User) error {
        if user.Name == "" {
            return errors.NewValidationError("用户名不能为空")
        }
        return nil
    })

    // 创建服务（此处示例，实际请使用应用层 Application）
    userService := crud.NewCRUDService[*User, int64](repo)

    // 创建路由器
    router := mocks.NewMockRouter()

    // 注册 RESTful API
    err := restful.RegisterRESTfulAPI(router, userService, validator)
    if err != nil {
        log.Fatalf("注册 API 失败: %v", err)
    }

    router.PrintRoutes()
}
```

## 注意事项

1. 这些组件仅用于示例和测试目的
2. 不要在生产环境中使用这些模拟组件
3. 所有的模拟组件都是内存存储，重启后数据会丢失
4. GenericMockRepository 支持并发访问，使用了读写锁
