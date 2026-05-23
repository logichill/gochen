package repo

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gochen/db/orm"
	"gochen/domain/audited"
	"gochen/errors"
	"gochen/ident"
)

// AuditStore 基于 gochen/db/orm 的审计记录存储实现。
//
// 说明：
// - 该实现会自动探测 ctx 中的事务会话（`orm.SessionFromContext`），确保可与 ORM Repo 在同一事务中提交；
// - 为统一不同方言与适配器行为，默认使用框架 ID 生成器写入 record.ID（避免依赖自增回填）。
type AuditStore struct {
	orm   orm.IOrm
	model orm.IModel
	meta  *orm.ModelMeta
	idGen ident.IGenerator[int64]
}

type auditStoreRow struct {
	ID        int64     `json:"id"`
	EntityID  string    `json:"entity_id"`
	Operation string    `json:"operation"`
	Operator  string    `json:"operator"`
	Timestamp time.Time `json:"timestamp"`
	Changes   string    `json:"changes"`
	Metadata  string    `json:"metadata"`
}

// NewAuditStore 创建审计存储。
func NewAuditStore(ormEngine orm.IOrm, tableName string, opts ...func(*AuditStore)) (*AuditStore, error) {
	if ormEngine == nil {
		return nil, errors.NewCode(errors.InvalidInput, "orm cannot be nil")
	}
	tableName = strings.TrimSpace(tableName)
	if tableName == "" {
		return nil, errors.NewCode(errors.InvalidInput, "table name cannot be empty")
	}

	store := &AuditStore{
		orm:   ormEngine,
		meta:  &orm.ModelMeta{ModelFactory: orm.NewModelFactory[auditStoreRow](), Table: tableName},
		idGen: ident.DefaultInt64Generator(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(store)
		}
	}
	if store.idGen == nil {
		store.idGen = ident.DefaultInt64Generator()
	}
	model, err := ormEngine.Model(store.meta)
	if err != nil {
		return nil, err
	}
	store.model = model
	return store, nil
}

// WithAuditIDGenerator 配置审计记录 ID 生成器（可选）。
func WithAuditIDGenerator(gen ident.IGenerator[int64]) func(*AuditStore) {
	return func(s *AuditStore) {
		s.idGen = gen
	}
}

func (s *AuditStore) modelFor(ctx context.Context) (orm.IModel, error) {
	if session, ok := orm.SessionFromContext(ctx); ok && session != nil {
		return session.Model(s.meta)
	}
	return s.model, nil
}

// SaveAuditRecord 写入一条审计记录。
func (s *AuditStore) SaveAuditRecord(ctx context.Context, record audited.AuditRecord) (int64, error) {
	if ctx == nil {
		return 0, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if s == nil {
		return 0, errors.NewCode(errors.Internal, "audit store is nil")
	}
	if strings.TrimSpace(record.EntityID) == "" {
		return 0, errors.NewCode(errors.InvalidInput, "entityID cannot be empty")
	}
	if strings.TrimSpace(string(record.Operation)) == "" {
		return 0, errors.NewCode(errors.InvalidInput, "operation cannot be empty")
	}
	if strings.TrimSpace(record.Operator) == "" {
		return 0, errors.NewCode(errors.InvalidInput, "operator cannot be empty")
	}

	if record.ID == 0 {
		id, err := s.idGen.Next()
		if err != nil {
			return 0, err
		}
		record.ID = id
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now()
	}

	changesJSON := "{}"
	if len(record.Changes) > 0 {
		if !json.Valid(record.Changes) {
			return 0, errors.NewCode(errors.InvalidInput, "invalid audit record changes")
		}
		changesJSON = string(record.Changes)
	}

	metadataJSON := "{}"
	if len(record.Metadata) > 0 {
		if !json.Valid(record.Metadata) {
			return 0, errors.NewCode(errors.InvalidInput, "invalid audit record metadata")
		}
		metadataJSON = string(record.Metadata)
	}

	row := auditStoreRow{
		ID:        record.ID,
		EntityID:  record.EntityID,
		Operation: string(record.Operation),
		Operator:  record.Operator,
		Timestamp: record.Timestamp,
		Changes:   changesJSON,
		Metadata:  metadataJSON,
	}
	m, err := s.modelFor(ctx)
	if err != nil {
		return 0, err
	}
	if err := m.Create(ctx, row); err != nil {
		return 0, err
	}
	return record.ID, nil
}

// ListAuditRecordsByEntity 从存储中查询数据。
//
// 参数：
// - offset：分页偏移量（从 0 开始）
// - limit：分页大小（最大返回条数）
func (s *AuditStore) ListAuditRecordsByEntity(ctx context.Context, entityID string, offset, limit int) ([]audited.AuditRecord, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if s == nil {
		return nil, errors.NewCode(errors.Internal, "audit store is nil")
	}
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return nil, errors.NewCode(errors.InvalidInput, "entityID cannot be empty")
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 100
	}

	var rows []auditStoreRow
	m, err := s.modelFor(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.Find(ctx, &rows,
		orm.WithWhere("entity_id = ?", entityID),
		orm.WithOrderBy("timestamp", true),
		orm.WithOrderBy("id", true),
		orm.WithOffset(offset),
		orm.WithLimit(limit),
	); err != nil {
		return nil, err
	}

	out := make([]audited.AuditRecord, 0, len(rows))
	for i := range rows {
		var changes json.RawMessage
		if raw := strings.TrimSpace(rows[i].Changes); raw != "" {
			b := []byte(raw)
			if !json.Valid(b) {
				return nil, errors.NewCode(errors.Internal, "failed to decode audit record changes").
					WithContext("audit_id", rows[i].ID)
			}
			changes = b
		}
		var metadata json.RawMessage
		if raw := strings.TrimSpace(rows[i].Metadata); raw != "" {
			b := []byte(raw)
			if !json.Valid(b) {
				return nil, errors.NewCode(errors.Internal, "failed to decode audit record metadata").
					WithContext("audit_id", rows[i].ID)
			}
			metadata = b
		}
		out = append(out, audited.AuditRecord{
			ID:        rows[i].ID,
			EntityID:  rows[i].EntityID,
			Operation: audited.AuditOperation(rows[i].Operation),
			Operator:  rows[i].Operator,
			Timestamp: rows[i].Timestamp,
			Changes:   changes,
			Metadata:  metadata,
		})
	}
	return out, nil
}
