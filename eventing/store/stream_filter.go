package store

import (
	"sort"
	"time"

	"gochen/eventing"
)

// FilterEventsWithOptions 过滤事件列表并带选项。
func FilterEventsWithOptions[ID comparable](events []eventing.Event[ID], opts *StreamOptions) *StreamResult[ID] {
	events = append([]eventing.Event[ID](nil), events...)
	// 统一排序，确保游标语义稳定
	sort.Slice(events, func(i, j int) bool {
		return eventBefore(events[i], events[j])
	})
	return filterOrderedEventsWithOptions(events, opts)
}

// FilterOrderedEventsWithOptions 过滤已按 timestamp/id 升序排列的事件列表。
func FilterOrderedEventsWithOptions[ID comparable](events []eventing.Event[ID], opts *StreamOptions) *StreamResult[ID] {
	return filterOrderedEventsWithOptions(events, opts)
}

func filterOrderedEventsWithOptions[ID comparable](events []eventing.Event[ID], opts *StreamOptions) *StreamResult[ID] {
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

	limit := NormalizeStreamLimit(opts.Limit)
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
		result.EventCursors = make([]string, n)
		for i := range result.Events {
			result.EventCursors[i] = result.Events[i].GetID()
		}
	}

	return result
}

func eventBefore[ID comparable](left eventing.Event[ID], right eventing.Event[ID]) bool {
	leftTime, rightTime := left.GetTimestamp(), right.GetTimestamp()
	if leftTime.Equal(rightTime) {
		return left.GetID() < right.GetID()
	}
	return leftTime.Before(rightTime)
}
