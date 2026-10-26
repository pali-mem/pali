package sqlitevec

// Embeddings are stored as little-endian float32 bytes in embedding_blob.
// embedding_json holds '[]' for those rows; rows written before the blob
// column existed keep their JSON and a NULL blob until MigrateLegacyEmbeddings
// converts them.
const (
	UpsertEmbeddingSQL = `
INSERT INTO memory_embeddings (tenant_id, memory_id, embedding_json, embedding_blob, updated_at)
VALUES (?, ?, '[]', ?, ?)
ON CONFLICT(tenant_id, memory_id) DO UPDATE SET
	embedding_json = excluded.embedding_json,
	embedding_blob = excluded.embedding_blob,
	updated_at = excluded.updated_at
`

	DeleteEmbeddingSQL = `
DELETE FROM memory_embeddings
WHERE tenant_id = ? AND memory_id = ?
`

	ListEmbeddingsByTenantSQL = `
SELECT memory_id, embedding_blob, CASE WHEN embedding_blob IS NULL THEN embedding_json ELSE '' END
FROM memory_embeddings
WHERE tenant_id = ?
`

	ListLegacyEmbeddingsSQL = `
SELECT tenant_id, memory_id, embedding_json
FROM memory_embeddings
WHERE embedding_blob IS NULL
LIMIT ?
`

	ConvertLegacyEmbeddingSQL = `
UPDATE memory_embeddings
SET embedding_blob = ?, embedding_json = '[]'
WHERE tenant_id = ? AND memory_id = ? AND embedding_blob IS NULL
`
)
