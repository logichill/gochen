// Package crud 提供简单 CRUD 场景的实体基类与仓储 ports。
//
// 适用于配置、字典、简单管理等不需要复杂审计的场景。
//
// # 核心类型。
//
// 实体基类：
//   - [Entity] — CRUD 实体泛型基类（仅 ID + Version）
//
// 仓储 ports：
//   - [IRepository] — 简单 CRUD 仓储接口。
//   - [IQueryRepository] — CRUD 读扩展接口。
//   - [IBatchOperations] — 批量操作接口。
//
// # 查询协议（已外移）
//
// 通用“适配器查询协议”（过滤/排序/字段投影/分页）已迁移到 `gochen/db/query`，
// 避免 domain 层长期承载与 HTTP/ORM 绑定的查询语义。
//
// # 错误契约。
//
//   - Get: 未找到返回 errors.NotFound
//   - Create: 已存在/唯一约束冲突返回 errors.Conflict
//   - Update: 未找到返回 errors.NotFound；乐观锁冲突返回 errors.Concurrency
//   - Delete: 默认实现可幂等（未找到不报错）
//   - Exists: 仅在查询失败时返回错误。
//   - List/Count: 空结果返回空 slice/0 + nil
package crud
