# 审计修复批次发布说明（2026-10）

本批次基于 2026-10 全量审计（round 1–9，见 `audit/final-findings.md`）修复 4 条 P1 静默缺陷与一批 P2 问题。其中多项修复改变了既有行为（多数方向为 fail-closed 收紧或语义恢复），升级前请逐条核对影响面。行为契约登记与守护测试随代码提交，条目编号对应 `audit/final-findings.md`。

## 1. 监控：事件存储错误率口径统一

- **对应审计条目**：P2#27（fingerprint `01b2af82`）、P2#9。
- **变更点**：事件存储错误率的分子分母统一为"调用次数"——从"失败调用次数 / 成功事件条数"改为"失败调用次数 / 总调用次数"；投影错误率分母不再重复计入失败数；summary 与 health 两处公式对齐；新增 `EventStoreSaves` / `EventStoreLoads` 调用计数指标。
- **旧行为**：分母为成功事件条数，批量形态下一次调用写入/读取多条事件会把分母放大，错误率被稀释；50% unhealthy 阈值实际不可达；summary 与 health 口径互不相同。
- **新行为**：同量故障下错误率数值普遍变大；批量策略变化不再导致错误率数量级漂移。
- **影响面**：监控面板与告警消费方——unhealthy 可能首次出现，告警阈值需按新口径复核；依赖旧指标名称的看板需切换到新计数指标。

## 2. 熔断器：取消不再豁免失败计数

- **对应审计条目**：P1-4（fingerprint `9eed6358`）。
- **变更点**：`Call` 恢复"任何错误一律计失败"，取消（context canceled / 调用方超时）不再被豁免；新增 `CallContext(ctx, fn)` 与 `Config.CallTimeout`；取消豁免仅凭 `WithCancelCause` 的 cause 对比判定（熔断器自身 `CallTimeout` 到期以专属 cause `errBreakerCallTimeout` 取消派生 ctx，与调用方取消可区分）。
- **旧行为**：取消类错误被豁免不计失败，下游真实故障可借取消形态逃过熔断统计，熔断器长期不跳闸。
- **新行为**：所有从 `fn` 返回的错误都计入熔断统计；需要超时预算的调用方使用 `CallContext` + `Config.CallTimeout`。
- **影响面**：以 `Call` 包装可能返回取消错误的调用方——取消风暴现在会正常推高失败率并触发熔断；此前依赖取消豁免的误配置需迁移到 `CallContext`。
- **HTTP 中间件接线（后续收口）**：`gochen-runtime/http/middleware` 的熔断中间件已迁移到 `CallContext`，并把派生 ctx（含 `Config.CallTimeout` 预算与调用方取消信号）经 `SetContext` 注入 `IRequestContext`——handler 经 `ctx.RequestContext()` 看到的即带预算的 ctx，只要 handler 监听请求 ctx 的 `Done()`，`CallTimeout` 预算即对其生效。已知局限（ctx 语义固有）：完全无视请求 ctx 且返回 nil 的 handler，预算到期无法强制中断，只能按成功记账。

## 3. 投影：追赶失败可恢复、真实空洞不再被节流掩盖

- **对应审计条目**：P1-3（fingerprint `865e3285`）、P2#13（fingerprint `379f7ab9`）。
- **变更点**：追赶（catch-up）失败不再把投影置为 error 终态，保留可重试状态并自动恢复；已确认的真实事件空洞不再被 30s 节流抑制，缺口语义显式化。
- **旧行为**：一次追赶失败即永久卡死投影，读模型静默落后且无自愈路径；真实空洞受节流抑制，可能被永久跳过（丢事件）。
- **新行为**：瞬时故障（如存储抖动）后投影自动重试追上；确认为真实空洞时不再等待节流窗口。
- **影响面**：运维观察——投影不再出现"假死"终态，告警应关注追赶延迟而非仅终态错误；空洞处理路径的日志/事件形态有变化。

## 4. Outbox：MarkAsPublished 失败有界重试、生命周期与 claim 门控修复

