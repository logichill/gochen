package testkit

import (
	"context"
	"reflect"
	"strings"

	"gochen/domain/audited"
	"gochen/domain/spec"
	"gochen/errors"
)

// SaveAuditRecord 保存一条审计记录并返回生成的记录 ID。
func (r *AuditedMemoryRepository[T, ID]) SaveAuditRecord(ctx context.Context, record audited.AuditRecord) (int64, error) {
	if err := validateContext(ctx); err != nil {
		return 0, err
	}
	prepared, err := r.prepareAuditRecord(ctx, record)
	if err != nil {
		return 0, err
	}
	if transactionState, ok := r.auditedTransactionFromContext(ctx); ok {
		transaction := transactionState.transaction
		transaction.mu.Lock()
		defer transaction.mu.Unlock()
		if transaction.closed {
			return 0, errors.NewCode(errors.FailedPrecondition, "audited memory transaction already closed")
		}
		if err := checkAuditUniqueness(transaction.audits, []memoryAuditRecord{prepared}); err != nil {
			return 0, err
		}
		return appendAuditRecord(transaction.audits, &transaction.nextAuditID, prepared), nil
	}
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	if err := checkAuditUniqueness(r.audits, []memoryAuditRecord{prepared}); err != nil {
		return 0, err
	}
	id := appendAuditRecord(r.audits, &r.nextAuditID, prepared)
	r.auditRevision++
	return id, nil
}

// SaveAuditRecords 原子保存一批审计记录。
func (r *AuditedMemoryRepository[T, ID]) SaveAuditRecords(ctx context.Context, records []audited.AuditRecord) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := audited.ValidateTransferPairs(records); err != nil {
		return err
	}
	prepared := make([]memoryAuditRecord, len(records))
	for i, record := range records {
		var err error
		prepared[i], err = r.prepareAuditRecord(ctx, record)
		if err != nil {
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
		if err := checkAuditUniqueness(transaction.audits, prepared); err != nil {
			return err
		}
		for _, prepared := range prepared {
			appendAuditRecord(transaction.audits, &transaction.nextAuditID, prepared)
		}
		return nil
	}
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	if err := checkAuditUniqueness(r.audits, prepared); err != nil {
		return err
	}
	for _, prepared := range prepared {
		appendAuditRecord(r.audits, &r.nextAuditID, prepared)
	}
	r.auditRevision++
	return nil
}

// ListAuditRecords 按实体 ID 与规约返回审计记录分页结果。
//
// 规约通过实体标识与完整维度快照求值，支持实体物理删除后的历史读取。
func (r *AuditedMemoryRepository[T, ID]) ListAuditRecords(ctx context.Context, entityID string, offset, limit int, s spec.Spec) ([]audited.AuditRecord, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return nil, errors.NewCode(errors.InvalidInput, "audit entity ID is required")
	}
	if err := s.Err(); err != nil {
		return nil, errors.Wrap(err, errors.Internal, "invalid audit spec")
	}
	if s.HasNamed() {
		return nil, errors.NewCode(errors.Internal, "audit spec must not contain named conditions")
	}
	for _, ref := range s.Refs() {
		field, err := r.metadata.Resolve(ref)
		if err != nil {
			return nil, err
		}
		if !field.Identity && (field.Dimension == "" || !r.execution.IsProtected(field) || field.Name == r.execution.IsolationField()) {
			return nil, errors.NewCode(errors.Internal, "audit condition references an unauthorized field").WithContext("reference", ref.String())
		}
	}
	var records []memoryAuditRecord
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
	interpreter := spec.NewInterpreter()
	visible := make([]audited.AuditRecord, 0, len(records))
	for _, record := range records {
		if name := r.execution.IsolationField(); name != "" {
			field, _ := r.metadata.Field(name)
			isolation, _, err := r.isolationID(ctx)
			if err != nil {
				return nil, err
			}
			expected, err := field.Convert(isolation)
			if err != nil {
				return nil, err
			}
			current := record.isolation
			for expected.Kind() == reflect.Ptr {
				expected = expected.Elem()
			}
			if !reflect.DeepEqual(current, expected.Interface()) {
				continue
			}
		}
		if kind := strings.TrimSpace(audited.AuditResourceKind(ctx)); kind != "" && record.ResourceKind != kind {
			continue
		}
		normalized, err := audited.NewDimensionSnapshot(r.metadata, record.Dimensions.Values())
		if err != nil {
			return nil, errors.Wrap(err, errors.Internal, "incomplete audit snapshot")
		}
		record.Dimensions = normalized
		ok, matchErr := interpreter.MatchSource(s, record.Dimensions.Source(r.metadata, record.EntityID))
		if matchErr != nil {
			return nil, matchErr
		}
		if ok {
			visible = append(visible, record.AuditRecord)
		}
	}
	start, end := pageBounds(len(visible), offset, limit)
	return visible[start:end], nil
}

