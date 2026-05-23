# 授权边界与业务数据可见性解耦改造方案

本文档给出当前 `gochen + gochen-iam + 下游 ERP order` 权限模型的改造方案。目标不是继续围绕一个抽象词做命名优化，而是把当前被混用的几类概念拆开，并落到可迁移、可实施的设计上。

## 1. 背景

当前模型里，`Scope` 同时承担了多种不同语义：

- `Scope` 树：授权域节点与覆盖关系
- `ActiveScope`：本次请求当前以哪个授权域工作
- `DataScope`：当前请求默认可见的数据范围
- `ManagedScopeID`：资源归属的管理边界
- `GrantScopeID`：角色绑定在哪个授权域生效

这套模型在 IAM 自身还能勉强自洽，但到了下游业务，问题会迅速暴露。`ERP order` 就是一个典型例子：

- 订单实体没有 `managed_scope_id`
- 订单读权限却复用了 `DataScope.VisibleScopeIDs`
- `VisibleScopeIDs` 里塞的不是授权域节点，而是“销售负责人用户 ID 集合”
- repo 又把 `sales_owner` 当成了框架级“边界列”

这说明当前问题已经不是“名字不够好”，而是“一个模型同时承担了授权域、资源归属、业务可见性三件事”。

## 2. 现状诊断

### 2.1 当前模型其实混了三层

#### A. 授权层

回答的是：

- 这个人持有什么权限？
- 这个权限在哪个授权域内生效？
- 当前请求以哪个授权域工作？

对应对象：

- `Scope`
- `UserRoleBinding(grant_scope_id)`
- `ActiveScope`
- `Permission`

#### B. 资源归属层

回答的是：

- 这条资源归哪个边界负责？
- 写入和审计应该落在哪个边界下？

对应对象：

- `managed_scope_id`
- `WriteConstraint`

#### C. 业务可见性层

回答的是：

- 当前用户这次应该看到哪些业务数据？
- 是“本人可见”“本组可见”“本区域可见”还是“全部可见”？

这部分在很多业务里并不天然等于授权域树。

`ERP order` 当前实际走的是：

- `tenant_id`：租户隔离
- `sales_owner`：负责人边界
- `read_all / read_group / read_self`：业务可见性策略

它不是标准的 `managed_scope_id IN visible_scopes` 模型。

### 2.2 当前实现的主要问题

#### 问题 1：`DataScope` 被当成“万能读边界”

在框架设计里，`DataScope` 原本更适合表达：

- “当前请求能看到哪些 `managed_scope_id`”

但在 `ERP order` 中，它被拿来装“用户 ID 集合”。这会导致：

- 同一个类型在不同模块里承载不同数据语义
- repo 层看起来在消费统一边界，实际消费的是不同维度的数据
- 读边界和授权域树失去可解释性

#### 问题 2：业务字段兼职权限字段

`sales_owner` 本来是业务负责人字段，但当前又被兼职成读边界列。结果是：

- “负责人”与“可见性边界”绑死
- 后续若要支持“订单归销售组、负责人可转派”，会出现语义冲突

#### 问题 3：创建时字段判定缺乏实体分类

现在创建时常见做法是“从当前上下文继承 scope”，这对 `User` / `Group` / `Role` 有一定合理性，但对 `Order` 这类交易聚合并不成立。

一个交易订单：

- 不应该有 `home_scope_id`
- 是否需要 `managed_scope_id`，取决于它是否真的属于某个业务边界池
- `sales_owner` 应该表达负责人，而不是默认承担所有数据权限语义

## 3. 改造目标

改造后必须满足四个目标：

1. 授权域、资源归属、业务可见性三层职责显式分离
2. 框架默认能力只覆盖稳定共性，不强迫业务把所有读权限都映射成 `Scope`
3. 对 `ERP order` 这类交易聚合给出清晰、可迁移的落地模型
4. 创建时字段判定规则能按实体类别稳定解释，而不是一律“继承当前 scope”

## 4. 目标模型

### 4.1 第一层：授权层保留 `Scope`，但只负责授权

`Scope` 在短期内不做数据库/接口级重命名，继续表示：

- 授权域节点
- 授权覆盖关系
- 角色绑定生效的域
- 当前请求的工作域