- **对应审计条目**：P2#7、P2#8、P2#10、P2#22（outbox 终态完备性家族）。
- **变更点**：
  - 发布成功但 `MarkAsPublished` 持续失败的条目恢复有界重试，达到上限进入 DLQ，不再无界重投；
  - 未启动即 `Stop` 不再置不可逆终态，outbox 可重新启动；
  - claim 门控收窄为仅拦截 Pending 超限条目；
  - 导出 `IOutboxMaxRetriesAware` 接口，publisher 与仓储按协商语义对齐 MaxRetries；SQL 仓储三处阈值（重试上限、DLQ 判定、协商）一致；
  - `DeletePublished` 分批删除补主键 keyset 游标：每批从上一批最后主键继续扫描，消除 O(批数×表头行数) 的表头重扫（审计 P2#26，fingerprint `28876db2`，含 PG 子查询同病 `449d2d72` 与覆盖索引契约 `4781128c`）；
  - `DeletePublished` 对 `RowsAffected` 恒 0 的路径加防护，杜绝潜在死循环面（`5d79180d`）；
  - 清理 SQL 增加三方言（MySQL/PostgreSQL/SQLite）生成矩阵断言（`6cf3c0cb`）；
  - 同文件顺带修复：`DeletePublished` 取消（Canceled）不再伪装为 Timeout（`ee3afcb`）；批间部分失败时已删除计数不再被静默丢弃（`4dd96725`）；
  - 超时归一（后续收口）：`DeletePublished` 中 `DeadlineExceeded` 无论发生在哪个阶段（SELECT / DELETE / 批间休眠）统一归 `errors.Timeout`（保留原始错误链与诊断字段）——此前同一超时因发生阶段不同会在 `errors.Database` 与 `errors.Timeout` 之间漂移，误导调用方的重试/告警判定；主动 `Canceled` 仍如实返回原始 `ctx.Err()`。
- **旧行为**：mark 持续失败导致条目被无限重投（重复发布风险）或静默滞留；Stop 终态不可逆；claim 门控误伤范围过宽；三层 MaxRetries 配置可能互不一致；分批删除每批从头重扫不可删"表头"，积压越大删除越慢；Canceled 错误被包装成 Timeout 误导排查；部分批次失败后已删计数丢失。
- **新行为**：mark 失败按统一阈值重试后进 DLQ；生命周期可重入；`IOutboxMaxRetriesAware` 成为仓储与 publisher 的协商契约；分批删除按 keyset 游标推进，删除耗时与表头积压解耦；取消语义如实上报；批间计数准确。
- **影响面**：自定义 Outbox 仓储实现需评估是否实现 `IOutboxMaxRetriesAware`；依赖"Stop 后不可重启"假设的部署脚本需调整；DLQ 监控需覆盖 mark 失败进 DLQ 的新路径。

## 5. Messaging：PublishAll 幂等检查点恢复逐层注册

- **对应审计条目**：P1-2（fingerprint `8bff8bd7`）。
- **变更点**：`PublishAll` 的幂等检查点恢复逐层（逐消息）注册，失败重试不再被整批静默去重。
- **旧行为**：批量发布中途失败后重试时，已注册的批次级检查点使剩余消息被误判为"已发布"而静默丢弃，且调用方收到假成功。
- **新行为**：检查点精确到已成功发布的消息，重试只补发未完成部分。
- **影响面**：使用批量发布的下游——消息丢失与假成功风险消除；重试路径的发布次数可能比旧行为多（补发未确认消息），消费方应保持幂等。

## 6. Operation memory store：容量淘汰与幂等绑定修复

- **对应审计条目**：P1-1（fingerprint `f430e68e`）、P2#23（fingerprint `1fc930c2881777f6`）。
- **变更点**：
  - 容量淘汰（`WithMaxRecords`）不再淘汰本次调用刚写入的记录——宁可短暂软超限也不丢写入；
  - 幂等键绑定前校验记录存在，杜绝"幽灵绑定"（幂等键指向已不存在的记录导致操作重复执行副作用）；
  - `recordOrder` 增加按 id 的 map 索引（`recordOrderIndex`），Put/Delete 成员查询由 O(maxRecords) 线性扫描降为 O(1)，被 Delete 移除的 id 以占位回收（惰性压缩）。
