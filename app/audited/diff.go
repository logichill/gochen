package audited

import (
	"encoding/json"
	"time"

	"gochen/errors"
)

func marshalAuditSnapshotResult[T any](entity T) (json.RawMessage, error) {
	changes, err := json.Marshal(entity)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "marshal audit snapshot")
	}
	return changes, nil
}

func (s *Application[T, ID]) marshalAuditSnapshot(entity T) (json.RawMessage, error) {
	target, err := s.projectForAudit(entity)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "audit projection failed")
	}
	return marshalAuditSnapshotResult(target)
}

// restoreManagedUpdateFields 防止普通更新覆盖由审计仓储管理的字段。
func (s *Application[T, ID]) restoreManagedUpdateFields(before, entity T) error {
	beforeAudited, err := s.asAuditedEntity(before)
	if err != nil {
		return err
	}
	audited, err := s.asAuditedEntity(entity)
	if err != nil {
		return err
	}
	audited.SetCreatedInfo(beforeAudited.GetCreatedBy(), beforeAudited.GetCreatedAt())
	audited.SetUpdatedAt(beforeAudited.GetUpdatedAt())
	if setter, ok := audited.(interface{ SetUpdatedBy(string) }); ok {
		setter.SetUpdatedBy(beforeAudited.GetUpdatedBy())
	} else if audited.GetUpdatedBy() != beforeAudited.GetUpdatedBy() {
		return errors.NewCode(errors.InvalidInput, "updated_by is managed by the audited repository")
	}
	if setter, ok := audited.(interface {
		SetDeletedInfo(*time.Time, *string)
	}); ok {
		setter.SetDeletedInfo(beforeAudited.GetDeletedAt(), beforeAudited.GetDeletedBy())
	} else if !sameOptionalTime(audited.GetDeletedAt(), beforeAudited.GetDeletedAt()) ||
		!sameOptionalString(audited.GetDeletedBy(), beforeAudited.GetDeletedBy()) {
		return errors.NewCode(errors.InvalidInput, "delete fields are managed by the audited application")
	}
	return nil
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
