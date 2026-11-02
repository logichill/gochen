package auth

import (
	"gochen/domain/meta"
	"gochen/errors"
)

// Copy 返回授权词汇的独立副本。
func (v Vocabulary) Copy() Vocabulary {
	return Vocabulary{Refs: append([]meta.Ref(nil), v.Refs...), Assignable: append([]meta.Dimension(nil), v.Assignable...)}
}

// Validate 校验词汇在实体上具有合法绑定。
func (v Vocabulary) Validate(metadata *meta.Metadata) error {
	for _, ref := range v.Refs {
		if _, err := metadata.Resolve(ref); err != nil {
			return err
		}
	}
	for _, dimension := range v.Assignable {
		if _, err := metadata.Resolve(meta.ByDimension(dimension)); err != nil {
			return err
		}
	}
	return nil
}

// ValidateAgainst 校验实际引用和赋值；字段别名按实体字段身份比较。
func (d Decision) ValidateAgainst(v Vocabulary, metadata *meta.Metadata) error {
	if !d.Allowed {
		return nil
	}
	if err := d.Spec.Err(); err != nil {
		return errors.Wrap(err, errors.Internal, "authorizer returned an invalid spec")
	}
	if d.Spec.HasNamed() {
		return errors.NewCode(errors.Internal, "authorization spec must use typed field references")
	}
	fields := make(map[string]struct{}, len(v.Refs))
	for _, ref := range v.Refs {
		field, err := metadata.Resolve(ref)
		if err != nil {
			return errors.Wrap(err, errors.Internal, "invalid authorization vocabulary")
		}
		fields[field.Name] = struct{}{}
	}
	for _, ref := range d.Spec.Refs() {
		field, err := metadata.Resolve(ref)
		if err != nil {
			return errors.Wrap(err, errors.Internal, "decision reference has no binding")
		}
		if _, ok := fields[field.Name]; !ok {
			return errors.NewCode(errors.Internal, "decision reference is outside vocabulary").WithContext("reference", ref.String())
		}
	}
	assignable := make(map[meta.Dimension]struct{}, len(v.Assignable))
	for _, dim := range v.Assignable {
		assignable[dim] = struct{}{}
	}
	seen := make(map[meta.Dimension]struct{}, len(d.Assign))
	for _, assignment := range d.Assign {
		if err := assignment.Validate(); err != nil {
			return errors.Wrap(err, errors.Internal, "invalid decision assignment")
		}
		if _, ok := assignable[assignment.Dimension]; !ok {
			return errors.NewCode(errors.Internal, "decision assignment is outside vocabulary").WithContext("dimension", string(assignment.Dimension))
		}
		if _, duplicate := seen[assignment.Dimension]; duplicate {
			return errors.NewCode(errors.Internal, "duplicate decision assignment")
		}
		seen[assignment.Dimension] = struct{}{}
	}
	return nil
}