- **旧行为**：高并发下"写入→淘汰→幂等键绑定到空记录"可造成同一操作重复执行；启用容量上限后每次 Put 写锁内全表线性扫描，Delete 残留项无界积累。
- **新行为**：写入与幂等绑定的时序安全；容量淘汰 FIFO 语义保持，成员管理 O(1)。
- **影响面**：启用 `WithMaxRecords` 的部署——峰值时记录数可能短暂超过配置上限；此前因幽灵绑定被掩盖的重复执行问题可能以幂等命中正确拦截面目出现（属预期行为）。

## 7. Scoped 安全：Purge 实体审计调阅 fail-closed、Get 移除绕过 DataScope 的快速路径

- **对应审计条目**：P2#1、P2#2。
- **变更点**：
  - `AuditTrail` 对已被 Purge（物理删除）的实体 fail-closed——实体边界无法解析时返回 `NotFound`，不再降级直读审计史（降级会绕过 DataScope，构成越范围调阅）；
  - `Get` 删除绕过 DataScope 的快速路径，读取与列表走同一约束链路。
- **旧行为**：Purge 后的实体审计史可直接调阅，不受主体数据范围约束；Get 存在绕过数据范围过滤的读路径。
- **新行为**：所有读取（含审计调阅）统一经过 DataScope / 隔离约束；Purge 实体按 `NotFound` 拒绝。
- **影响面**：Purge 后实体的审计史当前**无对外读取入口**——合规/运维调阅需由 DBA 或内部服务直读审计存储（并自行承担范围审计）；Get 性能快速路径消失，读取统一走约束 SQL。若业务需要产品化的受控调阅入口（带 Reason/Operator 凭据），需另行设计，已列为开放项（`IAuditOperations` 当前不提供跨隔离读取 API）。

## 8. Saga 与 testkit 事务语义

- **对应审计条目**：P2#15、P2#16、P2#17、P2#18。
- **变更点**：
  - Saga 补偿保留租约失效保护与专用超时，补偿执行不再复用业务步骤的租约/超时配置；
  - testkit 嵌套事务不再触碰宿主回调；保存点（savepoint）语义与 gochen-runtime 对齐；补齐环检测（嵌套深度成环时快速失败）。
- **旧行为**：补偿可能在租约已失效的上下文中执行且无独立超时；testkit 嵌套事务与 runtime 真实语义分叉（宿主回调被误触发、保存点行为不一致、无环保护）。
- **新行为**：补偿拥有独立的租约保护与超时预算；testkit 事务行为与生产仓储一致，替身可信度提升。
- **影响面**：依赖 testkit 旧行为编写的测试可能暴露此前被掩盖的问题（属修复目的）；Saga 部署需确认补偿超时预算的新默认值满足最长补偿路径。

## 9. Projection：重放路径指标去双计

- **对应审计条目**：P2#12（fingerprint `16e96665`）。
- **变更点**：删除重放路径的外层 `recordReplayMetrics` 补记——指标统一由 `applyEventCommon` 公共路径记录，每个重放/重建事件恰好计一次。
- **旧行为**：`applyEventCommon` 打点后外层再次补记，重放期间吞吐/错误率虚高一倍（实测 2 事件 4 计），且补记的 0 耗时稀释平均耗时。
- **新行为**：重放 N 个事件，`RecordEventProcessed`/`RecordProjectionUpdate` 各恰好打点 N 次；失败事件计一次失败。
- **影响面**：监控消费方——重放/追赶期间的事件吞吐、投影错误率、平均耗时数值回落到真实值（虚高一倍/被稀释的形态消失）；投影错误率（`ProjectionErrors/ProjectionUpdates`）口径不受影响。

## 10. Cache：StartCleanup/Close 生命周期同步化

