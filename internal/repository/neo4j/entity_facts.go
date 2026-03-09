package neo4j

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/pali-mem/pali/internal/domain"
)

const (
	defaultTimeout   = 2 * time.Second
	defaultBatchSize = 256
)

var schemaStatements = []string{
	`CREATE CONSTRAINT pali_entity_identity IF NOT EXISTS
FOR (e:PaliEntity) REQUIRE (e.tenant_id, e.name) IS UNIQUE`,
	`CREATE CONSTRAINT pali_entity_fact_identity IF NOT EXISTS
FOR (f:PaliEntityFact) REQUIRE (f.tenant_id, f.entity, f.relation, f.value, f.memory_id) IS UNIQUE`,
	`CREATE CONSTRAINT pali_memory_identity IF NOT EXISTS
FOR (m:PaliMemory) REQUIRE (m.tenant_id, m.id) IS UNIQUE`,
	`CREATE INDEX pali_entity_fact_lookup IF NOT EXISTS
FOR (f:PaliEntityFact) ON (f.tenant_id, f.entity, f.relation, f.created_at_ns)`,
}

const upsertEntityFactsCypher = `
UNWIND $rows AS row
MERGE (e:PaliEntity {tenant_id: row.tenant_id, name: row.entity})
MERGE (f:PaliEntityFact {
	tenant_id: row.tenant_id,
	entity: row.entity,
	relation: row.relation,
	value: row.value,
	memory_id: row.memory_id
})
ON CREATE SET
	f.id = row.id,
	f.created_at_ns = row.created_at_ns
SET
	f.relation_raw = row.relation_raw
MERGE (e)-[:HAS_FACT]->(f)
FOREACH (_ IN CASE WHEN row.memory_id = '' THEN [] ELSE [1] END |
	MERGE (m:PaliMemory {tenant_id: row.tenant_id, id: row.memory_id})
	MERGE (f)-[:SOURCE_MEMORY]->(m)
)
`

const listEntityFactsByRelationCypher = `
MATCH (f:PaliEntityFact {
	tenant_id: $tenant_id,
	entity: $entity,
	relation: $relation
})
WITH f
ORDER BY f.created_at_ns DESC
LIMIT $limit
RETURN
	f.id AS id,
	f.tenant_id AS tenant_id,
	f.entity AS entity,
	f.relation AS relation,
	coalesce(f.relation_raw, '') AS relation_raw,
	f.value AS value,
	coalesce(f.memory_id, '') AS memory_id,
	coalesce(f.created_at_ns, 0) AS created_at_ns
`

type Options struct {
	URI       string
	Username  string
	Password  string
	Database  string
	Timeout   time.Duration
	BatchSize int
}

type EntityFactRepository struct {
	driver    neo4j.DriverWithContext
	database  string
	batchSize int
}

func NewEntityFactRepository(opts Options) (*EntityFactRepository, error) {
	uri := strings.TrimSpace(opts.URI)
	if uri == "" {
		return nil, domain.ErrInvalidInput
	}
	username := strings.TrimSpace(opts.Username)
	password := strings.TrimSpace(opts.Password)
	if username == "" || password == "" {
		return nil, domain.ErrInvalidInput
	}
	database := strings.TrimSpace(opts.Database)
	if database == "" {
		database = "neo4j"
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	driver, err := neo4j.NewDriverWithContext(
		uri,
		neo4j.BasicAuth(username, password, ""),
		func(cfg *neo4j.Config) {
			cfg.SocketConnectTimeout = timeout
		},
	)
	if err != nil {
		return nil, fmt.Errorf("initialize neo4j driver: %w", err)
	}
	repo := &EntityFactRepository{
		driver:    driver,
		database:  database,
		batchSize: batchSize,
	}

	initCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := repo.initialize(initCtx); err != nil {
		_ = driver.Close(initCtx)
		return nil, err
	}
	return repo, nil
}

func (r *EntityFactRepository) Close() error {
	if r == nil || r.driver == nil {
		return nil
	}
	return r.driver.Close(context.Background())
}

func (r *EntityFactRepository) initialize(ctx context.Context) error {
	if r == nil || r.driver == nil {
		return fmt.Errorf("neo4j entity fact repository is not initialized")
	}
	if err := r.driver.VerifyConnectivity(ctx); err != nil {
		return fmt.Errorf("verify neo4j connectivity: %w", err)
	}

	session := r.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: r.database,
		AccessMode:   neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	for _, statement := range schemaStatements {
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			result, runErr := tx.Run(ctx, statement, nil)
			if runErr != nil {
				return nil, runErr
			}
			_, consumeErr := result.Consume(ctx)
			return nil, consumeErr
		})
		if err != nil {
			return fmt.Errorf("initialize neo4j schema: %w", err)
		}
	}
	return nil
}

