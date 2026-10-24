package operation

import (
	"gochen/contextx"
)

func (r *Runner) failedResult(spec *Spec, operationID string, result *Result, err error) *Result {
	status := StatusFailed
	if contextx.IsAfterCommitError(err) {
		status = StatusDegraded
	}
	return r.buildMergedResult(spec, operationID, result, status, operationErrorFromError(err))
}

func (r *Runner) buildMergedResult(spec *Spec, operationID string, result *Result, status Status, opErr *OperationError) *Result {
	if result == nil {
		result = &Result{}
	}
	result = CloneResult(result)
	if opErr == nil {
		opErr = result.Error
	}
	merged := &Result{
		Resource:       mergeResource(result.Resource, spec.Resource),
		Result:         result.Result,
		Error:          opErr,
		AffectedScopes: mergeScopes(spec.AffectedScopes, result.AffectedScopes),
		StatusURL:      result.StatusURL,
		StreamURL:      result.StreamURL,
		RetryAfterMs:   result.RetryAfterMs,
	}
	merged.Operation = Operation{
		ID:     operationID,
		Type:   spec.Type,
		Mode:   spec.Mode,
		Status: status,
	}
	if spec.Mode == ModeTracked {
		if merged.StatusURL == "" && r.statusURLBuilder != nil {
			merged.StatusURL = r.statusURLBuilder(merged.Operation.ID)
		}
		if merged.StreamURL == "" && r.streamURLBuilder != nil {
			merged.StreamURL = r.streamURLBuilder(merged.Operation.ID)
		}
		if merged.RetryAfterMs == 0 {
			merged.RetryAfterMs = r.defaultRetryAfterMs
		}
	}
	return merged
}
