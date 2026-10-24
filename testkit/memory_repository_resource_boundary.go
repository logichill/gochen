package testkit

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"gochen/auth/scoped"
	"gochen/codec/casecodec"
	"gochen/domain"
	"gochen/domain/crud"
)

// 本文件为 MemoryRepository 提供 L3 资源边界读取探针的内存实现，
// 与 runtime/db/orm/repo 的 SQL 实现保持相同语义：
//   - 授权输入始终严格隔离（与 Get 相同的租户可见性判定）；
//   - ResolveResourceByID / ResolveResourcesByID 排除已软删实体（与 SQL 侧
//     `deleted_at IS NULL` 过滤对齐）；
//   - ResolveResourceByIDIncludingDeleted 允许命中软删记录；
//   - 边界字段（managed_scope_id / owner_id / version）按 SQL 侧同名约定
//     从实体字段反射读取（gorm column / db / json tag 或 snake_case 字段名），
//     租户归属优先取 crud.ITenantEntity.GetTenantID()；
//   - 批量解析结果严格按入参 ID 同序，任一缺失即 NotFound。
//
// 有了这套实现，L3 安全装饰器在单测中即可走真实边界探针路径（含批量探测
// 与软删边界解析），与生产 SQL 仓储行为保持一致。

// ResolveResourceByID 实现 scoped.IResourceBoundaryReader[ID]。
func (r *MemoryRepository[T, ID]) ResolveResourceByID(ctx context.Context, id ID) (scoped.Resource, error) {
	entity, err := r.resolveBoundaryEntity(ctx, id, false)
	if err != nil {
		return scoped.Resource{}, err
	}
	return memoryResourceFromEntity[T, ID](r.ResourceKind(), entity), nil
}

// ResolveResourcesByID 实现 scoped.IBatchResourceBoundaryReader[ID]。
func (r *MemoryRepository[T, ID]) ResolveResourcesByID(ctx context.Context, ids []ID) ([]scoped.Resource, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	entities := make(map[string]T, len(ids))
	err := r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		isolationID, isolated, isoErr := r.readIsolationID(ctx)
		if isoErr != nil {
			return isoErr
		}
		for _, id := range ids {
			entity, ok := state.entities[id]
			if !ok {
				return entityNotFound(id)
			}
			if isolated && !visibleInIsolation[T, ID](entity, isolationID) {
				return entityNotFound(id)
			}
			if isMemoryEntityDeleted(entity) {
				return entityNotFound(id)
			}
			entities[formatMemoryResourceID(id)] = entity
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	resources := make([]scoped.Resource, 0, len(ids))
	for _, id := range ids {
		entity, ok := entities[formatMemoryResourceID(id)]
		if !ok {
			return nil, entityNotFound(id)
		}
		resources = append(resources, memoryResourceFromEntity[T, ID](r.ResourceKind(), entity))
	}
	return resources, nil
}

// ResolveResourceByIDIncludingDeleted 实现 scoped.IDeletedResourceBoundaryReader[ID]。
func (r *MemoryRepository[T, ID]) ResolveResourceByIDIncludingDeleted(ctx context.Context, id ID) (scoped.Resource, error) {
	entity, err := r.resolveBoundaryEntity(ctx, id, true)
	if err != nil {
		return scoped.Resource{}, err
	}
	return memoryResourceFromEntity[T, ID](r.ResourceKind(), entity), nil
}

// resolveBoundaryEntity 按主键解析边界实体（严格隔离 + 可选软删过滤）。
func (r *MemoryRepository[T, ID]) resolveBoundaryEntity(ctx context.Context, id ID, includeDeleted bool) (T, error) {
	var result T
	err := r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		entity, ok := state.entities[id]
		if !ok {
			return entityNotFound(id)
		}
		isolationID, isolated, isoErr := r.readIsolationID(ctx)
		if isoErr != nil {
			return isoErr
		}
		if isolated && !visibleInIsolation[T, ID](entity, isolationID) {
			return entityNotFound(id)
		}
		if !includeDeleted && isMemoryEntityDeleted(entity) {
			return entityNotFound(id)
		}
		result = shallowClone(entity)
		return nil
	})
	return result, err
}

