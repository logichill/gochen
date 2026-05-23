package sqlstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gochen/auth"
	"gochen/db"
	"gochen/errors"
)

// AuthzLogStore 表示基于 db.IDatabase 的 authz log 持久化实现。
type AuthzLogStore struct {
	db        db.IDatabase
	tableName string
}

// NewAuthzLogStore 创建 SQL authz log store。
func NewAuthzLogStore(database db.IDatabase, tableName string) (*AuthzLogStore, error) {
	if database == nil {
		return nil, errors.NewCode(errors.InvalidInput, "authz log database cannot be nil")
	}
	normalizedTableName, err := normalizeSQLTableName(database, tableName, "authz_logs")
	if err != nil {
		return nil, err
	}
	return &AuthzLogStore{
		db:        database,
		tableName: normalizedTableName,
	}, nil
}

// SaveAuthzLogEntry 保存 authz 日志。
func (s *AuthzLogStore) SaveAuthzLogEntry(ctx context.Context, entry auth.AuthzLogEntry) error {
	entry = normalizeAuthzLogEntry(entry)
	query := fmt.Sprintf(`INSERT INTO %s
		(id, entry_type, decision_id, principal_id, permission, operation, effect, reason_code, snapshot_version, snapshot_key, consistency, latency_ms, cache_hit, resources_json, execution_json, metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.tableName)
	if _, err := s.db.Exec(ctx, query,
		entry.ID,
		string(entry.Type),
		entry.DecisionID,
		entry.PrincipalID,
		entry.Permission,
		entry.Operation,
		entry.Effect,
		entry.ReasonCode,
		entry.SnapshotVersion,
		entry.SnapshotKey,
		string(entry.Consistency),
		entry.LatencyMs,
		entry.CacheHit,
		authzLogEntryResourcesJSON(entry),
		authzLogEntryExecutionJSON(entry),
		authzLogEntryMetadataJSON(entry),
		entry.Timestamp,
	); err != nil {
		return errors.Wrap(err, errors.Database, "insert authz log entry failed").WithContext("entry_id", entry.ID)
	}
	return nil
}

// ListAuthzLogEntries 列出 authz 日志。
func (s *AuthzLogStore) ListAuthzLogEntries(ctx context.Context, entryType auth.AuthzLogEntryType, limit int) ([]auth.AuthzLogEntry, error) {
	query := fmt.Sprintf(`SELECT id, entry_type, decision_id, principal_id, permission, operation, effect, reason_code, snapshot_version, snapshot_key, consistency, latency_ms, cache_hit, resources_json, execution_json, metadata_json, created_at FROM %s`, s.tableName)
	args := make([]any, 0, 2)
	if entryType = normalizeAuthzLogEntryType(entryType); entryType != "" {
		query += ` WHERE entry_type = ?`
		args = append(args, string(entryType))
	}
	query += ` ORDER BY created_at DESC, id DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, errors.Database, "list authz log entries failed")
	}
	defer rows.Close()

	result := make([]auth.AuthzLogEntry, 0)
	for rows.Next() {
		var (
			entry         auth.AuthzLogEntry
			entryTypeRaw  string
			consistency   string
			resourcesJSON string
			executionJSON string
			metadataJSON  string
			cacheHit      bool
		)
		if err := rows.Scan(
			&entry.ID,
			&entryTypeRaw,
			&entry.DecisionID,
			&entry.PrincipalID,
			&entry.Permission,
			&entry.Operation,
			&entry.Effect,
			&entry.ReasonCode,
			&entry.SnapshotVersion,
			&entry.SnapshotKey,
			&consistency,
			&entry.LatencyMs,
			&cacheHit,
			&resourcesJSON,
			&executionJSON,
			&metadataJSON,
			&entry.Timestamp,
		); err != nil {
			return nil, errors.Wrap(err, errors.Database, "scan authz log entry failed")
		}
		entry.Type = normalizeAuthzLogEntryType(auth.AuthzLogEntryType(entryTypeRaw))
		entry.Consistency = normalizeConsistencyMode(auth.ConsistencyMode(consistency))
		entry.CacheHit = cacheHit
		if strings.TrimSpace(resourcesJSON) != "" && resourcesJSON != "[]" {
			if err := json.Unmarshal([]byte(resourcesJSON), &entry.Resources); err != nil {
				return nil, errors.Wrap(err, errors.Internal, "decode authz log resources failed").WithContext("entry_id", entry.ID)
			}
		}
		if strings.TrimSpace(executionJSON) != "" && executionJSON != "{}" {
			if err := json.Unmarshal([]byte(executionJSON), &entry.Execution); err != nil {
				return nil, errors.Wrap(err, errors.Internal, "decode authz log execution failed").WithContext("entry_id", entry.ID)
			}
		}
		if strings.TrimSpace(metadataJSON) != "" && metadataJSON != "{}" {
			meta := make(map[string]any)
			if err := json.Unmarshal([]byte(metadataJSON), &meta); err != nil {
				return nil, errors.Wrap(err, errors.Internal, "decode authz log metadata failed").WithContext("entry_id", entry.ID)
			}
			if matchedRules, ok := meta["matched_rules"].([]any); ok && len(matchedRules) > 0 {
				entry.MatchedRules = make([]string, 0, len(matchedRules))
				for _, rule := range matchedRules {
					if text, ok := rule.(string); ok && strings.TrimSpace(text) != "" {
						entry.MatchedRules = append(entry.MatchedRules, strings.TrimSpace(text))
					}
				}
				delete(meta, "matched_rules")
			}
			if derived, ok := meta[snapshotVersionDerivedMetadataKey].(bool); ok {
				entry.SnapshotVersionDerived = derived
				delete(meta, snapshotVersionDerivedMetadataKey)
			}
			if len(meta) > 0 {
				entry.Metadata = meta
			}
		}
		entry = normalizeAuthzLogEntry(entry)
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, errors.Database, "iterate authz log entries failed")
	}
	return result, nil
}

