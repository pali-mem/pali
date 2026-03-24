// Command evalidmap builds fixture-to-memory ID catalogs from SQLite data.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

type indexCatalog struct {
	AllByIndex       map[string][]string `json:"all_by_index"`
	RawByIndex       map[string][]string `json:"raw_by_index"`
	CanonicalByIndex map[string][]string `json:"canonical_by_index"`
}

func main() {
	var (
		dbPath       string
		idMapPath    string
		outPath      string
		sourcePrefix string
	)

	flag.StringVar(&dbPath, "db", "", "Path to SQLite database")
	flag.StringVar(&idMapPath, "id-map", "", "Path to JSON map keyed by fixture index")
	flag.StringVar(&outPath, "out", "", "Output JSON path (default stdout)")
	flag.StringVar(&sourcePrefix, "source-prefix", "eval_row_", "Source prefix used in memories.source")
	flag.Parse()

	if strings.TrimSpace(dbPath) == "" || strings.TrimSpace(idMapPath) == "" {
		exitf("both -db and -id-map are required")
	}

	allSet := map[string]map[string]struct{}{}
	rawSet := map[string]map[string]struct{}{}
	canonicalSet := map[string]map[string]struct{}{}

	rawMap, err := readRawIDMap(idMapPath)
	if err != nil {
		exitf("read id map: %v", err)
	}
	for idx, ids := range rawMap {
		for _, id := range ids {
			addID(allSet, idx, id)
			addID(rawSet, idx, id)
		}
	}

	if err := enrichFromSQLite(dbPath, sourcePrefix, rawMap, allSet, rawSet, canonicalSet); err != nil {
		exitf("enrich from sqlite: %v", err)
	}

	catalog := indexCatalog{
		AllByIndex:       flattenSetMap(allSet),
		RawByIndex:       flattenSetMap(rawSet),
		CanonicalByIndex: flattenSetMap(canonicalSet),
	}
	if err := writeCatalog(catalog, outPath); err != nil {
		exitf("write output: %v", err)
	}
}

func readRawIDMap(path string) (map[string][]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(raw))
	for idx, value := range raw {
		idx = strings.TrimSpace(idx)
		if idx == "" {
			continue
		}
		switch typed := value.(type) {
		case string:
			id := strings.TrimSpace(typed)
			if id != "" {
				out[idx] = append(out[idx], id)
			}
		case []any:
			for _, item := range typed {
				if s, ok := item.(string); ok {
					id := strings.TrimSpace(s)
					if id != "" {
						out[idx] = append(out[idx], id)
					}
				}
			}
		}
	}
	return out, nil
}

func enrichFromSQLite(
	dbPath, sourcePrefix string,
	rawMap map[string][]string,
	allSet, rawSet, canonicalSet map[string]map[string]struct{},
) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	if err := enrichFromSourceIndex(db, sourcePrefix, allSet, rawSet, canonicalSet); err != nil {
		return err
	}
	if err := enrichFromSourceTurnHash(db, rawMap, allSet, rawSet, canonicalSet); err != nil {
		return err
	}
	return nil
}

func enrichFromSourceIndex(
	db *sql.DB,
	sourcePrefix string,
	allSet, rawSet, canonicalSet map[string]map[string]struct{},
) error {
	pattern := regexp.MustCompile("^" + regexp.QuoteMeta(strings.TrimSpace(sourcePrefix)) + `(\d+)(?::|$)`)
	rows, err := db.Query("SELECT id, source, kind FROM memories WHERE source LIKE ?", strings.TrimSpace(sourcePrefix)+"%")
	if err != nil {
		return err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var (
			id     string
			source string
			kind   string
		)
		if err := rows.Scan(&id, &source, &kind); err != nil {
			return err
		}
		id = strings.TrimSpace(id)
		source = strings.TrimSpace(source)
		kind = strings.ToLower(strings.TrimSpace(kind))
		if id == "" || source == "" {
			continue
		}
		matches := pattern.FindStringSubmatch(source)
		if len(matches) < 2 {
			continue
		}
		idx := matches[1]
		addID(allSet, idx, id)
		if kind == "raw_turn" {
			addID(rawSet, idx, id)
		} else {
			addID(canonicalSet, idx, id)
		}
	}
	return rows.Err()
}

