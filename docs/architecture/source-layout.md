# 源码文件组织

同一类型的方法可以在同一个 Go 包内分布到多个文件。文件使用稳定的主体前缀和职责后缀，使相邻文件能表达实现归属；命名规则见 [SPEC](../../SPEC.md#2-命名规范-naming)。

主文件保留类型与构造过程，相关行为及专用辅助逻辑放入对应职责文件。接口、配置和独立状态类型继续按自身职责命名。文件数量由职责决定，不要求每个方法或每个类型独占文件。

## 主要实现入口

| 包 | 主体与实现文件 |
| --- | --- |
| `app/crud`、`app/audited` | `application.go` 与 `application_*.go` 承载读写、批量、钩子、审计和生命周期，具体分工见 [app](../../app/README.md#crud-与审计实现的文件组织) |
| `app/eventsourced` | `command_service.go` 及 `command_service_hooks.go`、`command_service_operation.go`；`command_hooks.go` 和 `command_operation.go` 保留钩子与 resolver 契约；`repository_read.go`、`repository_write.go` 承载普通仓储读写 |
| `app/operation` | `runner_execute.go`、`runner_idempotency.go`、`runner_result.go` 承载执行、幂等和结果构建；`context.go` 定义操作 ID 上下文；`memory_store_*.go` 承载存储扩展 |
| `app/operation/internal/singleflight` | `registry.go` 管理进行中的调用，`call.go` 承载等待与结果，`singleflight.go` 编排 owner 执行与异常收尾 |
| `app/query` | `schema.go`、`schema_validate.go` 管理查询结构与校验；`field.go`、`field_value.go` 定义字段与值转换；`filter_builder.go`、`query_filters.go` 承载条件构建与集合；`range_parse.go`、`range_convert.go` 承载区间解析与类型转换 |
| `app/query/querybind` | `bind.go` 遍历查询目标并选择解码器，`bind_value.go` 承载默认值绑定与类型赋值 |
| `app/security/action`、`app/security/scoped` | `crud_decorator.go` 与 `crud_decorator_read.go`、`crud_decorator_write.go` 组织基础权限包装；scoped 的范围处理位于 `crud_decorator_scope.go`；`batch_decorator.go`、`audited_decorator.go` 保留独立可选能力包装 |
| `auth/governance` | `decorator.go` 固定治理组合顺序，`decorator_snapshot.go`、`decorator_audit.go`、`decorator_metrics.go` 分别实现快照、审计、指标；契约与配置位于 `snapshot.go`、`audit.go`、`audit_sanitize.go`、`metrics.go`、`options.go` |
| `auth/scoped` | `decision.go` 表达判定结果，`authorizer.go` 承载授权执行；`scope_context.go` 承载范围上下文与解析；`resource_registry.go` 承载资源解析器注册表 |
| `cache` | `cache.go` 保留缓存对象与基本读写，`cache_eviction.go` 管理过期、淘汰与回调，`cache_stats.go` 承载统计，`config.go` 定义配置 |
| `clock` | `manual_clock.go` 承载手动时钟与时间推进，`manual_clock_timer.go` 实现其 Timer / Ticker，继续共享时钟锁 |
| `contextx` | `tx_callback.go` 管理事务链路的提交后回调，`after_commit_dispatcher.go` 实现可挂接到事务会话的独立分发器 |
| `process/workflow` | `engine_*.go` 承载启动、推进、驳回、生命周期与钩子执行；`node_hook.go` 保留钩子类型及注册表；内存存储按 `memory_store_definition.go`、`memory_store_instance.go`、`memory_store_query.go` 组织定义、实例和查询 |
| `process/task` | `supervisor.go` 及 `supervisor_schedule.go`、`supervisor_lifecycle.go`、`supervisor_failure.go` |
| `eventing/bus`、`eventing/registry` | 总线的处理器契约和函数适配位于 `handler.go`；事件注册表的反序列化位于 `registry_decode.go` |
| `eventing/monitoring` | `health.go` 管理健康检查注册与汇总，`health_metrics.go` 根据指标生成健康检查 |
| `eventing/outbox` | `publisher_serial_*.go` 与 `publisher_parallel_*.go` 分别承载串行、并行发布；共享发布与回写位于 `publisher_core.go`、`publisher_core_mark.go`；内存仓储按保存、领取续租、结果状态组织为 `memory_repository_*.go` |
| `eventing/projection` | `manager_*.go` 承载注册、事件处理、回放与重建；`projection_runtime.go`、`projection_handler.go` 承载单个投影的运行状态和消息适配；`checkpoint_rebuild.go` 保留重建游标强制对齐与存储适配 |
| `eventing/store` | `memory_store.go` 定义内存事件存储，`memory_store_append.go`、`memory_store_read.go`、`memory_store_batch.go`、`memory_store_index.go`、`memory_store_event.go` 承载追加、读取、原子批量、索引与事件快照辅助逻辑 |
| `eventing/store/cached` | `cached_store_*.go` 承载读写、缓存维护、统计和清理；`event_cache.go`、`cache_stats.go`、`config.go` 定义缓存数据、统计与配置 |
| `eventing/store/decorator` | `context.go`、`tenant.go`、`tracing.go`、`metrics.go` 承载对应的事件存储装饰器 |
| `eventing/store/snapshot` | `snapshot.go` 定义快照数据与契约，`memory_store.go` 实现内存存储，`manager_snapshot.go` 编排快照操作；`strategy_*.go` 按事件数、时间、大小、组合与预设组织策略 |
| `eventing/subscription` | `subscription.go` 定义订阅对象，`subscription_run.go` 承载 Run 循环，`subscription_cursor.go` 管理游标持久化，`subscription_handler.go` 处理事件、重试与异常；配置位于 `config.go` |
| `eventing/upcast` | `upgrade_payload.go` 负责载荷版本升级，`hydrate_payload.go` 负责已升级载荷的类型还原 |
| `httpx` | `route_registry_server.go`、`route_registry_group.go` 分别包装服务器与路由组，`route_path.go` 提供路径工具；`route_builder_register.go`、`route_builder_handler.go` 承载注册与处理链，`permission_middleware.go` 提供独立权限中间件 |
| `messaging` | `bus.go` 及 `bus_publish.go`、`bus_batch.go`、`bus_subscription.go`；`bus_contract.go` 定义总线与中间件契约 |
| `messaging/transport/memory` | `transport.go` 及 `transport_publish.go`、`transport_subscription.go`、`transport_dispatch.go`、`transport_lifecycle.go`、`transport_worker.go` |
| `messaging/transport/direct` | `sync.go` 定义同步传输，`sync_publish.go` 承载发布，`sync_lifecycle.go` 承载启停与停止快照 |
| `messaging/command/middleware` | `idempotency.go`、`idempotency_batch.go`、`idempotency_state.go`；聚合锁使用 `aggregate_lock.go`、`aggregate_lock_handles.go`、`aggregate_lock_cleanup.go` |
| `testkit` | `memory_repository_*.go` 承载读写、批量、事务、内部状态及隔离与授权扩展；`audited_repository_read.go`、`audited_repository_audit.go`、`audited_repository_tx.go` 承载审计仓储的查询、审计记录和事务；`recording_router.go`、`recording_server.go` 分别实现路由记录与服务器 |
| `testkit/assert`、`testkit/require` | 保持对应的 `assert_*.go` / `require_*.go` 文件族，按错误、相等性、值状态、集合、数值比较、执行行为组织断言 |
| `observe` | `tracer_noop.go` 与 `tracer_simple.go` 分别定义空实现和本地实现；`tracer_simple_span.go`、`tracer_propagation.go` 承载 Span 与上下文传播 |
| `observe/logging` | `logger.go` 定义日志接口；`config.go` 定义级别与格式；`field.go`、`field_normalize.go` 承载字段构造与规范化；`logger_write.go` 承载共享输出辅助逻辑 |
| `policy/ratelimit` | `limiter.go` 管理限流器与各 key 的桶，`token_bucket.go` 实现单个令牌桶的补充与消耗 |

## 文件拆分与能力边界

文件拆分共享原对象的字段、锁与事务边界。批量发布的提交作用域、幂等预占、投影串行化和事务回调都由既有实现维护。

装饰器与能力包装器仍按行为契约保留。例如 `app/eventsourced/repository_atomic.go` 只在底层支持原子批量追加时暴露 `SaveAll`；将该方法直接放到普通仓储会改变能力探测结果。权限、缓存、上下文和指标装饰器也各自承担实际行为。

通用的事件存储指标装饰器位于 `eventing/store/decorator`，其职责不依赖缓存。构造入口与迁移说明见 [EventStore](../../eventing/store/README.md#存储装饰器)。

Outbox 的批量标记直接通过 `ParallelPublisher.SetBatchOperations` 配置。原有未接入内层配置的 `BatchPublisher` 包装已删除，迁移说明见 [Outbox](../../eventing/outbox/README.md#批量标记与文件组织)。

## 整理范围与保留原则

检查覆盖 Core 的全部能力目录，以及测试辅助、脚本与示例。整理依据是类型归属、独立职责和真实扩展边界：混在契约文件中的具体实现归回实现文件，多个独立实现按主体分开，单个对象的行为按职责聚合。

已经内聚的代码继续保持原组织。例如 `codec` 的独立编解码器、`db` 的契约与数据库错误识别、`domain` 的实体与事件元数据机制、`errors` 的错误模型、`gen` 的各类生成器、`validate` 的规则族，以及 `schema_infer.go`、工作流 `graph.go`、重试和熔断算法。Saga 已按 `orchestrator_*.go` 组织；示例和脚本按各自场景组织。它们无需为了统一文件长度继续拆分。

测试可以继续按跨文件的行为场景组织，不要求与实现文件一一对应。`require` 的失败终止语义、重建 checkpoint 的强制保存语义、路由包装器的冲突登记与链式调用语义，以及权限包装器的能力探测与授权语义均应保留。
