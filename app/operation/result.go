package operation

// Operation 描述一次写操作当前对外可观察的执行状态。
type Operation struct {
	ID     string `json:"id,omitempty"`
	Type   string `json:"type"`
	Mode   Mode   `json:"mode"`
	Status Status `json:"status"`
}

// OperationError 表示结构化的 operation 错误信息。
type OperationError struct {
	Code    string         `json:"code,omitempty"`
	Message string         `json:"message,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// Result 表示统一的写操作 envelope。
type Result struct {
	Operation      Operation       `json:"operation"`
	Resource       *Resource       `json:"resource,omitempty"`
	Result         map[string]any  `json:"result,omitempty"`
	Error          *OperationError `json:"error,omitempty"`
	AffectedScopes []string        `json:"affected_scopes,omitempty"`
	StatusURL      string          `json:"status_url,omitempty"`
	StreamURL      string          `json:"stream_url,omitempty"`
	RetryAfterMs   int             `json:"retry_after_ms,omitempty"`
}

// MergeResult 返回一份新的 result，并按“result > resource > spec”的优先级补齐资源信息。
func MergeResult(result *Result, spec *Spec, resource *Resource) *Result {
	if result == nil {
		result = &Result{}
	}
	merged := *result
	merged.Resource = mergeResource(result.Resource, resource, specResource(spec))
	return &merged
}

func mergeResource(candidates ...*Resource) *Resource {
	var merged *Resource
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		if merged == nil {
			copyValue := *candidate
			merged = &copyValue
			continue
		}
		if merged.Type == "" {
			merged.Type = candidate.Type
		}
		if merged.ID == "" {
			merged.ID = candidate.ID
		}
	}
	if merged != nil && merged.Type == "" && merged.ID == "" {
		return nil
	}
	return merged
}

func specResource(spec *Spec) *Resource {
	if spec == nil {
		return nil
	}
	return spec.Resource
}
