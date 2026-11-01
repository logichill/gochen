package auth

import (
	"gochen/domain/spec"
	"gochen/errors"
)

// Validate 校验词汇只声明已知的角色、字段与命名条件。
//
// available 由 Application 构造时汇总：角色取框架已知集合，
// 字段取实体字段元数据，命名条件取自仓储注册。
func (v Vocabulary) Validate(available Vocabulary) error {
	fields := stringSet(available.Fields)
	named := stringSet(available.Named)
	for _, role := range v.Roles {
		if !role.IsValid() {
			return errors.NewCode(errors.InvalidInput, "vocabulary references unknown role").
				WithContext("role", string(role))
		}
		if len(available.Roles) > 0 && !hasRole(available.Roles, role) {
			return errors.NewCode(errors.InvalidInput, "vocabulary references unknown role").
				WithContext("role", string(role))
		}
	}
	for _, field := range v.Fields {
		if _, ok := fields[field]; !ok {
			return errors.NewCode(errors.InvalidInput, "vocabulary references unknown field").
				WithContext("field", field)
		}
	}
	for _, name := range v.Named {
		if _, ok := named[name]; !ok {
			return errors.NewCode(errors.InvalidInput, "vocabulary references unknown named condition").
				WithContext("named", name)
		}
	}
	return nil
}

// ValidateAgainst 校验判定结果只引用词汇内声明的项。
func (d Decision) ValidateAgainst(v Vocabulary) error {
	if !d.Allowed {
		return nil
	}
	roles := roleSet(v.Roles)
	fields := stringSet(v.Fields)
	named := stringSet(v.Named)
	for _, field := range d.Spec.Fields() {
		if _, ok := fields[field]; ok {
			continue
		}
		if _, ok := roles[spec.Role(field)]; ok {
			continue
		}
		return errors.NewCode(errors.Internal, "decision spec references field outside vocabulary").
			WithContext("field", field)
	}
	for _, name := range d.Spec.NamedNames() {
		if _, ok := named[name]; !ok {
			return errors.NewCode(errors.Internal, "decision spec references named condition outside vocabulary").
				WithContext("named", name)
		}
	}
	for _, assignment := range d.Assign {
		if err := assignment.Validate(); err != nil {
			return err
		}
		if _, ok := roles[assignment.Role]; !ok && len(v.Roles) > 0 {
			return errors.NewCode(errors.Internal, "decision assignment references role outside vocabulary").
				WithContext("role", string(assignment.Role))
		}
	}
	return nil
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func roleSet(roles []spec.Role) map[spec.Role]struct{} {
	set := make(map[spec.Role]struct{}, len(roles))
	for _, role := range roles {
		set[role] = struct{}{}
	}
	return set
}

func hasRole(roles []spec.Role, target spec.Role) bool {
	for _, role := range roles {
		if role == target {
			return true
		}
	}
	return false
}
