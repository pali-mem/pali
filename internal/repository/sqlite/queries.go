package sqlite

const (
	InsertTenantSQL = `
INSERT INTO tenants(id, name, created_at)
VALUES (?, ?, ?)
`

	TenantExistsSQL = `
SELECT EXISTS(SELECT 1 FROM tenants WHERE id = ?)
`

	CountTenantMemoriesSQL = `
SELECT COUNT(1) FROM memories WHERE tenant_id = ?
`

	ListTenantsSQL = `
SELECT id, name, created_at
FROM tenants
ORDER BY created_at DESC
LIMIT ?
`

	InsertMemorySQL = `
INSERT INTO memories(id, tenant_id, content, tier, tags_json, source, created_by, kind, importance_score, recall_count, created_at, updated_at, last_accessed_at, last_recalled_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

	DeleteMemorySQL = `
DELETE FROM memories
WHERE tenant_id = ? AND id = ?
`

	SearchMemoriesSQL = `
SELECT m.id, m.tenant_id, m.content, m.tier, m.tags_json, m.source, m.created_by, m.kind, m.importance_score, m.recall_count, m.created_at, m.updated_at, m.last_accessed_at, m.last_recalled_at
FROM memory_fts f
JOIN memories m
  ON m.id = f.memory_id
 AND m.tenant_id = f.tenant_id
WHERE f.tenant_id = ?
  AND f.content MATCH ?
ORDER BY bm25(memory_fts), m.updated_at DESC
LIMIT ?
`

	ListMemoriesRecentSQL = `
SELECT id, tenant_id, content, tier, tags_json, source, created_by, kind, importance_score, recall_count, created_at, updated_at, last_accessed_at, last_recalled_at
FROM memories
WHERE tenant_id = ?
ORDER BY updated_at DESC
LIMIT ?
`

	InsertMemoryFTSSQL = `
INSERT INTO memory_fts(content, tenant_id, memory_id)
VALUES (?, ?, ?)
`

	DeleteMemoryFTSSQL = `
DELETE FROM memory_fts
WHERE tenant_id = ? AND memory_id = ?
`

	GetMemoriesByIDsBaseSQL = `
SELECT id, tenant_id, content, tier, tags_json, source, created_by, kind, importance_score, recall_count, created_at, updated_at, last_accessed_at, last_recalled_at
FROM memories
WHERE tenant_id = ?
`

	InsertEntityFactSQL = `
INSERT OR IGNORE INTO entity_facts(id, tenant_id, entity, relation, value, memory_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
`

	ListEntityFactsByEntityRelationSQL = `
SELECT id, tenant_id, entity, relation, value, memory_id, created_at
FROM entity_facts
WHERE tenant_id = ?
  AND entity = ?
  AND relation = ?
ORDER BY created_at DESC
LIMIT ?
`
)