- **对应审计条目**：P2#20（fingerprint `056f2b0a`，含 `8adb0e29`/`b35a6cf3`）。
- **变更点**：`StartCleanup`/`Close` 的双 `sync.Once` 改为单一互斥锁保护的生命周期状态机：`stopCh` 由 `StartCleanup` 创建、`Close` 关闭，协程退出经 per-start done channel 等待（弃用可被 Add/Wait 竞态复用的 `sync.WaitGroup`）。
- **旧行为**：双 Once 无同步，并发 Start/Close 实测 DATA RACE；`Close→StartCleanup→Close` 序列永久阻塞；清理协程可泄漏。三仓库当前无生产调用方（潜伏缺陷）。
- **新行为**：采用**可重启语义**——`Close` 停止当前清理协程后可再次 `StartCleanup` 重启；`Close` 幂等且不阻塞在已停止实例上；`Close→StartCleanup→Close` 任意交错不死锁、不泄漏 goroutine（`-race` 并发 Start/Close 测试守护）。
- **影响面**：无现网调用方；API 签名不变。此前若依赖"Close 后 Start 永不生效"的隐含行为，需改为依赖显式重启语义。

## 11. Testkit：资源边界列名与生产 casecodec 同源

- **对应审计条目**：P2#25（fingerprint `0260d854`）。
- **变更点**：`MemoryRepository` 资源边界解析的 `memoryToSnakeCase` 由自制实现改为委托 `gochen/codec/casecodec`（与 runtime `db/orm/repo/datascope_schema.go` 同源同参）；新增完全不打 tag 的缩写字段（OwnerID/ManagedScopeID）边界解析用例。
- **旧行为**：自制 snake_case 把缩写词渲染为 `owner_i_d`/`managed_scope_i_d`，与生产 `owner_id`/`managed_scope_id` 分叉——未打 json/db tag 的缩写字段边界资源 OwnerID/ManagedScopeID 恒空，违背替身"与生产对齐"承诺。
- **新行为**：列名口径与生产完全一致，无 tag 缩写字段能正确解析出边界列；无 tag 用例使替身测试真正走到 snake_case 转换路径，可抓到与生产的再度分叉。
- **影响面**：仅测试替身；依赖旧错误口径（`owner_i_d`）编写的替身侧测试（若有）需改回生产口径。

## 12. Cache：Set 机会式清扫按扫描数截断（后续收口）

- **对应审计条目**：P2#19（fingerprint `7a1eb1b8`）。
- **变更点**：`Set` 的机会式过期清扫截断条件由"驱逐数达上限"改为"**扫描条目数**达上限（`maxOpportunisticSweep`）"——无论本次实际驱逐多少，单次 `Set` 持写锁的扫描量有界、与缓存总规模无关；截断为抽样式（map 遍历顺序随机），未扫到的过期条目留给后续 `Set` 或后台清理兜底。同步修正了旧注释"限制单次扫描量"的失实表述（旧实现限制的是驱逐数，扫描并不设防）。
- **旧行为**：`MaxSize=0` + TTL 且无过期项时驱逐数恒为 0、截断永不触发，每次 `Set` 持写锁全表扫描——累计插入实测 2k≈75ms、4k≈300ms、8k≈1.2s（严格 O(n²) 增长），相对 HEAD 为真实生产性能回归。
- **新行为**：单次 `Set` 持锁扫描量恒 ≤ `maxOpportunisticSweep`；包内新增 `lastSweepScanned` 可观测字段供测试断言截断语义。
- **影响面**：`MaxSize=0` + TTL 的大缓存写入吞吐恢复正常量级；过期回收从"尽力全量"变为"每 Set 有界抽样 + 后台清理兜底"，纯依赖机会式清扫（未启动后台清理）的场景过期回收节奏略有变化。

## 未修清单（本批已知未修 P2）

以下条目经本轮修复后仍未处理，每条含未修原因与建议排期：

