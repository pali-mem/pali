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
	f.relation_raw = row.relation_raw,
	f.observed_at_ns = row.observed_at_ns,
	f.valid_from_ns = row.valid_from_ns,
	f.valid_to_ns = row.valid_to_ns,
	f.invalidated_by_fact_id = row.invalidated_by_fact_id,
	f.confidence = row.confidence
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
	coalesce(f.observed_at_ns, 0) AS observed_at_ns,
	coalesce(f.valid_from_ns, 0) AS valid_from_ns,
	coalesce(f.valid_to_ns, 0) AS valid_to_ns,
	coalesce(f.invalidated_by_fact_id, '') AS invalidated_by_fact_id,
	coalesce(f.confidence, 0.0) AS confidence,
	coalesce(f.created_at_ns, 0) AS created_at_ns
`

const listEntityFactsByNeighborhoodCypher = `
MATCH (seed:PaliEntity {tenant_id: $tenant_id})
WHERE seed.name IN $seeds
MATCH (seed)-[:HAS_FACT]->(:PaliEntityFact)-[:SOURCE_MEMORY]->(m:PaliMemory)<-[:SOURCE_MEMORY]-(f:PaliEntityFact)
WHERE f.tenant_id = $tenant_id
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
	coalesce(f.observed_at_ns, 0) AS observed_at_ns,
	coalesce(f.valid_from_ns, 0) AS valid_from_ns,
	coalesce(f.valid_to_ns, 0) AS valid_to_ns,
	coalesce(f.invalidated_by_fact_id, '') AS invalidated_by_fact_id,
	coalesce(f.confidence, 0.0) AS confidence,
	coalesce(f.created_at_ns, 0) AS created_at_ns
`

const invalidateEntityFactsByRelationCypher = `
MATCH (f:PaliEntityFact {
	tenant_id: $tenant_id,
	entity: $entity,
	relation: $relation
})
WHERE coalesce(f.invalidated_by_fact_id, '') = ''
  AND coalesce(f.valid_to_ns, 0) = 0
  AND f.value <> $active_value
SET f.valid_to_ns = $valid_to_ns,
	f.invalidated_by_fact_id = $invalidated_by_fact_id
`

const listEntityFactPathsCypherTemplate = `
MATCH (seed:PaliEntity {tenant_id: $tenant_id})
WHERE seed.name IN $seed_entities
MATCH p = (seed)-[:HAS_FACT|SOURCE_MEMORY*1..%d]-(targetFact:PaliEntityFact)-[:SOURCE_MEMORY]->(m:PaliMemory)
WHERE targetFact.tenant_id = $tenant_id
  AND m.tenant_id = $tenant_id
  AND ALL(node IN nodes(p) WHERE coalesce(node.tenant_id, $tenant_id) = $tenant_id)
  AND (
    $temporal_validity = false OR
    (
      coalesce(targetFact.invalidated_by_fact_id, '') = '' AND
      coalesce(targetFact.valid_to_ns, 0) = 0
    )
  )
WITH
	m.id AS memory_id,
	[node IN nodes(p) WHERE 'PaliEntityFact' IN labels(node) | coalesce(node.id, '')] AS path_fact_ids,
	[node IN nodes(p) WHERE 'PaliEntity' IN labels(node) | coalesce(node.name, '')] AS path_entities,
	[node IN nodes(p) WHERE 'PaliEntityFact' IN labels(node) | coalesce(node.relation, '')] AS path_relations,
	length(p) AS path_length
WITH
	memory_id,
	collect(path_fact_ids) AS fact_paths,
	collect(path_entities) AS entity_paths,
	collect(path_relations) AS relation_paths,
	min(path_length) AS path_length,
	count(*) AS support_count,
	max(
		CASE
			WHEN size($relation_hints) = 0 THEN 1
			WHEN any(rel IN path_relations WHERE rel IN $relation_hints) THEN 1
			ELSE 0
		END
	) AS hint_match
WITH
	memory_id,
	coalesce(fact_paths[0], []) AS fact_ids,
	coalesce(entity_paths[0], []) AS entities,
	coalesce(relation_paths[0], []) AS relations,
	path_length,
	support_count,
	CASE
		WHEN support_count >= 4 THEN 1.0
		ELSE toFloat(support_count) / 4.0
	END AS support_score,
	CASE
		WHEN path_length <= 1 THEN 1.0
		ELSE 1.0 / toFloat(path_length)
	END AS path_score,
	CASE
		WHEN hint_match = 1 THEN 1.0
		ELSE 0.45
	END AS hint_score
RETURN
	memory_id,
	fact_ids,
	entities,
	relations,
	path_length,
	support_count,
	true AS temporal_valid,
	((support_score * 0.45) + (path_score * 0.25) + (hint_score * 0.30)) AS traversal_score
