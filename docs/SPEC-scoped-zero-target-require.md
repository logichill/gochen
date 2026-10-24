# Scoped 零目标 Require 行为契约

本文登记 `auth/scoped` 授权判定中"零目标 Require"的行为契约、变更动机与兼容性影响。对应审计条目 P2#6（fingerprint `9a4cca4e0f718be1`，round 1–4 共 5 次确认，定性为有意变更缺守护）。

## 契约

`IAuthorizer.Require` / `IAuthorizer.Authorize` 的判定入口统一收敛到 `Decision.AllowedFor(len(targets))`：

| 调用形态 | 判据 | 语义 |
| --- | --- | --- |
| 有具体目标（`len(targets) > 0`） | `Decision.IsAllowed()` | 必须是 allow **且**授权资源清单非空；空资源 allow 一律 `Forbidden` |
| 零目标（动作级，`len(targets) == 0`） | `Decision.IsActionAllowed()` | 只看动作层 Effect；allow 即放行，`AuthorizedResources` 为空不构成拒绝 |

即：**零目标 Require 时资源为空 → 按 action-only 判定（`IsActionAllowed`）**。零目标调用完全不经由"必须有授权资源"的判据兜底，deny 只能来自评估器返回 deny。

## 动机

零目标的典型场景是列表读：调用方问的是"能不能列这类资源"，可见范围随后由 `DataScope` 在仓储层过滤。此时 PDP 无从也无需回显一份资源清单：

- 旧判据 `IsAllowed` 要求非空授权资源，零目标调用必然被 `Forbidden` 误杀（fail-closed 误伤合法动作级授权）。
- 为满足旧判据而伪造占位资源，会把"空 allow 通行证"这个判据本要防的东西请回来。
- Runtime 评估器（`gochen-runtime/host/authz`）对无目标场景的设计即"动作通过即 allow，且不回填占位资源"；本次变更是 Core 判据与评估器设计的契约对齐。

## 铁律与边界

1. `IsActionAllowed()` **不得**用于写路径或已知目标的读路径——那些场景必须走 `IsAllowed`，由授权资源清单明确界定作用对象。
2. 有具体目标时行为不变：allow 且授权资源为空仍然 `Forbidden`，判据没有放宽。
3. 零目标放行后，实际可见范围仍由 `DataScope`（与 L2 隔离）在数据访问层收敛，放行不等于无界访问。
4. 调用方不得为了绕过 `IsAllowed` 而故意丢弃目标走零目标路径；零目标只用于"本来就没有具体资源目标"的语义。

## 兼容性影响

- 基线（HEAD 70fb8ac）中所有 `Require` 一律要求非空授权资源，零目标 + 空 allow 输入会被拒绝；变更后同一输入放行，方向为 fail-open。
- 现网调用点无行为变化：全仓三处 `Require`（gochen-iam menu/tenant、ems sso）均传 ≥1 目标，`app/security` 链路走 `Authorize` + 自有判据。
- 风险转移：未来任何因调用方缺陷传空目标的**写路径** `Require`，将由基线的 fail-closed 变为放行（若评估器对该动作为 allow）。防线在铁律第 1 条与评审约定，不依赖判据兜底。

## 守护

- 单元判据表：`auth/scoped/decision_test.go` `TestDecision_AllowedFor`。
- 端到端链路（公开决策入口）：`auth/scoped/decision_test.go`
  - `TestRequireZeroTargetAllowsActionLevelAllow` — 空资源 + action 允许 → 放行；
  - `TestRequireZeroTargetDeniesActionDeny` — 空资源 + action 拒绝 → `Forbidden`；
  - `TestRequireWithTargetStillAllowsExplicitResource` / `TestRequireWithTargetStillDeniesEmptyAllow` — 非空资源判据不受影响；
  - `TestAuthorizeZeroTargetReturnsActionLevelDecision` — 决策口原样返回动作级决策。

相关背景见[分层授权](architecture/layered-authz.md)。