这层只回答：

- “你在哪个授权域内拥有哪些权限”

它不再直接承担所有业务数据可见性解释。

#### 本层保留的对象

- `Scope`
- `ActiveScopeID`
- `GrantScopeID`
- `NamespaceScopeID`
- `Permission`

### 4.2 第二层：资源归属层回到 `managed_scope_id`

`managed_scope_id` 只用于表达：

- “这条资源归哪个管理边界负责”

它适用于：

- 用户
- 组织
- 角色
- 订单池、客户池、项目池、区域库存这类确实有“归哪个业务边界管”的资源

它不适用于：

- 纯主体默认归属之外的概念
- 单纯的负责人字段
- 临时查询视图

### 4.3 第三层：业务可见性层单独建模

业务可见性不再强迫全部复用 `DataScope`。

改造后的原则是：

- 框架通用读边界只表达 `managed_scope_id` 过滤
- 业务专用可见性单独建模

对于通用场景，仍然可以是：

- `managed_scope_id IN visible_scope_ids`

对于订单这类业务场景，允许额外存在：

- 本人可见：`sales_owner = current_user`
- 本组可见：`managed_scope_id IN current_visible_scopes`
- 全部可见：只受 tenant 限制

这里有一个关键变化：

> “本组可见”不再通过“展开组织成员 -> 塞用户 ID -> 复用 DataScope”实现，而是通过订单自身的 `managed_scope_id` 来实现。

## 5. `ERP order` 的推荐落地模型

### 5.1 订单模型调整

订单实体建议新增：

```go
ManagedScopeID int64 `json:"managed_scope_id" gorm:"not null;index"`
```

字段职责调整为：

- `tenant_id`：租户隔离
- `managed_scope_id`：订单归属的销售组/业务单元边界
- `sales_owner`：当前负责人/跟单人

这三个字段分别回答三件不同的事：

- 订单属于哪个租户
- 订单归哪个业务边界负责
- 当前谁在跟进这张单

### 5.2 订单读权限策略

订单模块不再把用户 ID 塞进 `DataScope.VisibleScopeIDs`。

改造后读权限语义改成：

- `read_all`
  - 条件：`tenant_id = current_tenant`
- `read_group`
  - 条件：`tenant_id = current_tenant AND managed_scope_id IN current_visible_scope_ids`
- `read_self`
  - 条件：`tenant_id = current_tenant AND sales_owner = current_user`

也就是说：

- 组级权限回到授权域树
- 个人权限继续走业务负责人字段

### 5.3 订单创建规则

订单创建时不引入 `home_scope_id`。

订单创建应显式判定两件事：

#### 1. `managed_scope_id`

默认规则：

- 手工录单：取当前工作域 `ActiveScopeID`
- 如果当前工作域不是有效业务边界，则回退到当前租户的默认销售根域
- 渠道同步：优先取“店铺/渠道账号绑定的负责业务域”，缺失时回退租户默认销售根域

#### 2. `sales_owner`

默认规则：

- 手工录单：默认当前操作人
- 允许具备订单管理权限的人改派给他人
- 渠道同步：优先取店铺负责人映射；缺失时可为空，进入待分配状态

这样创建出来的订单含义清晰：

- 归哪个组管，由 `managed_scope_id` 决定
- 当前谁负责，由 `sales_owner` 决定

### 5.4 订单流转规则

订单流转不应修改 `home_scope_id`，因为订单没有该字段。

对于 `managed_scope_id`：

- 普通编辑不允许直接修改
- 变更归属应走显式“转派/转组”操作
- 转组应单独审计

对于 `sales_owner`：

- 可作为日常业务操作调整
- 调整负责人不应等同于改变订单归属边界

## 6. 实体字段判定规则总表

| 实体类型 | `home_scope_id` | `managed_scope_id` | 说明 |
| --- | --- | --- | --- |
| `User` | 需要 | 需要 | 主体默认归属 + 账号资源归属 |
| `Group` | 不需要 | 需要 | 组织资源归属 |
| `Role` | 不需要 | 用 `namespace_scope_id` 表达 | 角色模板定义域 |
| `Order` | 不需要 | 推荐需要 | 订单归哪个业务边界池负责 |
| `AfterSaleOrder` | 不需要 | 推荐需要 | 应与订单边界一致或从订单继承 |
| 纯查询视图/报表 DTO | 不需要 | 不需要 | 不做持久化边界字段 |

