package main

import (
	"context"
	"fmt"
	"log"

	"gochen/app/eventsourced"
	deventsourced "gochen/domain/eventsourced"
	gerrors "gochen/errors"
	"gochen/eventing"
	ebus "gochen/eventing/bus"
	"gochen/eventing/registry"
	estore "gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/messaging"
	mtransport "gochen/messaging/transport/memory"
)

// 领域事件载荷
type AccountOpened struct{ Initial int }

// EventType 返回开户事件的类型标识。
func (e *AccountOpened) EventType() string { return "AccountOpened" }

// Deposited 表示入金事件。
type Deposited struct{ Amount int }

// EventType 返回入金事件的类型标识。
func (e *Deposited) EventType() string { return "Deposited" }

// Withdrawn 表示出金事件。
type Withdrawn struct{ Amount int }

// EventType 返回出金事件的类型标识。
func (e *Withdrawn) EventType() string { return "Withdrawn" }

// 聚合
type Account struct {
	*deventsourced.EventSourcedAggregate[int64] `aggregate:"account"`
	Balance                                     int
}

// NewAccount 创建事件溯源账户聚合。
func NewAccount(metadataRegistry *deventsourced.MetadataRegistry, id int64) *Account {
	account, err := deventsourced.New[Account, int64](metadataRegistry, id)
	if err != nil {
		panic(err)
	}
	return account
}

// ApplyAccountOpened 应用开户事件。
func (a *Account) ApplyAccountOpened(e *AccountOpened) {
	a.Balance = e.Initial
}

// ApplyDeposited 应用入金事件。
func (a *Account) ApplyDeposited(e *Deposited) {
	a.Balance += e.Amount
}

// ApplyWithdrawn 应用出金事件。
func (a *Account) ApplyWithdrawn(e *Withdrawn) {
	a.Balance -= e.Amount
}

// Open 执行开户命令。
func (a *Account) Open(initial int) error {
	if a.GetVersion() != 0 {
		return fmt.Errorf("already opened")
	}
	return a.ApplyAndRecord(&AccountOpened{Initial: initial})
}

// Deposit 执行入金命令。
func (a *Account) Deposit(n int) error {
	return a.ApplyAndRecord(&Deposited{Amount: n})
}

// Withdraw 执行出金命令。
func (a *Account) Withdraw(n int) error {
	if a.Balance < n {
		return fmt.Errorf("insufficient")
	}
	return a.ApplyAndRecord(&Withdrawn{Amount: n})
}

// 命令：开账户
type OpenAccountCmd struct {
	ID      int64
	Initial int
}

// AggregateID 返回开户命令对应的聚合 ID。
func (c *OpenAccountCmd) AggregateID() int64 { return c.ID }

// 命令：入金
type DepositCmd struct {
	ID     int64
	Amount int
}

// AggregateID 返回入金命令对应的聚合 ID。
func (c *DepositCmd) AggregateID() int64 { return c.ID }

// 命令：出金
type WithdrawCmd struct {
	ID     int64
	Amount int
}

// AggregateID 返回出金命令对应的聚合 ID。
func (c *WithdrawCmd) AggregateID() int64 { return c.ID }

