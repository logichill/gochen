# app/operation Implementation Guide

> 目标：把 `README` 的设计语言与 `SPEC` 的硬约束，落成一份可直接指导编码的实现说明。

## 1. 实现者先建立正确心智

`app/operation` 不是一个新的业务层，也不是 `Command` 的替代品。

它的推荐放置位置是：

- 在 application service 边界包装写入口；
- 用统一的 `Spec -> Execute -> Result` 模式暴露写操作执行过程；
- 让业务继续保留自己的 service / command / aggregate 语义。

一句话：

> `operation` 统一的是“写入口返回协议”，不是“消息分发机制”。

## 2. 推荐分层

第一阶段建议把结构压到最薄：

- `mode.go`
- `status.go`
- `spec.go`
- `result.go`
- `runner.go`

不要第一阶段就拆出大量子包。

推荐职责分配：

- `Mode`：区分 `inline` / `tracked`
- `Status`：描述生命周期状态
- `Spec`：业务声明的写操作规格
- `Result`：统一 envelope
- `Runner`：统一包装入口

## 3. 推荐的最小模型

### 3.1 Spec

`Spec` 由业务显式声明，至少包含：

- `Type`
- `Mode`
- `Resource`
- `AffectedScopes`
- `IdempotencyKey`（可选）
- `Metadata`（可选）
- `SettlementHint`（可选）

要求：

- `Type` 必填，例如 `task.create`
- `Mode` 必填，不自动推断
- `AffectedScopes` 建议由业务声明，不由框架硬编码

### 3.2 Result

`Result` 是统一返回值，不是简单业务 DTO。

最低建议字段：

- `Operation`
- `Resource`
- `Result`
- `Error`
- `AffectedScopes`
- `StatusURL`
- `StreamURL`
- `RetryAfterMs`

注意：

- `inline` 也建议保留 `operation` 节点；
- `tracked` 必须有稳定的 `operation.id`。

### 3.3 Runner

第一阶段最实用的抽象就是：

```go
type Runner interface {
    Execute(ctx context.Context, spec *Spec, fn func(ctx context.Context) (*Result, error)) (*Result, error)
}
```

为什么是这个形态：

- `fn` 保留业务写逻辑原样；
- `Runner` 统一包裹前后处理；
- 不需要让业务代码学习新的 handler 体系；
- 可以同时兼容 CRUD 和 EventSourced。

## 4. Runner 的职责边界

`Runner` 应该负责：

- 校验 `ctx` 与 `spec`
- 校验 `Type / Mode`
- 对 `inline` / `tracked` 做模式分流
- 统一生成 `operation` 节点
- 合并业务返回的 `Resource / Result / AffectedScopes`
- 在失败时生成统一失败 envelope 或错误语义

`Runner` 不应该负责：

- 重写具体业务逻辑
- 取代业务 service / repository
- 把所有命令都变成状态机
- 逼迫业务直接操作 tracked 底层状态

## 5. `inline` 推荐行为

`inline` 是默认路径，应尽可能轻。

推荐行为：

1. 校验 `spec`
2. 调用 `fn`
3. 成功时统一补 `operation.mode = inline`
4. 成功时统一补 `operation.status = settled`
5. 失败时统一补 `operation.status = failed`
6. 返回统一 `Result`

第一阶段允许：

- 没有真实持久化的 operation 记录
- 没有 status query / stream 能力

## 6. `tracked` 第一阶段推荐行为

第一阶段的 `tracked` 只要求“协议可扩展”，不要求所有能力补齐。

推荐行为：

1. 校验 `spec`
2. 生成 `operation.id`
3. 调用 `fn`
4. 若业务写成功，返回：
   - `mode = tracked`
   - `status = accepted`
   - `status_url`
   - 可选 `stream_url`
   - 可选 `retry_after_ms`

第一阶段允许先不实现：

- durable operation store
- SSE
- polling fallback
- 复杂 settlement 协调器

但必须保证：

- 协议形状稳定
- 后续能平滑扩展而不推翻第一阶段接口

## 7. 为什么不要先桥接 CommandBus

原因不是“做不到”，而是“不优雅”：

