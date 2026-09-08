# Contrib 适配器

`gochen-contrib` 是独立 module，提供第三方生态驱动。Core 定义契约，Runtime 提供 Host、DI、标准 HTTP / SQL 与迁移运行时，Contrib 将具体第三方库接入这些边界。

## 当前能力

| Contrib 路径 | 职责 |
| --- | --- |
| `http/gin` | 实现 Core HTTP 上下文、服务器与路由组契约 |
| `data/db/gorm` | 基于 GORM 的数据库与事务适配 |
| `data/orm/gorm` | 实现 ORM / Model 查询能力及模型到迁移草稿的转换 |
| `data/db/driver` | 数据库驱动注册 |
| `lock/redis` | Redis 分布式锁 |
| `observe/otel` | OpenTelemetry 追踪适配 |
| `observe/prometheus` | Prometheus 指标适配 |
| `migration` | 数据库迁移 CLI 与数据库管理能力，复用 Runtime migration runner |

Contrib 当前使用单一 `go.mod`，业务按包导入所需适配器。Core 与 Runtime 不反向导入 Contrib。Redis 在此提供锁能力；网络消息 Transport 由业务或扩展仓库实现，接入契约见 [messaging](../../messaging/README.md#外部-transport-接入)。

## 装配方式

1. 在组合根创建具体 driver / client 及适配器，处理构造错误。
2. HTTP server 通过 Runtime `host/config.WithHTTPServer` 注入；ORM 交给通用 `db/orm/repo.NewRepo`；锁、追踪和指标通过对应组件的接口注入。
3. 生命周期由 Runtime Host 或业务组合根管理；适配器不创建另一套宿主、DI 或后台调度系统。

常用便捷入口见 [Quick 装配](quick-assembly.md)，完整示例与配置参数见 `gochen-contrib` 仓库对应包的 GoDoc。

## 适配契约

- HTTP 适配器实现 `httpx.IContext`、`IServer`、`IRouteGroup`，保留请求 context、取消信号、统一响应和 body 限制；身份语义通过 `contextx` 传递。
- ORM 适配器准确声明 `Capabilities`，不支持的能力返回 `Unsupported`；`IModel.Dialect()` 返回实际方言，标识符引用与参数绑定按方言处理。
- 事务适配器保证业务 SQL 使用事务 session，并正确处理提交后回调；不能在事务未提交时触发 `PostCommit` 副作用。
- 隔离与授权由受保护 Application 和显式配置的通用 Repo 执行，适配器不得另设默认放行路径。
- Redis 锁按持有者 token 释放和续租，失锁必须可被调用方感知；日志通过 `observe/logging` 注入。
- 第三方错误在边界转换为 `gochen/errors`，保留可诊断的 cause。

数据库草稿能力的使用与限制见[数据库 Schema 与迁移](db-schema-migration-guide.md)。

## 验证

在 `gochen-contrib` 仓库独立执行 `GOWORK=off go build ./...`、`GOWORK=off go vet ./...`、`GOWORK=off go test -count=1 ./...`。真实数据库、Redis 等外部环境测试使用 `integration` tag，并按测试文件说明准备资源。