// main 演示 int64 聚合 ID 下的事件溯源仓储、总线与命令服务。
func main() {
	log.Println("=== Event Sourcing Demo ===")
	metadataRegistry := deventsourced.NewMetadataRegistry()
	mustMetadata(metadataRegistry.Register(&Account{}, "account"))

	// 事件存储 + 总线（内存）
	store := estore.NewMemoryEventStore()
	bus := ebus.NewEventBus(messaging.NewMessageBus(mtransport.NewMemoryTransport(1000, 2)))
	reg := registry.NewRegistry()
	mustRegister := func(eventType string, factory func() any) {
		if err := reg.Register(eventType, factory); err != nil {
			panic(err)
		}
	}
	mustRegister("AccountOpened", func() any { return &AccountOpened{} })
	mustRegister("Deposited", func() any { return &Deposited{} })
	mustRegister("Withdrawn", func() any { return &Withdrawn{} })
	upgraders := upcast.NewUpgraderRegistry()

	// 构造 IEventStore 实现
	eventStoreAdapter, err := eventsourced.NewDomainEventStore(eventsourced.DomainEventStoreOptions[*Account, int64]{
		AggregateType:    "account",
		EventStore:       store,
		EventBus:         bus,
		PublishEvents:    false, // 简化示例：禁用事件发布（生产环境需配置 OutboxRepo）
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	if err != nil {
		panic(err)
	}

	// 构造领域层仓储
	repo, err := eventsourced.NewEventSourcedRepository(eventsourced.RepositoryOptions[*Account, int64]{
		AggregateType:    "account",
		Sample:           &Account{},
		Factory:          func(id int64) (*Account, error) { return NewAccount(metadataRegistry, id), nil },
		Store:            eventStoreAdapter,
		MetadataRegistry: metadataRegistry,
	})
	if err != nil {
		panic(err)
	}

	// 构造应用层命令服务模板（支持并发冲突自动重试配置）。
	//
	// 注意：这里仅演示如何配置；并发冲突是否发生取决于底层 IEventStore 的实现与并发写入场景。
	svc, err := eventsourced.NewEventSourcedService[*Account, int64](repo, &eventsourced.EventSourcedServiceOptions[*Account, int64]{
		ConcurrencyRetry: eventsourced.DefaultRetryConfig(),
	})
	if err != nil {
		panic(err)
	}
	if err := svc.RegisterCommandHandler(&OpenAccountCmd{}, func(ctx context.Context, cmd eventsourced.IEventSourcedCommand[int64], aggregate *Account) error {
		_ = ctx
		c := cmd.(*OpenAccountCmd)
		return aggregate.Open(c.Initial)
	}); err != nil {
		panic(err)
	}
	if err := svc.RegisterCommandHandler(&DepositCmd{}, func(ctx context.Context, cmd eventsourced.IEventSourcedCommand[int64], aggregate *Account) error {
		_ = ctx
		c := cmd.(*DepositCmd)
		return aggregate.Deposit(c.Amount)
	}); err != nil {
		panic(err)
	}
	if err := svc.RegisterCommandHandler(&WithdrawCmd{}, func(ctx context.Context, cmd eventsourced.IEventSourcedCommand[int64], aggregate *Account) error {
		_ = ctx
		c := cmd.(*WithdrawCmd)
		return aggregate.Withdraw(c.Amount)
	}); err != nil {
		panic(err)
	}

	// 订阅打印事件
	unsub, err := bus.SubscribeEvent(context.Background(), "*", ebus.EventHandlerFunc(func(ctx context.Context, evt eventing.IEvent) error {
		aggID := int64(0)
		if typed, ok := evt.(eventing.ITypedEvent[int64]); ok {
			aggID = typed.GetAggregateID()
		}
		log.Printf("event: %s v%d -> agg=%d", evt.GetType(), evt.GetVersion(), aggID)
		return nil
	}))
	if err != nil {
		panic(err)
	}
	defer func() { _ = unsub(context.Background()) }()

	ctx := context.Background()

	// 演示1：使用 GetOrCreate 创建新账户（幂等创建模式）
	log.Println("\n--- Demo 1: GetOrCreate (idempotent creation) ---")
	acc, err := repo.GetOrCreate(ctx, 1001)
	if err != nil {
		panic(err)
	}
	if acc.GetVersion() == 0 {
		log.Println("Account 1001 is new, opening...")
		if err := acc.Open(100); err != nil {
			panic(err)
		}
	}
	if err := acc.Deposit(50); err != nil {
		panic(err)
	}
	if err := repo.Save(ctx, acc); err != nil {
		panic(err)
	}
	log.Printf("Account 1001 balance: %d, version: %d", acc.Balance, acc.GetVersion())

	// 演示2：使用 Get 加载已存在的聚合
	log.Println("\n--- Demo 2: Get (load existing) ---")
	loaded, err := repo.Get(ctx, 1001)
	if err != nil {
		panic(err)
	}
	if err := loaded.Withdraw(30); err != nil {
		panic(err)
	}
	if err := repo.Save(ctx, loaded); err != nil {
		panic(err)
	}
	log.Printf("Account 1001 after withdrawal: balance=%d, version=%d", loaded.Balance, loaded.GetVersion())

	// 演示3：Get 对不存在的聚合返回 NotFound
	log.Println("\n--- Demo 3: Get (not found) ---")
	_, err = repo.Get(ctx, 9999)
	if gerrors.Is(err, gerrors.NotFound) {
		log.Println("Account 9999 not found (as expected)")
	} else if err != nil {
		panic(err)
	}

	// 演示4：通过命令服务执行（模板化加载/执行业务/保存）。
	log.Println("\n--- Demo 4: EventSourcedService (command execution template) ---")
	if err := svc.ExecuteCommand(ctx, &OpenAccountCmd{ID: 2001, Initial: 100}); err != nil {
		panic(err)
	}
	if err := svc.ExecuteCommand(ctx, &DepositCmd{ID: 2001, Amount: 50}); err != nil {
		panic(err)
	}
	if err := svc.ExecuteCommand(ctx, &WithdrawCmd{ID: 2001, Amount: 30}); err != nil {
		panic(err)
	}
	acc2, err := repo.Get(ctx, 2001)
	if err != nil {
		panic(err)
	}
	log.Printf("Account 2001: balance=%d, version=%d", acc2.Balance, acc2.GetVersion())

	// 最终状态
	log.Println("\n--- Final State ---")
	final, err := repo.Get(ctx, 1001)
	if err != nil {
		panic(err)
	}
	log.Printf("Account 1001: balance=%d, version=%d", final.Balance, final.GetVersion())
}

func mustMetadata(_ *deventsourced.Metadata, err error) {
	if err != nil {
		panic(err)
	}
}
