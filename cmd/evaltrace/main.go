// Command evaltrace generates a trace report for evaluation and runtime data.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jconfig "github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
	"github.com/pali-mem/pali/internal/config"
	_ "modernc.org/sqlite"
)

type traceReport struct {
	GeneratedAtUTC string        `json:"generated_at_utc"`
	Config         configSummary `json:"config"`
	SQLite         sqliteTrace   `json:"sqlite"`
	EvalCoverage   *evalCoverage `json:"eval_coverage,omitempty"`
	Neo4j          *neo4jTrace   `json:"neo4j,omitempty"`
}

type configSummary struct {
	EntityFactBackend string `json:"entity_fact_backend"`
	ParserEnabled     bool   `json:"parser_enabled"`
	ParserProvider    string `json:"parser_provider"`
	ParserModel       string `json:"parser_model"`
}

type sqliteTrace struct {
	MemoriesTotal      int64            `json:"memories_total"`
	ByKind             map[string]int64 `json:"by_kind"`
	ByExtractorVersion map[string]int64 `json:"by_extractor_version"`
	ParserDerivedCount int64            `json:"parser_derived_count"`
	SourceSuffixCount  map[string]int64 `json:"source_suffix_count"`
}

type evalCoverage struct {
	CasesTotal               int64    `json:"cases_total"`
	CasesWithFixtureIndexes  int64    `json:"cases_with_fixture_indexes"`
	FixtureIndexRefsTotal    int64    `json:"fixture_index_refs_total"`
	FixtureIndexesUnique     int64    `json:"fixture_indexes_unique"`
	IndexesWithRawIDs        int64    `json:"indexes_with_raw_ids"`
	IndexesWithAnyIDs        int64    `json:"indexes_with_any_ids"`
	IndexesWithCanonicalIDs  int64    `json:"indexes_with_canonical_ids"`
	CasesWithCanonicalTarget int64    `json:"cases_with_canonical_target"`
	MissingCanonicalExamples []string `json:"missing_canonical_examples,omitempty"`
}

type neo4jTrace struct {
	Connected         bool            `json:"connected"`
	Error             string          `json:"error,omitempty"`
	EntitiesCount     int64           `json:"entities_count,omitempty"`
	FactsCount        int64           `json:"facts_count,omitempty"`
	SourceMemoryEdges int64           `json:"source_memory_edges,omitempty"`
	TopRelations      []relationCount `json:"top_relations,omitempty"`
}

type relationCount struct {
	Relation string `json:"relation"`
	Count    int64  `json:"count"`
}

type indexCatalog struct {
	AllByIndex       map[string][]string `json:"all_by_index"`
	RawByIndex       map[string][]string `json:"raw_by_index"`
	CanonicalByIndex map[string][]string `json:"canonical_by_index"`
}

func main() {
	var (
		dbPath      string
		outPath     string
		configPath  string
		idCatalog   string
		evalSetPath string
	)
	flag.StringVar(&dbPath, "db", "", "Path to sqlite db")
	flag.StringVar(&outPath, "out", "", "Output trace JSON path")
	flag.StringVar(&configPath, "config", "", "Rendered config YAML path")
	flag.StringVar(&idCatalog, "id-catalog", "", "Path to id catalog JSON")
	flag.StringVar(&evalSetPath, "eval-set", "", "Path to eval set JSON")
	flag.Parse()

	if strings.TrimSpace(dbPath) == "" || strings.TrimSpace(outPath) == "" {
		exitf("-db and -out are required")
	}

	cfgSummary := configSummary{}
	var cfg config.Config
	hasConfig := false
	if strings.TrimSpace(configPath) != "" {
		loaded, err := config.Load(configPath)
		if err != nil {
			exitf("load config: %v", err)
		}
		cfg = loaded
		hasConfig = true
		cfgSummary = configSummary{
			EntityFactBackend: strings.TrimSpace(cfg.EntityFactBackend),
			ParserEnabled:     cfg.Parser.Enabled,
			ParserProvider:    strings.TrimSpace(cfg.Parser.Provider),
			ParserModel:       parserModel(cfg),
		}
	}

	sqlTrace, err := buildSQLiteTrace(dbPath)
	if err != nil {
		exitf("build sqlite trace: %v", err)
	}

	var cov *evalCoverage
	if strings.TrimSpace(idCatalog) != "" && strings.TrimSpace(evalSetPath) != "" {
		coverage, err := buildEvalCoverage(idCatalog, evalSetPath)
		if err != nil {
			exitf("build eval coverage: %v", err)
		}
		cov = coverage
	}

	var neoTrace *neo4jTrace
	if hasConfig && strings.EqualFold(strings.TrimSpace(cfg.EntityFactBackend), "neo4j") {
		nt := probeNeo4j(cfg)
		neoTrace = &nt
	}

	report := traceReport{
		GeneratedAtUTC: time.Now().UTC().Format(time.RFC3339),
		Config:         cfgSummary,
		SQLite:         sqlTrace,
		EvalCoverage:   cov,
		Neo4j:          neoTrace,
	}
	if err := writeJSON(outPath, report); err != nil {
		exitf("write trace: %v", err)
	}
}

