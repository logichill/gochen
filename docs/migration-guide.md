# Migration Guide（索引与性能建议）

本仓库不内置迁移 CLI，但提供事件存储/Outbox/投影检查点等表结构与索引建议，便于配合 `golang-migrate` 等工具。

## 事件存储（domain_events）

推荐索引：
```sql
CREATE INDEX idx_domain_events_timestamp_id ON domain_events (timestamp, id);
CREATE INDEX idx_domain_events_type_timestamp ON domain_events (type, timestamp);
CREATE INDEX idx_domain_events_aggregate ON domain_events (aggregate_type, aggregate_id, version);
```
适用场景：
- Replay/投影：`timestamp,id` 支持游标扫描
- 按类型过滤：`type,timestamp` 减少回表
- 聚合顺序：`aggregate_type,aggregate_id,version` 支持快速重放聚合

基础表结构示例：
```sql
CREATE TABLE domain_events (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    aggregate_id INTEGER NOT NULL,
    aggregate_type TEXT NOT NULL,
    version INTEGER NOT NULL,
    schema_version INTEGER NOT NULL,
    timestamp DATETIME NOT NULL,
    payload TEXT NOT NULL,
    metadata TEXT NOT NULL,
    UNIQUE(aggregate_id, aggregate_type, version)
);
```

## Outbox（outbox）

关键索引：
```sql
CREATE INDEX idx_outbox_status_created_at ON outbox(status, created_at);
CREATE INDEX idx_outbox_next_retry_at ON outbox(next_retry_at);
```

## 投影检查点（projection_checkpoints）

```sql
CREATE TABLE projection_checkpoints (
    projection_name TEXT PRIMARY KEY,
    position INTEGER NOT NULL,
    last_event_id TEXT NOT NULL,
    last_event_time DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
```

## 使用建议
- 迁移工具：优先使用 `golang-migrate`/`atlas` 等，将上述索引纳入迁移脚本。
- 按需调整表名：如果自定义表名，保持索引字段一致。
- 针对高吞吐场景可考虑分区（按时间/租户分区事件表）。
