package middleware

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gochen/contextx"
	"gochen/errors"
	"gochen/messaging"
	"gochen/messaging/command"
	"gochen/messaging/transport/memory"
	"gochen/testkit/assert"
	"gochen/testkit/require"
)

type idempotencyMiddlewareFunc func(context.Context, messaging.IMessage, messaging.HandlerFunc) error

func (f idempotencyMiddlewareFunc) Name() string { return "IdempotencyTest" }
func (f idempotencyMiddlewareFunc) Handle(ctx context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
	return f(ctx, msg, next)
}

type idempotencyBatchCapture struct {
	*IdempotencyMiddleware
	finishers chan func(bool)
}

func (m *idempotencyBatchCapture) BeginPublishAllBatch(ctx context.Context) (context.Context, func(bool)) {
	ctx, finish := m.IdempotencyMiddleware.BeginPublishAllBatch(ctx)
	m.finishers <- finish
	return ctx, finish
}

func idempotencyTestCommand(id string) *command.Command {
	return command.NewCommand(id, "UpdateUser", "same-user", "User", nil)
}

func awaitIdempotencyPublish(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("publisher did not finish")
		return nil
	}
}

func TestIdempotencyMiddleware_MixedPublishWithAggregateLockDoesNotDeadlock(t *testing.T) {
	ctx := context.Background()
	idem := NewIdempotencyMiddleware(nil)
	defer func() { _ = idem.Stop(ctx) }()
	capture := &idempotencyBatchCapture{IdempotencyMiddleware: idem, finishers: make(chan func(bool), 1)}
	transport := memory.NewMemoryTransportForTest(8)
	require.NoError(t, transport.Start(ctx))
	defer func() { _ = transport.Stop(ctx) }()
	bus := messaging.NewMessageBus(transport)
	atSecond := make(chan struct{})
	singleHasAggregate := make(chan struct{})
	type singleContextKey struct{}
	bus.Use(idempotencyMiddlewareFunc(func(ctx context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
		if msg.GetID() == "b" {
			close(atSecond)
			<-singleHasAggregate
		}
		return next(ctx, msg)
	}))
	bus.Use(NewAggregateLockMiddleware(nil))
	bus.Use(idempotencyMiddlewareFunc(func(ctx context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
		if ctx.Value(singleContextKey{}) == true {
			close(singleHasAggregate)
		}
		return next(ctx, msg)
	}))
	bus.Use(capture)
	batchDone := make(chan error, 1)
	go func() {
		batchDone <- bus.PublishAll(ctx, []messaging.IMessage{idempotencyTestCommand("a"), idempotencyTestCommand("b")})
	}()
	finish := <-capture.finishers
	defer finish(false)
	<-atSecond
	singleDone := make(chan error, 1)
	go func() {
		singleDone <- bus.Publish(context.WithValue(ctx, singleContextKey{}, true), idempotencyTestCommand("a"))
	}()
	select {
	case err := <-batchDone:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		// 回归时主动释放批次锁，避免测试遗留死锁 goroutine。
		finish(false)
		_ = awaitIdempotencyPublish(t, batchDone)
		t.Error("batch and single publish deadlocked on command and aggregate locks")
	}
	assert.ErrorIs(t, awaitIdempotencyPublish(t, singleDone), errors.Concurrency)
	assert.Equal(t, 2, idem.GetProcessedCount())
	pending, err := transport.StopWithSnapshot(ctx)
	require.NoError(t, err)
	ids := make([]string, len(pending))
	for i, msg := range pending {
		ids[i] = msg.GetID()
	}
	assert.Equal(t, []string{"a", "b"}, ids)
}

type idempotencyFailFirstBatchTransport struct {
	*memory.MemoryTransport
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (t *idempotencyFailFirstBatchTransport) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	if t.calls.Add(1) == 1 {
		close(t.entered)
		<-t.release
		return errors.NewCode(errors.Dependency, "first batch failed before enqueue")
	}
	return t.MemoryTransport.PublishAll(ctx, messages)
}

