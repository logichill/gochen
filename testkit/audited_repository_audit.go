package testkit

import (
	"context"
	"strings"

	"gochen/domain/audited"
	"gochen/errors"
)

// SaveAuditRecord 保存一条审计记录并返回生成的记录 ID。
func (r *AuditedMemoryRepository[T, ID]) SaveAuditRecord(ctx context.Context, record audited.AuditRecord) (int64, error) {
	if err := validateContext(ctx); err != nil {
		return 0, err
	}
	if err := validateAuditRecord(record); err != nil {
		return 0, err
	}
	if transactionState, ok := r.auditedTransactionFromContext(ctx); ok {
		transaction := transactionState.transaction
		transaction.mu.Lock()
		defer transaction.mu.Unlock()
		if transaction.closed {
			return 0, errors.NewCode(errors.FailedPrecondition, "audited memory transaction already closed")
		}
		return appendAuditRecord(transaction.audits, &transaction.nextAuditID, record), nil
	}
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	id := appendAuditRecord(r.audits, &r.nextAuditID, record)
	r.auditRevision++
	return id, nil
}

// SaveAuditRecords 原子保存一批审计记录。
func (r *AuditedMemoryRepository[T, ID]) SaveAuditRecords(ctx context.Context, records []audited.AuditRecord) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	for _, record := range records {
		if err := validateAuditRecord(record); err != nil {
			return err
		}
	}
	if len(records) == 0 {
		return nil
	}
	if transactionState, ok := r.auditedTransactionFromContext(ctx); ok {
		transaction := transactionState.transaction
		transaction.mu.Lock()
		defer transaction.mu.Unlock()
		if transaction.closed {
			return errors.NewCode(errors.FailedPrecondition, "audited memory transaction already closed")
		}
		for _, record := range records {
			appendAuditRecord(transaction.audits, &transaction.nextAuditID, record)
		}
		return nil
	}
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	for _, record := range records {
		appendAuditRecord(r.audits, &r.nextAuditID, record)
	}
	r.auditRevision++
	return nil
}

// ListAuditRecordsByEntity 按实体 ID 返回审计记录分页结果。
func (r *AuditedMemoryRepository[T, ID]) ListAuditRecordsByEntity(ctx context.Context, entityID string, offset, limit int) ([]audited.AuditRecord, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return nil, errors.NewCode(errors.InvalidInput, "audit entity ID is required")
	}
	var records []audited.AuditRecord
	if transactionState, ok := r.auditedTransactionFromContext(ctx); ok {
		transaction := transactionState.transaction
		transaction.mu.Lock()
		defer transaction.mu.Unlock()
		if transaction.closed {
			return nil, errors.NewCode(errors.FailedPrecondition, "audited memory transaction already closed")
		}
		records = cloneAuditRecords(transaction.audits[entityID])
	} else {
		r.auditMu.RLock()
		records = cloneAuditRecords(r.audits[entityID])
		r.auditMu.RUnlock()
	}
	start, end := pageBounds(len(records), offset, limit)
	return records[start:end], nil
}

func validateAuditRecord(record audited.AuditRecord) error {
	if strings.TrimSpace(record.EntityID) == "" {
		return errors.NewCode(errors.InvalidInput, "audit entity ID is required")
	}
	if strings.TrimSpace(string(record.Operation)) == "" {
		return errors.NewCode(errors.InvalidInput, "audit operation is required")
	}
	if strings.TrimSpace(record.Operator) == "" {
		return errors.NewCode(errors.InvalidInput, "audit operator is required")
	}
	return nil
}

func appendAuditRecord(records map[string][]audited.AuditRecord, nextID *int64, record audited.AuditRecord) int64 {
	record.EntityID = strings.TrimSpace(record.EntityID)
	record.ID = *nextID
	*nextID = *nextID + 1
	records[record.EntityID] = append(records[record.EntityID], cloneAuditRecord(record))
	return record.ID
}

func cloneAuditMap(records map[string][]audited.AuditRecord) map[string][]audited.AuditRecord {
	cloned := make(map[string][]audited.AuditRecord, len(records))
	for entityID, entityRecords := range records {
		cloned[entityID] = cloneAuditRecords(entityRecords)
	}
	return cloned
}

func cloneAuditRecords(records []audited.AuditRecord) []audited.AuditRecord {
	cloned := make([]audited.AuditRecord, len(records))
	for i, record := range records {
		cloned[i] = cloneAuditRecord(record)
	}
	return cloned
}

func cloneAuditRecord(record audited.AuditRecord) audited.AuditRecord {
	record.Changes = append([]byte(nil), record.Changes...)
	record.Metadata = append([]byte(nil), record.Metadata...)
	return record
}
