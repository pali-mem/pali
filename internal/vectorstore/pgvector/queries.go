package pgvector

const (
	createExtensionSQL = `CREATE EXTENSION IF NOT EXISTS vector`

	createTableSQL = `
CREATE TABLE IF NOT EXISTS %s (
	tenant_id TEXT NOT NULL,
	memory_id TEXT NOT NULL,
	embedding vector NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (tenant_id, memory_id)
)`

	upsertSQL = `
INSERT INTO %s (tenant_id, memory_id, embedding, updated_at)
VALUES ($1, $2, $3::vector, $4)
ON CONFLICT (tenant_id, memory_id) DO UPDATE SET
	embedding = EXCLUDED.embedding,
	updated_at = EXCLUDED.updated_at`

	deleteSQL = `
DELETE FROM %s
WHERE tenant_id = $1 AND memory_id = $2`

	searchSQL = `
SELECT memory_id, 1 - (embedding <=> $2::vector) AS similarity
FROM %s
WHERE tenant_id = $1
ORDER BY embedding <=> $2::vector
LIMIT $3`

	detectVectorDimSQL = `
SELECT vector_dims(embedding)
FROM %s
LIMIT 1`
)
