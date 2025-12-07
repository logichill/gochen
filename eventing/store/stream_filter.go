package store

import (
	"sort"
	"time"

	"gochen/eventing"
)

// FilterEventsWithOptions 按 StreamOptions 过滤并分页事件，统一 After/Types/时间窗口语义。
func FilterEventsWithOptions[ID comparable](events []eventing.Event[ID], opts *StreamOptions) *StreamResult[ID] {
	if opts == nil {
		opts = &StreamOptions{}
	}

	// 预取 After 对应的时间戳，便于“同时间戳 + ID”的去重比较
	var afterTimestamp time.Time
	if opts.After != "" {
		for i := range events {
			if events[i].GetID() == opts.After {
				afterTimestamp = events[i].GetTimestamp()
				break
			}
		}
	}

	typeFilter := make(map[string]struct{})
	for _, t := range opts.Types {
		typeFilter[t] = struct{}{}
	}
	aggregateFilter := make(map[string]struct{})
	for _, t := range opts.AggregateTypes {
		aggregateFilter[t] = struct{}{}
	}

	// 统一排序，确保游标语义稳定
	sort.Slice(events, func(i, j int) bool {
		ti, tj := events[i].GetTimestamp(), events[j].GetTimestamp()
		if ti.Equal(tj) {
			return events[i].GetID() < events[j].GetID()
		}
		return ti.Before(tj)
	})

	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}
	result := &StreamResult[ID]{
		Events: make([]eventing.Event[ID], 0, limit),
	}

	matched := 0
	for _, evt := range events {
		// 时间窗口
		if !opts.FromTime.IsZero() && evt.GetTimestamp().Before(opts.FromTime) {
			continue
		}
		if !opts.ToTime.IsZero() && evt.GetTimestamp().After(opts.ToTime) {
			continue
		}

		// After 游标过滤
		if opts.After != "" {
			if !afterTimestamp.IsZero() {
				if evt.GetTimestamp().Before(afterTimestamp) {
					continue
				}
				if evt.GetTimestamp().Equal(afterTimestamp) && evt.GetID() <= opts.After {
					continue
				}
			} else {
				// 未找到游标时间戳，回退到 ID 比较（假设 ID 单调）
				if evt.GetID() <= opts.After {
					continue
				}
			}
		}

		// 类型过滤
		if len(typeFilter) > 0 {
			if _, ok := typeFilter[evt.GetType()]; !ok {
				continue
			}
		}
		if len(aggregateFilter) > 0 {
			if _, ok := aggregateFilter[evt.GetAggregateType()]; !ok {
				continue
			}
		}
		// 通过所有过滤条件后才计入匹配数量
		if matched < limit {
			result.Events = append(result.Events, evt)
		} else {
			// 已经返回了 limit 条，再发现一条满足条件的事件，则说明还有更多数据
			result.HasMore = true
			break
		}
		matched++
	}

	if n := len(result.Events); n > 0 {
		result.NextCursor = result.Events[n-1].GetID()
	}

	return result
}
