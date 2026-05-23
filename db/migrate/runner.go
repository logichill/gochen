package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gochen/contextx"
	"gochen/db"
	"gochen/db/dialect"
	"gochen/db/sql/safeident"
	goerrors "gochen/errors"
)

const defaultStateTable = "schema_migrations"
const defaultMigrationType = "schema"
const lockMigrationType = "__gochen_migration_lock"
const defaultLockTimeout = 15 * time.Second
const defaultLockStaleAfter = defaultLockTimeout

// Status 描述当前 migration 状态。
type Status struct {
	Type    string
	Version uint64
	Dirty   bool
}

type runnerOptions struct {
	migrationType  string
	stateTable     string
	disableTx      bool
	lockTimeout    time.Duration
	lockStaleAfter time.Duration
	now            func() time.Time
}

// RunnerOption 用于配置 Runner。
type RunnerOption func(*runnerOptions)

// WithMigrationType 配置 Runner 管理的 migration 类型命名空间。
func WithMigrationType(name string) RunnerOption {
	return func(o *runnerOptions) {
		if strings.TrimSpace(name) != "" {
			o.migrationType = strings.TrimSpace(name)
		}
	}
}

// WithStateTable 配置 Runner 使用的状态表名称。
func WithStateTable(name string) RunnerOption {
	return func(o *runnerOptions) {
		if strings.TrimSpace(name) != "" {
			o.stateTable = strings.TrimSpace(name)
		}
	}
}

// WithoutTransaction 禁用单个 migration 文件的事务包装。
func WithoutTransaction() RunnerOption {
	return func(o *runnerOptions) {
		o.disableTx = true
	}
}

// WithLockTimeout 配置获取数据库级 migration 锁的等待时间。
func WithLockTimeout(timeout time.Duration) RunnerOption {
	return func(o *runnerOptions) {
		o.lockTimeout = timeout
	}
}

// WithLockStaleAfter 配置异常退出后 stale migration lock 的回收阈值。
func WithLockStaleAfter(staleAfter time.Duration) RunnerOption {
	return func(o *runnerOptions) {
		o.lockStaleAfter = staleAfter
	}
}

// Runner 提供最小可用的 migration 执行能力。
type Runner struct {
	db             db.IDatabase
	source         ISource
	dialect        dialect.Dialect
	migrationType  string
	stateTable     string
	disableTx      bool
	lockTimeout    time.Duration
	lockStaleAfter time.Duration
	lockToken      uint64
	lockStop       chan struct{}
	lockDone       chan struct{}
	lockErr        atomic.Pointer[lockHeartbeatFailure]
	withLockActive bool
	now            func() time.Time
	mu             sync.Mutex
}

