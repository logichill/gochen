package scoped

import (
	"gochen/domain/meta"
	"gochen/domain/spec"
)

// ScopeIn 返回主体范围集合内的正谓词。
func ScopeIn(dimension meta.Dimension, scopeIDs ...int64) spec.Spec {
	return spec.In(meta.ByDimension(dimension), int64sToAny(scopeIDs)...)
}

// OwnerIs 返回归属主体为 owner 的正谓词。
func OwnerIs(dimension meta.Dimension, owner string) spec.Spec {
	return spec.Eq(meta.ByDimension(dimension), owner)
}

func int64sToAny(values []int64) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
