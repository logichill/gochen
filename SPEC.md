# Gochen 项目开发规范 (SPEC)

## 1. 架构设计原则 (Architecture)

1. **接口优先**: 基础能力必须定义接口，接口命名统一为 `I` 前缀（如 `ILogger`, `IDatabase`）。
2. **依赖接口**: 跨能力域消费行为优先依赖消费侧最小接口。DTO、值对象、配置以及有明确共享身份语义的注册表可以使用具体类型；对可替换的服务实现应使用接口，避免将数据值强行接口化。
3. **依赖注入**: 功能模块只提供实现（如 `SQLEventStore`），不决定使用哪个实现。具体装配由组合根（main/server）负责。
4. **构造函数**: 默认返回具体 struct，由调用方在装配时转为接口变量。仅在需隐藏实现时直接返回接口。
5. **避免兼容**: 拒绝过度兼容，彻底删除不好的冗余垫片

## 2. 命名规范 (Naming)

1. **接口 (Interfaces)**

   - 必须以 `I` 开头（如 `IRepository`）。
   - 方法名必须以 **动词** 开头（如 `Get`, `Save`）。
   - 避免泛化命名（如 `IManager`, `IHandler`），应准确描述职责。

2. **结构体 (Structs)**

   - 使用名词，体现本质（如 `User`, `MemoryCache`）。
   - **禁止** `I` 前缀或 `Impl` 后缀。

3. **方法 (Methods)**

   - **CRUD**: `Create`/`Add`, `Get`/`Find`/`List`, `Update`, `Delete`。
   - **布尔值**: 必须使用 `Is`, `Has`, `Can`, `Should` 前缀。
   - **Getter/Setter**: Getter 直接使用字段名（如 `Name()`），Setter 使用 `Set` 前缀（如 `SetName()`）。
   - **例外（框架层基础接口）**：为避免歧义与业务方法冲突，`gochen/domain` 等框架层基础接口统一采用 `GetXxx/SetXxx` 风格（如 `GetID()`、`GetCreatedAt()`）。

4. **变量与常量**

   - 变量名简短有意义（如 `ctx`, `err`, `userID`）。
   - 常量使用 **CamelCase**（如 `MaxRetries`），**禁止**蛇形全大写（`MAX_RETRIES`）。
   - 缩写词保持全大写（如 `ServeHTTP`, `UserID`, `JSONData`）。

5. **包 (Packages)**
   - 使用 **小写单数** 名词（如 `user`, `order`），禁止复数或下划线。

6. **源文件 (Files)**

   - 同一类型的实现跨文件时，使用稳定的主体前缀和职责后缀，例如 `application_bulk.go`、`engine_lifecycle.go`、`manager_replay.go`。
   - 主文件承载类型定义和构造；读写、批量、生命周期等方法及其专用辅助逻辑按职责内聚。独立接口、配置、状态类型和算法按自身职责命名。
   - 文件拆分保持包、方法集、共享状态与执行语义一致；只为整理文件时，不新增包装类型或装饰器。已有包装器是否保留，以其能力和行为契约判断。
   - 按职责拆分，不设文件行数或文件数量指标。具体组织见[源码文件组织](docs/architecture/source-layout.md)。

## 3. 文档与注释

1. **GoDoc**: 所有导出类型和方法必须包含注释。
2. **语言**: 统一使用 **简体中文**。
3. **格式**: 第一句为完整摘要；仅当参数、返回值或错误语义存在**非显然契约**（如取消传播、并发约束、失败行为、特殊默认值）时才单独说明，避免逐参数/返回值套模板。

### 3.1 示例代码中的错误处理约定

1. **示例代码中的快速失败**

   - `examples/*` 目录下的示例代码使用 `must(err)` 或 `log.Fatal(err)` 来简化错误处理，让读者专注核心流程：

   ```go
   // 示例代码推荐写法：使用 must 快速失败
   agg, err := repo.Get(ctx, id)
   must(err)  // 失败时立即终止程序
   ```

   - `must(err)` 在示例中的作用：
     - 明确表达"这里失败就不应该继续"
     - 避免用 `_` 掩盖错误，让初学者理解每个操作都可能失败
     - 简化代码，让读者专注业务逻辑而非错误处理细节

2. **生产代码应返回错误**

   - 生产代码不应使用 `must(err)` 或 `panic`，而是应该返回错误，让调用方决定如何处理：

   ```go
   // 生产代码推荐写法：返回错误由上层处理
   if err := repo.Save(ctx, aggregate); err != nil {
       return fmt.Errorf("save aggregate %d: %w", aggregate.ID, err)
   }
   ```

   - 生产代码的错误处理策略：
     - 在业务边界统一通过 `errors.Normalize` 转换为 `AppError`
     - 根据错误码决定重试、降级或告警
     - 记录结构化日志（包含 error_code、aggregate_id 等关键字段）

