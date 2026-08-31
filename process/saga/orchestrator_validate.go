package saga

import "fmt"

func validateSagaSteps(steps []*SagaStep) error {
	if len(steps) == 0 {
		return fmt.Errorf("saga has no steps")
	}

	seenNames := make(map[string]struct{}, len(steps))
	for i, step := range steps {
		if step == nil {
			return fmt.Errorf("step %d is nil", i)
		}
		if step.Name == "" {
			return fmt.Errorf("step %d name is empty", i)
		}
		if _, exists := seenNames[step.Name]; exists {
			return fmt.Errorf("step %q is duplicated", step.Name)
		}
		seenNames[step.Name] = struct{}{}
		if step.Command == nil {
			return fmt.Errorf("step %q command is nil", step.Name)
		}
	}

	return nil
}