func parserModel(cfg config.Config) string {
	provider := strings.ToLower(strings.TrimSpace(cfg.Parser.Provider))
	switch provider {
	case "openrouter":
		if strings.TrimSpace(cfg.Parser.OpenRouterModel) != "" {
			return strings.TrimSpace(cfg.Parser.OpenRouterModel)
		}
		return strings.TrimSpace(cfg.OpenRouter.ScoringModel)
	default:
		return strings.TrimSpace(cfg.Parser.OllamaModel)
	}
}

func buildSQLiteTrace(dbPath string) (sqliteTrace, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return sqliteTrace{}, err
	}
	defer func() {
		_ = db.Close()
	}()

	trace := sqliteTrace{
		ByKind:             map[string]int64{},
		ByExtractorVersion: map[string]int64{},
		SourceSuffixCount:  map[string]int64{},
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&trace.MemoriesTotal); err != nil {
		return sqliteTrace{}, err
	}

	kindRows, err := db.Query("SELECT kind, COUNT(*) FROM memories GROUP BY kind")
	if err != nil {
		return sqliteTrace{}, err
	}
	for kindRows.Next() {
		var (
			kind  string
			count int64
		)
		if err := kindRows.Scan(&kind, &count); err != nil {
			_ = kindRows.Close()
			return sqliteTrace{}, err
		}
		kind = strings.TrimSpace(kind)
		if kind == "" {
			kind = "(empty)"
		}
		trace.ByKind[kind] = count
	}
	if err := kindRows.Close(); err != nil {
		return sqliteTrace{}, err
	}

	if err := db.QueryRow(`
		SELECT COUNT(*) FROM memories
		WHERE lower(trim(source)) = 'parser' OR source LIKE '%:parser%'
	`).Scan(&trace.ParserDerivedCount); err != nil {
		return sqliteTrace{}, err
	}

	extractorRows, err := db.Query(`
		SELECT
			COALESCE(NULLIF(TRIM(extractor), ''), '(empty)') AS extractor,
			COALESCE(NULLIF(TRIM(extractor_version), ''), '(empty)') AS extractor_version,
			COUNT(*)
		FROM memories
		GROUP BY extractor, extractor_version
	`)
	if err != nil {
		return sqliteTrace{}, err
	}
	for extractorRows.Next() {
		var (
			extractor string
			version   string
			count     int64
		)
		if err := extractorRows.Scan(&extractor, &version, &count); err != nil {
			_ = extractorRows.Close()
			return sqliteTrace{}, err
		}
		key := strings.TrimSpace(extractor) + "@" + strings.TrimSpace(version)
		trace.ByExtractorVersion[key] = count
	}
	if err := extractorRows.Close(); err != nil {
		return sqliteTrace{}, err
	}

	sourceRows, err := db.Query("SELECT source FROM memories")
	if err != nil {
		return sqliteTrace{}, err
	}
	for sourceRows.Next() {
		var source string
		if err := sourceRows.Scan(&source); err != nil {
			_ = sourceRows.Close()
			return sqliteTrace{}, err
		}
		source = strings.TrimSpace(source)
		if source == "" {
			trace.SourceSuffixCount["(empty)"]++
			continue
		}
		if strings.EqualFold(source, "parser") {
			trace.SourceSuffixCount["parser"]++
			continue
		}
		suffix := "(base)"
		if idx := strings.LastIndex(source, ":"); idx >= 0 && idx+1 < len(source) {
			suffix = strings.ToLower(strings.TrimSpace(source[idx+1:]))
			if suffix == "" {
				suffix = "(base)"
			}
		}
		trace.SourceSuffixCount[suffix]++
	}
	if err := sourceRows.Close(); err != nil {
		return sqliteTrace{}, err
	}

	return trace, nil
}

