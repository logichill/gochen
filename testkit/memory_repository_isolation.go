package testkit

import (
	"context"
	"strings"

	"gochen/contextx"
	"gochen/domain"
	"gochen/domain/meta"
	"gochen/errors"
	"reflect"
)

// L2 空间隔离的内存实现。
//
// 与 runtime/db/orm/repo 的 SQL 实现保持**相同语义**：
//   - 隔离由构造期显式声明启用，未声明即不启用（无隐式推断）；
//   - 隔离键来源可换（默认 contextx.TenantID），解析不到即 fail-closed；
//   - Create 标记归属；读/写路径按归属过滤；
//   - 归属不匹配时按 NotFound 处理（防探测），而不是泄露"存在但无权"；
//   - 隔离字段无法绑定或无法转换隔离键时，构造返回错误。
//
// 内存仓储通过 Ref 绑定实体字段，SQL 仓储通过 Column 绑定同一字段。
//
// 两侧按同一组语义断言编写特征测试（见 memory_repository_isolation_test.go 与
// runtime 的 isolation_test.go），保证跨存储语义一致。

// MemoryOption 配置内存仓储的可选能力。
type MemoryOption[T domain.IEntity[ID], ID comparable] func(*MemoryRepository[T, ID])

// IsolationResolveFunc 解析本次请求所属的隔离空间键。
//
// 与 ormrepo.IsolationCols.Resolve 同构：返回空字符串等同"解析不到"（fail-closed），
// 返回 error 则直接上抛。
type IsolationResolveFunc func(ctx context.Context) (string, error)

// WithMemoryIsolation 显式启用 L2 空间隔离，隔离键取自 contextx.TenantID。
//
// 构造期要求实体绑定 tenant 维度，失败时 NewMemoryRepository 返回错误。
func WithMemoryIsolation[T domain.IEntity[ID], ID comparable]() MemoryOption[T, ID] {
	return withMemoryIsolation[T, ID](nil)
}

// WithMemoryIsolationResolver 启用 L2 空间隔离并自定义隔离键来源。
//
// 对应 SQL 侧的 ormrepo.WithIsolation(IsolationCols{Column: ..., Resolve: ...})：
// 默认绑定 tenant 维度，可通过 WithMemoryIsolationField 指定其他字段或维度。
func WithMemoryIsolationResolver[T domain.IEntity[ID], ID comparable](resolve IsolationResolveFunc) MemoryOption[T, ID] {
	return withMemoryIsolation[T, ID](resolve)
}

func withMemoryIsolation[T domain.IEntity[ID], ID comparable](resolve IsolationResolveFunc) MemoryOption[T, ID] {
	return func(r *MemoryRepository[T, ID]) {
		if r == nil {
			return
		}
		r.isolated = true
		r.isolationRef = meta.ByDimension("tenant")
		r.isolationResolve = resolve
	}
}

// WithMemoryIsolationField 显式绑定任意字段或维度作为隔离槽位。
func WithMemoryIsolationField[T domain.IEntity[ID], ID comparable](ref meta.Ref, resolve IsolationResolveFunc) MemoryOption[T, ID] {
	return func(r *MemoryRepository[T, ID]) {
		r.isolated = true
		r.isolationRef = ref
		r.isolationResolve = resolve
	}
}

// isolationID 解析当前请求的隔离标识；启用隔离但解析不到时 fail-closed。
//
// **写路径专用**：不认跨隔离开闸凭据。读路径请用 readIsolationID。
func (r *MemoryRepository[T, ID]) isolationID(ctx context.Context) (string, bool, error) {
	if r == nil || !r.isolated {
		return "", false, nil
	}
	key, err := r.resolveIsolationKey(ctx)
	if err != nil {
		// 解析器自身故障绝不能被当成"无隔离"而放行。
		return "", false, err
	}
	if key == "" {
		return "", false, errors.NewCode(errors.Forbidden,
			"isolated repository requires an isolation key for this request")
	}
	return key, true, nil
}

// readIsolationID 是读路径上的隔离解析：两道闸都合上时放行跨隔离读。
//
// 与 ormrepo 的 readQuery 同一口径——写路径与未显式改用本方法的路径
// 一律走严格档，忘记表态不会意外敞开。
func (r *MemoryRepository[T, ID]) readIsolationID(ctx context.Context) (string, bool, error) {
	if r == nil || !r.isolated {
		return "", false, nil
	}
	relaxed, err := r.crossIsolationRelaxed(ctx)
	if err != nil {
		return "", false, err
	}
	if relaxed {
		return "", false, nil
	}
	return r.isolationID(ctx)
}

func (r *MemoryRepository[T, ID]) resolveIsolationKey(ctx context.Context) (string, error) {
	if r.isolationResolve != nil {
		key, err := r.isolationResolve(ctx)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(key), nil
	}
	return strings.TrimSpace(contextx.TenantID(ctx)), nil
}

// visible 判断实体是否属于指定隔离空间。
func (r *MemoryRepository[T, ID]) visibleInIsolation(entity T, isolationID string) bool {
	field, err := r.metadata.Resolve(r.isolationRef)
	if err != nil {
		return false
	}
	current, ok := field.Value(entity)
	if !ok {
		return false
	}
	expected, err := field.Convert(isolationID)
	return err == nil && reflect.DeepEqual(current.Interface(), expected.Interface())
}

// stampIsolation 在写入前标记归属。
func (r *MemoryRepository[T, ID]) stampIsolation(entity T, isolationID string) error {
	field, err := r.metadata.Resolve(r.isolationRef)
	if err != nil {
		return err
	}
	converted, err := field.Convert(isolationID)
	if err != nil {
		return err
	}
	target, ok := field.Value(entity)
	if !ok || !target.CanSet() {
		return errors.NewCode(errors.InvalidInput, "isolation field is not settable")
	}
	target.Set(converted)
	return nil
}
