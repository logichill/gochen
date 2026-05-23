package main

import (
	"context"
	"fmt"
	"log"

	"gochen/app/eventsourced"
	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
)

// 字符串 ID 版本的账户领域事件
type StringAccountOpened struct{ Initial int }

// EventType 返回开户事件的类型标识。
func (e *StringAccountOpened) EventType() string { return "StringAccountOpened" }

// StringDeposited 表示入金事件（string 聚合 ID）。
type StringDeposited struct{ Amount int }

// EventType 返回入金事件的类型标识。
func (e *StringDeposited) EventType() string { return "StringDeposited" }

// StringWithdrawn 表示出金事件（string 聚合 ID）。
type StringWithdrawn struct{ Amount int }

// EventType 返回出金事件的类型标识。
func (e *StringWithdrawn) EventType() string { return "StringWithdrawn" }

// StringAccount 演示使用 string 作为聚合 ID 的事件溯源账户。
type StringAccount struct {
	*deventsourced.EventSourcedAggregate[string] `aggregate:"string_account"`
	Balance                                      int
}

// NewStringAccount 创建一个使用 string ID 的账户聚合。
func NewStringAccount(metadataRegistry *deventsourced.MetadataRegistry, id string) *StringAccount {
	account, err := deventsourced.New[StringAccount, string](metadataRegistry, id)
	if err != nil {
		panic(err)
	}
	return account
}

// 命令：开账户
type OpenStringAccountCmd struct {
	ID      string
	Initial int
}

// AggregateID 返回开户命令对应的聚合 ID。
func (c *OpenStringAccountCmd) AggregateID() string { return c.ID }

// 命令：入金
type DepositStringCmd struct {
	ID     string
	Amount int
}

// AggregateID 返回入金命令对应的聚合 ID。
func (c *DepositStringCmd) AggregateID() string { return c.ID }

// 命令：出金
type WithdrawStringCmd struct {
	ID     string
	Amount int
}

// AggregateID 返回出金命令对应的聚合 ID。
func (c *WithdrawStringCmd) AggregateID() string { return c.ID }

// Open 在首次开户时记录开户事件。
func (a *StringAccount) Open(initial int) error {
	if a.GetVersion() != 0 {
		return fmt.Errorf("account %s already opened", a.GetID())
	}
	return a.ApplyAndRecord(&StringAccountOpened{Initial: initial})
}

// Deposit 记录一笔入金事件。
func (a *StringAccount) Deposit(n int) error {
	return a.ApplyAndRecord(&StringDeposited{Amount: n})
}

// Withdraw 在余额充足时记录一笔出金事件。
func (a *StringAccount) Withdraw(n int) error {
	if a.Balance < n {
		return fmt.Errorf("insufficient balance: %d < %d", a.Balance, n)
	}
	return a.ApplyAndRecord(&StringWithdrawn{Amount: n})
}

// ApplyStringAccountOpened 用开户事件初始化账户余额。
func (a *StringAccount) ApplyStringAccountOpened(evt *StringAccountOpened) {
	a.Balance = evt.Initial
}

// ApplyStringDeposited 把入金金额累加到当前余额。
func (a *StringAccount) ApplyStringDeposited(evt *StringDeposited) {
	a.Balance += evt.Amount
}

// ApplyStringWithdrawn 从当前余额中扣减出金金额。
func (a *StringAccount) ApplyStringWithdrawn(evt *StringWithdrawn) {
	a.Balance -= evt.Amount
}

