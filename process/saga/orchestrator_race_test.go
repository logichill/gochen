package saga

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"gochen/errors"
	"gochen/messaging/command"
)

// inMemoryStateStore 为并发测试提供带锁的 SagaStateStore 实现。
type inMemoryStateStore struct {
	mu     sync.Mutex
	states map[string]*SagaState
}

// newInMemoryStateStore result：返回的实例（类型：*inMemoryStateStore）。
//
// 返回：
func newInMemoryStateStore() *inMemoryStateStore {
	return &inMemoryStateStore{
		states: make(map[string]*SagaState),
	}
}

// Load 解析数据。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - sagaID：对象/实体标识
//
// 返回：
// - result1：返回结果（类型：*SagaState）
// - err：错误信息（nil 表示成功）
func (s *inMemoryStateStore) Load(ctx context.Context, sagaID string) (*SagaState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.states[sagaID]; ok {
		return st, nil
	}
	return nil, errors.NewCode(errors.NotFound, fmt.Sprintf("saga not found: %s", sagaID))
}

// Save ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - state：状态对象（类型：*SagaState）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *inMemoryStateStore) Save(ctx context.Context, state *SagaState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.states[state.SagaID]; exists {
		return errors.NewCode(errors.Conflict, fmt.Sprintf("saga already exists: %s", state.SagaID))
	}
	s.states[state.SagaID] = state
	return nil
}

// Update 更新对象并写入存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - state：状态对象（类型：*SagaState）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *inMemoryStateStore) Update(ctx context.Context, state *SagaState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state.SagaID] = state
	return nil
}

// Get 从存储中查询数据。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - sagaID：对象/实体标识
//
// 返回：
// - result1：返回结果（类型：*SagaState）
// - err：错误信息（nil 表示成功）
func (s *inMemoryStateStore) Get(ctx context.Context, sagaID string) (*SagaState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.states[sagaID]; ok {
		return st, nil
	}
	return nil, errors.NewCode(errors.NotFound, fmt.Sprintf("saga not found: %s", sagaID))
}

// Delete 删除数据并同步到存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - sagaID：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *inMemoryStateStore) Delete(ctx context.Context, sagaID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, sagaID)
	return nil
}

// List 从存储中查询对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - status：参数值（具体语义见函数上下文）（类型：SagaStatus）
//
// 返回：
// - result1：列表结果（元素类型：*SagaState）
// - err：错误信息（nil 表示成功）
func (s *inMemoryStateStore) List(ctx context.Context, status SagaStatus) ([]*SagaState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*SagaState, 0, len(s.states))
	for _, st := range s.states {
		out = append(out, st)
	}
	return out, nil
}

// TestSagaOrchestrator_ConcurrentExecute 验证 SagaOrchestrator ConcurrentExecute。
func TestSagaOrchestrator_ConcurrentExecute(t *testing.T) {
	stateStore := newInMemoryStateStore()

	cmdExecutor := command.NewCommandExecutor()
	require.NoError(t, cmdExecutor.RegisterHandler("TestCommand", func(ctx context.Context, cmd *command.Command) error {
		return nil
	}))

	o := NewSagaOrchestrator(cmdExecutor, nil, stateStore)

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
								return command.NewCommand("cmd1", "TestCommand", "1", "Test", nil), nil
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

// ID 返回当前值。
//
// 返回：
// - result：文本结果
func (s *raceSimpleSaga) ID() string { return s.id }

// Steps 返回当前值。
//
// 返回：
// - result：列表结果（元素类型：*SagaStep）
func (s *raceSimpleSaga) Steps() []*SagaStep { return s.steps }

// OnComplete ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *raceSimpleSaga) OnComplete(ctx context.Context) error { return nil }

// OnFailed ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - err：待检查/包装的错误（类型：error）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *raceSimpleSaga) OnFailed(ctx context.Context, err error) error {
	_ = err
	return nil
}
