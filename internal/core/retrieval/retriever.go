package retrieval

type Candidate struct {
	MemoryID   string
	Similarity float64
}

type Retriever interface {
	Retrieve(query string, topK int) ([]Candidate, error)
}
