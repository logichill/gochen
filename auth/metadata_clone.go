package auth

func cloneMetadataMap(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = cloneMetadataValue(value)
	}
	return cloned
}

func cloneMetadataValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return cloneMetadataMap(v)
	case []any:
		if len(v) == 0 {
			return []any(nil)
		}
		cloned := make([]any, len(v))
		for i, item := range v {
			cloned[i] = cloneMetadataValue(item)
		}
		return cloned
	case []string:
		return append([]string(nil), v...)
	case []byte:
		return append([]byte(nil), v...)
	case map[string]string:
		if len(v) == 0 {
			return map[string]string(nil)
		}
		cloned := make(map[string]string, len(v))
		for key, item := range v {
			cloned[key] = item
		}
		return cloned
	default:
		return value
	}
}
