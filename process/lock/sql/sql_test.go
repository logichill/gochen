package sql

import (
	"context"
	"sync"
	"testing"
	"time"

	"gochen/db"
	basicdb "gochen/db/sql/stdsql"
	_ "modernc.org/sqlite"
)

// setupTestDB t：测试上下文（testing.T）（类型：*testing.T）。
//
// 参数：
//
// 返回：
// - result：测试返回值（类型：db.IDatabase）
func setupTestDB(t *testing.T) db.IDatabase {
	t.Helper()
	database, err := basicdb.New(db.DBConfig{
		Driver:       "sqlite",
		Database:     ":memory:",
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	return database
}

// TestNewSQLLockProvider_ValidationAndDefaults 验证 NewSQLLockProvider ValidationAndDefaults。
func TestNewSQLLockProvider_ValidationAndDefaults(t *testing.T) {
	_, err := NewSQLLockProvider(nil, &SQLLockProviderConfig{Owner: "a"})
	if err == nil {
		t.Fatalf("expected error for nil database")
	}

	database := setupTestDB(t)

	_, err = NewSQLLockProvider(database, &SQLLockProviderConfig{Owner: ""})
	if err == nil {
		t.Fatalf("expected error for empty owner")
	}

	_, err = NewSQLLockProvider(database, &SQLLockProviderConfig{Owner: "a", TableName: "bad-name;"})
	if err == nil {
		t.Fatalf("expected error for invalid table name")
	}

	p, err := NewSQLLockProvider(database, &SQLLockProviderConfig{Owner: "a"})
	if err != nil {
		t.Fatalf("NewSQLLockProvider: %v", err)
	}
	if p.tableName != "distributed_locks" {
		t.Fatalf("expected default table name, got %q", p.tableName)
	}
	if p.ttl != 30*time.Second {
		t.Fatalf("expected default ttl, got %v", p.ttl)
	}
	if p.pollInterval != 50*time.Millisecond {
		t.Fatalf("expected default poll interval, got %v", p.pollInterval)
	}
	if p.logger == nil {
		t.Fatalf("expected logger")
	}
}

// TestSQLLockProvider_AcquireTimeout 验证 SQLLockProvider AcquireTimeout。
func TestSQLLockProvider_AcquireTimeout(t *testing.T) {
	database := setupTestDB(t)
	p1, err := NewSQLLockProvider(database, &SQLLockProviderConfig{
		Owner:        "a",
		TTL:          5 * time.Second,
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSQLLockProvider: %v", err)
	}
	p2, err := NewSQLLockProvider(database, &SQLLockProviderConfig{
		Owner:        "b",
		TTL:          5 * time.Second,
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSQLLockProvider: %v", err)
	}

	release, err := p1.Acquire(context.Background(), "k")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = p2.Acquire(ctx, "k")
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
}

// TestSQLLockProvider_ExpiresAndCanBeReacquired 验证 SQLLockProvider ExpiresAndCanBeReacquired。
func TestSQLLockProvider_ExpiresAndCanBeReacquired(t *testing.T) {
	database := setupTestDB(t)

	p1, err := NewSQLLockProvider(database, &SQLLockProviderConfig{
		Owner:        "a",
		TTL:          30 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSQLLockProvider: %v", err)
	}
	p2, err := NewSQLLockProvider(database, &SQLLockProviderConfig{
		Owner:        "b",
		TTL:          30 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSQLLockProvider: %v", err)
	}

	release, err := p1.Acquire(context.Background(), "k")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// 故意不 release，等待 TTL 过期
	_ = release
	time.Sleep(80 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	r2, err := p2.Acquire(ctx, "k")
	if err != nil {
		t.Fatalf("Acquire after expire: %v", err)
	}
	r2()
}

// TestSQLLockProvider_ConcurrentAcquireSameKey 验证 SQLLockProvider ConcurrentAcquireSameKey。
func TestSQLLockProvider_ConcurrentAcquireSameKey(t *testing.T) {
	database := setupTestDB(t)
	p, err := NewSQLLockProvider(database, &SQLLockProviderConfig{
		Owner:        "a",
		TTL:          200 * time.Millisecond,
		PollInterval: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSQLLockProvider: %v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	var inCS int
	var mu sync.Mutex

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			release, err := p.Acquire(ctx, "k")
			if err != nil {
				t.Errorf("Acquire error: %v", err)
				return
			}
			mu.Lock()
			inCS++
			if inCS != 1 {
				t.Errorf("expected mutual exclusion, inCS=%d", inCS)
			}
			mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			inCS--
			mu.Unlock()
			release()
		}()
	}
	wg.Wait()
}
