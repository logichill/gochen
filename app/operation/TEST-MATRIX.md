# app/operation Test Matrix

本文档用于收口 `app/operation` 的验证面，避免后续新增写操作时只验证“能返回 200”，却遗漏协议、收敛与可观测性。

## 1. 推荐测试矩阵

| 场景 | 后端验证 | 前端验证 | 当前覆盖 |
| --- | --- | --- | --- |
| CRUD `inline` | `Runner` 返回 `settled`；不生成 tracked URL | `handleOperationResult` 直接 invalidate scopes | `gochen/app/crud` |
| EventSourced `inline` | `Runner.Execute + ExecuteCommand` 返回统一 envelope | 页面无额外 polling | `gochen/app/eventsourced` |
| `tracked` 初始接受 | envelope 带 `operation.id/status_url/stream_url/retry_after_ms` | store 记录 operation 占位 | `gochen/app/operation`、`alife` |
| `tracked` accepted -> processing | 未收敛时状态自动推进为 `processing` | 前端继续 stream/poll | `gochen/app/operation/Tracker` |
| `tracked` settlement | business checker 返回已收敛后写回 `settled` | invalidate 受影响 scopes | `alife/internal/progress/router`、`satellite/operation` |
| SSE 优先 | `GET /operations/:id/stream` 推送最新 envelope | stream 成功时不再触发 polling | `alife`、`galaxy` |
| polling fallback | stream 失败后仍能轮询到 `settled` | operation store 标记 stream 失败但不中断 | `galaxy`、`alife-ui` |
| durable store | 重载后仍可按 operation id 读取状态 | 前端刷新页面后仍能继续跟踪 | `alife/internal/progress/repo/operation` |
| degraded/timeout | 无 status/stream 或轮询耗尽时进入 `degraded/timeout` | UI 不阻塞，并执行必要 invalidate | `satellite/operation` |

## 2. 新增业务写操作时的最小检查单

1. 后端是否明确声明 `inline` 或 `tracked`，而不是默认猜测。
2. `tracked` 操作是否填写 `AffectedScopes`。
3. 若存在读侧延迟，可否通过 settlement checker 判断“现在已可见”。
4. 是否暴露了 status query / SSE stream 中至少一种，另一种由前端降级兜底。
5. 前端 store 是否统一走 `settleOperationResult(...)` 或同等级能力，而不是页面自行写 refresh/polling。
6. 是否补了至少一条协议测试和一条真实链路测试。

## 3. 可观测字段建议

对 `tracked` 操作，建议日志/埋点统一携带以下字段：

- `operation.id`
- `operation.type`
- `operation.mode`
- `operation.status`
- `resource.type`
- `resource.id`
- `affected_scopes`
- `retry_after_ms`
- `status_url`
- `stream_url`
- `settlement.result`：`settled | pending | error`
- `transport`：`stream | poll | inline`
- `degraded_reason`：如 `missing_status_url`、`stream_failed`、`polling_exhausted`

## 4. 回归命令建议

后端框架：

```bash
go test ./app/operation ./app/crud ./app/eventsourced -count=1
```

业务后端：

```bash
go test ./internal/progress/router ./internal/progress/... -count=1 -mod=mod
go test ./... -count=1 -mod=mod
```

前端框架：

```bash
npm test -- src/galaxy/satellite/operation/runtime.test.ts
npm run typecheck
```

业务前端：

```bash
npm run typecheck
npx eslint src/modules/tasks/store/TaskStore.ts src/modules/honors/service/operations.ts
```
