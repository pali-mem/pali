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
INSERT INTO memories(id, tenant_id, content, query_view_text, tier, tags_json, source, created_by, kind, canonical_key, source_turn_hash, source_fact_index, extractor, extractor_version, importance_score, recall_count, created_at, updated_at, last_accessed_at, last_recalled_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

	DeleteMemorySQL = `
DELETE FROM memories
WHERE tenant_id = ? AND id = ?
`

	SearchMemoriesSQL = `
SELECT m.id, m.tenant_id, m.content, m.query_view_text, m.tier, m.tags_json, m.source, m.created_by, m.kind, m.canonical_key, m.source_turn_hash, m.source_fact_index, m.extractor, m.extractor_version, m.importance_score, m.recall_count, m.created_at, m.updated_at, m.last_accessed_at, m.last_recalled_at
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
SELECT id, tenant_id, content, query_view_text, tier, tags_json, source, created_by, kind, canonical_key, source_turn_hash, source_fact_index, extractor, extractor_version, importance_score, recall_count, created_at, updated_at, last_accessed_at, last_recalled_at
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
SELECT id, tenant_id, content, query_view_text, tier, tags_json, source, created_by, kind, canonical_key, source_turn_hash, source_fact_index, extractor, extractor_version, importance_score, recall_count, created_at, updated_at, last_accessed_at, last_recalled_at
FROM memories
WHERE tenant_id = ?
`

	FindMemoryByCanonicalKeySQL = `
SELECT id, tenant_id, content, query_view_text, tier, tags_json, source, created_by, kind, canonical_key, source_turn_hash, source_fact_index, extractor, extractor_version, importance_score, recall_count, created_at, updated_at, last_accessed_at, last_recalled_at
FROM memories
WHERE tenant_id = ?
  AND canonical_key = ?
LIMIT 1
`

	ListMemoriesBySourceTurnHashSQL = `
SELECT id, tenant_id, content, query_view_text, tier, tags_json, source, created_by, kind, canonical_key, source_turn_hash, source_fact_index, extractor, extractor_version, importance_score, recall_count, created_at, updated_at, last_accessed_at, last_recalled_at
FROM memories
WHERE tenant_id = ?
  AND source_turn_hash = ?
ORDER BY
  CASE kind
    WHEN 'raw_turn' THEN 0
    WHEN 'event' THEN 1
    WHEN 'observation' THEN 2
    ELSE 3
  END,
  updated_at DESC
LIMIT ?
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

	UpsertMemoryIndexJobSQL = `
INSERT INTO memory_index_jobs(id, tenant_id, memory_id, op, state, last_error, attempts, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, memory_id, op) DO UPDATE SET
	state = excluded.state,
	last_error = excluded.last_error,
	attempts = CASE
		WHEN excluded.state = 'failed' THEN memory_index_jobs.attempts + 1
		WHEN excluded.state = 'pending' THEN memory_index_jobs.attempts
		ELSE memory_index_jobs.attempts
	END,
	updated_at = excluded.updated_at
`

	UpdateMemoryIndexJobStateSQL = `
UPDATE memory_index_jobs
SET state = ?,
	last_error = ?,
	attempts = CASE
		WHEN ? = 'failed' THEN attempts + 1
		ELSE attempts
	END,
	updated_at = ?
WHERE tenant_id = ?
  AND memory_id = ?
  AND op = ?
`

	UpsertMemoryPostprocessJobSQL = `
INSERT INTO memory_postprocess_jobs(id, ingest_id, tenant_id, memory_id, job_type, status, attempts, max_attempts, available_at, lease_owner, leased_until, last_error, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, memory_id, job_type) DO UPDATE SET
	ingest_id = excluded.ingest_id,
	status = 'queued',
	attempts = 0,
	max_attempts = excluded.max_attempts,
	available_at = excluded.available_at,
	lease_owner = '',
	leased_until = '',
	last_error = '',
	updated_at = excluded.updated_at
`

	GetMemoryPostprocessJobIDSQL = `
SELECT id
FROM memory_postprocess_jobs
WHERE tenant_id = ?
  AND memory_id = ?
  AND job_type = ?
LIMIT 1
`

	GetMemoryPostprocessJobByIDSQL = `
SELECT id, ingest_id, tenant_id, memory_id, job_type, status, attempts, max_attempts, available_at, lease_owner, leased_until, last_error, created_at, updated_at
FROM memory_postprocess_jobs
WHERE id = ?
LIMIT 1
`

	ListMemoryPostprocessJobsBaseSQL = `
SELECT id, ingest_id, tenant_id, memory_id, job_type, status, attempts, max_attempts, available_at, lease_owner, leased_until, last_error, created_at, updated_at
FROM memory_postprocess_jobs
`

	ListMemoryPostprocessJobIDsForClaimSQL = `
SELECT id
FROM memory_postprocess_jobs
WHERE available_at <= ?
  AND status IN ('queued', 'failed')
  AND (leased_until = '' OR leased_until <= ?)
ORDER BY available_at ASC, created_at ASC
LIMIT ?
`

	MarkMemoryPostprocessJobClaimedSQL = `
UPDATE memory_postprocess_jobs
SET status = 'running',
	lease_owner = ?,
	leased_until = ?,
	updated_at = ?
WHERE id = ?
`

	MarkMemoryPostprocessJobSucceededSQL = `
UPDATE memory_postprocess_jobs
SET status = 'succeeded',
	last_error = '',
	lease_owner = '',
	leased_until = '',
	updated_at = ?
WHERE id = ?
`

	MarkMemoryPostprocessJobFailedSQL = `
UPDATE memory_postprocess_jobs
SET status = ?,
	attempts = ?,
	available_at = ?,
	last_error = ?,
	lease_owner = '',
	leased_until = '',
	updated_at = ?
WHERE id = ?
`
)