// isMemoryEntityDeleted 对实现 ISoftDeletable 的实体返回软删状态；
// 未实现软删语义的实体恒为未删除。
func isMemoryEntityDeleted(entity any) bool {
	softDeletable, ok := entity.(interface {
		IsDeleted() bool
	})
	return ok && softDeletable.IsDeleted()
}

// memoryResourceFromEntity 从实体提取授权边界，字段读取约定与
// runtime/db/orm/repo/resource_boundary.go 的 resourceFromEntity 对齐。
//
// ID 必须像生产侧一样经 IEntity[ID].GetID() 直调提取：实体普遍以具体类型
// （如 int64）实现 GetID，interface{ GetID() any } 断言永远不命中，
// 静默产出空 ID 资源会让写约束匹配直接失效。
func memoryResourceFromEntity[T domain.IEntity[ID], ID comparable](kind string, entity T) scoped.Resource {
	resource := scoped.Resource{Kind: strings.TrimSpace(kind)}
	resource.ID = strings.TrimSpace(memorySprintID(entity.GetID()))
	if tenant, ok := interface{}(entity).(interface{ GetTenantID() string }); ok {
		resource.TenantID = strings.TrimSpace(tenant.GetTenantID())
	}
	if versioned, ok := interface{}(entity).(interface{ GetVersion() uint64 }); ok {
		resource.Revision = strconv.FormatUint(versioned.GetVersion(), 10)
	}
	resource.ManagedScopeID = memoryNormalizePositiveID(memoryInt64Field(entity, "managed_scope_id"))
	resource.OwnerID = strings.TrimSpace(memoryStringField(entity, "owner_id"))
	resource.TenantID = strings.TrimSpace(resource.TenantID)
	resource.Revision = strings.TrimSpace(resource.Revision)
	return resource
}

// memoryFieldValueByIndex 按列名解析字段索引并逐级取值；解析路径穿过
// nil 指针或非结构体层级时返回 false（对齐 gochen-runtime 侧 resolveField
// 的 nil 守卫），避免 reflect.Value.FieldByIndex 在穿透 nil 指针时 panic。
func memoryFieldValueByIndex(value reflect.Value, column string) (reflect.Value, bool) {
	index, ok := memoryFieldByColumn(value.Type(), column)
	if !ok {
		return reflect.Value{}, false
	}
	for _, i := range index {
		if value.Kind() == reflect.Ptr {
			if value.IsNil() {
				return reflect.Value{}, false
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		value = value.Field(i)
	}
	return value, true
}

// memoryInt64Field 按列名约定读取 int64 字段（gorm column / db / json tag 或 snake_case）。
func memoryInt64Field(entity any, column string) int64 {
	value := reflect.ValueOf(entity)
	for value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return 0
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return 0
	}
	fieldValue, ok := memoryFieldValueByIndex(value, column)
	if !ok {
		return 0
	}
	for fieldValue.Kind() == reflect.Ptr {
		if fieldValue.IsNil() {
			return 0
		}
		fieldValue = fieldValue.Elem()
	}
	switch fieldValue.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fieldValue.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if fieldValue.Uint() > uint64(^uint64(0)>>1) {
			return 0
		}
		return int64(fieldValue.Uint())
	case reflect.String:
		parsed, err := strconv.ParseInt(strings.TrimSpace(fieldValue.String()), 10, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

// memoryStringField 按列名约定读取 string 字段。
func memoryStringField(entity any, column string) string {
	value := reflect.ValueOf(entity)
	for value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return ""
	}
	fieldValue, ok := memoryFieldValueByIndex(value, column)
	if !ok {
		return ""
	}
	for fieldValue.Kind() == reflect.Ptr {
		if fieldValue.IsNil() {
			return ""
		}
		fieldValue = fieldValue.Elem()
	}
	if fieldValue.Kind() != reflect.String {
		return ""
	}
	return strings.TrimSpace(fieldValue.String())
}

// memoryFieldByColumn 在实体类型上按 SQL 侧列名约定查找字段索引。
func memoryFieldByColumn(typ reflect.Type, target string) ([]int, bool) {
	target = strings.TrimSpace(target)
	if typ == nil || target == "" {
		return nil, false
	}
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil, false
	}
	return memoryWalkFieldByColumn(typ, target, nil, map[reflect.Type]int{})
}

// memoryWalkFieldByColumn 递归查找字段；visiting 记录当前递归栈上的类型，
// 自引用/互引用实体（如树形结构的 *Parent/*Child 字段）重复进入同一类型时
// 直接跳过，避免无限递归导致栈溢出（与 gochen-runtime
// db/orm/repo/datascope_schema.go 的 findFieldByColumn 一致）。
func memoryWalkFieldByColumn(t reflect.Type, target string, prefix []int, visiting map[reflect.Type]int) ([]int, bool) {
	if visiting[t] > 0 {
		return nil, false
	}
	visiting[t]++
	defer func() { visiting[t]-- }()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		index := append(append([]int(nil), prefix...), i)
		if memoryColumnName(field) == target {
			return index, true
		}
		fieldType := field.Type
		for fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}
		if fieldType.Kind() != reflect.Struct || fieldType.PkgPath() == "time" {
			continue
		}
		if matched, ok := memoryWalkFieldByColumn(fieldType, target, index, visiting); ok {
			return matched, true
		}
	}
	return nil, false
}

