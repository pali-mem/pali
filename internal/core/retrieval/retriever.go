package retrieval

// Candidate represents a retrieval candidate and its similarity score.
type Candidate struct {
	MemoryID   string
	Similarity float64
}

// Retriever fetches candidates for a query.
type Retriever interface {
	Retrieve(query string, topK int) ([]Candidate, error)
}
