package aggregatecheck

import (
	"gochen/errors"
	"gochen/internal/depcheck"
)

func validateNotNilAggregate(value any, label, aggregateType string) error {
	if depcheck.IsNil(value) {
		return errors.NewCode(errors.InvalidInput, label+" cannot be nil").
			WithContext("aggregate_type", aggregateType)
	}
	return nil
}

// ValidateSample 校验装配期聚合样本非空。
func ValidateSample(sample any, aggregateType string) error {
	return validateNotNilAggregate(sample, "aggregate sample", aggregateType)
}

// ValidateInstance 校验运行期聚合实例非空。
func ValidateInstance(aggregate any, aggregateType string) error {
	return validateNotNilAggregate(aggregate, "aggregate", aggregateType)
}