ORDER BY traversal_score DESC, support_count DESC, path_length ASC, memory_id ASC
LIMIT $limit
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
			validToNS := int64(0)
			if fact.ValidTo != nil && !fact.ValidTo.IsZero() {
				validToNS = fact.ValidTo.UTC().UnixNano()
			}
			rows = append(rows, map[string]any{
				"id":                     fact.ID,
				"tenant_id":              fact.TenantID,
				"entity":                 fact.Entity,
				"relation":               fact.Relation,
				"relation_raw":           fact.RelationRaw,
				"value":                  fact.Value,
				"memory_id":              fact.MemoryID,
				"created_at_ns":          fact.CreatedAt.UTC().UnixNano(),
				"observed_at_ns":         timeOrZeroNS(fact.ObservedAt),
				"valid_from_ns":          timeOrZeroNS(fact.ValidFrom),
				"valid_to_ns":            validToNS,
				"invalidated_by_fact_id": strings.TrimSpace(fact.InvalidatedByFactID),
				"confidence":             clamp01(fact.Confidence),
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

func (r *EntityFactRepository) ListByEntityNeighborhood(
	ctx context.Context,
	tenantID string,
	seeds []string,
	limit int,
) ([]domain.EntityFact, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, domain.ErrInvalidInput
	}
	if r == nil || r.driver == nil {
		return nil, fmt.Errorf("neo4j entity fact repository is not initialized")
	}
	if limit <= 0 {
		limit = 128
	}
	normalizedSeeds := make([]string, 0, len(seeds))
	seen := make(map[string]struct{}, len(seeds))
	for _, seed := range seeds {
		entity := normalizeEntityFactKey(seed)
		if entity == "" {
			continue
		}
		if _, ok := seen[entity]; ok {
			continue
		}
		seen[entity] = struct{}{}
		normalizedSeeds = append(normalizedSeeds, entity)
	}
	if len(normalizedSeeds) == 0 {
		return []domain.EntityFact{}, nil
	}

	session := r.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: r.database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	raw, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, runErr := tx.Run(ctx, listEntityFactsByNeighborhoodCypher, map[string]any{
			"tenant_id": tenantID,
			"seeds":     normalizedSeeds,
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
		return nil, fmt.Errorf("neo4j list entity fact neighborhood: %w", err)
	}

	facts, ok := raw.([]domain.EntityFact)
	if !ok {
		return nil, fmt.Errorf("neo4j entity fact neighborhood query returned unexpected type: %T", raw)
	}
	return facts, nil
}

func (r *EntityFactRepository) ListByEntityPaths(
	ctx context.Context,
	tenantID string,
	query domain.EntityFactPathQuery,
) ([]domain.EntityFactPathCandidate, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, domain.ErrInvalidInput
	}
	seedEntities := normalizeEntityFactKeys(query.SeedEntities)
	if len(seedEntities) == 0 {
		return []domain.EntityFactPathCandidate{}, nil
	}
	if r == nil || r.driver == nil {
		return nil, fmt.Errorf("neo4j entity fact repository is not initialized")
	}
	relationHints := normalizeEntityFactKeys(query.RelationHints)
	maxHops := query.MaxHops
	if maxHops <= 0 {
		maxHops = 2
	}
	if maxHops > 4 {
		maxHops = 4
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 128
	}

	session := r.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: r.database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	cypher := fmt.Sprintf(listEntityFactPathsCypherTemplate, graphPathMaxEdges(maxHops))
	raw, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, runErr := tx.Run(ctx, cypher, map[string]any{
			"tenant_id":         tenantID,
			"seed_entities":     seedEntities,
			"relation_hints":    relationHints,
			"temporal_validity": query.TemporalValidity,
			"limit":             limit,
		})
		if runErr != nil {
			return nil, runErr
		}

		out := make([]domain.EntityFactPathCandidate, 0, limit)
		for result.Next(ctx) {
			candidate, recordErr := scanEntityFactPathRecord(result.Record())
			if recordErr != nil {
				return nil, recordErr
			}
			out = append(out, candidate)
		}
		if iterErr := result.Err(); iterErr != nil {
			return nil, iterErr
		}
		return out, nil
	})
	if err != nil {
		return nil, fmt.Errorf("neo4j list entity fact paths: %w", err)
	}

	candidates, ok := raw.([]domain.EntityFactPathCandidate)
	if !ok {
		return nil, fmt.Errorf("neo4j entity fact path query returned unexpected type: %T", raw)
	}
	return candidates, nil
}

