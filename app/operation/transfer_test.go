package operation

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"gochen/auth"
	"gochen/contextx"
	"gochen/errors"
)

func TestExecuteTransferVersionZeroConcurrentReplayAndDegradedResult(t *testing.T) {
	runner := NewRunner(&RunnerOptions{Store: NewMemoryStore()})
	specification := &Spec{Type: "document.move", Mode: ModeTracked, Resource: &Resource{Type: "document", ID: "1"}, IdempotencyKey: "move-1"}
	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	version := uint64(0)
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	handler := func(ctx context.Context, expected uint64) (*Result, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		if expected != 0 || OperationIDFromContext(ctx) == "" {
			return nil, errors.NewCode(errors.Internal, "missing caller version or operation id")
		}
		return &Result{Result: map[string]any{"version": 1}}, contextx.WrapAfterCommitError(errors.NewCode(errors.Internal, "notification failed"))
	}
	const clients = 8
	results := make([]*Result, clients)
	errorsFound := make([]error, clients)
	var workers sync.WaitGroup
	for i := 0; i < clients; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			results[i], errorsFound[i] = runner.ExecuteTransfer(ctx, specification, &version, handler)
		}(i)
	}
	<-started
	close(release)
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
	for i := range results {
		if results[i] == nil || results[i].Operation.ID != results[0].Operation.ID || results[i].Operation.Status != StatusDegraded || results[i].Error == nil || (errorsFound[i] != nil && !contextx.IsAfterCommitError(errorsFound[i])) {
			t.Fatalf("client %d result=%+v, err=%v", i, results[i], errorsFound[i])
		}
	}
	for _, missing := range []*uint64{nil} {
		if _, err := runner.ExecuteTransfer(context.Background(), specification, missing, handler); errors.Code(err) != errors.InvalidInput {
			t.Fatalf("missing version=%v", err)
		}
	}
}

func TestExecuteTransferScopesIdentityAndResource(t *testing.T) {
	runner := NewRunner(&RunnerOptions{Store: NewMemoryStore()})
	version := uint64(0)
	identities := []struct{ tenant, operator, subject, operation, resource, id string }{
		{"t1", "alice", "", "document.move", "document", "1"},
		{"t2", "alice", "", "document.move", "document", "1"},
		{"t1", "bob", "", "document.move", "document", "1"},
		{"t1", "alice", "bob", "document.move", "document", "1"},
		{"t1", "alice", "", "document.reassign", "document", "1"},
		{"t1", "alice", "", "document.move", "invoice", "1"},
		{"t1", "alice", "", "document.move", "document", "2"},
	}
	seen := map[string]bool{}
	for i, identity := range identities {
		ctx, err := contextx.WithTenantID(context.Background(), identity.tenant)
		if err != nil {
			t.Fatal(err)
		}
		ctx, err = contextx.WithOperator(ctx, identity.operator)
		if err != nil {
			t.Fatal(err)
		}
		if identity.subject != "" {
			ctx, err = auth.WithPrincipal(ctx, auth.Principal{SubjectID: identity.subject})
			if err != nil {
				t.Fatal(err)
			}
		}
		s := &Spec{Type: identity.operation, Mode: ModeTracked, Resource: &Resource{Type: identity.resource, ID: identity.id}, IdempotencyKey: "same-request"}
		calls := 0
		handler := func(context.Context, uint64) (*Result, error) {
			calls++
			return &Result{Result: map[string]any{"caller": i}}, nil
		}
		first, err := runner.ExecuteTransfer(ctx, s, &version, handler)
		if err != nil {
			t.Fatal(err)
		}
		if i == 3 {
			// Principal.SubjectID 与 operator=bob 具有相同主体作用域。
			if calls != 0 || !seen[first.Operation.ID] {
				t.Fatalf("principal identity not used: %+v calls=%d", first, calls)
			}
		} else {
			if calls != 1 || seen[first.Operation.ID] {
				t.Fatalf("scope collision: %+v calls=%d", identity, calls)
			}
		}
		seen[first.Operation.ID] = true
		replay, err := runner.ExecuteTransfer(ctx, s, &version, handler)
		if err != nil || replay.Operation.ID != first.Operation.ID || calls > 1 {
			t.Fatalf("replay=%+v err=%v calls=%d", replay, err, calls)
		}
		if s.IdempotencyKey != "same-request" {
			t.Fatalf("input spec changed: %+v", s)
		}
	}
}

