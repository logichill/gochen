package operation

import (
	"strings"
	"unicode"

	"gochen/errors"
)

// Resource 标识本次写操作天然关联的资源。
type Resource struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
}

const maxIdempotencyKeyLength = 256

// Spec 描述一次写操作的协议元数据。
type Spec struct {
	Type           string         `json:"type"`
	Mode           Mode           `json:"mode"`
	Resource       *Resource      `json:"resource,omitempty"`
	AffectedScopes []string       `json:"affected_scopes,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	SettlementHint any            `json:"settlement_hint,omitempty"`
}

// Validate 校验 Spec 是否满足最小约束。
func (s *Spec) Validate() error {
	if s == nil {
		return errors.NewCode(errors.InvalidInput, "operation spec cannot be nil")
	}
	if strings.TrimSpace(s.Type) == "" {
		return errors.NewCode(errors.InvalidInput, "operation spec type cannot be empty")
	}
	if !s.Mode.IsValid() {
		return errors.NewCode(errors.InvalidInput, "operation spec mode is invalid").
			WithContext("mode", string(s.Mode))
	}
	idempotencyKey := strings.TrimSpace(s.IdempotencyKey)
	if idempotencyKey != "" && s.Mode != ModeTracked {
		return errors.NewCode(errors.InvalidInput, "operation idempotency key requires tracked mode").
			WithContext("mode", string(s.Mode))
	}
	if idempotencyKey != "" {
		if len(idempotencyKey) > maxIdempotencyKeyLength {
			return errors.NewCode(errors.InvalidInput, "operation idempotency key is too long").
				WithContext("max_length", maxIdempotencyKeyLength)
		}
		if idempotencyKey != s.IdempotencyKey {
			return errors.NewCode(errors.InvalidInput, "operation idempotency key cannot have leading or trailing whitespace")
		}
		if containsSpaceOrControl(idempotencyKey) {
			return errors.NewCode(errors.InvalidInput, "operation idempotency key contains invalid characters")
		}
	}
	return nil
}

func containsSpaceOrControl(value string) bool {
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return false
}
