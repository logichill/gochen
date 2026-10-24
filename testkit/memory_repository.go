package testkit

import (
	"sync"

	appcrud "gochen/app/crud"
	"gochen/domain"
	domaincrud "gochen/domain/crud"
	"gochen/gen"
)

// MemoryRepository 是支持 CRUD、查询、批量操作和内存事务的泛型测试仓储。
//
// 实体写入和读取时会对“指向 struct 的指针”做一层浅拷贝；包含 map、slice、指针字段的实体若需
// 完全隔离，应在测试模型中自行复制这些引用字段。本实现通过插入顺序提供稳定的 List 结果。
type MemoryRepository[T domain.IEntity[ID], ID comparable] struct {
	mu        sync.RWMutex
	state     memoryState[T, ID]
	revision  uint64
	generator gen.IGenerator[ID]
	isolated  bool
	// isolationResolve 为 nil 时隔离键取自 contextx.TenantID（见 memory_repository_isolation.go）。
	isolationResolve IsolationResolveFunc
	// crossIsolationReads 记录本仓储是否有被开闸跨隔离只读的资格
	// （见 memory_repository_cross_isolation.go）。
	crossIsolationReads bool
	crossIsolationAudit ICrossIsolationAudit
	// scopeEntityType 非空表示已显式声明 L3 范围能力（见 memory_repository_scoped.go）。
	scopeEntityType string
}

// NewMemoryRepository 创建泛型内存仓储。
//
// generator 可为 nil；此时 Create 要求实体在调用前已具有非零 ID。实体 ID 为零且配置了 generator
// 时，实体还必须实现 domain.ISettableID，仓储才可回填生成值。
func NewMemoryRepository[T domain.IEntity[ID], ID comparable](
	generator gen.IGenerator[ID],
	opts ...MemoryOption[T, ID],
) *MemoryRepository[T, ID] {
	r := &MemoryRepository[T, ID]{
		state:     memoryState[T, ID]{entities: make(map[ID]T)},
		generator: generator,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	// 选项之间存在依赖（开闸阀依赖隔离声明），因此校验放在全部选项应用之后。
	r.validateCrossIsolationDeclaration()
	return r
}

var (
	_ appcrud.ITransactional                                        = (*MemoryRepository[*domaincrud.Entity[int64], int64])(nil)
	_ domaincrud.IRepository[*domaincrud.Entity[int64], int64]      = (*MemoryRepository[*domaincrud.Entity[int64], int64])(nil)
	_ domaincrud.IQueryRepository[*domaincrud.Entity[int64], int64] = (*MemoryRepository[*domaincrud.Entity[int64], int64])(nil)
	_ domaincrud.IBatchOperations[*domaincrud.Entity[int64], int64] = (*MemoryRepository[*domaincrud.Entity[int64], int64])(nil)
)
