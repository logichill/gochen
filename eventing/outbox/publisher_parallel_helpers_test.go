package outbox

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"gochen/testkit/require"
)

// 并发安全的 Outbox 仓储 mock，用于 ParallelPublisher 并发测试。
type concurrentOutboxRepo struct {
	mu        sync.Mutex
	entries   map[int64]*OutboxEntry[int64]
	published map[int64]int // entryID -> 次数
	failed    map[int64]int // entryID -> 次数
}

func newConcurrentOutboxRepo(entries []OutboxEntry[int64]) *concurrentOutboxRepo {
	m := &concurrentOutboxRepo{
		entries:   make(map[int64]*OutboxEntry[int64], len(entries)),
		published: make(map[int64]int),
		failed:    make(map[int64]int),
	}
	for i := range entries {
		e := entries[i]
		ee := e
		m.entries[ee.ID] = &ee
	}
	return m
}

// 并发安全的事件总线 mock：仅计数 PublishEvent 次数。
type concurrentEventBus struct {
	count int32
}

// dlqRecordingRepo 用于验证 ParallelPublisher 在失败时的 DLQ 语义。
type dlqRecordingRepo struct {
	mu     sync.Mutex
	failed []struct {
		id        int64
		errorMsg  string
		nextRetry time.Time
	}
}

type dlqRecordingDLQ struct {
	mu    sync.Mutex
	moved []OutboxEntry[int64]
}

type aggregateOrderBus struct {
	mu sync.Mutex

	versions []uint64
	v1Seen   chan struct{}
	allowV1  chan struct{}
}

func newAggregateOrderBus() *aggregateOrderBus {
	return &aggregateOrderBus{
		v1Seen:  make(chan struct{}),
		allowV1: make(chan struct{}),
	}
}

type slowPublishBus struct {
	concurrentEventBus
	delay time.Duration
}

type cancelAwarePublishBus struct {
	concurrentEventBus
	started  chan struct{}
	canceled chan struct{}
}

func closeOnce(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func newProcessingOutboxEntry(t *testing.T, id int64) OutboxEntry[int64] {
	t.Helper()
	evt := newTestEvent(id, 1, fmt.Sprintf("event-%d", id), nil)
	entry, err := EventToOutboxEntry(evt.AggregateID, evt)
	require.NoError(t, err)
	entry.ID = id
	entry.Status = OutboxStatusProcessing
	entry.ClaimToken = fmt.Sprintf("claim-%d", id)
	leaseUntil := time.Now().Add(time.Minute)
	entry.LeaseUntil = &leaseUntil
	return *entry
}

type contextSensitiveMarkRepo struct {
	claimEntries []OutboxEntry[int64]

	markPublishedCalls     int32
	markPublishedSuccesses int32
}
