package repo

import "strings"

// isSafeFieldName 判断字段名是否为“安全标识符”。
//
// 允许形式：
//   - 单一标识符：foo, bar_1
//   - 带点限定名：table.column
//
// 规则（按段）：
//   - 每段不能为空；
//   - 首字符必须是字母或下划线 [A-Za-z_]；
//   - 后续字符必须是字母、数字或下划线 [A-Za-z0-9_]。
func isSafeFieldName(name string) bool {
	if name == "" {
		return false
	}
	parts := strings.Split(name, ".")
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			ch := part[i]
			if i == 0 {
				if !((ch >= 'a' && ch <= 'z') ||
					(ch >= 'A' && ch <= 'Z') ||
					ch == '_') {
					return false
				}
			} else {
				if !((ch >= 'a' && ch <= 'z') ||
					(ch >= 'A' && ch <= 'Z') ||
					(ch >= '0' && ch <= '9') ||
					ch == '_') {
					return false
				}
			}
		}
	}
	return true
}

// isAllowedField 检查字段名是否同时满足“安全标识符”和模型元数据中的字段集合。
//
// 若无法从模型中获取字段元数据（例如适配器未填充），只要名称语法安全即视为允许。
func (q *queryBuilder) isAllowedField(field string) bool {
	if !isSafeFieldName(field) {
		return false
	}

	if q.model == nil {
		return true
	}
	meta := q.model.Meta()
	if meta == nil || len(meta.Fields) == 0 {
		return true
	}
	for _, f := range meta.Fields {
		if f.Column == field || f.Name == field {
			return true
		}
	}
	return false
}

// helper 暴露给 Repo[T] 级别调用，避免在外部重复访问 IModel。
func isFieldAllowed(q *queryBuilder, field string) bool {
	if q == nil {
		return false
	}
	return q.isAllowedField(field)
}