| # | 问题 | 未修原因 | 建议排期 |
|---|------|----------|----------|
| P2#3（`4e3d67ea`） | `Application.List` 的 limit 默认值口径与 `PaginationOptions.Validate` 分叉（1000 vs 10） | 统一口径需选定默认值并评估 API 兼容性，属行为变更决策，不在审计合并前 Top12 清单 | 合并前决策 |
| P2#4（`39392d1b`） | 快照 NotFound（无快照常态）也触发 fail-closed 重置，未实现 `IReplayResettable` 的自定义聚合恢复直接 Unsupported | 需作者定义"无快照常态"与"快照损坏"的区分策略（行为变更决策），不在 Top12 清单 | 合并前 |
| P2#5（`68884dc8`） | 审计默认全字段明文 before/after，AuditProjector 无默认防线 | HEAD 既有而非本批回归，当前四仓库无敏感实体暴露（R5 限定），需作者做默认投影/脱敏策略决策 | 合并后（首个维护版本） |
| P2#11（`fa43670f`） | outbox 发布后清理 goroutine 不受 supervisor 约束、无 panic 恢复 | 生命周期改造需与 Stop 语义协同设计，不在 Top12 清单；行为已在代码注释与发行说明中如实标注 | 合并前 |
| P2#14（`699e7ae6`） | 重放把 `ProjectionLag` 覆盖为历史事件年龄，MaxProjectionLag 健康检查误报 | 修复需给 `applyEventCommon` 增加"重放不打 lag"开关，涉及指标语义决策，不在 Top12 清单 | 合并前 |
| P2#21（`b839077f`） | ratelimit MaxKeys 满后每新 key 全局锁内全量排序 O(n log n)，高基数攻击吞吐塌陷 | 需重写为堆/分片淘汰结构，改动面较大且不在 Top12 清单 | 合并后（性能专项） |
| P2#24（`7223da4b`） | 默认快照策略退化为"仅版本恰为 N 整数倍"，批量步进不整除 freq 时快照永不创建 | 属"策略唯一裁决"意图内的角落行为，需作者决定是否恢复"至少每 N 版本一次"承诺 | 合并前 |

## 同批守护（无行为变更）

- **P2#6**（fingerprint `9a4cca4e`）：零目标 Require 判据 `IsAllowed`→`IsActionAllowed` 的有意对齐保留原行为，补登记行为契约（`docs/SPEC-scoped-zero-target-require.md`）与端到端守护测试（`auth/scoped/decision_test.go`）。零目标调用为动作级判定（allow 即放行），有目标调用仍要求非空授权资源，现网调用点无行为变化。

## 待补清单

以下为 `audit/final-findings.md` §六"合并前另需动作"中**尚未完成**、需作者在合并前排期的项：

1. **其余历史有意行为变更的发布说明（约 20 项）仍需补齐**：本文档仅覆盖本修复批次。原文提及的存量变更——批量语义、分页规范化、判等收紧、EnableBatch 默认值变更、处理器命名/发现收紧（旧命名处理器静默丢失）、错误码变化等——仍需作者按模块补写发布说明。
2. **API 破坏矩阵（§7.2，约 15 项）需并入发布说明**：`Metadata` 三字段私有化与 `EventHandler` 删除、`IEventSourcedAggregate` 新增 `ClearUncommittedEvents`、crud Hooks Stage 化 / `BatchWriter` 删除 / `RunBefore*` ×9 删除、`IAuditOperations` 方法集变化（M2/M3/M12 三处传导）、audited `BatchWriter` 与 outbox `BatchPublisher` 删除等编译期响亮破坏项。
3. **按各报告"测试缺口"清单补钉关键分支**：仍开放的是 commandflow panic 隔离路径、contextx 回调封存语义两处（outbox 生命周期未启动即 Stop/重启、breaker 取消路径与矩阵、cache 生命周期与清扫截断已于本批补齐，不再列入）。
4. ~~**未跟踪守护测试文件需 git add**~~：**已完成**——本批全部守护测试（含 `auth/scoped/decision_test.go`、`eventing/store/decorator/close_test.go`、cache/projection/breaker/sqlstore 等新增测试与 SPEC/发行说明）均已纳入暂存区，untracked 仅剩审计排除项 `assembly` 产物目录。
