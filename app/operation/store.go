package operation

import (
	"context"
	"sync"

	"gochen/errors"
)

// IStore 抽象 operation envelope 的最小持久化能力。
type IStore interface {
	Get(ctx context.Context, id string) (*Result, error)
	Put(ctx context.Context, result *Result) error
	Delete(ctx context.Context, id string) error
}

// MemoryStore 提供轻量内存实现，适合第一阶段或单进程场景。
type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]*Result
}

// NewMemoryStore 创建内存 operation store。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		records: make(map[string]*Result),
	}
}

// Get 按 operation id 读取 envelope。
func (s *MemoryStore) Get(ctx context.Context, id string) (*Result, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result, ok := s.records[id]
	if !ok {
		return nil, errors.NewCode(errors.NotFound, "operation not found").WithContext("operation_id", id)
	}
	return CloneResult(result), nil
}

// Put 保存或覆盖 operation envelope。
func (s *MemoryStore) Put(ctx context.Context, result *Result) error {
	if result == nil {
		return errors.NewCode(errors.InvalidInput, "operation result cannot be nil")
	}
	if result.Operation.ID == "" {
		return errors.NewCode(errors.InvalidInput, "operation id cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[result.Operation.ID] = CloneResult(result)
	return nil
}

// Delete 删除 operation envelope。
func (s *MemoryStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return nil
}

// CloneResult 深拷贝 operation result，避免调用方共享内部可变引用。
func CloneResult(result *Result) *Result {
	if result == nil {
		return nil
	}

	cloned := *result
	if result.Resource != nil {
		resource := *result.Resource
		cloned.Resource = &resource
	}
	if result.Error != nil {
		errCopy := *result.Error
		if result.Error.Details != nil {
			errCopy.Details = cloneMap(result.Error.Details)
		}
		cloned.Error = &errCopy
	}
	if result.Result != nil {
		cloned.Result = cloneMap(result.Result)
	}
	if result.AffectedScopes != nil {
		cloned.AffectedScopes = append([]string(nil), result.AffectedScopes...)
	}
	return &cloned
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	target := make(map[string]any, len(source))
	for key, value := range source {
		target[key] = cloneValue(value)
	}
	return target
}

func cloneSlice(source []any) []any {
	if source == nil {
		return nil
	}
	target := make([]any, len(source))
	for i, value := range source {
		target[i] = cloneValue(value)
	}
	return target
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		return cloneSlice(typed)
	default:
		return value
	}
}
