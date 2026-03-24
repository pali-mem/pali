package memory

import "github.com/pali-mem/pali/internal/domain"

type lexicalCandidate struct {
	Memory domain.Memory
	Score  float64
}

type scoredMemory struct {
	Memory domain.Memory
	Score  float64
}

type candidateSignal struct {
	DenseScore   float64
	DenseRank    int
	LexicalScore float64
	LexicalRank  int
	RRFScore     float64
}