func (r *EntityFactRepository) InvalidateEntityRelation(
	ctx context.Context,
	tenantID, entity, relation, activeValue, invalidatedByFactID string,
	validTo time.Time,
) error {
	tenantID = strings.TrimSpace(tenantID)
	entity = normalizeEntityFactKey(entity)
	relation = normalizeEntityFactKey(relation)
	activeValue = normalizeEntityFactValue(activeValue)
	invalidatedByFactID = strings.TrimSpace(invalidatedByFactID)
	if tenantID == "" || entity == "" || relation == "" || activeValue == "" || invalidatedByFactID == "" || validTo.IsZero() {
		return domain.ErrInvalidInput
	}
	if r == nil || r.driver == nil {
		return fmt.Errorf("neo4j entity fact repository is not initialized")
	}
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: r.database,
		AccessMode:   neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, runErr := tx.Run(ctx, invalidateEntityFactsByRelationCypher, map[string]any{
			"tenant_id":              tenantID,
			"entity":                 entity,
			"relation":               relation,
			"active_value":           activeValue,
			"invalidated_by_fact_id": invalidatedByFactID,
			"valid_to_ns":            validTo.UTC().UnixNano(),
		})
		if runErr != nil {
			return nil, runErr
		}
		_, consumeErr := result.Consume(ctx)
		return nil, consumeErr
	})
	if err != nil {
		return fmt.Errorf("neo4j invalidate entity facts by relation: %w", err)
	}
	return nil
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
	if fact.ObservedAt.IsZero() {
		fact.ObservedAt = fact.CreatedAt
	}
	if fact.ValidFrom.IsZero() {
		fact.ValidFrom = fact.ObservedAt
	}
	if fact.Confidence < 0 {
		fact.Confidence = 0
	}
	if fact.Confidence > 1 {
		fact.Confidence = 1
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
	observedAtNSRaw, _ := record.Get("observed_at_ns")
	if observedAtNS, ok := asInt64(observedAtNSRaw); ok && observedAtNS > 0 {
		fact.ObservedAt = time.Unix(0, observedAtNS).UTC()
	}
	validFromNSRaw, _ := record.Get("valid_from_ns")
	if validFromNS, ok := asInt64(validFromNSRaw); ok && validFromNS > 0 {
		fact.ValidFrom = time.Unix(0, validFromNS).UTC()
	}
	validToNSRaw, _ := record.Get("valid_to_ns")
	if validToNS, ok := asInt64(validToNSRaw); ok && validToNS > 0 {
		validTo := time.Unix(0, validToNS).UTC()
		fact.ValidTo = &validTo
	}
	invalidatedByFactID, _ := record.Get("invalidated_by_fact_id")
	fact.InvalidatedByFactID = strings.TrimSpace(asString(invalidatedByFactID))
	confidenceRaw, _ := record.Get("confidence")
	if confidence, ok := asFloat64(confidenceRaw); ok {
		fact.Confidence = clamp01(confidence)
	}

	return fact, nil
}

func scanEntityFactPathRecord(record *neo4j.Record) (domain.EntityFactPathCandidate, error) {
	var candidate domain.EntityFactPathCandidate

	memoryID, ok := record.Get("memory_id")
	if !ok {
		return domain.EntityFactPathCandidate{}, fmt.Errorf("neo4j entity path row missing memory_id")
	}
	candidate.MemoryID = strings.TrimSpace(asString(memoryID))
	if candidate.MemoryID == "" {
		return domain.EntityFactPathCandidate{}, fmt.Errorf("neo4j entity path row has empty memory_id")
	}

	factIDsRaw, _ := record.Get("fact_ids")
	candidate.FactIDs = uniqueStrings(asStringSlice(factIDsRaw))

	entitiesRaw, _ := record.Get("entities")
	candidate.Entities = uniqueStrings(asStringSlice(entitiesRaw))

	relationsRaw, _ := record.Get("relations")
	candidate.Relations = uniqueStrings(asStringSlice(relationsRaw))

	pathLengthRaw, _ := record.Get("path_length")
	if pathLength, ok := asInt64(pathLengthRaw); ok && pathLength > 0 {
		candidate.PathLength = int(pathLength)
	}

	supportCountRaw, _ := record.Get("support_count")
	if supportCount, ok := asInt64(supportCountRaw); ok && supportCount > 0 {
		candidate.SupportCount = int(supportCount)
	}

	temporalValidRaw, _ := record.Get("temporal_valid")
	candidate.TemporalValid = asBool(temporalValidRaw)

	traversalScoreRaw, _ := record.Get("traversal_score")
	if traversalScore, ok := asFloat64(traversalScoreRaw); ok {
		candidate.TraversalScore = clamp01(traversalScore)
	}

	return candidate, nil
}

func asString(v any) string {
	if v == nil {
		return ""
	}
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

func asFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int64:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	default:
		return 0, false
	}
}

func asBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int64:
		return x != 0
	case int:
		return x != 0
	case float64:
		return x != 0
	default:
		return false
	}
}

func asStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		out := make([]string, 0, len(x))
		for _, item := range x {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			out = append(out, item)
		}
		return out
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			value := strings.TrimSpace(asString(item))
			if value == "" {
				continue
			}
			out = append(out, value)
		}
		return out
	default:
		value := strings.TrimSpace(asString(v))
		if value == "" {
			return []string{}
		}
		return []string{value}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func graphPathMaxEdges(maxHops int) int {
	if maxHops <= 0 {
		return 3
	}
	return maxHops * 3
}

func normalizeEntityFactKeys(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizeEntityFactKey(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
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
	return out
}

func timeOrZeroNS(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixNano()
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
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
