package orm

import (
	"context"
	"database/sql"
	"testing"

	"gochen/contextx"
	"gochen/db"
)

type testSession struct {
	dispatcher contextx.IAfterCommitDispatcher
}

func (s *testSession) Capabilities() Capabilities                 { return nil }
func (s *testSession) WithContext(context.Context) IOrm           { return s }
func (s *testSession) Model(*ModelMeta) (IModel, error)           { return nil, nil }
func (s *testSession) Begin(context.Context) (IOrmSession, error) { return s, nil }
func (s *testSession) BeginTx(context.Context, *sql.TxOptions) (IOrmSession, error) {
	return s, nil
}
func (s *testSession) Database() db.IDatabase                                 { return nil }
func (s *testSession) Commit() error                                          { return nil }
func (s *testSession) Rollback() error                                        { return nil }
func (s *testSession) AfterCommitDispatcher() contextx.IAfterCommitDispatcher { return s.dispatcher }

func TestCollectQueryOptionsPreservesOrderedInputs(t *testing.T) {
	t.Parallel()

	opts := CollectQueryOptions(
		WithWhere("tenant_id = ?", "t-1"),
		WithJoin(LeftJoin("profiles", "p", On("users.id", "p.user_id"))),
		WithGroupBy("tenant_id"),
		WithOrderBy("created_at", true),
		WithLimit(20),
		WithOffset(40),
		WithSelect("id", "name"),
		WithPreload("Roles"),
		WithForUpdate(),
	)

	if len(opts.Where) != 1 || opts.Where[0].Expr != "tenant_id = ?" {
		t.Fatalf("unexpected where: %#v", opts.Where)
	}
	if len(opts.Joins) != 1 || opts.Joins[0].Alias != "p" || opts.Joins[0].Type != JoinLeft {
		t.Fatalf("unexpected joins: %#v", opts.Joins)
	}
	if len(opts.GroupBy) != 1 || opts.GroupBy[0] != "tenant_id" {
		t.Fatalf("unexpected group by: %#v", opts.GroupBy)
	}
	if len(opts.OrderBy) != 1 || opts.OrderBy[0].Column != "created_at" || !opts.OrderBy[0].Desc {
		t.Fatalf("unexpected order by: %#v", opts.OrderBy)
	}
	if opts.Limit != 20 || opts.Offset != 40 || !opts.ForUpdate {
		t.Fatalf("unexpected pagination/lock flags: %#v", opts)
	}
	if len(opts.Select) != 2 || len(opts.Preload) != 1 {
		t.Fatalf("unexpected select/preload: %#v", opts)
	}
}

func TestWithTxSessionStoresOwnedFlagAndDispatcher(t *testing.T) {
	t.Parallel()

	dispatcher := contextx.NewAfterCommitDispatcher()
	session := &testSession{dispatcher: dispatcher}

	ctx, err := WithTxSession(context.Background(), session, true)
	if err != nil {
		t.Fatalf("WithTxSession returned error: %v", err)
	}

	gotSession, owned, ok := TxSessionFromContext(ctx)
	if !ok || gotSession != session || !owned {
		t.Fatalf("unexpected tx session state: session=%T owned=%v ok=%v", gotSession, owned, ok)
	}

	called := false
	if err := contextx.AppendAfterCommit(ctx, func(context.Context) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("AppendAfterCommit returned error: %v", err)
	}
	if err := dispatcher.RunAfterCommit(); err != nil {
		t.Fatalf("RunAfterCommit returned error: %v", err)
	}
	if !called {
		t.Fatal("expected after commit callback to run through injected dispatcher")
	}
}

func TestWithTxSessionRejectsNilInputs(t *testing.T) {
	t.Parallel()

	if _, err := WithTxSession(nil, &testSession{}, true); err == nil {
		t.Fatal("expected nil ctx to fail")
	}
	if _, err := WithTxSession(context.Background(), nil, true); err == nil {
		t.Fatal("expected nil session to fail")
	}
}
