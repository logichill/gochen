// Package audited 提供审计/软删除场景的实体接口与仓储 ports。
//
// 适用于需要记录操作人、支持软删除与恢复的场景。
//
// # 核心类型。
//
// 能力接口：
//   - [IAuditable] — 审计追踪接口（扩展 domain.ITimestamps）
//   - domain.ISoftDeletable — 软删除接口（领域最小语义）。
//   - [IAuditedEntity] — 带审计与软删除能力的实体接口。
//
// 实体基类：
//   - [AuditedEntity] — 审计实体泛型基类。
//
// 仓储 ports：
//   - [IAuditedRepository] — 带审计语义的实体仓储接口。
//   - [IAuditStore] — 审计记录存储接口。
//   - [IRestoreRepository] — 支持读取已软删实体的仓储能力。
//   - [IDeletedQueryRepository] — 支持查询已删除列表的仓储能力。
//
// 审计记录：
//   - [AuditRecord] — 审计记录结构。
//
// # 与 ITimestamps 的关系。
//
// IAuditable 扩展了 domain.ITimestamps，额外包含操作人（By）信息。
// 实现 IAuditable 的实体同时满足 ITimestamps 接口。
//
// # 版本号递增。
//
// SetUpdatedInfo 和 SetUpdatedAt 设置审计信息，保留当前版本。
// CRUD/Audited 仓储以写入前版本约束原子更新，并统一计算下一版本。
//
// # 软删除语义。
//
//   - SoftDelete/SoftDeleteBy: 重复软删返回 errors.Conflict
//   - Restore: 对未删除实体恢复返回 errors.Conflict
package audited
