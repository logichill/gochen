package contracttest

import (
	"context"
	"reflect"
	"testing"

	"gochen/eventing/store"
)

// RunEventStream 验证聚合分页、全局游标续读与过滤；不要求游标为某种固定编码。
func RunEventStream(t *testing.T, factory func(*testing.T) store.IEventStreamStore[int64]) {
	t.Helper()
	t.Run("aggregate_pages", func(t *testing.T) {
		s := factory(t)
		appendEvents(t, s, "A", 1, 0, event("a1", "A", 1, 1), event("a2", "A", 1, 2), event("a3", "A", 1, 3))
		first, err := s.StreamAggregate(context.Background(), &store.AggregateStreamOptions[int64]{AggregateType: "A", AggregateID: 1, Limit: 2})
		must(t, err)
		if first == nil || len(first.Events) != 2 || !first.HasMore || first.NextVersion != 2 {
			t.Fatalf("first page = %+v", first)
		}
		last, err := s.StreamAggregate(context.Background(), &store.AggregateStreamOptions[int64]{AggregateType: "A", AggregateID: 1, AfterVersion: first.NextVersion, Limit: 2})
		must(t, err)
		if last == nil || len(last.Events) != 1 || last.HasMore || last.Events[0].ID != "a3" {
			t.Fatalf("last page = %+v", last)
		}
	})
	t.Run("global_cursor_resume", func(t *testing.T) {
		s := factory(t)
		appendEvents(t, s, "A", 1, 0, event("a1", "A", 1, 1), event("a2", "A", 1, 2), event("a3", "A", 1, 3))
		first, err := s.StreamEvents(context.Background(), &store.StreamOptions{Limit: 2})
		must(t, err)
		if first == nil || len(first.Events) != 2 || !first.HasMore || first.NextCursor == "" {
			t.Fatalf("first stream page = %+v", first)
		}
		last, err := s.StreamEvents(context.Background(), &store.StreamOptions{After: first.NextCursor, Limit: 2})
		must(t, err)
		if last == nil || len(last.Events) != 1 || last.HasMore {
			t.Fatalf("last stream page = %+v", last)
		}
		ids := []string{first.Events[0].ID, first.Events[1].ID, last.Events[0].ID}
		if !reflect.DeepEqual(ids, []string{"a1", "a2", "a3"}) {
			t.Fatalf("cursor lost or repeated events: %v", ids)
		}
		// 每条事件的游标可独立用于恢复处理进度；未提供时契约规定使用事件 ID。
		cursor := first.Events[0].ID
		if len(first.EventCursors) > 0 {
			if len(first.EventCursors) != len(first.Events) {
				t.Fatal("event cursors do not match events")
			}
			cursor = first.EventCursors[0]
		}
		resumed, err := s.StreamEvents(context.Background(), &store.StreamOptions{After: cursor, Limit: 2})
		must(t, err)
		if resumed == nil || len(resumed.Events) != 2 || resumed.Events[0].ID != "a2" || resumed.Events[1].ID != "a3" {
			t.Fatalf("resumed stream = %+v", resumed)
		}
	})
	t.Run("aggregate_type_filter", func(t *testing.T) {
		s := factory(t)
		appendEvents(t, s, "A", 1, 0, event("a1", "A", 1, 1))
		appendEvents(t, s, "B", 1, 0, event("b1", "B", 1, 1))
		page, err := s.StreamEvents(context.Background(), &store.StreamOptions{AggregateTypes: []string{"B"}})
		must(t, err)
		if page == nil || len(page.Events) != 1 || page.Events[0].ID != "b1" {
			t.Fatalf("filtered stream = %+v", page)
		}
	})
}
