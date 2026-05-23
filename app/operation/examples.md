# app/operation Examples

> 本文档提供最小样例，帮助不了解讨论背景的人快速理解“什么时候 inline，什么时候 tracked，以及应该怎么接入”。

## 1. CRUD inline

### 适用场景

- 普通后台管理创建/更新；
- 写入成功后结果立即可读；
- 不需要等待读模型收敛。

### 例子

场景：创建一个用户资料。

```go
spec := &operation.Spec{
    Type: "profile.create",
    Mode: operation.ModeInline,
    Resource: &operation.Resource{
        Type: "profile",
    },
    AffectedScopes: []string{
        "profiles:list",
    },
}
result, err := runner.Execute(ctx, spec, func(ctx context.Context) (*operation.Result, error) {
    if err := app.Create(ctx, profile); err != nil {
        return nil, err
    }
    return operation.MergeResult(&operation.Result{
        Result: map[string]any{
            "profile_id": profile.ID,
        },
    }, spec, &operation.Resource{
        ID: strconv.FormatInt(profile.ID, 10),
    }), nil
})
```

### 结果特点

- `mode = inline`
- `status = settled`
- 前端收到后直接刷新 `profiles:list`

## 2. EventSourced inline

### 适用场景

- 使用 command 模式；
- 但命令执行完成后结果立即可见；
- 不需要等待额外收敛条件。

### 例子

场景：一个事件溯源聚合的同步命令执行后，返回结果立即可见。

```go
spec := &operation.Spec{
    Type: "wallet.freeze",
    Mode: operation.ModeInline,
    Resource: &operation.Resource{
        Type: "wallet",
        ID:   walletID,
    },
    AffectedScopes: []string{
        "wallets:list",
        "wallets:detail:" + walletID,
    },
}
result, err := runner.Execute(ctx, spec, func(ctx context.Context) (*operation.Result, error) {
    if err := svc.ExecuteCommand(ctx, cmd); err != nil {
        return nil, err
    }
    return operation.MergeResult(nil, spec, &operation.Resource{ID: walletID}), nil
})
```

### 结论

即使使用 `Command`，也仍然可以是 `inline`。

不要把“用了 command”误等于“必须 tracked”。

## 3. tracked operation

### 适用场景

- 写侧已经返回成功；
- 但 Query Side 结果延迟可见；
- 前端需要知道“已接受 / 收敛中 / 已完成”。

### 例子

场景：完成任务后，需要等待任务、积分、等级读模型全部收敛。

```go
spec := &operation.Spec{
    Type: "task.complete",
    Mode: operation.ModeTracked,
    Resource: &operation.Resource{
        Type: "task",
        ID:   taskID,
    },
    AffectedScopes: []string{
        "tasks:list",
        "tasks:detail:" + taskID,
        "points:" + userID,
        "levels:" + userID,
    },
    SettlementHint: taskCompleteSettlementHint,
}
result, err := runner.Execute(ctx, spec, func(ctx context.Context) (*operation.Result, error) {
    if err := svc.ExecuteCommand(ctx, cmd); err != nil {
        return nil, err
    }
    return operation.MergeResult(nil, spec, &operation.Resource{ID: taskID}), nil
})
```

### 结果特点

- `mode = tracked`
- 初始 `status = accepted`
- 返回 `operation_id`
- 后续由状态查询或 SSE 推进到 `settled`

## 4. 前端消费 operation envelope

### inline

前端收到：

```json
{
  "operation": {
    "mode": "inline",
    "status": "settled"
  },
  "affected_scopes": ["tasks:list"]
}
```

处理方式：

- 不进入 tracking；
- 直接 invalidate `tasks:list`。

### tracked

前端收到：

```json
{
  "operation": {
    "id": "op_123",
    "mode": "tracked",
    "status": "accepted"
  },
  "status_url": "/api/v1/operations/op_123",
  "stream_url": "/api/v1/operations/op_123/stream",
  "affected_scopes": ["tasks:list", "points:5"]
}
```

处理方式：

- 建立 operation tracking；
- 优先用 SSE；
- 无法建立时降级为 polling；
- settled 后 invalidate `tasks:list` 与 `points:5`。

## 5. 反例

### 反例 1：所有 command 都 tracked

错误原因：

- 技术实现风格替代了语义判断；
- 会让大量同步命令也进入重型路径。

### 反例 2：把 operation 直接做进 CommandBus

错误原因：

- 让 `CommandBus` 职责膨胀；
- CRUD 写入口无法统一；
- 写入口协议与消息分发机制被错误耦合。

### 反例 3：前端只看 success/failure

错误原因：

- 无法表达 `accepted / processing / degraded`；
- 读模型延迟可见时只能散落刷新逻辑。
