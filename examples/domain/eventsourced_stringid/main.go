package main

import (
	"context"
	"fmt"
	"log"

	"gochen/app/eventsourced"
	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
)

// 字符串 ID 版本的账户领域事件
type StringAccountOpened struct{ Initial int }

func (e *StringAccountOpened) EventType() string { return "StringAccountOpened" }

type StringDeposited struct{ Amount int }

func (e *StringDeposited) EventType() string { return "StringDeposited" }

type StringWithdrawn struct{ Amount int }

func (e *StringWithdrawn) EventType() string { return "StringWithdrawn" }

// 使用 string 作为聚合 ID 的账户聚合
type StringAccount struct {
	*deventsourced.EventSourcedAggregate[string]
	Balance int
}

func NewStringAccount(id string) *StringAccount {
	return &StringAccount{
		EventSourcedAggregate: deventsourced.NewEventSourcedAggregate[string](id, "string_account"),
	}
}

func (a *StringAccount) Open(initial int) error {
	if a.GetVersion() != 0 {
		return fmt.Errorf("account %s already opened", a.GetID())
	}
	return a.ApplyAndRecord(&StringAccountOpened{Initial: initial})
}

func (a *StringAccount) Deposit(n int) error {
	return a.ApplyAndRecord(&StringDeposited{Amount: n})
}

func (a *StringAccount) Withdraw(n int) error {
	if a.Balance < n {
		return fmt.Errorf("insufficient balance: %d < %d", a.Balance, n)
	}
	return a.ApplyAndRecord(&StringWithdrawn{Amount: n})
}

func (a *StringAccount) ApplyEvent(evt domain.IDomainEvent) error {
	switch e := evt.(type) {
	case *StringAccountOpened:
		a.Balance = e.Initial
	case *StringDeposited:
		a.Balance += e.Amount
	case *StringWithdrawn:
		a.Balance -= e.Amount
	}
	return a.EventSourcedAggregate.ApplyEvent(evt)
}

func main() {
	log.Println("=== Event Sourcing Demo (string aggregate ID) ===")

	ctx := context.Background()

	// 1) 使用字符串 ID 的内存 EventStore（示例实现）
	eventStore := NewStringMemoryEventStore()

	// 2) 构造领域层 IEventStore[string] 实现（应用层适配器）
	storeAdapter, err := eventsourced.NewDomainEventStore(eventsourced.DomainEventStoreOptions[*StringAccount, string]{
		AggregateType: "string_account",
		Factory:       NewStringAccount,
		EventStore:    eventStore,
		// 该示例聚焦 ID 类型，不演示 EventBus/Outbox 组合
	})
	if err != nil {
		log.Fatalf("failed to create DomainEventStore: %v", err)
	}

	// 3) 构造领域层仓储：IEventSourcedRepository[StringAccount, string]
	repo, err := deventsourced.NewEventSourcedRepository[*StringAccount, string](
		"string_account",
		NewStringAccount,
		storeAdapter,
	)
	if err != nil {
		log.Fatalf("failed to create repository: %v", err)
	}

	// 4) 使用字符串 ID 操作账户

	// 第一个聚合实例：开户 + 存款
	accountID := "ACC-1001"
	acc := NewStringAccount(accountID)

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
	acc, err = repo.GetByID(ctx, accountID)
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
	final, err := repo.GetByID(ctx, accountID)
	if err != nil {
		log.Fatalf("reload account failed: %v", err)
	}
	log.Printf("Account[%s] Balance=%d Version=%d", accountID, final.Balance, final.GetVersion())
}
