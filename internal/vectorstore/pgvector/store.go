package pgvector

type Store struct {
	DSN string
}

func NewStore(dsn string) *Store { return &Store{DSN: dsn} }