func TestIdempotencyMiddleware_BatchConcurrencyRetryDoesNotDropCommands(t *testing.T) {
	ctx := context.Background()
	idem := NewIdempotencyMiddleware(nil)
	defer func() { _ = idem.Stop(ctx) }()
	transport := &idempotencyFailFirstBatchTransport{
		MemoryTransport: memory.NewMemoryTransportForTest(8),
		entered:         make(chan struct{}),
		release:         make(chan struct{}),
	}
	require.NoError(t, transport.Start(ctx))
	defer func() { _ = transport.Stop(ctx) }()
	unblock := sync.OnceFunc(func() { close(transport.release) })
	defer unblock()
	bus := messaging.NewMessageBus(transport)
	bus.Use(idempotencyMiddlewareFunc(func(ctx context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
		err := next(ctx, msg)
		if errors.Is(err, errors.Concurrency) {
			return next(ctx, msg)
		}
		return err
	}))
	bus.Use(idem)
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- bus.PublishAll(ctx, []messaging.IMessage{idempotencyTestCommand("a"), idempotencyTestCommand("b")})
	}()
	<-transport.entered
	secondErr := bus.PublishAll(ctx, []messaging.IMessage{idempotencyTestCommand("a"), idempotencyTestCommand("c")})
	assert.ErrorIs(t, secondErr, errors.Concurrency)
	assert.Equal(t, 0, transport.Stats().QueueDepth)
	assert.Equal(t, 0, idem.GetProcessedCount())
	unblock()
	assert.ErrorIs(t, awaitIdempotencyPublish(t, firstDone), errors.Dependency)
	assert.NoError(t, bus.PublishAll(ctx, []messaging.IMessage{idempotencyTestCommand("a"), idempotencyTestCommand("c")}))
	assert.Equal(t, 2, idem.GetProcessedCount())
	pending, err := transport.StopWithSnapshot(ctx)
	require.NoError(t, err)
	ids := make([]string, len(pending))
	for i, msg := range pending {
		ids[i] = msg.GetID()
	}
	assert.Equal(t, []string{"a", "c"}, ids)
}

func TestIdempotencyMiddleware_BatchFailedAttemptCanRetry(t *testing.T) {
	for _, failure := range []string{"error", "panic"} {
		t.Run(failure, func(t *testing.T) {
			idem := NewIdempotencyMiddleware(nil)
			defer func() { _ = idem.Stop(context.Background()) }()
			ctx, finish := idem.BeginPublishAllBatch(context.Background())
			defer finish(false)
			cmd := idempotencyTestCommand("a")
			fail := func(context.Context, messaging.IMessage) error {
				if failure == "panic" {
					panic(assert.AnError)
				}
				return assert.AnError
			}
			if failure == "panic" {
				assert.PanicsWithValue(t, assert.AnError, func() { _ = idem.Handle(ctx, cmd, fail) })
			} else {
				assert.ErrorIs(t, idem.Handle(ctx, cmd, fail), assert.AnError)
			}
			calls := 0
			assert.NoError(t, idem.Handle(ctx, cmd, func(context.Context, messaging.IMessage) error {
				calls++
				return nil
			}))
			finish(true)
			assert.Equal(t, 1, calls)
			assert.Equal(t, 1, idem.GetProcessedCount())
		})
	}
}

