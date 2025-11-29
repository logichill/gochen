package repo

// SortDirection 排序方向
type SortDirection string

const (
	ASC  SortDirection = "ASC"
	DESC SortDirection = "DESC"
)

func (s SortDirection) IsValid() bool { return s == ASC || s == DESC }

// QueryOptions 查询选项（供通用仓储分页使用）
type QueryOptions struct {
	Page     int                      `json:"page"`
	Size     int                      `json:"size"`
	Order    string                   `json:"order"`
	Fields   []string                 `json:"fields"`
	Sorts    map[string]SortDirection `json:"sorts"`
	Filters  map[string]string        `json:"filters"`
	Advanced map[string]any           `json:"advanced"`
}

// PagedResult 分页结果（不再约束 T 为本地 IEntity）
type PagedResult[T any] struct {
	Data       []*T  `json:"data"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Size       int   `json:"size"`
	TotalPages int   `json:"total_pages"`
}