- `CRUD` 写入口根本不经过 `CommandBus`
- 如果把 `operation` 挤进总线层，会导致语义耦合
- `messaging/command` 会从“命令语义层”膨胀成“写操作运行时平台”
- 会诱导“所有 command 默认 tracked”的错误设计

因此，正确接法是：

- 业务服务仍然调用原本的 CRUD / Command 执行逻辑
- application service 在外围用 `Runner.Execute` 包装

## 8. CRUD 接入模式

推荐接法不是改掉 CRUD 的主体行为，而是提供可选扩展点。

目标文件：

- `/home/alex/data/priv/project/gochen/app/crud/application.go`

推荐思路：

1. 保持 `Application` 不感知 operation 依赖
2. 在调用方用 `Runner.Execute(...)` 显式包装写操作：
   - `Create`
   - `Update`
   - `Delete`
3. 原 `Create/Update/Delete` 语义保持不变
4. 需要 envelope 时由调用方显式构造统一 `Result`

推荐模式示意：

```go
result, err := runner.Execute(ctx, spec, func(ctx context.Context) (*operation.Result, error) {
    if err := s.repository.Create(ctx, entity); err != nil {
        return nil, err
    }
    return &operation.Result{
        Resource: &operation.Resource{Type: "user", ID: id},
        Result: map[string]any{"id": id},
    }, nil
})
```

关键点：

- CRUD 不应因为接入 operation 被迫放弃当前 facade；
- operation 是可选增强，而不是硬依赖。

## 9. EventSourced 接入模式

目标文件：

- `/home/alex/data/priv/project/gochen/app/eventsourced/command_service.go`

推荐思路：

1. 保持 `ExecuteCommand` 仍是命令执行入口
2. 把命令执行包进 `Runner.Execute`
3. 由业务决定该命令是 `inline` 还是 `tracked`
4. `EventSourcedService` 本身不暴露 `WithOperation` 变体

示意：

```go
result, err := runner.Execute(ctx, spec, func(ctx context.Context) (*operation.Result, error) {
    if err := service.ExecuteCommand(ctx, cmd); err != nil {
        return nil, err
    }
    return &operation.Result{
        Resource: &operation.Resource{Type: "task", ID: taskID},
    }, nil
})
```

关键点：

- EventSourced 不应因为“用了 command”就默认 tracked；
- 命令服务继续专注加载聚合、执行 handler、保存事件；
- operation 只提供执行过程表达。

## 10. 业务层如何声明 policy

框架只给机制，不替业务猜 policy。

业务层应负责声明：

- `Type`
- `Mode`
- `Resource`
- `AffectedScopes`
- 可选 `SettlementHint`

建议 policy 口径：

- `Type`：稳定、可观察、面向业务动作，如 `task.create`
- `AffectedScopes`：面向前端/缓存的失效域，而不是内部技术实现细节
- `Mode`：基于可见性与收敛语义判断

## 11. 对 JSON / HTTP 契约的建议

HTTP 层应尽量直接暴露 operation envelope，而不是再包一层新的业务响应语义。

至少应保证：

- 成功与 accepted 都能统一落在 `data.operation`
- 失败也能表达 operation 状态
- `affected_scopes` 稳定暴露给前端

如果后端每个业务路由都手动拼装结构，说明接入点还不够统一。

## 12. 第一阶段的实现边界

第一阶段应该做到：

- `inline` 真正跑通
- `tracked` 协议稳定
- CRUD / EventSourced 都能接

第一阶段不要抢先做：

- 完整持久化 store
- 完整 settlement DSL
- SSE / polling / timeout / degraded 的所有基础设施
- 复杂编排 / 工作流能力

## 13. 评审时重点看什么

完成后重点检查这些问题：

1. `operation` 是否仍然只是“执行过程外壳”？
2. 默认路径是否仍然轻量？
3. `CRUD` 与 `EventSourced` 的接入体验是否足够一致？
4. 旧行为在未启用 operation 时是否完全不变？
5. 是否有人开始把 `operation` 当作新的业务 handler 体系？

只要第 5 条开始出现，就说明实现方向错了。
