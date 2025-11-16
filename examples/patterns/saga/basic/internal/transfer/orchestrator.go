package transfer

import (
	"context"
	"fmt"
	"sync"
)

// State 简化状态存储（demo 用，直接内存）
type State struct {
	ID   string
	Data map[string]interface{}
}

// Orchestrator 最小 Saga 编排器
type Orchestrator struct {
	mu    sync.RWMutex
	store map[string]*State
}

func NewOrchestrator() *Orchestrator {
	return &Orchestrator{store: make(map[string]*State)}
}

func (o *Orchestrator) StartTransfer(ctx context.Context, from, to int64, amount int) (string, error) {
	id := fmt.Sprintf("tx-%d-%d-%d", from, to, amount)
	o.mu.Lock()
	o.store[id] = &State{ID: id, Data: map[string]interface{}{"from": from, "to": to, "amount": amount, "step": "started", "status": "pending"}}
	o.mu.Unlock()
	return id, nil
}

func (o *Orchestrator) Debit(ctx context.Context, id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	s, ok := o.store[id]
	if !ok { return fmt.Errorf("tx not found") }
	s.Data["step"] = "debited"
	return nil
}

func (o *Orchestrator) Credit(ctx context.Context, id string, ok bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	s, exists := o.store[id]
	if !exists { return fmt.Errorf("tx not found") }
	if ok {
		s.Data["step"] = "credited"
		s.Data["status"] = "success"
	} else {
		s.Data["step"] = "compensated"
		s.Data["status"] = "failed"
	}
	return nil
}

func (o *Orchestrator) GetState(ctx context.Context, id string) (*State, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	s, ok := o.store[id]
	if !ok { return nil, fmt.Errorf("tx not found") }
	return s, nil
}

