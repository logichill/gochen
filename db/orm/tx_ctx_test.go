package orm

import (
	"context"
	"errors"
	"testing"
	"time"

	"gochen/contextx"
)

func TestWithoutTxSessionPreservesRequestAndSeparatesTransactions(t *testing.T) {
	for _, useDispatcher := range []bool{false, true} {
		name := "lifecycle"
		if useDispatcher {
			name = "session_dispatcher"
		}
		t.Run(name, func(t *testing.T) {
			type requestKey struct{}
			request := new(int)
			base, cancel := context.WithTimeout(context.WithValue(t.Context(), requestKey{}, request), time.Minute)
			defer cancel()
			original, err := contextx.WithTenantID(base, "tenant-a")
			if err != nil {
				t.Fatal(err)
			}
			session := &testSession{}
			if useDispatcher {
				session.dispatcher = contextx.NewAfterCommitDispatcher()
			}
			original, err = WithTxSession(original, session, true)
			if err != nil {
				t.Fatal(err)
			}
			original, err = contextx.WithTxLifecycle(original, true)
			if err != nil {
				t.Fatal(err)
			}
			originalCalls := 0
			if err := contextx.AppendAfterCommit(original, func(context.Context) error {
				originalCalls++
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			detached, err := WithoutTxSession(original)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, ok := TxSessionFromContext(detached); ok {
				t.Fatal("detached context retained transaction session")
			}
			if _, ok := contextx.TxLifecycleFromContext(detached); ok {
				t.Fatal("detached context retained transaction lifecycle")
			}
			if err := contextx.AppendAfterCommit(detached, func(context.Context) error { return nil }); err == nil {
				t.Fatal("detached context still accepts callbacks into original transaction")
			}
			if got, owned, ok := TxSessionFromContext(original); !ok || !owned || got != session {
				t.Fatal("detaching changed the original transaction")
			}
			deadline, _ := original.Deadline()
			detachedDeadline, ok := detached.Deadline()
			if !ok || !deadline.Equal(detachedDeadline) || detached.Done() != original.Done() || detached.Value(requestKey{}) != request || contextx.TenantID(detached) != "tenant-a" {
				t.Fatal("detaching lost request values, deadline, or cancellation")
			}

			fresh, err := contextx.WithTxLifecycle(detached, true)
			if err != nil {
				t.Fatal(err)
			}
			freshCalls := 0
			if err := contextx.AppendAfterCommit(fresh, func(context.Context) error {
				freshCalls++
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := contextx.RunAfterCommit(fresh); err != nil {
					t.Fatal(err)
				}
			}
			if freshCalls != 1 || originalCalls != 0 {
				t.Fatalf("transactions shared callbacks: fresh=%d original=%d", freshCalls, originalCalls)
			}
			if useDispatcher {
				err = session.dispatcher.RunAfterCommit()
			} else {
				err = contextx.RunAfterCommit(original)
			}
			if err != nil || originalCalls != 1 || freshCalls != 1 {
				t.Fatalf("original callbacks changed: fresh=%d original=%d err=%v", freshCalls, originalCalls, err)
			}
			cancel()
			if !errors.Is(detached.Err(), context.Canceled) || !errors.Is(context.Cause(detached), context.Canceled) {
				t.Fatal("detached context lost cancellation")
			}
		})
	}
}

func TestWithoutTxSessionRejectsNilContext(t *testing.T) {
	if _, err := WithoutTxSession(nil); err == nil {
		t.Fatal("expected nil context to fail")
	}
}