func appendAuditRecord(records map[string][]memoryAuditRecord, nextID *int64, record memoryAuditRecord) int64 {
	record.EntityID = strings.TrimSpace(record.EntityID)
	record.ID = *nextID
	*nextID = *nextID + 1
	records[record.EntityID] = append(records[record.EntityID], cloneStoredAuditRecord(record))
	return record.ID
}

func cloneAuditMap(records map[string][]memoryAuditRecord) map[string][]memoryAuditRecord {
	cloned := make(map[string][]memoryAuditRecord, len(records))
	for entityID, entityRecords := range records {
		cloned[entityID] = cloneAuditRecords(entityRecords)
	}
	return cloned
}

func cloneAuditRecords(records []memoryAuditRecord) []memoryAuditRecord {
	cloned := make([]memoryAuditRecord, len(records))
	for i, record := range records {
		cloned[i] = cloneStoredAuditRecord(record)
	}
	return cloned
}

func cloneAuditRecord(record audited.AuditRecord) audited.AuditRecord {
	record.Changes = append([]byte(nil), record.Changes...)
	record.Metadata = append([]byte(nil), record.Metadata...)
	return record
}

type memoryAuditRecord struct {
	audited.AuditRecord
	isolation any
}

func checkAuditUniqueness(stored map[string][]memoryAuditRecord, incoming []memoryAuditRecord) error {
	type key struct{ operation, resource, entity, action string }
	seen := map[key]bool{}
	remember := func(record memoryAuditRecord) error {
		if record.Action != "transfer_out" && record.Action != "transfer_in" {
			return nil
		}
		k := key{record.OperationID, record.ResourceKind, record.EntityID, record.Action}
		if seen[k] {
			return errors.NewCode(errors.Conflict, "transfer audit already exists")
		}
		seen[k] = true
		return nil
	}
	for _, records := range stored {
		for _, record := range records {
			if err := remember(record); err != nil {
				return err
			}
		}
	}
	for _, record := range incoming {
		if err := remember(record); err != nil {
			return err
		}
	}
	return nil
}

func cloneStoredAuditRecord(record memoryAuditRecord) memoryAuditRecord {
	record.AuditRecord = cloneAuditRecord(record.AuditRecord)
	return record
}

func (r *AuditedMemoryRepository[T, ID]) prepareAuditRecord(ctx context.Context, record audited.AuditRecord) (memoryAuditRecord, error) {
	if err := audited.ValidateRecord(record, r.metadata); err != nil {
		return memoryAuditRecord{}, err
	}
	snapshot, err := audited.NewDimensionSnapshot(r.metadata, record.Dimensions.Values())
	if err != nil {
		return memoryAuditRecord{}, err
	}
	record.Dimensions = snapshot
	out := memoryAuditRecord{AuditRecord: record}
	if name := r.execution.IsolationField(); name != "" {
		key, _, err := r.isolationID(ctx)
		if err != nil {
			return memoryAuditRecord{}, err
		}
		field, _ := r.metadata.Field(name)
		converted, err := field.Convert(key)
		if err != nil {
			return memoryAuditRecord{}, err
		}
		for converted.Kind() == reflect.Ptr {
			converted = converted.Elem()
		}
		out.isolation = converted.Interface()
		if field.Dimension != "" {
			value, _ := record.Dimensions.Value(field.Dimension)
			if !reflect.DeepEqual(value, out.isolation) {
				return memoryAuditRecord{}, errors.NewCode(errors.Forbidden, "audit isolation snapshot differs from request")
			}
		}
	}
	return out, nil
}