func buildEvalCoverage(idCatalogPath, evalSetPath string) (*evalCoverage, error) {
	b, err := os.ReadFile(idCatalogPath)
	if err != nil {
		return nil, err
	}
	var catalog indexCatalog
	if err := json.Unmarshal(b, &catalog); err != nil {
		return nil, err
	}

	rawEval, err := os.ReadFile(evalSetPath)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(rawEval, &rows); err != nil {
		return nil, err
	}

	cov := &evalCoverage{}
	missingCanonical := []string{}
	seenIndex := map[string]struct{}{}

	for _, row := range rows {
		cov.CasesTotal++
		indexes := toIndexList(row["expected_fixture_indexes"])
		if len(indexes) == 0 {
			continue
		}
		cov.CasesWithFixtureIndexes++
		canonicalForCase := false
		for _, idx := range indexes {
			cov.FixtureIndexRefsTotal++
			if _, ok := seenIndex[idx]; !ok {
				seenIndex[idx] = struct{}{}
			}
			if len(catalog.RawByIndex[idx]) > 0 {
				cov.IndexesWithRawIDs++
			}
			if len(catalog.AllByIndex[idx]) > 0 {
				cov.IndexesWithAnyIDs++
			}
			if len(catalog.CanonicalByIndex[idx]) > 0 {
				cov.IndexesWithCanonicalIDs++
				canonicalForCase = true
			} else if len(missingCanonical) < 20 {
				missingCanonical = append(missingCanonical, idx)
			}
		}
		if canonicalForCase {
			cov.CasesWithCanonicalTarget++
		}
	}
	cov.FixtureIndexesUnique = int64(len(seenIndex))
	if len(missingCanonical) > 0 {
		dedup := map[string]struct{}{}
		out := make([]string, 0, len(missingCanonical))
		for _, idx := range missingCanonical {
			if _, ok := dedup[idx]; ok {
				continue
			}
			dedup[idx] = struct{}{}
			out = append(out, idx)
		}
		sort.Strings(out)
		cov.MissingCanonicalExamples = out
	}
	return cov, nil
}

func toIndexList(v any) []string {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		switch typed := item.(type) {
		case float64:
			out = append(out, fmt.Sprintf("%.0f", typed))
		case string:
			s := strings.TrimSpace(typed)
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func probeNeo4j(cfg config.Config) neo4jTrace {
	timeout := time.Duration(cfg.Neo4j.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	trace := neo4jTrace{}
	driver, err := neo4j.NewDriverWithContext(
		cfg.Neo4j.URI,
		neo4j.BasicAuth(cfg.Neo4j.Username, cfg.Neo4j.Password, ""),
		func(c *neo4jconfig.Config) {
			c.SocketConnectTimeout = timeout
		},
	)
	if err != nil {
		trace.Error = err.Error()
		return trace
	}
	defer func() {
		_ = driver.Close(context.Background())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), timeout*2)
	defer cancel()
	session := driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: cfg.Neo4j.Database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	runCount := func(query string) (int64, error) {
		val, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			res, err := tx.Run(ctx, query, nil)
			if err != nil {
				return int64(0), err
			}
			if !res.Next(ctx) {
				if err := res.Err(); err != nil {
					return int64(0), err
				}
				return int64(0), nil
			}
			c, _ := res.Record().Get("c")
			switch v := c.(type) {
			case int64:
				return v, nil
			case int:
				return int64(v), nil
			case float64:
				return int64(v), nil
			default:
				return int64(0), nil
			}
		})
		if err != nil {
			return 0, err
		}
		count, _ := val.(int64)
		return count, nil
	}

	entities, err := runCount("MATCH (e:PaliEntity) RETURN count(e) AS c")
	if err != nil {
		trace.Error = err.Error()
		return trace
	}
	facts, err := runCount("MATCH (f:PaliEntityFact) RETURN count(f) AS c")
	if err != nil {
		trace.Error = err.Error()
		return trace
	}
	edges, err := runCount("MATCH (:PaliEntityFact)-[r:SOURCE_MEMORY]->(:PaliMemory) RETURN count(r) AS c")
	if err != nil {
		trace.Error = err.Error()
		return trace
	}

	top, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (f:PaliEntityFact)
			RETURN coalesce(f.relation, '') AS relation, count(*) AS c
			ORDER BY c DESC
			LIMIT 20
		`, nil)
		if err != nil {
			return nil, err
		}
		out := make([]relationCount, 0, 20)
		for res.Next(ctx) {
			rec := res.Record()
			relation, _ := rec.Get("relation")
			c, _ := rec.Get("c")
			item := relationCount{Relation: strings.TrimSpace(fmt.Sprint(relation))}
			switch v := c.(type) {
			case int64:
				item.Count = v
			case int:
				item.Count = int64(v)
			case float64:
				item.Count = int64(v)
			}
			out = append(out, item)
		}
		if err := res.Err(); err != nil {
			return nil, err
		}
		return out, nil
	})
	if err != nil {
		trace.Error = err.Error()
		return trace
	}
	relations, _ := top.([]relationCount)

	trace.Connected = true
	trace.EntitiesCount = entities
	trace.FactsCount = facts
	trace.SourceMemoryEdges = edges
	trace.TopRelations = relations
	return trace
}

func writeJSON(path string, value any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func exitf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
