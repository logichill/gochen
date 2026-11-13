// Package auth 提供强类型声明编译、统一策略执行、可信事实与授权治理。
//
// 组合根通过 Compile 将 Descriptor、实体元数据、规则及可信提供方连接为冻结计划。
// Application 消费 IAuthorizer；多资源业务先预检全部资源，再由各应用正式授权并执行条件写入。
// 请求 IDs 与 Targets 表达客户端意图，NewFact 注册的提供方按需读取可信投影。
// 事实会话绑定本次内核、身份和意图，读取及返回值各自保持独立。
//
// New 组合 IPolicy 与 GovernanceOptions，执行固定的版本解析、判定校验、审计和指标链。
// 特殊领域规则实现 IPolicy 或 ISpecResolver，通用动作检查和交集组合由根包承担。
// OnPermissionDenied 仅在动作权限拒绝后进入；范围拒绝和提供方故障终止执行。
//
// auth/action 保留权限码及通配工具；auth/scoped 保留 DataScope、条件构造与身份绑定。
// L0–L4 表达职责：应用编排、动作权限、仓储隔离、范围条件和决策治理。
// 仓储继续以 contextx.TenantID 执行隔离，并原子落实 Decision.Spec、版本和归属保护。
//
// ValidateEntries 将驱动独立枚举的入口与编译计划逐项核对。
// Runtime Host 在业务模块与传输启动前执行门禁；显式放行同样需要来源与理由。
// 权限目录、物理存储和 Host 装配由 gochen-runtime 提供。
package auth
