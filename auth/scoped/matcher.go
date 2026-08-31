package scoped

import (
	"strings"

	"gochen/errors"
)

// 约束匹配 helper。

// RequireResource 要求约束中存在且仅存在一个匹配资源。
//
// 匹配不到即 Forbidden、匹配到多个即 InvalidInput——两者都不放行。
func (c WriteConstraint) RequireResource(kind, resourceID string) (ResourceConstraint, error) {
	constraint := c.Normalize()
	if len(constraint.Resources) == 0 {
		return ResourceConstraint{}, errors.NewCode(errors.Forbidden,
			"write constraint does not authorize any resource")
	}
	kind = strings.TrimSpace(kind)
	resourceID = strings.TrimSpace(resourceID)

	var matched ResourceConstraint
	count := 0
	for _, resource := range constraint.Resources {
		if resource.Kind != kind {
			continue
		}
		switch {
		case resourceID != "" && resource.ResourceID != "" && resource.ResourceID != resourceID:
			continue
		case resourceID == "" && resource.ResourceID != "":
			continue
		}
		matched = resource
		count++
	}
	switch {
	case count == 1:
		return matched, nil
	case count == 0:
		return ResourceConstraint{}, errors.NewCode(errors.Forbidden,
			"write constraint does not authorize the target resource").
			WithContext("resource_kind", kind).
			WithContext("resource_id", resourceID)
	default:
		return ResourceConstraint{}, errors.NewCode(errors.InvalidInput,
			"write constraint matches multiple target resources").
			WithContext("resource_kind", kind).
			WithContext("resource_id", resourceID)
	}
}

// SplitByTargets 按目标资源把批量约束安全拆分为多个单资源约束。
//
// 数量不符即报错，避免"部分授权"被当成全量授权。
func (c WriteConstraint) SplitByTargets(targets []Resource) ([]WriteConstraint, error) {
	constraint := c.Normalize()
	if len(targets) == 0 {
		return nil, nil
	}
	if len(constraint.Resources) != len(targets) {
		return nil, errors.NewCode(errors.InvalidInput, "write constraint resource count mismatch").
			WithContext("expected", len(targets)).
			WithContext("actual", len(constraint.Resources))
	}

	matcher := newConstraintResourceMatcher(constraint.Resources)
	result := make([]WriteConstraint, 0, len(targets))
	for _, target := range targets {
		index, err := matcher.take(target.Normalize())
		if err != nil {
			return nil, err
		}
		result = append(result, WriteConstraint{
			Resources: []ResourceConstraint{constraint.Resources[index]},
		})
	}
	return result, nil
}

// constraintResourceMatcher 以索引表承载"每个目标取一条未被占用的约束"。
//
// 逐个目标线性扫描全部约束是 O(N²)：批量上限默认 1000，一次批量就是
// 五十万次字符串比较，且随批量大小平方增长。这里预先建表把它降到 O(N)。
type constraintResourceMatcher struct {
	// exact 存携带具体资源 ID 的约束，按 (kind, resourceID) 索引。
	exact map[constraintResourceKey][]int
	// byKind 存未携带资源 ID 的约束，按 kind 索引，供兜底匹配。
	byKind map[string][]int
}

type constraintResourceKey struct {
	kind       string
	resourceID string
}

// newConstraintResourceMatcher 按两段匹配语义把约束分入两张互斥的索引表。
//
// 两表互斥（ResourceID 非空 / 为空）是等价性的前提：一条约束只可能被其中
// 一张表取走，因此各表内按下标升序出队，等同于原先"取最小的未占用下标"。
func newConstraintResourceMatcher(resources []ResourceConstraint) *constraintResourceMatcher {
	matcher := &constraintResourceMatcher{
		exact:  make(map[constraintResourceKey][]int, len(resources)),
		byKind: make(map[string][]int),
	}
	for i, resource := range resources {
		if resource.ResourceID == "" {
			matcher.byKind[resource.Kind] = append(matcher.byKind[resource.Kind], i)
			continue
		}
		key := constraintResourceKey{kind: resource.Kind, resourceID: resource.ResourceID}
		matcher.exact[key] = append(matcher.exact[key], i)
	}
	return matcher
}

// take 取出匹配 target 且尚未被占用的约束下标，并把它标记为已占用。
//
// 目标 ID 为空时 exact 表必然取不到（表内 resourceID 恒非空），自然落到兜底表。
func (m *constraintResourceMatcher) take(target Resource) (int, error) {
	// 优先按 (kind, resourceID) 精确匹配。
	key := constraintResourceKey{kind: target.Kind, resourceID: target.ID}
	if index, ok := popConstraintIndex(m.exact, key); ok {
		return index, nil
	}
	// 退化为按 kind 顺序匹配（约束未携带具体 ID 的场景）。
	if index, ok := popConstraintIndex(m.byKind, target.Kind); ok {
		return index, nil
	}
	return 0, errors.NewCode(errors.Forbidden, "write constraint does not authorize the target resource").
		WithContext("resource_kind", target.Kind).
		WithContext("resource_id", target.ID)
}

func popConstraintIndex[K comparable](indexes map[K][]int, key K) (int, bool) {
	queue := indexes[key]
	if len(queue) == 0 {
		return 0, false
	}
	if len(queue) == 1 {
		delete(indexes, key)
	} else {
		indexes[key] = queue[1:]
	}
	return queue[0], true
}