// main 演示 string 聚合 ID 下的事件溯源仓储与命令服务。
func main() {
	log.Println("=== Event Sourcing Demo (string aggregate ID) ===")

	ctx := context.Background()
	metadataRegistry := deventsourced.NewMetadataRegistry()
	mustMetadata(metadataRegistry.Register(&StringAccount{}, "string_account"))

	// 1) 使用字符串 ID 的内存 EventStore（示例实现）
	eventStore := NewStringMemoryEventStore()
	reg := registry.NewRegistry()
	mustRegister := func(eventType string, factory func() any) {
		if err := reg.Register(eventType, factory); err != nil {
			panic(err)
		}
	}
	mustRegister("StringAccountOpened", func() any { return &StringAccountOpened{} })
	mustRegister("StringDeposited", func() any { return &StringDeposited{} })
	mustRegister("StringWithdrawn", func() any { return &StringWithdrawn{} })
	upgraders := upcast.NewUpgraderRegistry()

	// 2) 构造领域层 IEventStore[string] 实现（应用层适配器）
	storeAdapter, err := eventsourced.NewDomainEventStore(eventsourced.DomainEventStoreOptions[*StringAccount, string]{
		AggregateType:    "string_account",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
		// 该示例聚焦 ID 类型，不演示 EventBus/Outbox 组合
	})
	if err != nil {
		log.Fatalf("failed to create DomainAggregateStore: %v", err)
	}

	// 3) 构造领域层仓储：IEventSourcedRepository[StringAccount, string]
	repo, err := eventsourced.NewEventSourcedRepository(eventsourced.RepositoryOptions[*StringAccount, string]{
		AggregateType:    "string_account",
		Sample:           &StringAccount{},
		Factory:          func(id string) (*StringAccount, error) { return NewStringAccount(metadataRegistry, id), nil },
		Store:            storeAdapter,
		MetadataRegistry: metadataRegistry,
	})
	if err != nil {
		log.Fatalf("failed to create repository: %v", err)
	}

	// 4) 构造应用层命令服务模板（支持并发冲突自动重试配置）。
	//
	// 注意：这里仅演示如何配置；并发冲突是否发生取决于底层 IEventStore 的实现与并发写入场景。
	svc, err := eventsourced.NewEventSourcedService[*StringAccount, string](repo, &eventsourced.EventSourcedServiceOptions[*StringAccount, string]{
		ConcurrencyRetry: eventsourced.DefaultRetryConfig(),
	})
	if err != nil {
		log.Fatalf("failed to create service: %v", err)
	}
	if err := svc.RegisterCommandHandler(&OpenStringAccountCmd{}, func(ctx context.Context, cmd eventsourced.IEventSourcedCommand[string], aggregate *StringAccount) error {
		_ = ctx
		c := cmd.(*OpenStringAccountCmd)
		return aggregate.Open(c.Initial)
	}); err != nil {
		log.Fatalf("failed to register OpenStringAccountCmd handler: %v", err)
	}
	if err := svc.RegisterCommandHandler(&DepositStringCmd{}, func(ctx context.Context, cmd eventsourced.IEventSourcedCommand[string], aggregate *StringAccount) error {
		_ = ctx
		c := cmd.(*DepositStringCmd)
		return aggregate.Deposit(c.Amount)
	}); err != nil {
		log.Fatalf("failed to register DepositStringCmd handler: %v", err)
	}
	if err := svc.RegisterCommandHandler(&WithdrawStringCmd{}, func(ctx context.Context, cmd eventsourced.IEventSourcedCommand[string], aggregate *StringAccount) error {
		_ = ctx
		c := cmd.(*WithdrawStringCmd)
		return aggregate.Withdraw(c.Amount)
	}); err != nil {
		log.Fatalf("failed to register WithdrawStringCmd handler: %v", err)
	}

	// 5) 手动使用仓储：开户 + 存款
	accountID := "ACC-1001"
	acc := NewStringAccount(metadataRegistry, accountID)

	if err := acc.Open(100); err != nil {
		log.Fatalf("open account failed: %v", err)
	}
	if err := acc.Deposit(50); err != nil {
		log.Fatalf("deposit failed: %v", err)
	}
	if err := repo.Save(ctx, acc); err != nil {
		log.Fatalf("save after deposit failed: %v", err)
	}

	// 重新加载聚合，继续操作（符合事件溯源的标准模式）
	acc, err = repo.Get(ctx, accountID)
	if err != nil {
		log.Fatalf("reload account failed: %v", err)
	}

	if err := acc.Withdraw(30); err != nil {
		log.Fatalf("withdraw failed: %v", err)
	}
	if err := repo.Save(ctx, acc); err != nil {
		log.Fatalf("save after withdraw failed: %v", err)
	}

	// 5) 最终读取并打印结果
	final, err := repo.Get(ctx, accountID)
	if err != nil {
		log.Fatalf("reload account failed: %v", err)
	}
	log.Printf("Account[%s] Balance=%d Version=%d", accountID, final.Balance, final.GetVersion())

	// 6) 使用命令服务模板：加载聚合 → 执行业务 → 保存
	log.Println("\n--- Demo: EventSourcedService (command execution template) ---")
	accountID2 := "ACC-2001"
	if err := svc.ExecuteCommand(ctx, &OpenStringAccountCmd{ID: accountID2, Initial: 100}); err != nil {
		log.Fatalf("execute open failed: %v", err)
	}
	if err := svc.ExecuteCommand(ctx, &DepositStringCmd{ID: accountID2, Amount: 50}); err != nil {
		log.Fatalf("execute deposit failed: %v", err)
	}
	if err := svc.ExecuteCommand(ctx, &WithdrawStringCmd{ID: accountID2, Amount: 30}); err != nil {
		log.Fatalf("execute withdraw failed: %v", err)
	}
	final2, err := repo.Get(ctx, accountID2)
	if err != nil {
		log.Fatalf("reload account failed: %v", err)
	}
	log.Printf("Account[%s] Balance=%d Version=%d", accountID2, final2.Balance, final2.GetVersion())
}

func mustMetadata(_ *deventsourced.Metadata, err error) {
	if err != nil {
		panic(err)
	}
}
