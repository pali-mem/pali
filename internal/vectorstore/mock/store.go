// Package mock provides a minimal vector-store implementation for tests.
package mock

type store struct{}

// NewStore returns a no-op vector store used in tests.
func NewStore() *store { return &store{} }