func TestIdempotencyMiddleware_BatchRetryRollsBackMessagesAndReservations(t *testing.T) {
	positions := []struct {
		name  string
		index int
	}{
		{"before_idempotency", 0},
		{"between_idempotency", 1},
		{"after_idempotency", 2},
	}
	for _, position := range positions {
		for _, phase := range []string{"before_next", "after_next"} {
			for _, failure := range []string{"error", "panic"} {
				t.Run(position.name+"/"+phase+"/"+failure, func(t *testing.T) {
					ctx := context.Background()
					first, second := NewIdempotencyMiddleware(nil), NewIdempotencyMiddleware(nil)
					defer func() { _ = first.Stop(ctx); _ = second.Stop(ctx) }()
					transport := memory.NewMemoryTransportForTest(8)
					require.NoError(t, transport.Start(ctx))
					defer func() { _ = transport.Stop(ctx) }()
					bus := messaging.NewMessageBus(transport)
					bus.Use(idempotencyMiddlewareFunc(func(ctx context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
						attempt := func() (err error) {
							defer func() {
								if r := recover(); r != nil {
									err = fmt.Errorf("middleware panicked: %v", r)
								}
							}()
							return next(ctx, msg)
						}
						if err := attempt(); err != nil {
							return attempt()
						}
						return nil
					}))
					failed := false
					fail := func() error {
						if failure == "panic" {
							panic(assert.AnError)
						}
						return assert.AnError
					}
					failing := idempotencyMiddlewareFunc(func(ctx context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
						failAttempt := msg.GetID() == "a" && !failed
						if failAttempt {
							failed = true
							if phase == "before_next" {
								return fail()
							}
						}
						if err := next(ctx, msg); err != nil {
							return err
						}
						if failAttempt {
							return fail()
						}
						return nil
					})
					idempotencies := []*IdempotencyMiddleware{first, second}
					for i := 0; i <= len(idempotencies); i++ {
						if i == position.index {
							bus.Use(failing)
						}
						if i < len(idempotencies) {
							bus.Use(idempotencies[i])
						}
					}
					require.NoError(t, bus.PublishAll(ctx, []messaging.IMessage{
						idempotencyTestCommand("kept"), idempotencyTestCommand("a"),
						idempotencyTestCommand("b"), idempotencyTestCommand("a"),
					}))
					assert.Equal(t, 3, first.GetProcessedCount())
					assert.Equal(t, 3, second.GetProcessedCount())
					assert.NoError(t, bus.Publish(ctx, idempotencyTestCommand("a")))
					pending, err := transport.StopWithSnapshot(ctx)
					require.NoError(t, err)
					ids := make([]string, len(pending))
					for i, msg := range pending {
						ids[i] = msg.GetID()
					}
					assert.Equal(t, []string{"kept", "a", "b"}, ids)
				})
			}
		}
	}
}

func TestIdempotencyMiddleware_BatchIgnoredFailureDoesNotMarkProcessed(t *testing.T) {
	// 下标表示插入两层幂等中间件之前、之间或之后；恢复层必须包住失败层。
	positions := []struct {
		name    string
		recover int
		fail    int
	}{
		{"recover_before/fail_before", 0, 0},
		{"recover_before/fail_between", 0, 1},
		{"recover_before/fail_after", 0, 2},
		{"recover_between/fail_between", 1, 1},
		{"recover_between/fail_after", 1, 2},
		{"recover_after/fail_after", 2, 2},
	}
	for _, position := range positions {
		for _, phase := range []string{"before_next", "after_next"} {
			for _, failure := range []string{"error", "panic"} {
				for _, retry := range []string{"same_batch", "next_batch", "single"} {
					t.Run(position.name+"/"+phase+"/"+failure+"/"+retry, func(t *testing.T) {
						ctx := context.Background()
						idempotencies := []*IdempotencyMiddleware{NewIdempotencyMiddleware(nil), NewIdempotencyMiddleware(nil)}
						for _, idem := range idempotencies {
							t.Cleanup(func() { _ = idem.Stop(ctx) })
						}
						transport := memory.NewMemoryTransportForTest(8)
						require.NoError(t, transport.Start(ctx))
						t.Cleanup(func() { _ = transport.Stop(ctx) })
						bus := messaging.NewMessageBus(transport)
						recovering := idempotencyMiddlewareFunc(func(ctx context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
							defer func() { _ = recover() }()
							_ = next(ctx, msg)
							return nil
						})
						failed := false
						fail := func() error {
							if failure == "panic" {
								panic(assert.AnError)
							}
							return assert.AnError
						}
						failing := idempotencyMiddlewareFunc(func(ctx context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
							failAttempt := msg.GetID() == "a" && !failed
							if failAttempt {
								failed = true
								if phase == "before_next" {
									return fail()
								}
							}
							if err := next(ctx, msg); err != nil {
								return err
							}
							if failAttempt {
								return fail()
							}
							return nil
						})
						for i := 0; i <= len(idempotencies); i++ {
							if i == position.recover {
								bus.Use(recovering)
							}
							if i == position.fail {
								bus.Use(failing)
							}
							if i < len(idempotencies) {
								bus.Use(idempotencies[i])
							}
						}
						messages := []messaging.IMessage{idempotencyTestCommand("kept"), idempotencyTestCommand("a"), idempotencyTestCommand("b")}
						if retry == "same_batch" {
							messages = append(messages, idempotencyTestCommand("a"))
						}
						require.NoError(t, bus.PublishAll(ctx, messages))
						if retry != "same_batch" {
							for _, idem := range idempotencies {
								assert.Equal(t, 2, idem.GetProcessedCount())
							}
							assert.Equal(t, 2, transport.Stats().QueueDepth)
							if retry == "single" {
								require.NoError(t, bus.Publish(ctx, idempotencyTestCommand("a")))
							} else {
								require.NoError(t, bus.PublishAll(ctx, []messaging.IMessage{idempotencyTestCommand("a"), idempotencyTestCommand("b")}))
							}
						}
						for _, idem := range idempotencies {
							assert.Equal(t, 3, idem.GetProcessedCount())
						}
						// 成功重试后的重复命令仍应去重，且不应残留争用锁。
						require.NoError(t, bus.Publish(ctx, idempotencyTestCommand("a")))
						pending, err := transport.StopWithSnapshot(ctx)
						require.NoError(t, err)
						ids := make([]string, len(pending))
						for i, msg := range pending {
							ids[i] = msg.GetID()
						}
						assert.Equal(t, []string{"kept", "b", "a"}, ids)
					})
				}
			}
		}
	}
}