func (r *EntityFactRepository) Store(ctx context.Context, fact domain.EntityFact) (domain.EntityFact, error) {
	out, err := r.StoreBatch(ctx, []domain.EntityFact{fact})
	if err != nil {
		return domain.EntityFact{}, err
	}
	if len(out) == 0 {
		return domain.EntityFact{}, fmt.Errorf("neo4j store returned empty batch")
	}
	return out[0], nil
}

func (r *EntityFactRepository) StoreBatch(ctx context.Context, facts []domain.EntityFact) ([]domain.EntityFact, error) {
	if len(facts) == 0 {
		return []domain.EntityFact{}, nil
	}
	if r == nil || r.driver == nil {
		return nil, fmt.Errorf("neo4j entity fact repository is not initialized")
	}

	now := time.Now().UTC()
	prepared := make([]domain.EntityFact, 0, len(facts))
	for i := range facts {
		fact := facts[i]
		if err := prepareEntityFactForStore(&fact, now); err != nil {
			return nil, fmt.Errorf("prepare entity fact[%d]: %w", i, err)
		}
		prepared = append(prepared, fact)
	}

	session := r.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: r.database,
		AccessMode:   neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	for start := 0; start < len(prepared); start += r.batchSize {
		end := minInt(start+r.batchSize, len(prepared))
		rows := make([]map[string]any, 0, end-start)
		for _, fact := range prepared[start:end] {
			rows = append(rows, map[string]any{
				"id":            fact.ID,
				"tenant_id":     fact.TenantID,
				"entity":        fact.Entity,
				"relation":      fact.Relation,
				"relation_raw":  fact.RelationRaw,
				"value":         fact.Value,
				"memory_id":     fact.MemoryID,
				"created_at_ns": fact.CreatedAt.UTC().UnixNano(),
			})
		}

		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			result, runErr := tx.Run(ctx, upsertEntityFactsCypher, map[string]any{"rows": rows})
			if runErr != nil {
				return nil, runErr
			}
			_, consumeErr := result.Consume(ctx)
			return nil, consumeErr
		})
		if err != nil {
			return nil, fmt.Errorf("neo4j store entity facts chunk [%d:%d]: %w", start, end, err)
		}
	}

	return prepared, nil
}

func (r *EntityFactRepository) ListByEntityRelation(
	ctx context.Context,
	tenantID, entity, relation string,
	limit int,
) ([]domain.EntityFact, error) {
	tenantID = strings.TrimSpace(tenantID)
	entity = normalizeEntityFactKey(entity)
	relation = normalizeEntityFactKey(relation)
	if tenantID == "" || entity == "" || relation == "" {
		return nil, domain.ErrInvalidInput
	}
	if limit <= 0 {
		limit = 100
	}
	if r == nil || r.driver == nil {
		return nil, fmt.Errorf("neo4j entity fact repository is not initialized")
	}

	session := r.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: r.database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	raw, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, runErr := tx.Run(ctx, listEntityFactsByRelationCypher, map[string]any{
			"tenant_id": tenantID,
			"entity":    entity,
			"relation":  relation,
			"limit":     limit,
		})
		if runErr != nil {
			return nil, runErr
		}

		out := make([]domain.EntityFact, 0, limit)
		for result.Next(ctx) {
			fact, recordErr := scanEntityFactRecord(result.Record())
			if recordErr != nil {
				return nil, recordErr
			}
			out = append(out, fact)
		}
		if iterErr := result.Err(); iterErr != nil {
			return nil, iterErr
		}
		return out, nil
	})
	if err != nil {
		return nil, fmt.Errorf("neo4j list entity facts by relation: %w", err)
	}

	facts, ok := raw.([]domain.EntityFact)
	if !ok {
		return nil, fmt.Errorf("neo4j entity fact query returned unexpected type: %T", raw)
	}
	return facts, nil
}

