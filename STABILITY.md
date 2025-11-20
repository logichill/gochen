# Gochen Shared - API 稳定性保证

**版本**: 1.0  
**更新日期**: 2025年  

---

## 版本规范

Gochen Shared 遵循 [语义化版本 2.0.0](https://semver.org/lang/zh-CN/)：

- **主版本号 (MAJOR)**: 不兼容的 API 变更
- **次版本号 (MINOR)**: 向下兼容的功能新增
- **修订号 (PATCH)**: 向下兼容的问题修复

---

## API 稳定性分级

### 🟢 稳定 API (Stable) - v1.0+

这些 API 已经过充分测试和实践验证，承诺向后兼容：

#### 领域层 (Domain Layer)

- ✅ `domain/entity`
  - `IEntity[T]` - 实体接口
  - `IAggregate[T]` - 聚合根接口
  - `IEventSourcedAggregate[T]` - 事件溯源聚合
  - `EntityFields` - 通用实体字段
  - `Aggregate[T]` - 基础聚合实现
  - `EventSourcedAggregate[T]` - 事件溯源聚合实现

- ✅ `domain/repository`
  - `IRepository[T, ID]` - 基础仓储接口
  - `IAuditedRepository[T, ID]` - 审计仓储接口
  - `IEventSourcedRepository[T, ID]` - 事件溯源仓储接口
  - `IBatchOperations[T, ID]` - 批量操作接口
  - `ITransactional` - 事务管理接口

#### 事件系统 (Eventing)

- ✅ `eventing`
  - `IEvent` - 事件接口
  - `IStorableEvent` - 可存储事件接口
  - `Event` - 事件实现
  - `NewEvent()` / `NewDomainEvent()` - 事件创建函数

- ✅ `eventing/store`
  - `IEventStore` - 事件存储核心接口
    - `AppendEvents()`
    - `LoadEvents()`
    - `StreamEvents()`
  - `IAggregateInspector` - 聚合检查器接口（可选扩展）
  - `IEventStoreExtended` - 扩展事件存储接口（可选扩展）

- ✅ `eventing/bus`
  - `IEventBus` - 事件总线接口

#### 消息系统 (Messaging)

- ✅ `messaging`
  - `IMessage` - 消息接口
  - `IMessageHandler` - 消息处理器接口
  - `IMessageBus` - 消息总线接口
  - `ITransport` - 传输层接口
  - `Message` - 消息实现
  - `MessageBus` - 消息总线实现

- ✅ `messaging/command`
  - `ICommand` - 命令接口
  - `ICommandHandler[T]` - 命令处理器接口
  - `ICommandBus` - 命令总线接口

#### 应用层 (Application)

- ✅ `app`
  - `IApplication[T]` - 应用服务接口
  - `Application[T]` - 应用服务实现
  - `ServiceConfig` - 服务配置

---

### 🟡 实验性 API (Experimental) - v0.x

这些 API 正在积极开发中，可能在未来版本中发生变更：

#### ⚠️ Saga 模式

- `saga`
  - `ISagaOrchestrator` - Saga 编排器接口
  - `ISagaStateStore` - 状态存储接口
  - ⚠️ **警告**: API 可能在 v1.0 前变更

**使用建议**: 可用于生产环境，但需关注版本更新

#### ⚠️ 远程桥接

- `bridge`
  - `IRemoteBridge` - 远程桥接接口
  - `ISerializer` - 序列化器接口
  - ⚠️ **警告**: 传输协议可能调整

**使用建议**: 适用于微服务通信，但建议锁定版本

#### ⚠️ 投影检查点

- `eventing/projection`
  - `ICheckpointStore` - 检查点存储接口
  - `ProjectionManager` - 投影管理器
  - ⚠️ **警告**: 检查点格式可能优化

**使用建议**: 核心功能稳定，但细节可能调整

---

### 🔴 已弃用 API (Deprecated)

这些 API 将在未来版本中移除，建议迁移到推荐替代方案：

#### ❌ 旧版投影接口

- `eventing/projection.IProjection` (旧版)
  - ⚠️ **弃用原因**: 缺少检查点和错误恢复能力
  - ✅ **替代方案**: 使用 `projection.ProjectionManager` + `ICheckpointStore`
  - 🗓️ **移除计划**: v2.0
  - 📚 **迁移指南**: 见 `docs/migration/projection-v2.md`

**迁移示例**:
```go
// 旧版（已弃用）
type MyProjection struct{}
func (p *MyProjection) Handle(ctx context.Context, event IEvent) error { ... }
projectionManager.Register(myProjection)

// 新版（推荐）
projManager := projection.NewProjectionManager(
    eventStore,
    projection.WithCheckpointStore(checkpointStore),
)
projManager.RegisterHandler("user-view", func(ctx context.Context, event IEvent) error {
    // 处理逻辑
})
```

---

## 兼容性承诺

### 我们保证

#### ✅ 稳定 API (v1.0+)

1. **接口签名不变**
   - 公共接口的方法签名不会变更
   - 新增方法会通过新接口扩展（如 `IEventStoreExtended`）

2. **结构体字段兼容**
   - 不会删除或重命名导出字段
   - 新增字段不会影响现有用法

3. **错误类型稳定**
   - 错误码（ErrorCode）保持不变
   - 错误包装链兼容 `errors.Is` / `errors.As`

4. **配置向后兼容**
   - 新增配置选项使用 Options 模式
   - 默认值保持合理且不变

#### ⚠️ 实验性 API (v0.x)

1. **可能变更**
   - 接口签名可能调整
   - 配置结构可能重组
   - 但会在 Release Notes 中明确说明

2. **平滑迁移**
   - 提供迁移指南
   - 保留旧版本至少一个 minor 版本（弃用期）

### 我们不保证

#### ❌ 不承诺兼容的场景

1. **未导出的标识符**
   - 小写开头的类型/函数/变量可随时变更

2. **测试辅助函数**
   - `*_test.go` 中的公共函数
   - `examples/` 目录中的代码

3. **实现细节**
   - 数据库表结构
   - 缓存键格式
   - 内部算法优化

4. **依赖版本**
   - 第三方库版本可能更新（遵循安全补丁）

---

## 版本升级指南

### 小版本升级 (v1.0 → v1.1)

**风险等级**: 🟢 低

**操作步骤**:
1. 更新 `go.mod`: `go get gochen@v1.1.0`
2. 运行测试: `go test ./...`
3. 查看 Release Notes 中的新功能

**回滚方案**:
```bash
go get gochen@v1.0.0
go mod tidy
```

### 大版本升级 (v1.x → v2.0)

**风险等级**: 🟡 中

**操作步骤**:
1. 阅读迁移指南: `docs/migration/v1-to-v2.md`
2. 替换弃用 API: 使用提供的代码工具
3. 更新依赖: `go get gochen/v2@latest`
4. 运行完整测试套件
5. 灰度发布验证

**建议**:
- 在测试环境充分验证
- 保留 v1.x 分支用于紧急修复
- 关注社区升级经验分享

---

## 弃用流程

### 1. 弃用公告 (Deprecation Notice)

- 在文档中标记 `@deprecated`
- 在代码注释中添加警告
- 在 Release Notes 中说明

```go
// Deprecated: Use ProjectionManager instead.
// This interface will be removed in v2.0.
// See migration guide: docs/migration/projection-v2.md
type IProjection interface {
    // ...
}
```

### 2. 弃用期 (Deprecation Period)

- 至少保留 **2 个 minor 版本**
- 提供完整的迁移文档和示例
- 在运行时打印警告日志（可配置）

### 3. 移除 (Removal)

- 仅在 **major 版本**中移除
- 提前 6 个月公告
- 提供自动迁移工具（如可能）

---

## 实验性功能标记

实验性功能使用以下方式标记：

```go
// Package saga 提供 Saga 编排能力（实验性）
//
// ⚠️ 实验性功能：API 可能在未来版本中变更
//
// 稳定性: Experimental (v0.9)
// 预计稳定版本: v1.2
package saga
```

---

## 安全补丁策略

### 支持的版本

| 版本 | 支持状态 | 安全补丁 |
|------|---------|---------|
| v1.x | ✅ 活跃支持 | ✅ 立即修复 |
| v0.9 | ⚠️ 维护模式 | ✅ 关键补丁 |
| < v0.9 | ❌ 不再支持 | ❌ 无补丁 |

### 漏洞报告

如发现安全漏洞，请通过以下方式报告：
- 邮件: security@gochen.dev（私密报告）
- 响应时间: 48 小时内确认
- 修复时间: 关键漏洞 7 天内发布补丁

---

## 社区反馈

### 提出 API 变更建议

1. 在 GitHub 提交 Issue，标签: `api-proposal`
2. 说明变更理由和影响范围
3. 提供示例代码和替代方案
4. 社区讨论至少 14 天

### API 变更投票

重大 API 变更需要核心维护者投票通过：
- ✅ 3/4 同意: 进入实现阶段
- ⚠️ 1/2 同意: 标记为实验性功能
- ❌ < 1/2: 建议被拒绝

---

## 版本发布节奏

- **Patch 版本**: 每月发布（bug 修复）
- **Minor 版本**: 每季度发布（新功能）
- **Major 版本**: 每年发布（重大变更）

**下一个 Milestone**:
- v1.1: 2025 Q2 - 增强测试覆盖率、优化性能
- v1.2: 2025 Q3 - Saga 模式稳定、远程桥接完善
- v2.0: 2026 Q1 - 移除弃用 API、架构优化

---

## 相关文档

- [CHANGELOG.md](./CHANGELOG.md) - 版本变更历史
- [BREAKING_CHANGES.md](./BREAKING_CHANGES.md) - 不兼容变更列表
- [MIGRATION_GUIDE.md](./docs/migration/) - 迁移指南
- [AUDIT_REPORT.md](./AUDIT_REPORT.md) - 代码质量审核报告

---

**最后更新**: 2025年  
**文档版本**: 1.0  
**维护团队**: Gochen Core Team
