package naming

import "unicode"

// SnakeCase converts Go-style identifiers to snake_case while keeping
// consecutive initialisms together, e.g. ManagedScopeID -> managed_scope_id.
func SnakeCase(in string) string {
	if in == "" {
		return ""
	}
	runes := []rune(in)
	out := make([]rune, 0, len(runes)+4)
	upperSeqLen := 0
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
				if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower && upperSeqLen > 1) {
					out = append(out, '_')
				}
			}
			out = append(out, unicode.ToLower(r))
			upperSeqLen++
			continue
		}
		upperSeqLen = 0
		if r == '-' {
			out = append(out, '_')
			continue
		}
		out = append(out, unicode.ToLower(r))
	}
	return string(out)
}
