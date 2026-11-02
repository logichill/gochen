# Operation 契约

本规范描述 `app/operation` 的现行数据与执行语义。装配示例见 [README](README.md)。

## 数据模型

| 类型 | 字段与用途 |
| --- | --- |
| `Resource` | `Type`、`ID`，标识操作关联资源 |
| `Spec` | 必填 `Type`、`Mode`；可选 Resource、AffectedScopes、IdempotencyKey、Metadata、SettlementHint |
| `Operation` | `ID`、`Type`、`Mode`、`Status` |
| `Result` | 嵌套 Operation，以及 Resource、Result、Error、AffectedScopes、StatusURL、StreamURL、RetryAfterMs |
| `OperationError` | `Code`、`Message`、`Details` |

`Spec.Metadata` 与 `SettlementHint` 是调用方协议元信息，不会自动复制到结果或由 Runner 执行。字段类型与 JSON tag 以 [spec.go](spec.go) 和 [result.go](result.go) 为准。

结果示例：

```json
{
  "operation": {
    "id": "op-123",
    "type": "order.update",
    "mode": "tracked",
    "status": "accepted"
  },
  "resource": {"type": "order", "id": "42"},
  "result": {"version": 3},
  "affected_scopes": ["order:list"],
  "status_url": "/operations/op-123",
  "stream_url": "/operations/op-123/stream",
  "retry_after_ms": 300
}
```

URL 路径只是示意，实际地址由接入方提供。

## 执行与状态

统一包装接口为 `IRunner.Execute(ctx, spec, Handler) (*Result, error)`。`NewRunner` 和 `DefaultRunner` 返回实现该接口的 `*Runner`。

1. 校验非 nil 且未取消的 ctx、非空 Type、有效 Mode 和非 nil handler。
2. tracked 生成非空 ID，并写入 handler 的派生 context；inline 不生成 ID。
3. 在当前调用栈执行 handler，合并结果；tracked 在配置 Store 时保存信封。

外部迁移入口使用 `Runner.ExecuteTransfer(ctx, spec, expectedVersion, handler)`，复用上述执行流程。入口要求 tracked 模式、非空白且通过 Spec.Validate 的幂等键、Resource.Type / ID、调用方版本及 handler；Store 同时实现 `IIdempotencyStore` 与 `IIdempotencyReservationStore`。主体使用 Principal.SubjectID，未绑定 Principal 时使用 contextx.Operator；缺失主体返回 InvalidInput。入口将租户、主体、操作类型、资源类型、实体 ID 和原始键编码后计算 SHA-256，统一用于结果查询与并发预占，输入 Spec 保持原值。版本 0 合法，缺失版本或最大 uint64 版本返回 InvalidInput。校验通过后将调用方版本原样传入 handler；相同作用域中的有效幂等键命中时返回首次结果及其 OperationID。

`Operation.ID/Type/Mode` 由 Runner 设置。inline 成功固定为 settled；tracked 默认 accepted，也接受 handler 返回的有效非 failed 状态。handler 返回普通 error 时生成 failed 信封；返回 contextx.AfterCommitError 时生成 degraded 终态，保留错误，表示主操作已完成但后续阶段失败，不应重试整个业务。业务回调成功后结果保存失败，返回原结果与 AfterCommitError，并保留幂等预占；调用方应依据返回的 operation ID 核对业务并补存结果，不能通过释放预占重跑主操作。调用方必须同时检查结果和 error。

| 状态 | 含义 | 终态 |
| --- | --- | --- |
| `accepted` | 已接受，等待可见结果 | 否 |
| `processing` | 尚未收敛 | 否 |
| `settled` | 已收敛 | 是 |
| `failed` | 操作失败 | 是 |
| `timeout` | 业务判定跟踪超时 | 是 |
| `degraded` | 业务判定以降级结果结束 | 是 |

Tracker 只负责 accepted → processing 与已收敛 → settled；timeout / degraded 由业务判断并写入。该包不执行重试队列、Saga 补偿或 Workflow 节点调度。

## 合并与错误

- Resource 逐字段优先取 handler 结果，再取 Spec；`MergeResult` 的显式 resource 参数位于二者之间。
- AffectedScopes 合并并去重。tracked 的 URL 和 RetryAfterMs 先使用 handler 值，缺省时使用 RunnerOptions。
- 信封和 Store 返回值使用快照，调用方不应借共享可变对象修改已保存状态。
- 由 error 生成的 `OperationError` 使用框架错误码，内部错误消息和敏感 details 会脱敏；handler 自行填写的业务结果仍由业务保证可对外公开。

## 存储与幂等

`IStore` 提供 Get / Put / Delete，未命中必须映射到 `errors.NotFound`。Store 可选，但可对外查询的 tracked 操作必须由接入方配置 Store 与查询入口。

非空幂等键仅用于 tracked，最多 256 字节，不含空白或控制字符。Runner 要求 Store 实现 `IIdempotencyStore`，按键读取既有结果并原子保存结果与键绑定。业务应将租户、操作与请求身份等必要维度纳入键，避免无关请求复用结果。

- 同一 Runner 实例按幂等键合并并发调用。
- 多实例避免重复执行 handler，需 Store 实现 `IIdempotencyReservationStore`，在回调前原子预占。
- `IIdempotencyReservationReleaseStore` 仅释放相同 operation ID 的未完成预占，不能删除最终结果或他人的占用。
- 仅实现结果保存原子性，不能保证多个进程不会同时执行业务回调；业务写入仍须满足自身的事务和幂等要求。

`MemoryStore` 使用锁保护数据，Delete 的有界 tombstone 用于阻止近期在途结果重新出现，不提供跨进程持久性。基础接口见 [store.go](store.go)，条件写入见 [store_transition.go](store_transition.go)。

## Tracker 与 Stream

`Tracker.Load` 读取并刷新结果；`ISettlementChecker` 由业务判断读模型或外部结果是否已可见。配置 Store 时要求 `IStatusTransitionStore`，只在原状态仍匹配时更新状态，保留存储中的其他字段；状态不匹配时返回当前结果，记录缺失返回 NotFound。Refresh 不再创建记录。遇到终态不调用 checker；checker 接收结果副本，仅作判定。Store 与 checker 的错误直接返回。

`IResultRecoveryStore.RecoverResult` 只补存缺失结果或完成同一 ID、原键的预占；同 ID 已有结果优先。普通 Put 是全量替换，不能代替条件推进或并发补存。业务修改收敛依据、身份字段或全量结果时仍须自行协调。

Runtime 的 `gochen-runtime/http/operation.Stream` 通过调用方的 load 函数轮询，首次或结果变化时发送 SSE，空闲时发送 keepalive，终态结束；ctx 取消时停止。接入方负责响应头、路由、鉴权、资源归属与保留周期。默认事件名为 `operation`，轮询间隔为 300ms，keepalive 为 15s，可通过 StreamOptions 调整。
