package testkit

import (
	"gochen/domain"
	"gochen/domain/spec"
	"gochen/errors"
)

// MemoryDataSourceID 是 testkit 内存仓储与内存审计存储共用的数据源标识（D17）。
const MemoryDataSourceID = "memory"

// WithMemoryDataSourceID 覆盖内存仓储的数据源标识；与审计存储不一致时构造失败。
func WithMemoryDataSourceID[T domain.IEntity[ID], ID comparable](id string) MemoryOption[T, ID] {
	return func(r *MemoryRepository[T, ID]) {
		if r == nil {
			return
		}
		r.dataSourceID = id
	}
}

// WithMemoryNamed 注册内存解释器支持的命名条件（仅读路径与契约测试使用）。
func WithMemoryNamed[T domain.IEntity[ID], ID comparable](
	name string,
	matcher spec.NamedMatcher,
) MemoryOption[T, ID] {
	return func(r *MemoryRepository[T, ID]) {
		if r == nil {
			return
		}
		if r.namedMatchers == nil {
			r.namedMatchers = make(map[string]spec.NamedMatcher)
		}
		r.namedMatchers[name] = matcher
		r.interpreter = nil
	}
}

// DataSourceID 返回内存仓储所在的数据源标识。
func (r *MemoryRepository[T, ID]) DataSourceID() string {
	if r == nil {
		return ""
	}
	return r.dataSourceID
}

// HasNamedCondition 报告内存仓储是否注册了指定命名条件。
func (r *MemoryRepository[T, ID]) HasNamedCondition(name string) bool {
	if r == nil {
		return false
	}
	_, ok := r.namedMatchers[name]
	return ok
}

// matchSpec 判断实体是否满足规约；规约或求值错误一律上抛。
func (r *MemoryRepository[T, ID]) matchSpec(s spec.Spec, entity T) (bool, error) {
	if r.interpreter == nil {
		opts := make([]spec.InterpreterOption, 0, len(r.namedMatchers))
		for name, matcher := range r.namedMatchers {
			opts = append(opts, spec.WithNamed(name, matcher))
		}
		r.interpreter = spec.NewInterpreter(opts...)
	}
	return r.interpreter.Match(s, entity)
}

// requireWriteSpec 校验写路径规约：必须是纯谓词，新建不得引用主键角色。
func requireWriteSpec(s spec.Spec, creating bool) error {
	if err := s.Err(); err != nil {
		return errors.Wrap(err, errors.Internal, "invalid spec")
	}
	if s.HasNamed() {
		return errors.NewCode(errors.Internal, "write spec must not contain named conditions")
	}
	if creating && s.ReferencesRole(spec.RoleID) {
		return errors.NewCode(errors.Internal, "create spec must not reference the id role")
	}
	return nil
}

// visibleEntity 报告实体是否未软删且满足规约。
func (r *MemoryRepository[T, ID]) visibleEntity(s spec.Spec, entity T) (bool, error) {
	if softDeleted(entity) {
		return false, nil
	}
	return r.matchSpec(s, entity)
}

func softDeleted(entity any) bool {
	deletable, ok := entity.(domain.ISoftDeletable)
	return ok && deletable.IsDeleted()
}
