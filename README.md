# Gochen

Gochen 是面向企业业务系统的 Go 框架，提供领域建模、应用服务、事件与消息、授权、流程和基础设施契约。本仓库是 Core，module path 为 `gochen`，使用 Go 1.27，生产代码、测试和示例均无第三方依赖。

## 仓库边界

| 仓库 / module | 职责 |
| --- | --- |
| `gochen` | 领域与应用契约、事件和消息内核、安全语义、流程运行时、内存实现 |
| `gochen-runtime` | Host / DI、配置加载、net/http、REST、SQL 仓储与迁移、安全装配 |
| `gochen-contrib` | Gin、GORM、Redis 锁、OTel、Prometheus、数据库驱动与迁移 CLI |

依赖方向为 `gochen-runtime → gochen`，`gochen-contrib → gochen-runtime / gochen`。业务项目在组合根创建并注入具体实现，按需选择 CRUD、审计型 CRUD 或事件溯源。

## 主要能力

| 能力 | Core 包 |
| --- | --- |
| 领域建模与应用模板 | `domain`、`app/crud`、`app/audited`、`app/eventsourced` |
| 查询与写操作协议 | `app/query`、`app/operation` |
| 授权契约与应用层保护 | `auth/action`、`auth/scoped`、`auth/governance`、`app/security` |
| 事件驱动 | `eventing/store`、`eventing/bus`、`eventing/outbox`、`eventing/projection` |
| 消息通信 | `messaging`、`messaging/command`、同步和内存异步 Transport |
| 过程与控制策略 | `process/saga`、`process/workflow`、`process/lock`、`process/task`、`policy` |
| 数据与 HTTP 抽象 | `db`、`db/orm`、`httpx` |
| 通用能力 | `errors`、`contextx`、`observe`、`clock`、`codec`、`gen`、`validate`、`cache`、`testkit` |

## 本地使用与验证

当前模块采用本地 workspace 或 `replace` 接入，具体配置见[下游接入指南](docs/guides/downstream-guide.md)。在本仓库独立验证：

```bash
GOWORK=off go list -m all
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test -count=1 ./...
```

第一条命令的输出应只有 `gochen`。Core 不应包含 `go.sum` 或第三方 `require`。

运行 Core 示例：

```bash
GOWORK=off go run ./examples/messaging/deadletter
GOWORK=off go run ./examples/task/policy
```

Host、REST、SQL 和安全装配示例位于 `gochen-runtime` 仓库 `examples/`。

## 文档入口

- [文档索引](docs/README.md)：模块说明与专题导航。
- [下游接入指南](docs/guides/downstream-guide.md)：依赖接入、能力选择与组合根。
- [框架架构](docs/architecture/framework-design.md)：职责边界与运行约定。
- [开发规范](SPEC.md)与[仓库约定](AGENTS.md)：贡献者规则。
- [示例索引](examples/README.md)。

## 许可证

[MIT License](LICENSE)。
