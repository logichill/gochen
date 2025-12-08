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

基础表结构示例（`aggregate_id` 为整数 ID）：
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

### 字符串/UUID 聚合 ID 场景

对于需要使用字符串或 UUID 作为聚合 ID 的场景，可以在保持整体表结构不变的前提下，将 `aggregate_id` 列调整为 `TEXT`（或对应数据库中的变长字符串类型）：

```sql
CREATE TABLE domain_events (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    version INTEGER NOT NULL,
    schema_version INTEGER NOT NULL,
    timestamp DATETIME NOT NULL,
    payload TEXT NOT NULL,
    metadata TEXT NOT NULL,
    UNIQUE(aggregate_id, aggregate_type, version)
);

CREATE INDEX idx_domain_events_aggregate ON domain_events (aggregate_type, aggregate_id, version);
```

迁移建议：
- 如需从 `INTEGER` ID 演进到字符串 ID，有两种常见路径：
  - **新聚合使用新 ID 策略**：为新场景引入单独的事件表或使用不同的 `aggregate_type` 前缀，将历史数值 ID 聚合与新字符串 ID 聚合隔离；
  - **一次性迁移**：在停机窗口内将旧的数值 ID 转换为字符串（例如 `CAST(id AS TEXT)` 或拼接前缀），并同步调整业务侧主键类型与代码中的 `ID` 泛型参数。
- 不建议在同一聚合类型内部混用多种 ID 形态（例如部分为 int64，部分为字符串），否则会显著增加迁移与排查复杂度。

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