判断标准只有一句话：

> 只有“主体默认归属”才需要 `home_scope_id`；只有“资源归属边界”才需要 `managed_scope_id`。

## 7. UI 与操作方式调整

### 7.1 IAM 后台

IAM 后台继续负责：

- 授权域树
- 角色模板
- 用户角色绑定
- 当前工作域切换

它不直接承担订单模块“本人/本组/全部”的业务规则配置。

### 7.2 ERP 订单后台

订单后台应明确分成三类信息：

#### A. 归属边界

- 订单归属业务域
- 默认隐藏或只读展示
- 通过“转组”动作修改

#### B. 当前负责人

- 跟单销售/客服
- 日常可编辑

#### C. 可见性

不做独立“规则编辑器”，而是由用户持有的权限自动决定：

- 本人可见
- 本组可见
- 全部可见

也就是说：

- UI 不让业务管理员去手写一条“某某能看这些订单”的表达式
- UI 只让管理员配置角色和授权域
- 订单模块根据角色权限与订单字段自动计算可见性

## 8. 技术改造方案

### 8.1 gochen / gochen-iam 侧

#### 必做

1. 在文档层收紧 `DataScope` 语义：
   - 只用于 `managed_scope_id` 型读边界
   - 不再鼓励承载用户 ID、负责人集合等业务含义
2. 在下游接入指南中增加规则：
   - 业务专用可见性不要伪装成 `DataScope.VisibleScopeIDs`
3. 保持 `WriteConstraint` 语义不变

#### 可选增强

后续可在框架中补一个更中性的读边界抽象，例如：

- `ReadPolicy`
- `VisibilityPolicy`

但第一阶段不要求框架立刻抽象出统一接口。优先先把误用场景清掉。

### 8.2 ERP 订单模块侧

#### 第一阶段

1. 给 `SalesOrder`、`AfterSaleOrder` 增加 `managed_scope_id`
2. repo 的边界列改为真实归属字段，不再使用 `sales_owner` 充当框架边界列
3. `read_group` 直接走 `managed_scope_id IN visible_scope_ids`
4. `read_self` 单独走 `sales_owner = current_user`

#### 第二阶段

1. 梳理店铺/渠道账号到业务域的映射
2. 明确“手工录单默认归属哪个业务域”
3. 增加订单转组/转派操作与审计

#### 第三阶段

1. 让售后、收款、开票等订单派生对象统一继承订单边界
2. 看板、统计、同步日志按相同口径复用边界规则

## 9. 迁移策略

### 9.1 兼容阶段

- 保留现有权限码
- 保留现有 `Scope` 树与 `ActiveScope` 模型
- 订单查询先双跑：
  - 老逻辑：`sales_owner` + 展开用户集合
  - 新逻辑：`managed_scope_id` + `sales_owner`

### 9.2 切换阶段

满足以下条件后切换：

1. 订单已回填 `managed_scope_id`
2. 渠道订单有默认业务域映射
3. 订单列表、自定义查询、看板核对通过

### 9.3 收敛阶段

切换后删除：

- 订单模块把用户 ID 塞进 `DataScope.VisibleScopeIDs` 的逻辑
- repo 用 `sales_owner` 充当框架边界列的做法

## 10. 最终判断

本次改造的关键不是继续讨论“`scope` 这个词换成什么更顺”，而是先把三个层次拆开：

1. **授权层**：谁在哪个授权域里拥有什么权限
2. **资源归属层**：这条资源归哪个业务边界负责
3. **业务可见性层**：当前请求应该看到哪些数据

对 `IAM` 来说，`Scope` 仍然成立。

对 `ERP order` 来说，真正正确的做法不是继续伪装成“也是一种 `DataScope`”，而是：

- 用 `managed_scope_id` 承担组级边界
- 用 `sales_owner` 承担负责人语义
- 用权限码决定是看本人、本组还是全部

这才是既能解释当前业务，又能和框架长期演进对齐的方案。
