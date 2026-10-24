package subscription

import (
	"context"
	"time"

	"gochen/eventing/store"
)

const cursorFlushTimeout = 5 * time.Second

// ICursorStore 持久化订阅游标；游标语义是最后一条成功处理事件对应的后继游标。
// 默认事件流实现使用事件 ID；支持非 ID 游标的存储实现应通过 StreamResult.EventCursors 提供可持久化游标。
type ICursorStore interface {
	LoadCursor(ctx context.Context, subscriptionName string) (string, error)
	SaveCursor(ctx context.Context, subscriptionName string, cursor string) error
}

// Cursor 返回当前消费游标（最后一条成功处理事件对应的后继游标）。
func (s *Subscription[ID]) Cursor() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cursor
}

func eventCursorAt[ID comparable](res *store.StreamResult[ID], i int) string {
	if res == nil || i < 0 || i >= len(res.Events) {
		return ""
	}
	if i < len(res.EventCursors) && res.EventCursors[i] != "" {
		return res.EventCursors[i]
	}
	if i == len(res.Events)-1 && res.NextCursor != "" {
		return res.NextCursor
	}
	return res.Events[i].GetID()
}

func (s *Subscription[ID]) flushCursor(ctx context.Context, cursor string) error {
	if cursor == "" || s.cfg.CursorStore == nil {
		return nil
	}
	flushCtx := cursorFlushContext(ctx)
	var cancel context.CancelFunc
	if flushCtx.Err() != nil {
		flushCtx, cancel = context.WithTimeout(context.Background(), cursorFlushTimeout)
	} else {
		flushCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), cursorFlushTimeout)
	}
	defer cancel()
	if err := s.cfg.CursorStore.SaveCursor(flushCtx, s.cfg.Name, cursor); err != nil {
		return err
	}
	s.setCursor(cursor)
	return nil
}

func cursorFlushContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (s *Subscription[ID]) loadPersistentCursor(ctx context.Context) error {
	if s.cfg.CursorStore == nil {
		return nil
	}
	cursor, err := s.cfg.CursorStore.LoadCursor(ctx, s.cfg.Name)
	if err != nil {
		return err
	}
	if cursor == "" {
		return nil
	}
	s.setCursor(cursor)
	return nil
}

func (s *Subscription[ID]) setCursor(cursor string) {
	s.mu.Lock()
	s.cursor = cursor
	s.mu.Unlock()
}