func TestExecuteTransferRejectsInvalidIdentityAndKeyBeforeHandler(t *testing.T) {
	runner := NewRunner(&RunnerOptions{Store: NewMemoryStore()})
	version := uint64(0)
	ctx, err := contextx.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", " ", "\t\n", " request", "request ", "req uest", "req\x00uest", string(make([]byte, 257))} {
		s := &Spec{Type: "document.move", Mode: ModeTracked, Resource: &Resource{Type: "document", ID: "1"}, IdempotencyKey: key}
		_, err := runner.ExecuteTransfer(ctx, s, &version, func(context.Context, uint64) (*Result, error) { t.Fatal("invalid key called handler"); return nil, nil })
		if errors.Code(err) != errors.InvalidInput {
			t.Errorf("key=%q err=%v", key, err)
		}
	}
	for _, resource := range []*Resource{nil, {}, {Type: "document"}, {ID: "1"}, {Type: " ", ID: "1"}} {
		s := &Spec{Type: "document.move", Mode: ModeTracked, Resource: resource, IdempotencyKey: "request"}
		_, err := runner.ExecuteTransfer(ctx, s, &version, func(context.Context, uint64) (*Result, error) {
			t.Fatal("invalid resource called handler")
			return nil, nil
		})
		if errors.Code(err) != errors.InvalidInput {
			t.Errorf("resource=%+v err=%v", resource, err)
		}
	}
	s := &Spec{Type: "document.move", Mode: ModeTracked, Resource: &Resource{Type: "document", ID: "1"}, IdempotencyKey: "request"}
	for _, missing := range []context.Context{nil, context.Background()} {
		_, err := runner.ExecuteTransfer(missing, s, &version, func(context.Context, uint64) (*Result, error) {
			t.Fatal("missing identity called handler")
			return nil, nil
		})
		if errors.Code(err) != errors.InvalidInput {
			t.Errorf("missing identity err=%v", err)
		}
	}
}

func TestExecuteTransferConcurrentSubjectsShareStoreWithoutSharingResults(t *testing.T) {
	store := NewMemoryStore()
	runners := []*Runner{NewRunner(&RunnerOptions{Store: store}), NewRunner(&RunnerOptions{Store: store})}
	var calls [2]atomic.Int32
	var workers sync.WaitGroup
	start := make(chan struct{})
	operationIDs := make([]string, 8)
	for i := range operationIDs {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			subject := i % 2
			ctx, err := contextx.WithOperator(context.Background(), []string{"alice", "bob"}[subject])
			if err != nil {
				t.Error(err)
				return
			}
			version := uint64(0)
			s := &Spec{Type: "document.move", Mode: ModeTracked, Resource: &Resource{Type: "document", ID: "1"}, IdempotencyKey: "same-request"}
			<-start
			result, err := runners[(i/2)%2].ExecuteTransfer(ctx, s, &version, func(context.Context, uint64) (*Result, error) {
				calls[subject].Add(1)
				return &Result{Result: map[string]any{"subject": subject}}, nil
			})
			if err != nil {
				t.Error(err)
				return
			}
			if result.Result["subject"] != subject {
				t.Errorf("subject %d received %+v", subject, result)
			}
			operationIDs[i] = result.Operation.ID
		}(i)
	}
	close(start)
	workers.Wait()
	if calls[0].Load() != 1 || calls[1].Load() != 1 {
		t.Fatalf("subject calls=%d/%d", calls[0].Load(), calls[1].Load())
	}
	for i, id := range operationIDs {
		if id == "" || id != operationIDs[i%2] {
			t.Errorf("client %d operation=%q", i, id)
		}
	}
	if operationIDs[0] == operationIDs[1] {
		t.Fatal("subjects shared operation")
	}
}