func enrichFromSourceTurnHash(
	db *sql.DB,
	rawMap map[string][]string,
	allSet, rawSet, canonicalSet map[string]map[string]struct{},
) error {
	rawIDToIndexes := map[string][]string{}
	for idx, ids := range rawMap {
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			rawIDToIndexes[id] = append(rawIDToIndexes[id], idx)
		}
	}
	if len(rawIDToIndexes) == 0 {
		return nil
	}

	rawIDs := make([]string, 0, len(rawIDToIndexes))
	for id := range rawIDToIndexes {
		rawIDs = append(rawIDs, id)
	}
	sort.Strings(rawIDs)

	type rawRef struct {
		tenantID string
		hash     string
	}
	rawRefByID := map[string]rawRef{}
	if err := queryInBatches(db, rawIDs, func(placeholders string, args []any) (*sql.Rows, error) {
		return db.Query(
			"SELECT id, tenant_id, source_turn_hash FROM memories WHERE id IN ("+placeholders+")",
			args...,
		)
	}, func(rows *sql.Rows) error {
		for rows.Next() {
			var (
				id       string
				tenantID string
				hash     string
			)
			if err := rows.Scan(&id, &tenantID, &hash); err != nil {
				return err
			}
			id = strings.TrimSpace(id)
			tenantID = strings.TrimSpace(tenantID)
			hash = strings.TrimSpace(hash)
			if id == "" || tenantID == "" || hash == "" {
				continue
			}
			rawRefByID[id] = rawRef{tenantID: tenantID, hash: hash}
		}
		return rows.Err()
	}); err != nil {
		return err
	}

	indexKeyToIndexes := map[string][]string{}
	hashes := map[string]struct{}{}
	for rawID, ref := range rawRefByID {
		key := ref.tenantID + "\x00" + ref.hash
		indexKeyToIndexes[key] = append(indexKeyToIndexes[key], rawIDToIndexes[rawID]...)
		hashes[ref.hash] = struct{}{}
	}
	if len(indexKeyToIndexes) == 0 {
		return nil
	}

	hashList := make([]string, 0, len(hashes))
	for hash := range hashes {
		hashList = append(hashList, hash)
	}
	sort.Strings(hashList)

	if err := queryInBatches(db, hashList, func(placeholders string, args []any) (*sql.Rows, error) {
		return db.Query(
			"SELECT id, tenant_id, kind, source_turn_hash FROM memories WHERE source_turn_hash IN ("+placeholders+")",
			args...,
		)
	}, func(rows *sql.Rows) error {
		for rows.Next() {
			var (
				id       string
				tenantID string
				kind     string
				hash     string
			)
			if err := rows.Scan(&id, &tenantID, &kind, &hash); err != nil {
				return err
			}
			id = strings.TrimSpace(id)
			tenantID = strings.TrimSpace(tenantID)
			kind = strings.ToLower(strings.TrimSpace(kind))
			hash = strings.TrimSpace(hash)
			if id == "" || tenantID == "" || hash == "" {
				continue
			}
			key := tenantID + "\x00" + hash
			indexes := indexKeyToIndexes[key]
			if len(indexes) == 0 {
				continue
			}
			for _, idx := range indexes {
				addID(allSet, idx, id)
				if kind == "raw_turn" {
					addID(rawSet, idx, id)
				} else {
					addID(canonicalSet, idx, id)
				}
			}
		}
		return rows.Err()
	}); err != nil {
		return err
	}

	return nil
}

func queryInBatches(
	db *sql.DB,
	values []string,
	queryFn func(placeholders string, args []any) (*sql.Rows, error),
	scanFn func(rows *sql.Rows) error,
) error {
	if len(values) == 0 {
		return nil
	}
	const batchSize = 300
	for start := 0; start < len(values); start += batchSize {
		end := start + batchSize
		if end > len(values) {
			end = len(values)
		}
		batch := values[start:end]
		args := make([]any, 0, len(batch))
		parts := make([]string, 0, len(batch))
		for _, v := range batch {
			args = append(args, v)
			parts = append(parts, "?")
		}
		rows, err := queryFn(strings.Join(parts, ","), args)
		if err != nil {
			return err
		}
		if err := scanFn(rows); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

func flattenSetMap(in map[string]map[string]struct{}) map[string][]string {
	out := make(map[string][]string, len(in))
	for idx, ids := range in {
		if len(ids) == 0 {
			continue
		}
		list := make([]string, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		sort.Strings(list)
		out[idx] = list
	}
	return out
}

func addID(target map[string]map[string]struct{}, idx, id string) {
	idx = strings.TrimSpace(idx)
	id = strings.TrimSpace(id)
	if idx == "" || id == "" {
		return
	}
	set, ok := target[idx]
	if !ok {
		set = map[string]struct{}{}
		target[idx] = set
	}
	set[id] = struct{}{}
}

func writeCatalog(catalog indexCatalog, outPath string) error {
	var (
		f   *os.File
		err error
	)
	if strings.TrimSpace(outPath) == "" {
		f = os.Stdout
	} else {
		f, err = os.Create(outPath)
		if err != nil {
			return err
		}
		defer func() {
			_ = f.Close()
		}()
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return enc.Encode(catalog)
}

func exitf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
