package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

var migrationStatements = []string{
	`CREATE TABLE IF NOT EXISTS tenants (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		created_at TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS memories (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			content TEXT NOT NULL,
			query_view_text TEXT NOT NULL DEFAULT '',
			tier TEXT NOT NULL,
			tags_json TEXT NOT NULL DEFAULT '[]',
			source TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT 'auto',
			kind TEXT NOT NULL DEFAULT 'raw_turn',
			canonical_key TEXT NOT NULL DEFAULT '',
			source_turn_hash TEXT NOT NULL DEFAULT '',
			source_fact_index INTEGER NOT NULL DEFAULT -1,
			extractor TEXT NOT NULL DEFAULT '',
			extractor_version TEXT NOT NULL DEFAULT '',
			importance_score REAL NOT NULL DEFAULT 0,
			recall_count INTEGER NOT NULL DEFAULT 0,
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_accessed_at TEXT NOT NULL,
			last_recalled_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
			FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
		);`,
	`CREATE TABLE IF NOT EXISTS memory_embeddings (
		tenant_id TEXT NOT NULL,
		memory_id TEXT NOT NULL,
		embedding_json TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (tenant_id, memory_id),
		FOREIGN KEY(memory_id) REFERENCES memories(id) ON DELETE CASCADE
	);`,
	`CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
		content,
		tenant_id UNINDEXED,
		memory_id UNINDEXED,
		tokenize='porter unicode61'
	);`,
	`INSERT INTO memory_fts(content, tenant_id, memory_id)
	SELECT m.content, m.tenant_id, m.id
	FROM memories m
	WHERE NOT EXISTS (
		SELECT 1
		FROM memory_fts f
		WHERE f.memory_id = m.id AND f.tenant_id = m.tenant_id
	);`,
	`DELETE FROM memory_fts
	WHERE NOT EXISTS (
		SELECT 1
		FROM memories m
		WHERE m.id = memory_fts.memory_id AND m.tenant_id = memory_fts.tenant_id
	);`,
	`ALTER TABLE memories ADD COLUMN tags_json TEXT NOT NULL DEFAULT '[]';`,
	`ALTER TABLE memories ADD COLUMN source TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE memories ADD COLUMN created_by TEXT NOT NULL DEFAULT 'auto';`,
	`ALTER TABLE memories ADD COLUMN kind TEXT NOT NULL DEFAULT 'raw_turn';`,
	`ALTER TABLE memories ADD COLUMN query_view_text TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE memories ADD COLUMN canonical_key TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE memories ADD COLUMN source_turn_hash TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE memories ADD COLUMN source_fact_index INTEGER NOT NULL DEFAULT -1;`,
	`ALTER TABLE memories ADD COLUMN extractor TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE memories ADD COLUMN extractor_version TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE memories ADD COLUMN importance_score REAL NOT NULL DEFAULT 0;`,
	`ALTER TABLE memories ADD COLUMN recall_count INTEGER NOT NULL DEFAULT 0;`,
	`ALTER TABLE memories ADD COLUMN metadata_json TEXT NOT NULL DEFAULT '{}';`,
	`ALTER TABLE memories ADD COLUMN last_accessed_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z';`,
	`ALTER TABLE memories ADD COLUMN last_recalled_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z';`,
	`CREATE INDEX IF NOT EXISTS idx_memories_tenant_updated ON memories(tenant_id, updated_at DESC);`,
	`CREATE INDEX IF NOT EXISTS idx_memories_tenant_kind_updated ON memories(tenant_id, kind, updated_at DESC);`,
	`CREATE INDEX IF NOT EXISTS idx_memories_tenant_created ON memories(tenant_id, created_at DESC);`,
	`CREATE INDEX IF NOT EXISTS idx_memories_tenant_accessed ON memories(tenant_id, last_accessed_at DESC);`,
	`CREATE INDEX IF NOT EXISTS idx_memories_tenant_canonical_key ON memories(tenant_id, canonical_key);`,
	`CREATE INDEX IF NOT EXISTS idx_memories_tenant_source_turn_hash ON memories(tenant_id, source_turn_hash);`,
	`CREATE TABLE IF NOT EXISTS entity_facts (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		entity TEXT NOT NULL,
		relation TEXT NOT NULL,
		relation_raw TEXT NOT NULL DEFAULT '',
		value TEXT NOT NULL,
		memory_id TEXT REFERENCES memories(id) ON DELETE CASCADE,
		observed_at TEXT NOT NULL DEFAULT '',
		valid_from TEXT NOT NULL DEFAULT '',
		valid_to TEXT NOT NULL DEFAULT '',
		invalidated_by_fact_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);`,
	`ALTER TABLE entity_facts ADD COLUMN relation_raw TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE entity_facts ADD COLUMN observed_at TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE entity_facts ADD COLUMN valid_from TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE entity_facts ADD COLUMN valid_to TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE entity_facts ADD COLUMN invalidated_by_fact_id TEXT NOT NULL DEFAULT '';`,
	`CREATE INDEX IF NOT EXISTS entity_facts_lookup ON entity_facts(tenant_id, entity, relation);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS entity_facts_dedupe ON entity_facts(tenant_id, entity, relation, value, memory_id);`,
	`CREATE TABLE IF NOT EXISTS memory_index_jobs (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		memory_id TEXT NOT NULL,
		op TEXT NOT NULL,
		state TEXT NOT NULL DEFAULT 'pending',
		last_error TEXT NOT NULL DEFAULT '',
		attempts INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(tenant_id, memory_id, op)
	);`,
	`CREATE INDEX IF NOT EXISTS idx_memory_index_jobs_state ON memory_index_jobs(state, updated_at DESC);`,
	`CREATE INDEX IF NOT EXISTS idx_memory_index_jobs_tenant_memory ON memory_index_jobs(tenant_id, memory_id, op);`,
	`CREATE TABLE IF NOT EXISTS memory_postprocess_jobs (
		id TEXT PRIMARY KEY,
		ingest_id TEXT NOT NULL DEFAULT '',
		tenant_id TEXT NOT NULL,
		memory_id TEXT NOT NULL,
		job_type TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'queued',
		attempts INTEGER NOT NULL DEFAULT 0,
		max_attempts INTEGER NOT NULL DEFAULT 5,
		available_at TEXT NOT NULL,
		lease_owner TEXT NOT NULL DEFAULT '',
		leased_until TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(tenant_id, memory_id, job_type)
	);`,
	`CREATE INDEX IF NOT EXISTS idx_memory_postprocess_jobs_poll ON memory_postprocess_jobs(status, available_at ASC, updated_at DESC);`,
	`CREATE INDEX IF NOT EXISTS idx_memory_postprocess_jobs_tenant ON memory_postprocess_jobs(tenant_id, updated_at DESC);`,
}

func RunMigrations(ctx context.Context, db *sql.DB) error {
	for _, stmt := range migrationStatements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				continue
			}
			return fmt.Errorf("sqlite migration failed: %w", err)
		}
	}
	return nil
}
