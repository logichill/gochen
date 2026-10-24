package eventsourced_test

import (
	"fmt"

	es "gochen/domain/eventsourced"
	"gochen/errors"
)

type deposited struct{ Amount int }

func (*deposited) EventType() string { return "Deposited" }

type account struct {
	*es.EventSourcedAggregate[int64] `aggregate:"account"`
	balance                          int
}

func (a *account) Deposit(amount int) error {
	if amount <= 0 {
		return errors.NewCode(errors.Validation, "amount must be positive")
	}
	return a.ApplyAndRecord(&deposited{Amount: amount})
}

func (a *account) ApplyDeposited(e *deposited) { a.balance += e.Amount }

// ExampleNew 展示启动预检、命令校验与独立实例回放的一致性验证。
func ExampleNew() {
	check := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	registry := es.NewMetadataRegistry()
	check(registry.RegisterSet(es.AggregateFromTag(&account{})))
	live, err := es.New[account, int64](registry, 1)
	check(err)
	check(live.Deposit(10))
	check(live.Deposit(20))
	fmt.Println(errors.Is(live.Deposit(0), errors.Validation))
	fmt.Println(live.balance, live.GetVersion(), len(live.GetUncommittedEvents()))

	replayed, err := es.New[account, int64](registry, 1)
	check(err)
	for _, event := range live.GetUncommittedEvents() {
		check(replayed.ApplyEvent(event))
	}
	fmt.Println(replayed.balance == live.balance, replayed.GetVersion() == live.GetVersion())
	fmt.Println(len(replayed.GetUncommittedEvents()))
	// Output:
	// true
	// 30 2 2
	// true true
	// 0
}
