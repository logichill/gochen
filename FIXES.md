# 代码审核问题修复指南

本文档提供针对审核报告中发现的关键问题的具体修复代码。

---

## 修复 1: 包命名不一致 🔴 高优先级

### 问题文件: `validation/validator.go`

**当前代码**:
```go
package validator  // ❌ 错误
```

**修复代码**:
```go
package validation  // ✅ 正确
```

### 影响范围
需要更新所有导入此包的文件：

```bash
# 查找所有使用 validator 包的文件
grep -r "validator\." --include="*.go" .

# 全局替换（建议使用 IDE 的重构功能）
find . -name "*.go" -type f -exec sed -i 's/validator\./validation\./g' {} \;
```

---

## 修复 2: Aggregate 并发安全 🔴 高优先级

### 问题文件: `domain/entity/aggregate.go`

**当前代码**:
```go
type Aggregate[T comparable] struct {
    EntityFields
    domainEvents []eventing.IEvent  // ❌ 无并发保护
}

func (a *Aggregate[T]) AddDomainEvent(evt eventing.IEvent) {
    if a.domainEvents == nil {
        a.domainEvents = make([]eventing.IEvent, 0)
    }
    a.domainEvents = append(a.domainEvents, evt)  // ❌ 竞态条件
}

func (a *Aggregate[T]) GetDomainEvents() []eventing.IEvent {
    return a.domainEvents  // ❌ 直接返回切片
}
```

**修复代码**:
```go
package entity

import (
	"sync"
	
	"gochen/eventing"
)

// Aggregate 基础聚合根（支持领域事件）
// 适用于传统 CRUD + 领域事件模式
//
// 使用场景:
//   - 不需要事件溯源，只需要发布领域事件
//   - 状态通过传统 CRUD 持久化
//   - 事件仅用于通知其他聚合或服务
//
// 示例:
//
//	type User struct {
//	    Aggregate[int64]
//	    Name  string
//	    Email string
//	}
type Aggregate[T comparable] struct {
	EntityFields
	domainEvents []eventing.IEvent
	mu           sync.RWMutex  // ✅ 添加锁保护
}

// GetAggregateType 返回聚合根类型
func (a *Aggregate[T]) GetAggregateType() string {
	return "Aggregate"
}

// GetDomainEvents 获取领域事件（返回副本）
func (a *Aggregate[T]) GetDomainEvents() []eventing.IEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	// ✅ 返回副本以保证并发安全
	if a.domainEvents == nil {
		return nil
	}
	events := make([]eventing.IEvent, len(a.domainEvents))
	copy(events, a.domainEvents)
	return events
}

// ClearDomainEvents 清空领域事件
func (a *Aggregate[T]) ClearDomainEvents() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.domainEvents = nil
}

// AddDomainEvent 添加领域事件（并发安全）
func (a *Aggregate[T]) AddDomainEvent(evt eventing.IEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	if a.domainEvents == nil {
		a.domainEvents = make([]eventing.IEvent, 0, 4)  // ✅ 预分配容量
	}
	a.domainEvents = append(a.domainEvents, evt)
}

// Validate 验证聚合根状态（默认实现）
func (a *Aggregate[T]) Validate() error {
	if a.IsDeleted() {
		return ErrAggregateDeleted
	}
	return nil
}
```

### 测试代码

创建 `domain/entity/aggregate_concurrent_test.go`:

```go
//go:build !race

package entity

import (
	"sync"
	"testing"
	
	"github.com/stretchr/testify/assert"
	
	"gochen/eventing"
	"gochen/messaging"
)

func TestAggregate_ConcurrentAddDomainEvent(t *testing.T) {
	agg := &Aggregate[int64]{}
	
	const goroutines = 10
	const eventsPerGoroutine = 100
	
	var wg sync.WaitGroup
	wg.Add(goroutines)
	
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				evt := &eventing.Event{
					Message: messaging.Message{
						ID:   fmt.Sprintf("evt-%d-%d", id, j),
						Type: "TestEvent",
					},
				}
				agg.AddDomainEvent(evt)
			}
		}(i)
	}
	
	wg.Wait()
	
	events := agg.GetDomainEvents()
	assert.Equal(t, goroutines*eventsPerGoroutine, len(events))
}

func TestAggregate_ConcurrentGetAndAdd(t *testing.T) {
	agg := &Aggregate[int64]{}
	
	// 初始添加一些事件
	for i := 0; i < 10; i++ {
		evt := &eventing.Event{
			Message: messaging.Message{
				ID:   fmt.Sprintf("evt-%d", i),
				Type: "TestEvent",
			},
		}
		agg.AddDomainEvent(evt)
	}
	
	const goroutines = 5
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)
	
	// 并发读取
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				events := agg.GetDomainEvents()
				assert.GreaterOrEqual(t, len(events), 10)
			}
		}()
	}
	
	// 并发写入
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				evt := &eventing.Event{
					Message: messaging.Message{
						ID:   fmt.Sprintf("concurrent-evt-%d-%d", id, j),
						Type: "TestEvent",
					},
				}
				agg.AddDomainEvent(evt)
			}
		}(i)
	}
	
	wg.Wait()
}
```

