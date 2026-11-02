// Package auth 提供统一授权端口、身份与共享值对象，以及 Allow / All 授权器。
//
// L1/L3/L4 能力按三个正交子包组织，各自对应一个渐进档位（见
// docs/architecture/framework-design.md）：
//
//	auth/action      L1  当前主体能否执行该动作（权限码）
//	auth/scoped      L3  主体在什么数据范围内、能对哪条资源做什么
//	auth/governance  L4  策略版本留痕、决策审计与监控
//
// 依赖拓扑（严格单向，禁止反向）：
//
//	action     -> auth, domain, errors, internal
//	scoped     -> auth, action, domain, errors, internal
//	governance -> auth, clock, gen, errors, internal
//
// L2 复用 contextx.TenantID 与资源隔离绑定，由具体 Repo 执行隔离；
// 不为它建立只有测试消费的转发契约包。
//
// 分包而不是分 Option 的原因：Go 以 package 为编译单元，
// 单纯增加配置项或 no-op 实现无法阻断依赖传递。
// 只有物理包边界才能让 L0/L1 场景真正不把高级安全能力编译进来。
//
// 其它约定：
//   - 动作权限与数据范围缺失时拒绝，快照解析失败默认拒绝；
//   - 治理审计默认尽力写入，WithStrictAudit 可改为阻断；指标失败保持业务判定；
//   - 安全 PEP 由业务 Application 直接调用 `auth.IAuthorizer` 完成，
//     决策中的 `spec.Spec` 交给仓储执行；
//   - 权限目录（registry / catalog sync）属于 Host 可选扩展，
//     位于 gochen-runtime/host/authz。
package auth
