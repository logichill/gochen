// Package auth 是安全能力的命名空间根，**本身不含任何实现**。
//
// L1/L3/L4 能力按三个正交子包组织，各自对应一个渐进档位（见
// docs/architecture/framework-design.md）：
//
//	auth/action      L1  当前主体能否执行该动作（权限码）
//	auth/scoped      L3  主体在什么数据范围内、能对哪条资源做什么
//	auth/governance  L4  策略快照一致性、决策审计留痕与监控
//
// 依赖拓扑（严格单向，禁止反向）：
//
//	action     -> errors
//	scoped     -> errors
//	governance -> scoped, clock, gen, errors
//
// L2 直接复用 contextx.TenantID 与 domain/crud.ITenantEntity，由具体 Repo 执行隔离；
// 不为它建立只有测试消费的转发契约包。
//
// 分包而不是分 Option 的原因：Go 以 package 为编译单元，
// 单纯增加配置项或 no-op 实现无法阻断依赖传递。
// 只有物理包边界才能让 L0/L1 场景真正不把高级安全能力编译进来。
//
// 其它约定：
//   - 全部能力默认 fail-closed：配置缺失、解析失败、范围为空一律拒绝；
//   - 安全 PEP 装饰器位于 gochen/app/security/*，
//     标准装配入口位于 runtime/security；
//   - 权限目录（registry / catalog sync）属于 Host 可选扩展，
//     位于 runtime/host/authz。
package auth