// NewRunner 创建 migration runner。
func NewRunner(database db.IDatabase, source ISource, opts ...RunnerOption) (*Runner, error) {
	if database == nil {
		return nil, goerrors.NewCode(goerrors.InvalidInput, "database cannot be nil")
	}
	if source == nil {
		return nil, goerrors.NewCode(goerrors.InvalidInput, "migration source cannot be nil")
	}

	options := runnerOptions{
		migrationType:  defaultMigrationType,
		stateTable:     defaultStateTable,
		lockTimeout:    defaultLockTimeout,
		lockStaleAfter: defaultLockStaleAfter,
		now:            time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	if !safeident.IsSafeIdentifier(options.stateTable) {
		return nil, goerrors.NewCode(goerrors.InvalidInput, "invalid migration state table").
			WithContext("table", options.stateTable)
	}
	if !isSafeMigrationType(options.migrationType) {
		return nil, goerrors.NewCode(goerrors.InvalidInput, "invalid migration type").
			WithContext("type", options.migrationType)
	}
	if isReservedMigrationType(options.migrationType) {
		return nil, goerrors.NewCode(goerrors.InvalidInput, "migration type is reserved").
			WithContext("type", options.migrationType)
	}

	return &Runner{
		db:             database,
		source:         source,
		dialect:        dialect.FromDatabase(database),
		migrationType:  options.migrationType,
		stateTable:     options.stateTable,
		disableTx:      options.disableTx,
		lockTimeout:    options.lockTimeout,
		lockStaleAfter: options.lockStaleAfter,
		now:            options.now,
	}, nil
}

// List 返回 source 中按版本升序排列的 migration 集合。
func (r *Runner) List(ctx context.Context) ([]Migration, error) {
	ctx = normalizeContext(ctx)
	migrations, err := r.list(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Migration, len(migrations))
	copy(out, migrations)
	return out, nil
}

// Version 返回当前 migration 版本；没有已应用版本时返回 ErrNilVersion。
func (r *Runner) Version(ctx context.Context) (uint64, bool, error) {
	status, err := r.Status(ctx)
	if err != nil {
		return 0, false, err
	}
	if status.Version == 0 && !status.Dirty {
		return 0, false, ErrNilVersion
	}
	return status.Version, status.Dirty, nil
}

// Status 返回当前 migration 版本与 dirty 状态。
func (r *Runner) Status(ctx context.Context) (Status, error) {
	if err := r.beginExclusiveOperation("status"); err != nil {
		return Status{}, err
	}
	defer r.mu.Unlock()
	return r.status(ctx)
}

// Up 按顺序执行全部未执行的 up migration。
func (r *Runner) Up(ctx context.Context) error {
	if err := r.beginExclusiveOperation("up"); err != nil {
		return err
	}
	defer r.mu.Unlock()

	ctx = normalizeContext(ctx)
	if err := r.ensureStateTable(ctx); err != nil {
		return err
	}
	if err := r.acquireLock(ctx); err != nil {
		return err
	}
	defer r.releaseLock()

	state, err := r.readStatus(ctx)
	if err != nil {
		return err
	}
	if state.Dirty {
		return fmt.Errorf("%w: version=%d", ErrDirty, state.Version)
	}

	migrations, err := r.list(ctx)
	if err != nil {
		return err
	}

	applied := 0
	for _, migration := range migrations {
		if migration.Version <= state.Version {
			continue
		}
		if migration.Up == nil {
			return goerrors.NewCode(goerrors.InvalidInput, "up migration file missing").
				WithContext("version", migration.Version).
				WithContext("name", migration.Name)
		}
		if err := r.applyFile(ctx, migration.Version, migration.Version, *migration.Up); err != nil {
			return err
		}
		state.Version = migration.Version
		state.Dirty = false
		applied++
	}
	if applied == 0 {
		return ErrNoChange
	}
	return nil
}

// Steps 按指定步数迁移；n > 0 执行 up，n < 0 执行 down。
func (r *Runner) Steps(ctx context.Context, n int) error {
	if n == 0 {
		return ErrNoChange
	}
	if n > 0 {
		return r.stepsUp(ctx, n)
	}
	return r.stepsDown(ctx, -n)
}

// Migrate 迁移到指定版本。
func (r *Runner) Migrate(ctx context.Context, target uint64) error {
	if err := r.beginExclusiveOperation("migrate"); err != nil {
		return err
	}
	defer r.mu.Unlock()

	ctx = normalizeContext(ctx)
	if err := r.ensureStateTable(ctx); err != nil {
		return err
	}
	if err := r.acquireLock(ctx); err != nil {
		return err
	}
	defer r.releaseLock()
	state, err := r.readStatus(ctx)
	if err != nil {
		return err
	}
	if state.Dirty {
		return fmt.Errorf("%w: version=%d", ErrDirty, state.Version)
	}
	if state.Version == target {
		return ErrNoChange
	}
	if target != 0 {
		if err := r.ensureTargetExists(ctx, target); err != nil {
			return err
		}
	}
	if target > state.Version {
		return r.applyUpTo(ctx, state, target, 0)
	}
	return r.applyDownTo(ctx, state, target, 0)
}

// Down 回滚当前最新版本的一步 migration。
func (r *Runner) Down(ctx context.Context) error {
	return r.Steps(ctx, -1)
}

func (r *Runner) stepsUp(ctx context.Context, steps int) error {
	if err := r.beginExclusiveOperation("steps_up"); err != nil {
		return err
	}
	defer r.mu.Unlock()

	ctx = normalizeContext(ctx)
	if err := r.ensureStateTable(ctx); err != nil {
		return err
	}
	if err := r.acquireLock(ctx); err != nil {
		return err
	}
	defer r.releaseLock()

	state, err := r.readStatus(ctx)
	if err != nil {
		return err
	}
	if state.Dirty {
		return fmt.Errorf("%w: version=%d", ErrDirty, state.Version)
	}
	return r.applyUpTo(ctx, state, 0, steps)
}

func (r *Runner) stepsDown(ctx context.Context, steps int) error {
	if err := r.beginExclusiveOperation("steps_down"); err != nil {
		return err
	}
	defer r.mu.Unlock()

	ctx = normalizeContext(ctx)
	if err := r.ensureStateTable(ctx); err != nil {
		return err
	}
	if err := r.acquireLock(ctx); err != nil {
		return err
	}
	defer r.releaseLock()

	state, err := r.readStatus(ctx)
	if err != nil {
		return err
	}
	if state.Dirty {
		return fmt.Errorf("%w: version=%d", ErrDirty, state.Version)
	}
	if state.Version == 0 {
		return ErrNilVersion
	}
	return r.applyDownTo(ctx, state, 0, steps)
}

// Force 强制设置当前 migration 版本并清理 dirty 状态。
func (r *Runner) Force(ctx context.Context, version uint64) error {
	if err := r.beginExclusiveOperation("force"); err != nil {
		return err
	}
	defer r.mu.Unlock()

	ctx = normalizeContext(ctx)
	if err := r.ensureStateTable(ctx); err != nil {
		return err
	}
	if err := r.acquireLock(ctx); err != nil {
		return err
	}
	defer r.releaseLock()
	return r.writeStatus(ctx, Status{Version: version, Dirty: false})
}

// WithLock 在持有数据库级 migration 锁的前提下执行 fn。
func (r *Runner) WithLock(ctx context.Context, fn func() error) (err error) {
	if fn == nil {
		return nil
	}
	if err := r.beginExclusiveOperation("with_lock"); err != nil {
		return err
	}
	ctx = normalizeContext(ctx)
	if err := r.ensureStateTable(ctx); err != nil {
		r.mu.Unlock()
		return err
	}
	if err := r.acquireLock(ctx); err != nil {
		r.mu.Unlock()
		return err
	}
	r.withLockActive = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.withLockActive = false
		r.releaseLock()
		r.mu.Unlock()
		if err != nil {
			return
		}
		err = r.lockHeartbeatError()
	}()
	if err = fn(); err != nil {
		return err
	}
	return r.lockHeartbeatError()
}

