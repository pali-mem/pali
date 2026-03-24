package testutil

// InMemoryDBDSN returns the SQLite DSN used for in-memory tests.
func InMemoryDBDSN() string { return ":memory:" }