func TestIdempotencyMiddleware_NestedPublishHasIndependentScope(t *testing.T) {
	for _, mode := range []string{"single", "batch_one", "batch_many"} {
		for _, outcome := range []string{"short_circuit", "forward", "error", "panic"} {
			for _, sharedBus := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/shared_bus_%t", mode, outcome, sharedBus), func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
					t.Cleanup(cancel)
					ctx, err := contextx.WithTenantID(ctx, "nested-tenant")
					require.NoError(t, err)
					ctx, err = contextx.WithTraceID(ctx, "nested-trace")
					require.NoError(t, err)
					deadline, _ := ctx.Deadline()
					checkContext := idempotencyMiddlewareFunc(func(derived context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
						assert.Equal(t, "nested-tenant", contextx.TenantID(derived))
						assert.Equal(t, "nested-trace", contextx.TraceID(derived))
						assert.Equal(t, ctx.Done(), derived.Done())
						gotDeadline, ok := derived.Deadline()
						assert.True(t, ok && gotDeadline.Equal(deadline))
						return next(derived, msg)
					})
					idempotencies := []*IdempotencyMiddleware{NewIdempotencyMiddleware(nil), NewIdempotencyMiddleware(nil)}
					for _, idem := range idempotencies {
						t.Cleanup(func() { _ = idem.Stop(ctx) })
					}
					transport := memory.NewMemoryTransportForTest(16)
					require.NoError(t, transport.Start(ctx))
					t.Cleanup(func() { _ = transport.Stop(ctx) })
					bus := messaging.NewMessageBus(transport)
					nestedBus := bus
					if !sharedBus {
						nestedBus = messaging.NewMessageBus(transport)
						nestedBus.Use(checkContext)
						for _, idem := range idempotencies {
							nestedBus.Use(idem)
						}
					}
					nestedIDs := []string{"a"}
					if mode == "batch_many" {
						nestedIDs = append(nestedIDs, "c")
					}
					bus.Use(checkContext)
					bus.Use(idempotencyMiddlewareFunc(func(derived context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
						if msg.GetID() != "route" {
							return next(derived, msg)
						}
						if mode == "single" {
							require.NoError(t, nestedBus.Publish(derived, idempotencyTestCommand("a")))
						} else {
							messages := make([]messaging.IMessage, len(nestedIDs))
							for i, id := range nestedIDs {
								messages[i] = idempotencyTestCommand(id)
							}
							require.NoError(t, nestedBus.PublishAll(derived, messages))
						}
						// 嵌套调用返回时应已提交并释放锁，外层 kept 仍只处于准备阶段。
						for _, idem := range idempotencies {
							assert.Equal(t, len(nestedIDs), idem.GetProcessedCount())
						}
						assert.NoError(t, nestedBus.Publish(ctx, idempotencyTestCommand("a")))
						switch outcome {
						case "forward":
							return next(derived, msg)
						case "error":
							return assert.AnError
						case "panic":
							panic(assert.AnError)
						default:
							return nil
						}
					}))
					for _, idem := range idempotencies {
						bus.Use(idem)
					}
					messages := []messaging.IMessage{
						idempotencyTestCommand("kept"), messaging.NewMessage("route", messaging.KindEvent, "Route", nil), idempotencyTestCommand("b"),
					}
					wantProcessed := len(nestedIDs)
					switch outcome {
					case "error":
						assert.ErrorIs(t, bus.PublishAll(ctx, messages), assert.AnError)
					case "panic":
						assert.PanicsWithValue(t, assert.AnError, func() { _ = bus.PublishAll(ctx, messages) })
					default:
						require.NoError(t, bus.PublishAll(ctx, messages))
						wantProcessed += 2
					}
					for _, idem := range idempotencies {
						assert.Equal(t, wantProcessed, idem.GetProcessedCount())
					}
					for _, id := range append(append([]string{}, nestedIDs...), "kept", "b") {
						require.NoError(t, bus.Publish(ctx, idempotencyTestCommand(id)))
					}
					pending, err := transport.StopWithSnapshot(ctx)
					require.NoError(t, err)
					ids := make([]string, len(pending))
					for i, msg := range pending {
						ids[i] = msg.GetID()
					}
					wantIDs := append(append([]string{}, nestedIDs...), "kept")
					if outcome == "forward" {
						wantIDs = append(wantIDs, "route")
					}
					wantIDs = append(wantIDs, "b")
					assert.Equal(t, wantIDs, ids)
				})
			}
		}
	}
}