运行竞态检测：
```bash
go test -race ./domain/entity/...
```

---

## 修复 3: 全局 Logger 并发安全 ⚠️ 中优先级

### 问题文件: `logging/logger.go`

**当前代码**:
```go
// 全局Logger
var globalLogger Logger = NewStdLogger("")

func SetLogger(logger Logger) {  // ❌ 无并发保护
    globalLogger = logger
}

func GetLogger() Logger {
    return globalLogger
}
```

**修复代码**:
```go
package logging

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"
)

// ... 其他代码保持不变 ...

// 全局 Logger（使用 atomic.Value 保证并发安全）
var globalLogger atomic.Value

func init() {
	globalLogger.Store(NewStdLogger(""))
}

// SetLogger 设置全局 Logger（并发安全）
func SetLogger(logger Logger) {
	if logger == nil {
		panic("logger cannot be nil")
	}
	globalLogger.Store(logger)
}

// GetLogger 获取全局 Logger（并发安全）
func GetLogger() Logger {
	return globalLogger.Load().(Logger)
}
```

### 测试代码

```go
// logging/logger_concurrent_test.go

package logging

import (
	"context"
	"sync"
	"testing"
)

func TestGlobalLogger_ConcurrentAccess(t *testing.T) {
	const goroutines = 10
	const operations = 1000
	
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)
	
	// 并发读取
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				logger := GetLogger()
				if logger == nil {
					t.Error("GetLogger returned nil")
				}
			}
		}()
	}
	
	// 并发写入
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				SetLogger(NewStdLogger(fmt.Sprintf("logger-%d", id)))
			}
		}(i)
	}
	
	wg.Wait()
}

func TestGlobalLogger_SetNil_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("SetLogger(nil) should panic")
		}
	}()
	
	SetLogger(nil)
}
```

---

## 修复 4: validation 包编译错误 🔴 高优先级

### 问题文件: `validation/validator.go`

**当前代码**:
```go
func NewValidationError(message string) error {
	return errors.NewValidationError(message)  // ❌ 函数不存在
}
```

**修复代码**:
```go
// NewValidationError 创建验证错误
func NewValidationError(message string) error {
	return errors.NewError(errors.ErrCodeValidation, message)  // ✅ 使用正确的函数
}
```

---

## 修复 5: 错误消息国际化 ⚠️ 中优先级

### 问题文件: `eventing/event.go`

**当前代码**:
```go
func (e *Event) Validate() error {
	if e.GetID() == "" {
		return fmt.Errorf("事件ID不能为空")
	}
	if e.AggregateID <= 0 {
		return fmt.Errorf("聚合ID必须大于0")
	}
	// ...
}
```

**修复代码**:
```go
func (e *Event) Validate() error {
	if e.GetID() == "" {
		return fmt.Errorf("event validation failed: event ID cannot be empty (aggregate=%s:%d, type=%s)", 
			e.AggregateType, e.AggregateID, e.GetType())
	}
	if e.AggregateID <= 0 {
		return fmt.Errorf("event validation failed: invalid aggregate ID %d (aggregate=%s, type=%s)", 
			e.AggregateID, e.AggregateType, e.GetType())
	}
	if e.AggregateType == "" {
		return fmt.Errorf("event validation failed: aggregate type cannot be empty (id=%s, aggregateID=%d)", 
			e.GetID(), e.AggregateID)
	}
	if e.GetType() == "" {
		return fmt.Errorf("event validation failed: event type cannot be empty (id=%s, aggregate=%s:%d)", 
			e.GetID(), e.AggregateType, e.AggregateID)
	}
	if e.Version <= 0 {
		return fmt.Errorf("event validation failed: invalid version %d (id=%s, aggregate=%s:%d, type=%s)", 
			e.Version, e.GetID(), e.AggregateType, e.AggregateID, e.GetType())
	}
	if e.SchemaVersion <= 0 {
		return fmt.Errorf("event validation failed: invalid schema version %d (id=%s, aggregate=%s:%d, type=%s)", 
			e.SchemaVersion, e.GetID(), e.AggregateType, e.AggregateID, e.GetType())
	}
	return nil
}
```

