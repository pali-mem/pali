package sqlitevec

const (
	UpsertEmbeddingSQL = `
INSERT INTO memory_embeddings (tenant_id, memory_id, embedding_json, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(tenant_id, memory_id) DO UPDATE SET
	embedding_json = excluded.embedding_json,
	updated_at = excluded.updated_at
`

	DeleteEmbeddingSQL = `
DELETE FROM memory_embeddings
WHERE tenant_id = ? AND memory_id = ?
`

	ListEmbeddingsByTenantSQL = `
SELECT memory_id, embedding_json
FROM memory_embeddings
WHERE tenant_id = ?
`
)