// DeleteAuthzLogEntriesBefore 删除早于 cutoff 的日志。
func (s *AuthzLogStore) DeleteAuthzLogEntriesBefore(ctx context.Context, cutoff time.Time) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE created_at < ?`, s.tableName)
	if _, err := s.db.Exec(ctx, query, cutoff); err != nil {
		return errors.Wrap(err, errors.Database, "delete authz log entries failed")
	}
	return nil
}

func normalizeAuthzLogEntry(entry auth.AuthzLogEntry) auth.AuthzLogEntry {
	entry.ID = strings.TrimSpace(entry.ID)
	entry.Type = normalizeAuthzLogEntryType(entry.Type)
	if entry.Type == "" {
		entry.Type = auth.AuthzLogEntryTypeDecision
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	entry.DecisionID = strings.TrimSpace(entry.DecisionID)
	entry.PrincipalID = strings.TrimSpace(entry.PrincipalID)
	entry.Permission = strings.TrimSpace(entry.Permission)
	entry.Operation = strings.TrimSpace(entry.Operation)
	entry.Effect = strings.TrimSpace(entry.Effect)
	entry.ReasonCode = strings.TrimSpace(entry.ReasonCode)
	entry.SnapshotVersion = strings.TrimSpace(entry.SnapshotVersion)
	entry.SnapshotKey = strings.TrimSpace(entry.SnapshotKey)
	entry.Consistency = normalizeConsistencyMode(entry.Consistency)
	entry.MatchedRules = normalizeStrings(entry.MatchedRules)
	entry.Execution = normalizeExecutionMetadata(entry.Execution)
	if len(entry.Resources) > 0 {
		resources := make([]auth.AuthzLoggedResource, 0, len(entry.Resources))
		for _, resource := range entry.Resources {
			resources = append(resources, normalizeLoggedResource(resource))
		}
		entry.Resources = resources
	} else {
		entry.Resources = nil
	}
	if len(entry.Metadata) == 0 {
		entry.Metadata = nil
	}
	return entry
}

func normalizeLoggedResource(resource auth.AuthzLoggedResource) auth.AuthzLoggedResource {
	resource.Kind = strings.TrimSpace(resource.Kind)
	resource.ID = strings.TrimSpace(resource.ID)
	resource.ManagedScopeID = auth.NormalizePositiveID(resource.ManagedScopeID)
	resource.OwnerID = strings.TrimSpace(resource.OwnerID)
	resource.Revision = strings.TrimSpace(resource.Revision)
	return resource
}

func normalizeAuthzLogEntryType(entryType auth.AuthzLogEntryType) auth.AuthzLogEntryType {
	switch auth.AuthzLogEntryType(strings.TrimSpace(string(entryType))) {
	case auth.AuthzLogEntryTypeDecision:
		return auth.AuthzLogEntryTypeDecision
	case auth.AuthzLogEntryTypeWrite:
		return auth.AuthzLogEntryTypeWrite
	default:
		return ""
	}
}

func normalizeConsistencyMode(mode auth.ConsistencyMode) auth.ConsistencyMode {
	switch auth.ConsistencyMode(strings.TrimSpace(string(mode))) {
	case auth.ConsistencyModeStrong:
		return auth.ConsistencyModeStrong
	case auth.ConsistencyModeBoundedStaleness:
		return auth.ConsistencyModeBoundedStaleness
	default:
		return auth.ConsistencyModeUnspecified
	}
}

func normalizeExecutionMetadata(metadata auth.ExecutionMetadata) auth.ExecutionMetadata {
	metadata.InitiatorID = strings.TrimSpace(metadata.InitiatorID)
	metadata.ActorID = strings.TrimSpace(metadata.ActorID)
	metadata.RequestID = strings.TrimSpace(metadata.RequestID)
	metadata.DecisionID = strings.TrimSpace(metadata.DecisionID)
	metadata.EventID = strings.TrimSpace(metadata.EventID)
	metadata.JobID = strings.TrimSpace(metadata.JobID)
	return metadata
}

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func authzLogEntryResourcesJSON(entry auth.AuthzLogEntry) string {
	if len(entry.Resources) == 0 {
		return "[]"
	}
	b, err := json.Marshal(entry.Resources)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func authzLogEntryExecutionJSON(entry auth.AuthzLogEntry) string {
	b, err := json.Marshal(entry.Execution)
	if err != nil {
		return "{}"
	}
	return string(b)
}

const snapshotVersionDerivedMetadataKey = "__snapshot_version_derived__"

func authzLogEntryMetadataJSON(entry auth.AuthzLogEntry) string {
	if len(entry.Metadata) == 0 && len(entry.MatchedRules) == 0 && !entry.SnapshotVersionDerived {
		return "{}"
	}
	payload := map[string]any{}
	for k, v := range entry.Metadata {
		payload[k] = v
	}
	if len(entry.MatchedRules) > 0 {
		payload["matched_rules"] = append([]string(nil), entry.MatchedRules...)
	}
	if entry.SnapshotVersionDerived {
		payload[snapshotVersionDerivedMetadataKey] = true
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(b)
}