---

## 修复 6: 领域层 GORM 标签移除 🔴 高优先级

### 方案 A: 使用纯领域模型 + DTO 映射（推荐）

#### 1. 修改领域模型

`domain/entity/entity.go`:
```go
// EntityFields 通用实体字段（用于嵌入）
// 纯领域模型，不包含任何基础设施标签
type EntityFields struct {
	ID        int64
	Version   int64
	CreatedAt time.Time
	CreatedBy string
	UpdatedAt time.Time
	UpdatedBy string
	DeletedAt *time.Time
	DeletedBy *string
}

// GetID 实现 IObject 接口
func (e *EntityFields) GetID() int64 {
	return e.ID
}

// ... 其他方法保持不变 ...
```

#### 2. 创建基础设施层 DTO

`infrastructure/persistence/entity_dto.go`:
```go
package persistence

import (
	"time"
	
	"gochen/domain/entity"
)

// EntityFieldsDTO 实体字段数据传输对象
// 包含 ORM 特定标签
type EntityFieldsDTO struct {
	ID        int64      `json:"id" gorm:"primaryKey"`
	Version   int64      `json:"version" gorm:"default:1"`
	CreatedAt time.Time  `json:"created_at" gorm:"autoCreateTime"`
	CreatedBy string     `json:"created_by"`
	UpdatedAt time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
	UpdatedBy string     `json:"updated_by"`
	DeletedAt *time.Time `json:"deleted_at,omitempty" gorm:"index"`
	DeletedBy *string    `json:"deleted_by,omitempty"`
}

// ToEntity 将 DTO 转换为领域实体
func (dto *EntityFieldsDTO) ToEntity() *entity.EntityFields {
	return &entity.EntityFields{
		ID:        dto.ID,
		Version:   dto.Version,
		CreatedAt: dto.CreatedAt,
		CreatedBy: dto.CreatedBy,
		UpdatedAt: dto.UpdatedAt,
		UpdatedBy: dto.UpdatedBy,
		DeletedAt: dto.DeletedAt,
		DeletedBy: dto.DeletedBy,
	}
}

// FromEntity 从领域实体创建 DTO
func FromEntity(e *entity.EntityFields) *EntityFieldsDTO {
	return &EntityFieldsDTO{
		ID:        e.ID,
		Version:   e.Version,
		CreatedAt: e.CreatedAt,
		CreatedBy: e.CreatedBy,
		UpdatedAt: e.UpdatedAt,
		UpdatedBy: e.UpdatedBy,
		DeletedAt: e.DeletedAt,
		DeletedBy: e.DeletedBy,
	}
}

// EntityMapper 实体映射器
type EntityMapper struct{}

func NewEntityMapper() *EntityMapper {
	return &EntityMapper{}
}

// ToDTO 将领域实体映射为 DTO
func (m *EntityMapper) ToDTO(e any) (any, error) {
	// 使用反射或类型断言实现通用映射
	// 这里提供基础实现
	switch v := e.(type) {
	case *entity.EntityFields:
		return FromEntity(v), nil
	default:
		return nil, fmt.Errorf("unsupported entity type: %T", e)
	}
}

// FromDTO 将 DTO 映射为领域实体
func (m *EntityMapper) FromDTO(dto any) (any, error) {
	switch v := dto.(type) {
	case *EntityFieldsDTO:
		return v.ToEntity(), nil
	default:
		return nil, fmt.Errorf("unsupported DTO type: %T", dto)
	}
}
```

#### 3. 使用示例

