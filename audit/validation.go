package audit

import (
	"bytes"
	"encoding/json"
	"strings"

	"gochen/domain/meta"
	"gochen/errors"
)

// ValidateRecord 校验审计证据的身份、载荷与完整维度事实。
func ValidateRecord(record AuditRecord, metadata *meta.Metadata) error {
	if strings.TrimSpace(record.EntityID) == "" || strings.TrimSpace(record.ResourceKind) == "" ||
		strings.TrimSpace(record.Action) == "" || strings.TrimSpace(record.Operator) == "" || strings.TrimSpace(record.OperationID) == "" {
		return errors.NewCode(errors.InvalidInput, "audit record requires resource, entity, action, operator and operation id")
	}
	for _, payload := range []json.RawMessage{record.Changes, record.Metadata} {
		if len(payload) > 0 && !json.Valid(payload) {
			return errors.NewCode(errors.InvalidInput, "invalid audit payload")
		}
	}
	_, err := NewDimensionSnapshot(metadata, record.Dimensions.Values())
	return err
}

// ValidateTransferPairs 要求同批迁移具有唯一的转出/接收记录及一致披露。
func ValidateTransferPairs(records []AuditRecord) error {
	type key struct{ operation, resource, entity string }
	type pair struct{ out, in *AuditRecord }
	pairs := map[key]pair{}
	for i := range records {
		record := &records[i]
		if record.Action != "transfer_out" && record.Action != "transfer_in" {
			continue
		}
		k := key{record.OperationID, record.ResourceKind, record.EntityID}
		p := pairs[k]
		if record.Action == "transfer_out" {
			if p.out != nil {
				return errors.NewCode(errors.InvalidInput, "duplicate transfer in operation")
			}
			p.out = record
		} else {
			if p.in != nil {
				return errors.NewCode(errors.InvalidInput, "duplicate transfer in operation")
			}
			p.in = record
		}
		pairs[k] = p
	}
	for _, p := range pairs {
		if p.out == nil || p.in == nil || !bytes.Equal(p.out.Changes, p.in.Changes) {
			return errors.NewCode(errors.InvalidInput, "transfer audit pair is incomplete or inconsistent")
		}
	}
	return nil
}
