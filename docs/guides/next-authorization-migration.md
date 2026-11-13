# next 授权执行迁移

本次为三核心仓协调的破坏性 API 收敛，不修改 main、不保留旧 API 别名。先阅读[执行设计](../architecture/authorization-execution-convergence.md)。

## 单资源

1. 在 `crud.Options.Actions` 声明具名动作，例如 approve；从 `crud.Describe` 取得完整动作清单再编译授权器，不能只编译 approve 后装配含标准 CRUD 动作的应用。
2. 具名服务方法内部调用 `Application.Execute(ctx, "approve", id, fn)`；fn 仅检查调用方版本和领域规则，不另开授权路径，不直接拿仓储写入。
3. 不再使用 CommandResource、Access、grants[0]、CommandRepository 或 Options.CommandGrant。
4. HTTP 对象实现 Runtime `rest.IEndpoint`，Execute 调同一具名业务方法，AuthorizationBinding 从实际 Application 的固定动作取得。业务方法不是 HTTP middleware：Worker 和进程内调用也得到相同授权。

实例：Runtime `examples/business-actions`；跨 net/http/Gin 的真实 SQL 合同见 Contrib `http/gin/typed_module_test.go`。

## 多资源

不将一个大 Command 改名成另一个通用 Runner。业务服务明确列出预检和事务顺序；全部预检成功后才开始副作用，每个 Application 仍执行正式授权。预检/正式判定会分别读取事实和产生治理记录，不承诺一次判定复用。

后续正式判定可能因撤权而拒绝。所有业务写入必须已证明加入同源同一事务才能保证整体回滚。外部消息、付款等不在 SQL 回滚范围，使用 outbox 或提交后执行；混合事件存储也不能自动原子化。

Runtime `examples/business-actions/authorization_test.go` 使用真实 SQLite 两张表，证明预检第二资源拒绝前不调用业务、第二次正式授权撤权时前面写入回滚、提交后回调不执行，以及成功路径同时提交。这不代表任意两仓储具有共同事务。

如项目曾使用可信事实复用，保留提供方身份、主体/租户/目标意图及会话生命周期合同。普通 `Authorize` 自动关闭事实会话；显式 `AuthorizeRequest` 返回的管理请求由调用方 CloseFacts。不要将授权成功布尔值写进 context，或把原 Decision 暴露给业务作为长效凭据。

## 显式 ES

`StateRepository` 是原始回放/提交工具，仍不自动代表业务授权。业务使用新 `StateApplication`，构造期注入 Authorizer、固定动作、可信状态事实投影与资源契约。普通状态 S 不要求继承实体基类。

固定阶段：授权 → 回放并校验基线可见性 → 调用业务决定事件 → 从原基线重放事件 → 检查真实最终状态 → expectedVersion 追加。最终状态不满足 Spec、保护维度或 Assign 时，事件一条也不能落库。不能在追加之后才拒绝，也不能直接修改 ES 状态来满足 Assign；必须由事件归约产生权威值。空事件不进行额外版本守卫，提交后失败仍表示已经提交，禁止当作未提交重试。

## 下游与发布边界

三核心仓内所有调用点和示例一起迁移。历史 IAM、Alife、业务下游的匹配版本源码不在本次云端 checkout，因此不声称其编译、行为或生产接线已验证；实际升级前必须逐项目迁移并验证。不要为绕过破坏性变化在核心恢复旧 Command 兼容层。

Git 的 next 保留此前审计/删除/Module 等改进，按新的增量提交收敛，不回退无关工作，也不重写共享分支历史。