`infrastructure/persistence/user_repository.go`:
```go
package persistence

import (
	"context"
	
	"gorm.io/gorm"
	
	"gochen/domain/entity"
	"gochen/errors"
)

// UserDTO 用户数据传输对象
type UserDTO struct {
	EntityFieldsDTO        // 嵌入 DTO 基类
	Name            string `gorm:"size:100;not null"`
	Email           string `gorm:"size:255;uniqueIndex;not null"`
}

// User 领域模型
type User struct {
	entity.EntityFields  // 嵌入纯领域基类
	Name  string
	Email string
}

// UserRepository 用户仓储实现
type UserRepository struct {
	db     *gorm.DB
	mapper *EntityMapper
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{
		db:     db,
		mapper: NewEntityMapper(),
	}
}

func (r *UserRepository) GetByID(ctx context.Context, id int64) (*User, error) {
	var dto UserDTO
	err := r.db.WithContext(ctx).First(&dto, id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.ErrNotFound
		}
		return nil, errors.WrapDatabaseError(ctx, err, "get user by id")
	}
	
	// DTO -> 领域模型
	user := &User{
		EntityFields: *dto.EntityFieldsDTO.ToEntity(),
		Name:         dto.Name,
		Email:        dto.Email,
	}
	
	return user, nil
}

func (r *UserRepository) Create(ctx context.Context, user *User) error {
	// 领域模型 -> DTO
	dto := &UserDTO{
		EntityFieldsDTO: *FromEntity(&user.EntityFields),
		Name:            user.Name,
		Email:           user.Email,
	}
	
	err := r.db.WithContext(ctx).Create(dto).Error
	if err != nil {
		return errors.WrapDatabaseError(ctx, err, "create user")
	}
	
	// 更新生成的 ID
	user.ID = dto.ID
	
	return nil
}
```

### 方案 B: 使用构建标签（折中方案）

如果不想创建 DTO 层，可以使用构建标签：

`domain/entity/entity.go`:
```go
//go:build !nogorm

package entity

import "time"

// EntityFields 通用实体字段（带 GORM 标签）
type EntityFields struct {
	ID        int64      `json:"id" gorm:"primaryKey"`
	Version   int64      `json:"version" gorm:"default:1"`
	CreatedAt time.Time  `json:"created_at" gorm:"autoCreateTime"`
	CreatedBy string     `json:"created_by"`
	UpdatedAt time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
	UpdatedBy string     `json:"updated_by"`
	DeletedAt *time.Time `json:"deleted_at,omitempty" gorm:"index"`
	DeletedBy *string    `json:"deleted_by,omitempty"`
}
```

`domain/entity/entity_nogorm.go`:
```go
//go:build nogorm

package entity

import "time"

// EntityFields 通用实体字段（无 GORM 标签）
type EntityFields struct {
	ID        int64
	Version   int64
	CreatedAt time.Time
	CreatedBy string
	UpdatedAt time.Time
	UpdatedBy string
	DeletedAt *time.Time
	DeletedBy *string
}
```

构建时可以选择：
```bash
# 使用 GORM 标签
go build ./...

# 不使用 GORM 标签（纯领域模型）
go build -tags nogorm ./...
```

**建议**: 使用方案 A（DTO 映射），因为它更符合 DDD 原则和清洁架构。

---

## 修复 7: DI 容器锁优化 ⚠️ 中优先级

### 问题文件: `di/container.go`

**修复代码**:
```go
func (c *BasicContainer) Resolve(name string) (any, error) {
	// 快速路径：检查是否已创建（只需读锁）
	c.mutex.RLock()
	inst, existsInst := c.instances[name]
	factory, existsSvc := c.services[name]
	c.mutex.RUnlock()
	
	// 如果已经创建，直接返回
	if existsInst {
		return inst, nil
	}
	
	// 如果服务未注册，返回错误
	if !existsSvc {
		return nil, errors.NewError(errors.ErrCodeNotFound, 
			fmt.Sprintf("服务 %s 未注册", name))
	}
	
	// 慢速路径：创建实例（不持锁，允许并发创建）
	newInst, err := c.createInstance(factory)
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeInternal, 
			fmt.Sprintf("创建服务 %s 失败", name))
	}
	
	// Double-check locking：检查是否有其他 goroutine 已经创建
	c.mutex.Lock()
	if existing, ok := c.instances[name]; ok {
		c.mutex.Unlock()
		return existing, nil  // 使用已存在的实例
	}
	c.instances[name] = newInst
	c.mutex.Unlock()
	
	return newInst, nil
}
```

---

## 修复 8: Snowflake 时钟回拨处理 ⚠️ 中优先级

