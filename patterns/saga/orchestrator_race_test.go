package saga

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"gochen/messaging"
	"gochen/messaging/command"
	synctransport "gochen/messaging/transport/sync"
)

// inMemoryStateStore 为并发测试提供带锁的 SagaStateStore 实现。
type inMemoryStateStore struct {
	mu     sync.Mutex
	states map[string]*SagaState
}

func newInMemoryStateStore() *inMemoryStateStore {
	return &inMemoryStateStore{
		states: make(map[string]*SagaState),
	}
}

func (s *inMemoryStateStore) Load(ctx context.Context, sagaID string) (*SagaState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.states[sagaID]; ok {
		return st, nil
	}
	return nil, NewSagaNotFoundError(sagaID)
}

func (s *inMemoryStateStore) Save(ctx context.Context, state *SagaState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state.SagaID] = state
	return nil
}

func (s *inMemoryStateStore) Update(ctx context.Context, state *SagaState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state.SagaID] = state
	return nil
}

func (s *inMemoryStateStore) Get(ctx context.Context, sagaID string) (*SagaState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.states[sagaID]; ok {
		return st, nil
	}
	return nil, NewSagaNotFoundError(sagaID)
}

func (s *inMemoryStateStore) Delete(ctx context.Context, sagaID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, sagaID)
	return nil
}

func (s *inMemoryStateStore) List(ctx context.Context, status SagaStatus) ([]*SagaState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*SagaState, 0, len(s.states))
	for _, st := range s.states {
		out = append(out, st)
	}
	return out, nil
}

// TestSagaOrchestrator_ConcurrentExecute 验证在多个 goroutine 并发执行 Saga 时，
// Orchestrator 与共享 SagaStateStore 不会产生数据竞态（配合 go test -race）。
func TestSagaOrchestrator_ConcurrentExecute(t *testing.T) {
	stateStore := newInMemoryStateStore()

	ctx := context.Background()

	// 使用同步 Transport + CommandBus，保证 Dispatch 为同步执行，便于并发测试
	transport := synctransport.NewSyncTransport()
	require.NoError(t, transport.Start(ctx))
	defer transport.Close()

	msgBus := messaging.NewMessageBus(transport)
	cmdBus := command.NewCommandBus(msgBus, nil)

	require.NoError(t, cmdBus.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))

	o := NewSagaOrchestrator(cmdBus, nil, stateStore)

	const (
		goroutines = 8
		perGor     = 10
	)

	var cmdCount int32
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGor; i++ {
				sagaID := fmt.Sprintf("saga-%d-%d", id, i)
				s := &raceSimpleSaga{
					id: sagaID,
					steps: []*SagaStep{
						{
							Name: "step1",
							Command: func(ctx context.Context) (*command.Command, error) {
								atomic.AddInt32(&cmdCount, 1)
								return command.NewCommand("cmd1", "TestCommand", 1, "Test", nil), nil
							},
						},
					},
				}
				_ = o.Execute(context.Background(), s)
			}
		}(g)
	}

	wg.Wait()

	// 简单检查：所有 saga 都应该在 stateStore 中有记录或处于完成/失败状态；
	// 这里不做严格行为断言，重点依赖 -race 检查状态更新的并发安全。
	stateStore.mu.Lock()
	defer stateStore.mu.Unlock()
	if len(stateStore.states) == 0 {
		t.Fatalf("expected some saga states saved, got 0")
	}
}

// raceSimpleSaga 为并发测试提供的极简 Saga 实现（避免与 orchestrator_test.go 中 simpleSaga 冲突）。
type raceSimpleSaga struct {
	id    string
	steps []*SagaStep
}

func (s *raceSimpleSaga) GetID() string                        { return s.id }
func (s *raceSimpleSaga) GetSteps() []*SagaStep                { return s.steps }
func (s *raceSimpleSaga) OnComplete(ctx context.Context) error { return nil }
func (s *raceSimpleSaga) OnFailed(ctx context.Context, err error) error {
	_ = err
	return nil
}
