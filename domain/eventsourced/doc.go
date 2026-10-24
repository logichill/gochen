// Package eventsourced 提供事件溯源聚合根基类与仓储 ports。
//
// 适用于关键交易、强审计、事件驱动等需要完整历史追溯的场景。
//
// # 核心类型。
//
// 聚合根：
//   - [IEventSourcedAggregate] — 事件溯源聚合根接口。
//   - [EventSourcedAggregate] — 聚合根泛型基类。
//   - [IVersionSettable] — 可设置版本的聚合接口（用于快照恢复）
//
// 仓储 ports：
//   - [IEventSourcedRepository] — 事件溯源仓储接口。
//   - [IEventStore] — 领域事件存储接口。
//
// 反射元数据：
//   - [Metadata] — 构造后只读的聚合元数据。
//   - [MetadataRegistry] — 当前应用运行时持有的聚合 metadata registry。
//   - [MetadataRegistration] / [EventSourcedSupport] — 模块级 aggregate metadata 声明语法糖。
//
// # 事件处理方法约定。
//
// 推荐模式（struct tag）：在嵌入字段上声明 `aggregate:"xxx"` tag，aggregateType 由框架自动提取。
//
//	type Account struct {
//	    *eventsourced.EventSourcedAggregate[int64] `aggregate:"account"`
//	    Balance int
//	}
//
//	func NewAccount(registry *eventsourced.MetadataRegistry, id int64) (*Account, error) {
//	    return eventsourced.New[Account, int64](registry, id)
//	}
//
//	func (a *Account) ApplyDeposited(e *Deposited) {
//	    a.Balance += e.Amount
//	}
//
// 显式模式：手动传入 aggregateType 字符串。
//
//	type Account struct {
//	    *eventsourced.EventSourcedAggregate[int64]
//	    Balance int
//	}
//
//	func NewAccount(registry *eventsourced.MetadataRegistry, id int64) (*Account, error) {
//	    a := &Account{}
//	    agg, err := eventsourced.InitAggregate[int64](registry, a, id, "account")
//	    if err != nil {
//	        return nil, err
//	    }
//	    a.EventSourcedAggregate = agg
//	    return a, nil
//	}
//
// # 反射路由约束。
//
// 使用显式 metadata 绑定时的约束：
//
//   - 事件处理方法必须命名为 ApplyXxx；ApplyEvent / ApplyAndRecord 为框架保留入口。
//     其他名称的方法不会作为处理器，校验等辅助方法应使用其他前缀。
//
//   - 处理器必须仅属于聚合指针类型的方法集。值接收器在装配时被拒绝；
//     值嵌入可提升指针方法，指针/接口嵌入的处理器须在外层用指针接收器显式转发。
//
//   - 恰好一个指针事件参数（如 *Deposited），实现 domain.IDomainEvent；
//     不返回值或仅返回 error，不允许可变参数；不合法的 ApplyXxx 在装配时失败。
//
//   - 同一 Go 类型只能有一个处理方法。
//
//   - 推荐在启动期先通过 MetadataRegistry.RegisterSet / Aggregate(...) / AggregateFromTag(...) /
//     RegisterModuleAggregates(...) 预热 metadata；若调用方未显式预热，InitAggregate(...) /
//     InitAggregateFromTag(...) 也会基于显式 registry 自动 ensure metadata。
//
//   - 若应用按模块集中装配 aggregates，可用 Aggregate(...) / AggregateFromTag(...) /
//     EventSourcedSupport / RegisterModuleAggregates(...) 在启动期统一注册并校验 metadata。
//
//   - BindMetadata(self, metadata) error — 推荐路径，显式绑定预编译元数据。
//
//   - BindMetadata(self, metadata) / InitAggregate(...) / InitAggregateFromTag(...) / New(...) —
//     对外统一走 error 型 fail-fast，不再要求 Must* 包装。
//
// # 业务使用约束。
//
// 命令方法先校验，再调用 ApplyAndRecord；ApplyXxx 仅更新内存状态，不做 I/O、
// 不读取当前时间或随机数。需要的时间与标识在命令阶段生成后写入事件。
// ApplyEvent 供仓储顺序回放，不去重；任何应用错误都要求丢弃当前实例并重新加载。
// 聚合实例不可并发使用，事件在应用后不可修改。业务测试必须覆盖命令失败无副作用、
// 事件逐条回放与命令执行结果一致，以及需要支持的历史事件版本。
package eventsourced
