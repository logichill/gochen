package app

import (
	"gochen/auth"
	"gochen/errors"
	"gochen/internal/depcheck"
	"reflect"
)

// ApplyAssignments 依据有效创建能力落位服务端权威赋值，拒绝已有字段冲突。
func ApplyAssignments(entity any, assignments []auth.Assignment, execution ExecutionDescriptor) error {
	if len(assignments) == 0 {
		return nil
	}
	if depcheck.IsNil(entity) || execution.Metadata() == nil {
		return errors.NewCode(errors.InvalidInput, "assignment requires an entity and bound resource metadata")
	}
	for _, assignment := range assignments {
		if err := assignment.Validate(); err != nil {
			return err
		}
		field, ok := execution.Metadata().DimensionField(assignment.Dimension)
		if !ok {
			return errors.NewCode(errors.Internal, "entity does not expose the assigned role").WithContext("role", string(assignment.Dimension))
		}
		converted, err := field.Convert(assignment.Value)
		if err != nil {
			return errors.Wrap(err, errors.InvalidInput, "assignment value conversion failed").WithContext("role", string(assignment.Dimension))
		}
		if !execution.CanAssign(assignment.Dimension) {
			return errors.NewCode(errors.Internal, "create assignment lacks resource capability")
		}
		current, ok := field.Value(entity)
		if !ok {
			return errors.NewCode(errors.Internal, "assignment target field cannot be read").WithContext("role", string(assignment.Dimension))
		}
		if !current.CanSet() {
			return errors.NewCode(errors.Internal, "assignment target field cannot be set").WithContext("role", string(assignment.Dimension))
		}
		if current.IsZero() {
			current.Set(converted)
			continue
		}
		if !reflect.DeepEqual(current.Interface(), converted.Interface()) {
			return errors.NewCode(errors.Forbidden, "assignment conflicts with an existing field value").WithContext("role", string(assignment.Dimension))
		}
	}
	return nil
}
