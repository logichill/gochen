// Package domain 提供 DDD 共享内核 / 建模工具箱。
//
// 本包为业务领域层提供可复用的基础抽象，而非具体业务的领域层。
//
// # 核心抽象。
//
// 实体与事件：
//   - [IEntity] — 实体接口，包含唯一标识与乐观锁版本控制。
//   - [IDomainEvent] — 领域事件接口。
//   - [IValidatable] — 可验证接口。
//   - [ISettableID] — 可选的 ID 回填接口。
//
// 横切能力：
//   - [ITimestamps] — 生命周期时间戳能力（CreatedAt/UpdatedAt）
//   - [Timestamps] — ITimestamps 的默认实现（用于嵌入）
//
// # 子包。
//
//   - [domain/crud] — 简单 CRUD 实体与仓储 ports
//   - [domain/audited] — 审计/软删除能力与仓储 ports
//   - [domain/eventsourced] — 事件溯源聚合根与仓储 ports
//
// # 分层边界。
//
// domain 包不依赖任何外层包（仅依赖标准库与 gochen/errors）。
// 依赖方向：业务领域层 → domain（共享内核）→ 标准库。
//
// # 错误策略。
//
// domain 内部使用 gochen/errors 返回结构化错误，这是共享内核的策略选择。
// 仓储 ports 的错误契约（NotFound / Conflict / Concurrency）通过错误码表达。
//
// # 更多信息。
//
// 详见 domain/README.md。
package domain
