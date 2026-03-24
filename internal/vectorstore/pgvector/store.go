package pgvector

// Store represents the pgvector configuration for wiring.
type Store struct {
	DSN string
}

// NewStore builds a pgvector store configuration holder.
func NewStore(dsn string) *Store { return &Store{DSN: dsn} }
