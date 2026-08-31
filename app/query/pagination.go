package query

import (
	"math"
	"strings"

	"gochen/errors"
)

// SortDirection 表示排序方向。
type SortDirection string

const (
	// ASC 表示升序（ascending）。
	ASC SortDirection = "asc"
	// DESC 表示降序（descending）。
	DESC SortDirection = "desc"
)

// IsValid 判断排序方向是否为已知值（asc/desc）。
func (s SortDirection) IsValid() bool { return s == ASC || s == DESC }

// ParseSortDirection 解析排序Direction。
func ParseSortDirection(s string) (SortDirection, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	dir := SortDirection(s)
	return dir, dir.IsValid()
}

// Sort 表示一个有序排序项（字段 + 方向）。
type Sort struct {
	Field     string        `json:"field"`
	Direction SortDirection `json:"direction"`
}

// PaginationOptions 表示分页查询请求。
type PaginationOptions struct {
	Page     int             `json:"page"`
	Size     int             `json:"size"`
	Filters  QueryFilters    `json:"filters"`
	Sorts    []Sort          `json:"sorts"`
	Fields   []string        `json:"fields"`
	Advanced AdvancedFilters `json:"advanced,omitempty"`
}

func (p *PaginationOptions) Offset() int {
	if p == nil || p.Page <= 1 || p.Size <= 0 {
		return 0
	}
	if p.Page-1 > math.MaxInt/p.Size {
		return math.MaxInt
	}
	return (p.Page - 1) * p.Size
}

func (p *PaginationOptions) Request() QueryRequest {
	if p == nil {
		return QueryRequest{}
	}
	return QueryRequest{
		Fields:  append([]string(nil), p.Fields...),
		Sorts:   append([]Sort(nil), p.Sorts...),
		Filters: p.Filters.Clone(),
	}
}

// AdvancedFilters 表示适配层允许的受控高级分页过滤能力。
//
// 说明：
// - 该结构仅保留框架明确支持的扩展过滤协议；
// - 禁止再通过 map[string]any 透传任意弱类型条件到 repository。
type AdvancedFilters struct {
	Or        []OrCondition  `json:"or,omitempty"`
	DateRange *DateRangeExpr `json:"date_range,omitempty"`
}

// IsZero 判断是否为空高级过滤集合。
func (f AdvancedFilters) IsZero() bool {
	return len(f.Or) == 0 && f.DateRange == nil
}

// OrCondition 表示一组 field=value 的 AND 条件；多个项之间按 OR 组合。
type OrCondition map[string]string

// DateRangeExpr 表示默认 created_at 字段的日期区间过滤。
type DateRangeExpr struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

// Validate 调整并验证分页参数，确保 page 与 size 在合理范围内。
//
// 参数：
// - maxSize：允许的最大 page size（<=0 表示不限制）。
func (p *PaginationOptions) Validate(maxSize int) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "pagination options cannot be nil")
	}
	if p.Page < 1 {
		p.Page = 1
	}
	if p.Size < 1 {
		p.Size = 10
	}
	if maxSize > 0 && p.Size > maxSize {
		p.Size = maxSize
	}
	if p.Page > 1 && p.Page-1 > math.MaxInt/p.Size {
		return errors.NewCode(errors.InvalidInput, "pagination offset overflows integer range").
			WithContext("page", p.Page).
			WithContext("size", p.Size)
	}
	return nil
}

// PagedResult 分页结果。
type PagedResult[T any] struct {
	Data       []T   `json:"data"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Size       int   `json:"size"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}
