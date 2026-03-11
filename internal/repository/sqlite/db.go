package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	// Register the modernc SQLite driver used by sql.Open("sqlite", ...).
	_ "modernc.org/sqlite"
)

const defaultDSN = "file:pali.db?cache=shared"

func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		dsn = defaultDSN
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(5 * time.Minute)

	if err := applyPragmas(ctx, db, dsn); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := RunMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func applyPragmas(ctx context.Context, db *sql.DB, dsn string) error {
	basePragmas := []struct {
		name string
		sql  string
	}{
		{name: "foreign_keys", sql: `PRAGMA foreign_keys = ON;`},
		{name: "cache_size", sql: `PRAGMA cache_size = -64000;`},
		{name: "temp_store", sql: `PRAGMA temp_store = MEMORY;`},
	}

	for _, pragma := range basePragmas {
		if _, err := db.ExecContext(ctx, pragma.sql); err != nil {
			return fmt.Errorf("set %s pragma: %w", pragma.name, err)
		}
	}

	// WAL and synchronous mode are write-throughput knobs for file-backed DBs.
	if isInMemoryDSN(dsn) {
		return nil
	}

	writePragmas := []struct {
		name string
		sql  string
	}{
		{name: "journal_mode", sql: `PRAGMA journal_mode = WAL;`},
		{name: "synchronous", sql: `PRAGMA synchronous = NORMAL;`},
	}

	for _, pragma := range writePragmas {
		if _, err := db.ExecContext(ctx, pragma.sql); err != nil {
			return fmt.Errorf("set %s pragma: %w", pragma.name, err)
		}
	}

	return nil
}

func isInMemoryDSN(dsn string) bool {
	raw := strings.ToLower(strings.TrimSpace(dsn))
	if raw == ":memory:" {
		return true
	}
	if strings.Contains(raw, "mode=memory") {
		return true
	}
	return strings.HasPrefix(raw, "file::memory:")
}
