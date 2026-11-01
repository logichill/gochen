package crud

const defaultMaxPageSize = 1000

// NormalizePageBounds 规范化分页参数：负 offset 归零；limit < 1 回退默认值，
// 超过应用上限时封顶。
func NormalizePageBounds(offset, limit, max int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 {
		limit = defaultMaxPageSize
	}
	if max > 0 && limit > max {
		limit = max
	}
	return offset, limit
}
