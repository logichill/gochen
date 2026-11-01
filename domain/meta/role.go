// Package meta 提供 Core 的字段元数据与唯一一套类型转换规则。
//
// 元数据只描述领域字段：字段名、Go 类型、角色与是否由框架托管；SQL 列名由
// Runtime 按命名约定解析，不在本包中出现。
package meta

import (
	"strings"

	"gochen/errors"
)

// Role 是框架定义的字段角色。实体通过字段标签声明哪个字段承担哪个角色。
type Role string

const (
	RoleID     Role = "@id"     // 主键
	RoleTenant Role = "@tenant" // 租户：只供仓储的隔离不变量使用
	RoleScope  Role = "@scope"  // 管理范围
	RoleOwner  Role = "@owner"  // 归属主体
)

// IsValid 报告角色是否属于框架定义的角色集合。
func (r Role) IsValid() bool {
	switch r {
	case RoleID, RoleTenant, RoleScope, RoleOwner:
		return true
	default:
		return false
	}
}

// ParseRole 解析实体标签中的角色写法，接受 "owner" 与 "@owner" 两种形式。
func ParseRole(raw string) (Role, bool) {
	switch strings.TrimSpace(raw) {
	case "id", string(RoleID):
		return RoleID, true
	case "tenant", string(RoleTenant):
		return RoleTenant, true
	case "scope", string(RoleScope):
		return RoleScope, true
	case "owner", string(RoleOwner):
		return RoleOwner, true
	default:
		return "", false
	}
}

func validateRole(role Role) error {
	if !role.IsValid() {
		return errors.NewCode(errors.InvalidInput, "unknown field role").
			WithContext("role", string(role))
	}
	return nil
}