// StateTableName 返回当前 Runner 使用的 migration 状态表名。
func (r *Runner) StateTableName() string {
	if r == nil {
		return ""
	}
	return r.stateTable
}

func (r *Runner) applyFile(ctx context.Context, dirtyVersion uint64, cleanVersion uint64, file File) error {
	content, err := r.source.Read(ctx, file)
	if err != nil {
		return err
	}
	if containsManualReviewGuard(string(content)) {
		return fmt.Errorf("%w: version=%d path=%s", ErrReviewRequired, dirtyVersion, file.Path)
	}
	statements := SplitStatements(string(content))
	if len(statements) == 0 {
		return goerrors.NewCode(goerrors.InvalidInput, "migration file is empty").
			WithContext("version", dirtyVersion).
			WithContext("path", file.Path)
	}

	if r.disableTx {
		return r.applyStatements(ctx, r.db, dirtyVersion, cleanVersion, file, statements, true)
	}
	if err := r.applyStatementsInTransaction(ctx, dirtyVersion, cleanVersion, file, statements); err == nil {
		return nil
	} else if !goerrors.Is(err, goerrors.ErrUnsupported) {
		return err
	}
	return r.applyStatements(ctx, r.db, dirtyVersion, cleanVersion, file, statements, true)
}

func (r *Runner) applyStatementsInTransaction(ctx context.Context, dirtyVersion uint64, cleanVersion uint64, file File, statements []string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		if goerrors.Is(err, goerrors.ErrUnsupported) {
			return err
		}
		return goerrors.Wrap(err, goerrors.Database, "begin migration transaction failed").
			WithContext("version", dirtyVersion).
			WithContext("direction", file.Direction).
			WithContext("path", file.Path)
	}

	txClosed := false
	defer func() {
		if !txClosed {
			_ = tx.Rollback()
		}
	}()

	if err := r.applyStatements(ctx, tx, dirtyVersion, cleanVersion, file, statements, false); err != nil {
		_ = tx.Rollback()
		txClosed = true
		if dirtyErr := r.writeStatus(ctx, Status{Type: r.migrationType, Version: dirtyVersion, Dirty: true}); dirtyErr != nil {
			return goerrors.Join(err, dirtyErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		wrapped := goerrors.Wrap(err, goerrors.Database, "commit migration transaction failed").
			WithContext("version", dirtyVersion).
			WithContext("direction", file.Direction).
			WithContext("path", file.Path)
		if dirtyErr := r.writeStatus(ctx, Status{Type: r.migrationType, Version: dirtyVersion, Dirty: true}); dirtyErr != nil {
			return goerrors.Join(wrapped, dirtyErr)
		}
		return wrapped
	}
	txClosed = true
	return nil
}

func (r *Runner) applyStatements(ctx context.Context, database db.IDatabase, dirtyVersion uint64, cleanVersion uint64, file File, statements []string, refreshLock bool) error {
	if err := r.lockHeartbeatError(); err != nil {
		return err
	}
	if refreshLock {
		if err := r.refreshLock(ctx); err != nil {
			return err
		}
	}
	if err := r.writeStatusWithDBOptions(ctx, database, Status{Type: r.migrationType, Version: dirtyVersion, Dirty: true}, refreshLock); err != nil {
		return err
	}
	for i, statement := range statements {
		if err := r.lockHeartbeatError(); err != nil {
			return err
		}
		if refreshLock {
			if err := r.refreshLock(ctx); err != nil {
				return err
			}
		}
		if _, err := database.Exec(ctx, statement); err != nil {
			return goerrors.Wrap(err, goerrors.Database, "apply migration failed").
				WithContext("version", dirtyVersion).
				WithContext("direction", file.Direction).
				WithContext("path", file.Path).
				WithContext("statement_index", i+1)
		}
	}
	if refreshLock {
		if err := r.refreshLock(ctx); err != nil {
			return err
		}
	}
	if err := r.lockHeartbeatError(); err != nil {
		return err
	}
	return r.writeStatusWithDBOptions(ctx, database, Status{Type: r.migrationType, Version: cleanVersion, Dirty: false}, refreshLock)
}

func (r *Runner) applyUpTo(ctx context.Context, state Status, target uint64, limit int) error {
	migrations, err := r.list(ctx)
	if err != nil {
		return err
	}

	applied := 0
	for _, migration := range migrations {
		if migration.Version <= state.Version {
			continue
		}
		if target > 0 && migration.Version > target {
			break
		}
		if limit > 0 && applied >= limit {
			break
		}
		if migration.Up == nil {
			return goerrors.NewCode(goerrors.InvalidInput, "up migration file missing").
				WithContext("version", migration.Version).
				WithContext("name", migration.Name)
		}
		if err := r.applyFile(ctx, migration.Version, migration.Version, *migration.Up); err != nil {
			return err
		}
		state.Version = migration.Version
		applied++
	}
	if target > 0 && state.Version != target {
		return goerrors.NewCode(goerrors.NotFound, "target migration version not found").
			WithContext("version", target)
	}
	if applied == 0 {
		return ErrNoChange
	}
	return nil
}

func (r *Runner) applyDownTo(ctx context.Context, state Status, target uint64, limit int) error {
	migrations, err := r.list(ctx)
	if err != nil {
		return err
	}

	applied := 0
	for state.Version > target {
		if limit > 0 && applied >= limit {
			break
		}
		index := findMigrationIndex(migrations, state.Version)
		if index < 0 {
			return goerrors.NewCode(goerrors.NotFound, "current migration version not found in source").
				WithContext("version", state.Version)
		}
		migration := migrations[index]
		if migration.Down == nil {
			return goerrors.NewCode(goerrors.InvalidInput, "down migration file missing").
				WithContext("version", migration.Version).
				WithContext("name", migration.Name)
		}
		nextVersion := previousVersion(migrations, index)
		if err := r.applyFile(ctx, migration.Version, nextVersion, *migration.Down); err != nil {
			return err
		}
		state.Version = nextVersion
		applied++
	}
	if applied == 0 {
		return ErrNoChange
	}
	return nil
}

func (r *Runner) ensureTargetExists(ctx context.Context, target uint64) error {
	migrations, err := r.list(ctx)
	if err != nil {
		return err
	}
	for _, migration := range migrations {
		if migration.Version == target {
			return nil
		}
	}
	return goerrors.NewCode(goerrors.NotFound, "target migration version not found").
		WithContext("version", target)
}

func previousVersion(migrations []Migration, index int) uint64 {
	if index <= 0 {
		return 0
	}
	return migrations[index-1].Version
}

func findMigrationIndex(migrations []Migration, version uint64) int {
	lo, hi := 0, len(migrations)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if migrations[mid].Version < version {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(migrations) && migrations[lo].Version == version {
		return lo
	}
	return -1
}

func (r *Runner) status(ctx context.Context) (Status, error) {
	ctx = normalizeContext(ctx)
	return r.readStatus(ctx)
}

func (r *Runner) ensureStateTable(ctx context.Context) error {
	table := r.quotedStateTable()
	query := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
  migration_type VARCHAR(64) PRIMARY KEY,
  version BIGINT NOT NULL DEFAULT 0,
  dirty INTEGER NOT NULL DEFAULT 0,
  updated_at TIMESTAMP NOT NULL
)`, table)
	if _, err := r.db.Exec(ctx, query); err != nil {
		return goerrors.Wrap(err, goerrors.Database, "ensure migration state table failed").
			WithContext("table", r.stateTable)
	}
	columns, err := r.stateColumns(ctx)
	if err != nil {
		return err
	}
	return validateStateTableColumns(r.stateTable, columns)
}

func (r *Runner) readStatus(ctx context.Context) (Status, error) {
	query := fmt.Sprintf("SELECT version, dirty FROM %s WHERE migration_type = ?", r.quotedStateTable())
	row := r.db.QueryRow(ctx, query, r.migrationType)
	var (
		version uint64
		dirty   int
	)
	err := row.Scan(&version, &dirty)
	if err == nil {
		return Status{Type: r.migrationType, Version: version, Dirty: dirty != 0}, nil
	}
	if err == sql.ErrNoRows {
		return Status{Type: r.migrationType}, nil
	}
	if isMissingStateTableError(err) {
		return Status{Type: r.migrationType}, nil
	}
	return Status{}, goerrors.Wrap(err, goerrors.Database, "read migration status failed").
		WithContext("table", r.stateTable)
}

func (r *Runner) writeStatus(ctx context.Context, status Status) error {
	return r.writeStatusWithDB(ctx, r.db, status)
}

func (r *Runner) writeStatusWithDB(ctx context.Context, database db.IDatabase, status Status) error {
	return r.writeStatusWithDBOptions(ctx, database, status, true)
}

func (r *Runner) writeStatusWithDBOptions(ctx context.Context, database db.IDatabase, status Status, refreshLock bool) error {
	if refreshLock {
		if err := r.refreshLockWithDB(ctx, database); err != nil {
			return err
		}
	}
	migrationType := strings.TrimSpace(status.Type)
	if migrationType == "" {
		migrationType = r.migrationType
	}
	now := r.now().UTC()
	if handled, err := r.writeStatusWithUpsert(ctx, database, migrationType, status, now); handled {
		return err
	}
	return r.writeCompatStatusWithDB(ctx, database, migrationType, status, now)
}

func (r *Runner) writeStatusWithUpsert(ctx context.Context, database db.IDatabase, migrationType string, status Status, now time.Time) (bool, error) {
	var query string
	switch r.dialect.Name() {
	case dialect.NameSQLite, dialect.NamePostgres:
		query = fmt.Sprintf(
			"INSERT INTO %s (migration_type, version, dirty, updated_at) VALUES (?, ?, ?, ?) "+
				"ON CONFLICT (migration_type) DO UPDATE SET version = excluded.version, dirty = excluded.dirty, updated_at = excluded.updated_at",
			r.quotedStateTable(),
		)
	case dialect.NameMySQL:
		query = fmt.Sprintf(
			"INSERT INTO %s (migration_type, version, dirty, updated_at) VALUES (?, ?, ?, ?) "+
				"ON DUPLICATE KEY UPDATE version = VALUES(version), dirty = VALUES(dirty), updated_at = VALUES(updated_at)",
			r.quotedStateTable(),
		)
	default:
		return false, nil
	}
	if _, err := database.Exec(ctx, query, migrationType, status.Version, boolToInt(status.Dirty), now); err != nil {
		return true, goerrors.Wrap(err, goerrors.Database, "upsert migration status failed").
			WithContext("table", r.stateTable)
	}
	return true, nil
}

func (r *Runner) writeCompatStatusWithDB(ctx context.Context, database db.IDatabase, migrationType string, status Status, now time.Time) error {
	query := fmt.Sprintf("SELECT COUNT(1) FROM %s WHERE migration_type = ?", r.quotedStateTable())
	row := database.QueryRow(ctx, query, migrationType)
	var count int
	if err := row.Scan(&count); err != nil {
		return goerrors.Wrap(err, goerrors.Database, "read migration state existence failed").
			WithContext("table", r.stateTable)
	}
	if count == 0 {
		insert := fmt.Sprintf(
			"INSERT INTO %s (migration_type, version, dirty, updated_at) VALUES (?, ?, ?, ?)",
			r.quotedStateTable(),
		)
		if _, err := database.Exec(ctx, insert, migrationType, status.Version, boolToInt(status.Dirty), now); err != nil {
			return goerrors.Wrap(err, goerrors.Database, "insert migration status failed").
				WithContext("table", r.stateTable)
		}
		return nil
	}
	return r.updateStatusRow(ctx, database, migrationType, status, now)
}

func (r *Runner) updateStatusRow(ctx context.Context, database db.IDatabase, migrationType string, status Status, now time.Time) error {
	update := fmt.Sprintf(
		"UPDATE %s SET version = ?, dirty = ?, updated_at = ? WHERE migration_type = ?",
		r.quotedStateTable(),
	)
	if _, err := database.Exec(ctx, update, status.Version, boolToInt(status.Dirty), now, migrationType); err != nil {
		return goerrors.Wrap(err, goerrors.Database, "update migration status failed").
			WithContext("table", r.stateTable)
	}
	return nil
}

func (r *Runner) beginExclusiveOperation(operation string) error {
	r.mu.Lock()
	if !r.withLockActive {
		return nil
	}
	r.mu.Unlock()
	return goerrors.NewCode(goerrors.Conflict, "runner operation cannot be called while WithLock callback is active").
		WithContext("operation", operation)
}

func (r *Runner) quotedStateTable() string {
	return r.dialect.QuoteIdentifier(r.stateTable)
}

func (r *Runner) list(ctx context.Context) ([]Migration, error) {
	migrations, err := r.source.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Migration, 0, len(migrations))
	for _, migration := range migrations {
		migrationType := migration.Type
		if migrationType == "" {
			migrationType = defaultMigrationType
		}
		if migrationType == r.migrationType {
			out = append(out, migration)
		}
	}
	return out, nil
}

func (r *Runner) stateColumns(ctx context.Context) (map[string]bool, error) {
	query := fmt.Sprintf("SELECT * FROM %s WHERE 1 = 0", r.quotedStateTable())
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, goerrors.Wrap(err, goerrors.Database, "inspect migration state table columns failed").
			WithContext("table", r.stateTable)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, goerrors.Wrap(err, goerrors.Database, "inspect migration state table columns failed").
			WithContext("table", r.stateTable)
	}
	out := make(map[string]bool, len(columns))
	for _, column := range columns {
		out[strings.ToLower(strings.TrimSpace(column))] = true
	}
	return out, nil
}

func validateStateTableColumns(table string, columns map[string]bool) error {
	for _, column := range []string{"migration_type", "version", "dirty", "updated_at"} {
		if columns[column] {
			continue
		}
		return goerrors.NewCode(goerrors.InvalidInput, "migration state table is not gochen format").
			WithContext("table", table).
			WithContext("missing_column", column)
	}
	return nil
}

func rowsAffected(result sql.Result) int64 {
	if result == nil {
		return 0
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0
	}
	return n
}

func waitForLockRetry(ctx context.Context, deadline time.Time, timeout time.Duration) error {
	if timeout <= 0 || !time.Now().Before(deadline) {
		return ErrLocked
	}
	wait := 10 * time.Millisecond
	remaining := time.Until(deadline)
	if remaining < wait {
		wait = remaining
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func boolToInt(flag bool) int {
	if flag {
		return 1
	}
	return 0
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return contextx.Background()
	}
	return ctx
}

func isMissingStateTableError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "no such table"),
		strings.Contains(text, "does not exist"),
		strings.Contains(text, "unknown table"):
		return true
	default:
		return false
	}
}