func TestIdempotencyMiddleware_NestedPublishContendsWithOuterReservation(t *testing.T) {
	for _, mode := range []string{"single", "batch_one", "batch_many"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			idem := NewIdempotencyMiddleware(nil)
			t.Cleanup(func() { _ = idem.Stop(ctx) })
			transport := memory.NewMemoryTransportForTest(8)
			require.NoError(t, transport.Start(ctx))
			t.Cleanup(func() { _ = transport.Stop(ctx) })
			bus := messaging.NewMessageBus(transport)
			bus.Use(idempotencyMiddlewareFunc(func(ctx context.Context, msg messaging.IMessage, next messaging.HandlerFunc) error {
				if msg.GetID() != "route" {
					return next(ctx, msg)
				}
				if mode == "single" {
					return bus.Publish(ctx, idempotencyTestCommand("a"))
				}
				messages := []messaging.IMessage{idempotencyTestCommand("a")}
				if mode == "batch_many" {
					messages = append(messages, idempotencyTestCommand("b"))
				}
				return bus.PublishAll(ctx, messages)
			}))
			bus.Use(idem)
			err := bus.PublishAll(ctx, []messaging.IMessage{
				idempotencyTestCommand("a"), messaging.NewMessage("route", messaging.KindEvent, "Route", nil),
			})
			assert.ErrorIs(t, err, errors.Concurrency)
			assert.Equal(t, 0, transport.Stats().QueueDepth)
			assert.Equal(t, 0, idem.GetProcessedCount())
			require.NoError(t, bus.Publish(ctx, idempotencyTestCommand("a")))
			assert.Equal(t, 1, transport.Stats().QueueDepth)
			assert.Equal(t, 1, idem.GetProcessedCount())
		})
	}
}
