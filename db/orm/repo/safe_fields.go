package repo

import "gochen/db/sql/safeident"

// isAllowedField 检查字段名是否同时满足“安全标识符”和模型元数据中的字段集合。
//
// 说明：
// - 若无法从模型中获取字段元数据（例如适配器未填充），只要名称语法安全即视为允许。
func (quer *queryBuilder) isAllowedField(field string) bool {
	if !safeident.IsSafeIdentifier(field) {
		return false
	}

	if quer.model == nil {
		return true
	}
	meta := quer.model.Meta()
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
