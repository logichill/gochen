package writeconstraint

import (
	"context"
	"fmt"
	"strconv"

	"gochen/domain/access"
)

// IVersionedEntity 定义构建写约束目标所需的实体标识字段。
type IVersionedEntity[ID comparable] interface {
	GetID() ID
	GetVersion() uint64
}

// TargetsForEntities 将实体映射为写约束资源边界。
func TargetsForEntities[ID comparable, T IVersionedEntity[ID]](ctx context.Context, entities []T) []access.ResourceBoundary {
	targets := make([]access.ResourceBoundary, 0, len(entities))
	scope, _ := access.DataScopeFromContext(ctx)
	for _, entity := range entities {
		target := access.ResourceBoundary{ID: FormatID(entity.GetID()), Revision: FormatVersion(entity.GetVersion())}
		if target.ID == "" {
			target = applyScope(target, scope)
		}
		targets = append(targets, target)
	}
	return targets
}

// TargetsForIDs 将原始 ID 映射为写约束资源边界。
func TargetsForIDs[ID comparable](ids []ID) []access.ResourceBoundary {
	targets := make([]access.ResourceBoundary, 0, len(ids))
	for _, id := range ids {
		targets = append(targets, access.ResourceBoundary{ID: FormatID(id)})
	}
	return targets
}

func applyScope(target access.ResourceBoundary, scope access.DataScope) access.ResourceBoundary {
	if target.ManagedScopeID == 0 && len(scope.VisibleScopeIDs) == 1 {
		target.ManagedScopeID = scope.VisibleScopeIDs[0]
	}
	return target
}

// FormatID 在 ID 为零值时返回空字符串，以便作用域回退填充目标。
func FormatID[ID comparable](id ID) string {
	var zero ID
	if id == zero {
		return ""
	}
	return fmt.Sprint(id)
}

// FormatVersion 在版本号为零时返回空修订号。
func FormatVersion(version uint64) string {
	if version == 0 {
		return ""
	}
	return strconv.FormatUint(version, 10)
}