### 问题文件: `idgen/snowflake/snowflake.go`

**修复代码**:
```go
const (
	// 时钟回拨容忍度（毫秒）
	maxClockBackwardTolerance = 5
)

// NextID 生成下一个ID
func (g *Generator) NextID() (int64, error) {
	g.mux.Lock()
	defer g.mux.Unlock()

	now := time.Now().UnixNano() / 1e6

	if now < g.lastTimestamp {
		// 计算时钟回拨偏移量
		offset := g.lastTimestamp - now
		
		// 如果回拨在容忍范围内，等待时钟追上
		if offset <= maxClockBackwardTolerance {
			time.Sleep(time.Duration(offset+1) * time.Millisecond)
			now = time.Now().UnixNano() / 1e6
			
			// 再次检查
			if now < g.lastTimestamp {
				return 0, fmt.Errorf("clock moved backwards by %dms after waiting, refusing to generate id", 
					g.lastTimestamp-now)
			}
		} else {
			// 回拨过大，直接拒绝
			return 0, fmt.Errorf("clock moved backwards by %dms (tolerance: %dms), refusing to generate id", 
				offset, maxClockBackwardTolerance)
		}
	}

	if now == g.lastTimestamp {
		g.sequence = (g.sequence + 1) & maxSequence
		if g.sequence == 0 {
			// 序列号用完，等待下一毫秒
			for now <= g.lastTimestamp {
				now = time.Now().UnixNano() / 1e6
			}
		}
	} else {
		g.sequence = 0
	}

	g.lastTimestamp = now

	id := ((now - epoch) << timestampLeftShift) |
		(g.datacenterID << datacenterIDShift) |
		(g.workerID << workerIDShift) |
		g.sequence

	return id, nil
}
```

---

## 一键应用所有修复

创建脚本 `scripts/apply_fixes.sh`:

```bash
#!/bin/bash

set -e

echo "🔧 应用代码审核修复..."

# 1. 修复包命名
echo "📦 修复包命名..."
find ./validation -name "*.go" -type f -exec sed -i 's/^package validator$/package validation/g' {} \;
find . -name "*.go" -type f -exec sed -i 's/validator\./validation\./g' {} \;

# 2. 运行格式化
echo "🎨 格式化代码..."
go fmt ./...

# 3. 运行 go mod tidy
echo "📦 整理依赖..."
go mod tidy

# 4. 运行测试
echo "🧪 运行测试..."
go test ./... -race -timeout 30s

# 5. 运行静态检查（如果安装了 staticcheck）
if command -v staticcheck &> /dev/null; then
    echo "🔍 运行静态检查..."
    staticcheck ./...
fi

echo "✅ 修复应用完成！"
```

使用：
```bash
chmod +x scripts/apply_fixes.sh
./scripts/apply_fixes.sh
```

---

## 验证修复

### 1. 验证并发安全

```bash
# 运行竞态检测
go test -race ./domain/entity/...
go test -race ./logging/...
```

### 2. 验证编译

```bash
# 确保所有包都能编译
go build ./...
```

### 3. 验证测试

```bash
# 运行所有测试
go test ./... -v
```

### 4. 验证静态检查

```bash
# 安装 golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# 运行 linter
golangci-lint run
```

---

## 持续改进

### 添加 pre-commit hook

`.git/hooks/pre-commit`:
```bash
#!/bin/bash

echo "🔍 运行 pre-commit 检查..."

# 格式化检查
if ! go fmt ./...; then
    echo "❌ 代码格式化失败"
    exit 1
fi

# 运行测试
if ! go test -short ./...; then
    echo "❌ 测试失败"
    exit 1
fi

# 竞态检测（关键包）
if ! go test -race -short ./domain/entity/... ./logging/...; then
    echo "❌ 竞态检测失败"
    exit 1
fi

echo "✅ pre-commit 检查通过"
```

```bash
chmod +x .git/hooks/pre-commit
```

---

## 总结

以上修复涵盖了代码审核报告中最关键的问题。建议按以下顺序应用修复：

1. ✅ 修复编译错误（validation 包）
2. ✅ 修复并发安全问题（Aggregate, Logger）
3. ✅ 修复包命名不一致
4. ✅ 改进错误消息（国际化）
5. ✅ 移除领域层基础设施代码（GORM 标签）

应用这些修复后，代码库的质量和健壮性将得到显著提升。