3. **特殊场景的显式标注**

   - 若业务逻辑中存在"预期失败"的场景（如 Saga 补偿、幂等检测），必须用注释明确说明：

   ```go
   // 预期失败场景：Credit 在 false 参数下会故意失败，触发 Saga 补偿逻辑
   if err := orch.Credit(ctx, id, false); err != nil {
       log.Printf("Credit failed as expected (triggering compensation): %v", err)
   }
   ```

   - 避免用裸的 `_` 掩盖错误，即使在示例中也应该让意图清晰可见。

## 4. 顶级包组织 (Package Organization)

本节约束由 [AGENTS.md](AGENTS.md#架构约定core-模块) 承载、通过代码评审执行（本项目不保留门禁测试代码），架构说明见[框架架构](docs/architecture/framework-design.md)。

1. **按能力内聚，不设包数量 KPI**：顶级包必须表达稳定能力，如 `clock`、`codec`、`eventing`；禁止仅为减少目录数量而建立 `common`、`foundation`、`data`、`util` 等收纳包。
2. **依赖图不是伪造的线性阶梯**：`app/eventsourced` 是 eventing/messaging 之上的应用编排；eventing/process 也存在经过审计的协作边。生产代码必须满足 `AGENTS.md`《架构约定》中的显式允许矩阵，新增依赖边必须同步说明架构理由。
3. **基础叶子隔离**：`clock`、`codec`、`gen`、`validate` 只允许依赖标准库、`gochen/errors` 与本包子树；基础叶子之间禁止互相 import，协作使用最小结构接口。
4. **领域地板**：`domain` 非测试代码只允许依赖标准库、`gochen/errors` 与自身子树。
5. **模块边界**：Core 禁止依赖 `gochen-runtime` 或 `gochen-contrib`；SQL、配置加载、具体 net/http server 与 Host/DI 运行时装配属于 Runtime。
6. **内存能力属于 Core**：不引入物理驱动且用于无外部环境测试的内存实现（如 cache、MemoryEventStore、testkit）可以留在 Core。
7. **发布边界**：开发态可用 `go.work` 与本地 replace；发布态必须使用真实模块版本，并在无 workspace、无本地 replace 的外部消费者中验证。
8. **CRUD 子树**：`app/crud.Application` 统一承载资源操作与删除生命周期。构造期必须显式选择 DeleteMode：DeleteSoft 时 Delete 只软删，DeletePurge 时 Delete 进入完整 Purge 流程；零值或未知值构造失败；Purge 始终物理清除，恢复和已删除查询独立选配。`app/crud/audit` 独立承载历史读取，CRUD 根包不依赖该子包。共享审计契约保留在顶级 `audit`，执行协议保留在 `app/internal/execution`。

## 5. 领域维度与条件写入

- 实体以 `entity:"id"` 声明唯一标识，以 `dim:"..."` 声明开放维度，以 `managed:"true"` 声明托管字段。维度支持标量或一层标量指针，名称与绑定在构造期验证。
- Eq / In / IsNull 接收 `meta.Ref`。ID / 普通字段 / 维度引用统一由元数据解析，SQL 列映射归 Runtime，类型转换与 NULL 语义由内存及 SQL 共用。
- 资源显式声明隔离、mutable、assignable、transferable；全部维度默认受普通更新保护。仓储生成只读 ExecutionDescriptor，Application 据此验证授权词汇。
- 授权 Vocabulary 使用 Refs / Assignable；授权条件采用类型化 Ref。Named 用于仓储业务读查询。mutable 和隔离字段保持独立于授权条件。
- 普通更新在输入或命令产出后、Before 前后检查保护前值；最终条件写入同时约束隔离、有效态、授权、版本与保护值，SET 排除保护字段。
- Transfer 在两阶段授权前通过仓储 CheckTransferEntry 检查上下文事务及仓储绑定会话。两阶段授权发生在事务外，目标意图冻结，权威变更来自接收 Decision.Assign。仓储在活动同源事务中执行版本写入、实际后像回读及全部维度和 Check 校验。
- AuditRecord 保存完整 DimensionSnapshot 和 OperationID；迁移两侧记录共享编号，按各自快照过滤。Projector、After 与 PostCommit 各自消费独立展示副本，普通写入及迁移共用此隔离规则。新增历史维度通过显式迁移补齐键集。
- 外部迁移入口必须提供幂等键与原调用方版本，REST 和命令入口共用 Runner.ExecuteTransfer。项目保证目标引用及状态约束，所有合法归属变更同步推进版本。

具体行为、错误语义与接入方式见[分层授权](docs/architecture/layered-authz.md)。