func prepareEntityFactForStore(fact *domain.EntityFact, now time.Time) error {
	if fact == nil {
		return domain.ErrInvalidInput
	}
	fact.TenantID = strings.TrimSpace(fact.TenantID)
	fact.Entity = normalizeEntityFactKey(fact.Entity)
	fact.Relation = normalizeEntityFactKey(fact.Relation)
	fact.RelationRaw = normalizeEntityFactRawRelation(fact.RelationRaw)
	fact.Value = normalizeEntityFactValue(fact.Value)
	fact.MemoryID = strings.TrimSpace(fact.MemoryID)
	if fact.TenantID == "" || fact.Entity == "" || fact.Relation == "" || fact.Value == "" {
		return domain.ErrInvalidInput
	}
	if fact.RelationRaw == "" {
		fact.RelationRaw = fact.Relation
	}
	if strings.TrimSpace(fact.ID) == "" {
		fact.ID = newID("ef")
	}
	if fact.CreatedAt.IsZero() {
		fact.CreatedAt = now
	}
	return nil
}

func normalizeEntityFactKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func normalizeEntityFactValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func normalizeEntityFactRawRelation(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func scanEntityFactRecord(record *neo4j.Record) (domain.EntityFact, error) {
	var fact domain.EntityFact

	id, ok := record.Get("id")
	if !ok {
		return domain.EntityFact{}, fmt.Errorf("neo4j entity fact row missing id")
	}
	fact.ID = asString(id)
	if fact.ID == "" {
		return domain.EntityFact{}, fmt.Errorf("neo4j entity fact row has empty id")
	}

	tenantID, ok := record.Get("tenant_id")
	if !ok {
		return domain.EntityFact{}, fmt.Errorf("neo4j entity fact row missing tenant_id")
	}
	fact.TenantID = asString(tenantID)

	entity, ok := record.Get("entity")
	if !ok {
		return domain.EntityFact{}, fmt.Errorf("neo4j entity fact row missing entity")
	}
	fact.Entity = asString(entity)

	relation, ok := record.Get("relation")
	if !ok {
		return domain.EntityFact{}, fmt.Errorf("neo4j entity fact row missing relation")
	}
	fact.Relation = asString(relation)

	relationRaw, _ := record.Get("relation_raw")
	fact.RelationRaw = normalizeEntityFactRawRelation(asString(relationRaw))
	if fact.RelationRaw == "" {
		fact.RelationRaw = fact.Relation
	}

	value, ok := record.Get("value")
	if !ok {
		return domain.EntityFact{}, fmt.Errorf("neo4j entity fact row missing value")
	}
	fact.Value = asString(value)

	memoryID, _ := record.Get("memory_id")
	fact.MemoryID = strings.TrimSpace(asString(memoryID))

	createdAtNSRaw, _ := record.Get("created_at_ns")
	if createdAtNS, ok := asInt64(createdAtNSRaw); ok && createdAtNS > 0 {
		fact.CreatedAt = time.Unix(0, createdAtNS).UTC()
	}

	return fact, nil
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func asInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case int32:
		return int64(x), true
	case float64:
		return int64(x), true
	case float32:
		return int64(x), true
	default:
		return 0, false
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func newID(prefix string) string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		now := time.Now().UnixNano()
		for i := range raw {
			raw[i] = byte(now >> (i * 8))
		}
	}
	return prefix + "_" + hex.EncodeToString(raw)
}
