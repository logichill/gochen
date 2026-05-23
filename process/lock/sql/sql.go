package sql

import (
	"context"
	"fmt"
	"gochen/contextx"
	"gochen/db"
	gerrors "gochen/errors"
	"gochen/logging"
	plock "gochen/process/lock"
	"regexp"
	"sync"
	"time"
)

// SQLLockProviderConfig SQL 分布式锁配置。
type SQLLockProviderConfig struct {
	// TableName 锁表名（默认：distributed_locks）
	TableName string

	// Owner 当前实例标识（用于安全释放）。
	Owner string

	// TTL 锁过期时间（默认：30s）。
	TTL time.Duration

	// PollInterval 未获取到锁时的轮询等待时间（默认：50ms）。
	PollInterval time.Duration

	Logger logging.ILogger
}

// SQLLockProvider 基于共享数据库表的分布式锁实现。
//
// 表结构（自动创建）：
// - lock_key TEXT PRIMARY KEY
// - owner TEXT NOT NULL
// - expires_at_ms INTEGER NOT NULL
type SQLLockProvider struct {
	db           db.IDatabase
	tableName    string
	owner        string
	ttl          time.Duration
	pollInterval time.Duration
	logger       logging.ILogger

	ensureMu  sync.Mutex
	ensured   bool
	ensureErr error
}

var lockTableNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// NewSQLLockProvider 创建SQLLock提供者。
func NewSQLLockProvider(database db.IDatabase, cfg *SQLLockProviderConfig) (*SQLLockProvider, error) {
	if database == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "database cannot be nil")
	}
	if cfg == nil {
		cfg = &SQLLockProviderConfig{}
	}
	if cfg.TableName == "" {
		cfg.TableName = "distributed_locks"
	}
	if !lockTableNamePattern.MatchString(cfg.TableName) {
		return nil, gerrors.NewCode(gerrors.InvalidInput, fmt.Sprintf("invalid table name: %s", cfg.TableName))
	}
	if cfg.Owner == "" {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "owner cannot be empty")
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 30 * time.Second
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 50 * time.Millisecond
	}
	if cfg.Logger == nil {
		cfg.Logger = logging.ComponentLogger("process.lock.sql").WithField("table", cfg.TableName)
	}

	return &SQLLockProvider{
		db:           database,
		tableName:    cfg.TableName,
		owner:        cfg.Owner,
		ttl:          cfg.TTL,
		pollInterval: cfg.PollInterval,
		logger:       cfg.Logger,
	}, nil
}

// ensureTable 确保表。
func (p *SQLLockProvider) ensureTable(ctx context.Context) error {
	p.ensureMu.Lock()
	defer p.ensureMu.Unlock()
	if p.ensured {
		return p.ensureErr
	}

	func() {
		_, err := p.db.Exec(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
lock_key TEXT PRIMARY KEY,
owner TEXT NOT NULL,
expires_at_ms INTEGER NOT NULL
)`, p.tableName))
		p.ensureErr = err
	}()
	p.ensured = true
	return p.ensureErr
}

func (p *SQLLockProvider) Acquire(ctx context.Context, key string) (func(), error) {
	if key == "" {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "lock key cannot be empty")
	}
	if ctx == nil {
		return nil, gerrors.NewCode(gerrors.InvalidInput, "ctx is nil")
	}
	if err := p.ensureTable(ctx); err != nil {
		return nil, gerrors.NewCodeWithCause(gerrors.Database, "ensure lock table failed", err)
	}

	start := time.Now()
	// Avoid per-iteration allocation from time.After in hot retry loops.
	pollTimer := time.NewTimer(0)
	if !pollTimer.Stop() {
		select {
		case <-pollTimer.C:
		default:
		}
	}
	defer pollTimer.Stop()

	for {
		now := time.Now().UnixMilli()
		expires := now + p.ttl.Milliseconds()

		_, err := p.db.Exec(ctx,
			fmt.Sprintf("INSERT INTO %s (lock_key, owner, expires_at_ms) VALUES (?, ?, ?)", p.tableName),
			key, p.owner, expires,
		)
		if err == nil {
			return p.releaseFunc(key), nil
		}

		// 尝试抢占过期锁
		res, uerr := p.db.Exec(ctx,
			fmt.Sprintf("UPDATE %s SET owner = ?, expires_at_ms = ? WHERE lock_key = ? AND expires_at_ms <= ?", p.tableName),
			p.owner, expires, key, now,
		)
		if uerr == nil {
			affected, _ := res.RowsAffected()
			if affected == 1 {
				return p.releaseFunc(key), nil
			}
		}

		// 若 ctx 已到期/取消，映射为 TIMEOUT
		select {
		case <-ctx.Done():
			if gerrors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, gerrors.NewCode(gerrors.Timeout, "lock acquire timeout").WithContext("key", key).WithContext("ms", time.Since(start).Milliseconds())
			}
			return nil, ctx.Err()
		default:
		}

		// 等待下一次尝试
		if !pollTimer.Stop() {
			select {
			case <-pollTimer.C:
			default:
			}
		}
		pollTimer.Reset(p.pollInterval)
		select {
		case <-pollTimer.C:
		case <-ctx.Done():
			if gerrors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, gerrors.NewCode(gerrors.Timeout, "lock acquire timeout").WithContext("key", key).WithContext("ms", time.Since(start).Milliseconds())
			}
			return nil, ctx.Err()
		}
	}
}

func (p *SQLLockProvider) releaseFunc(key string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(contextx.Background(), 5*time.Second)
			defer cancel()
			res, err := p.db.Exec(ctx,
				fmt.Sprintf("DELETE FROM %s WHERE lock_key = ? AND owner = ?", p.tableName),
				key, p.owner,
			)
			if err == nil {
				affected, _ := res.RowsAffected()
				if affected == 0 {
					p.logger.Warn(ctx, "lock release had no effect (owner mismatch or already expired)", logging.String("key", key))
				}
				return
			}
			p.logger.Warn(ctx, "lock release failed", logging.Error(err), logging.String("key", key))
		})
	}
}

var _ plock.ILockProvider = (*SQLLockProvider)(nil)