// memoryColumnName 与 runtime 侧 normalizeColumnName 一致：
// gorm column tag > db tag > json tag > snake_case 字段名。
func memoryColumnName(field reflect.StructField) string {
	if tag := strings.TrimSpace(field.Tag.Get("gorm")); tag != "" {
		for _, part := range strings.Split(tag, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "column:") {
				return strings.TrimSpace(strings.TrimPrefix(part, "column:"))
			}
		}
	}
	if tag := strings.TrimSpace(field.Tag.Get("db")); tag != "" && tag != "-" {
		return tag
	}
	if tag := strings.TrimSpace(field.Tag.Get("json")); tag != "" && tag != "-" {
		return strings.TrimSpace(strings.Split(tag, ",")[0])
	}
	return memoryToSnakeCase(field.Name)
}

// memoryToSnakeCase 把 Go 字段名渲染为 snake_case 列名。
//
// 委托 gochen/codec/casecodec（与 runtime db/orm/repo/datascope_schema.go
// 的 toSnakeCase 同源同参），保证替身与生产列名口径一致——自制实现会把
// 缩写词渲染为 owner_i_d/managed_scope_i_d，与生产 owner_id 分叉，导致
// 未打 tag 的缩写字段边界资源（OwnerID/ManagedScopeID）恒空（审计 P2#25）。
// 渲染不会失败，故忽略 error 以保持单返回值形态。
func memoryToSnakeCase(name string) string {
	encoded, _ := casecodec.Convert(casecodec.Snake, name)
	return encoded
}

func memoryNormalizePositiveID(id int64) int64 {
	if id <= 0 {
		return 0
	}
	return id
}

// memorySprintID 对任意 ID 做统一字符串化（与 fmt.Sprint 语义一致，nil 安全）。
func memorySprintID(id any) string {
	if id == nil {
		return ""
	}
	return fmt.Sprint(id)
}

var _ scoped.IResourceBoundaryReader[int64] = (*MemoryRepository[crud.ITenantEntity[int64], int64])(nil)
var _ scoped.IBatchResourceBoundaryReader[int64] = (*MemoryRepository[crud.ITenantEntity[int64], int64])(nil)
var _ scoped.IDeletedResourceBoundaryReader[int64] = (*MemoryRepository[crud.ITenantEntity[int64], int64])(nil)

// 普通实体（仅嵌 crud.Entity，不带租户）同样必须能解析出带 ID 的资源边界。
var _ scoped.IResourceBoundaryReader[int64] = (*MemoryRepository[*crud.Entity[int64], int64])(nil)
var _ scoped.IBatchResourceBoundaryReader[int64] = (*MemoryRepository[*crud.Entity[int64], int64])(nil)
