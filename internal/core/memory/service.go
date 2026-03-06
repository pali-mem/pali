package memory

import (
	"context"
	"strings"

	"github.com/vein05/pali/internal/domain"
)

type Service struct {
	repo       domain.MemoryRepository
	entityRepo domain.EntityFactRepository
	tenantRepo domain.TenantRepository
	vector     domain.VectorStore
	embedder   domain.Embedder
	scorer     domain.ImportanceScorer

	structured StructuredMemoryOptions
	ranking    RankingOptions
	parser     ParserOptions
	infoParser InfoParser
}

type StructuredMemoryOptions struct {
	Enabled               bool
	DualWriteObservations bool
	DualWriteEvents       bool
	QueryRoutingEnabled   bool
	MaxObservations       int
}

type RankingOptions struct {
	Algorithm string
	WAL       WALWeights
	Match     MatchWeights
}

type WALWeights struct {
	Recency    float64
	Relevance  float64
	Importance float64
}

type MatchWeights struct {
	Recency      float64
	Relevance    float64
	Importance   float64
	QueryOverlap float64
	Routing      float64
}

type ParserOptions struct {
	Enabled         bool
	Provider        string
	StoreRawTurn    bool
	MaxFacts        int
	DedupeThreshold float64
	UpdateThreshold float64
}

type ParsedFact struct {
	Content  string
	Kind     domain.MemoryKind
	Tags     []string
	Entity   string
	Relation string
	Value    string
}

type InfoParser interface {
	Parse(ctx context.Context, content string, maxFacts int) ([]ParsedFact, error)
}

type ServiceOption interface {
	apply(*Service)
}

type parserImplOption struct {
	parser InfoParser
}

type entityFactRepoOption struct {
	repo domain.EntityFactRepository
}

func (o StructuredMemoryOptions) apply(s *Service) {
	if s == nil {
		return
	}
	s.structured = o
	if s.structured.MaxObservations <= 0 {
		s.structured.MaxObservations = defaultStructuredMemoryOptions().MaxObservations
	}
}

func (o RankingOptions) apply(s *Service) {
	if s == nil {
		return
	}
	s.ranking = normalizeRankingOptions(o)
}

func (o ParserOptions) apply(s *Service) {
	if s == nil {
		return
	}
	s.parser = normalizeParserOptions(o)
}

func (o parserImplOption) apply(s *Service) {
	if s == nil {
		return
	}
	s.infoParser = o.parser
}

func (o entityFactRepoOption) apply(s *Service) {
	if s == nil {
		return
	}
	s.entityRepo = o.repo
}

func WithInfoParser(parser InfoParser) ServiceOption {
	return parserImplOption{parser: parser}
}

func WithEntityFactRepository(repo domain.EntityFactRepository) ServiceOption {
	return entityFactRepoOption{repo: repo}
}

func defaultStructuredMemoryOptions() StructuredMemoryOptions {
	return StructuredMemoryOptions{
		Enabled:               false,
		DualWriteObservations: false,
		DualWriteEvents:       false,
		QueryRoutingEnabled:   false,
		MaxObservations:       3,
	}
}

func defaultRankingOptions() RankingOptions {
	return RankingOptions{
		Algorithm: "wal",
		WAL: WALWeights{
			Recency:    1,
			Relevance:  1,
			Importance: 1,
		},
		Match: MatchWeights{
			Recency:      0.05,
			Relevance:    0.70,
			Importance:   0.10,
			QueryOverlap: 0.10,
			Routing:      0.05,
		},
	}
}

func defaultParserOptions() ParserOptions {
	return ParserOptions{
		Enabled:         false,
		Provider:        "heuristic",
		StoreRawTurn:    true,
		MaxFacts:        4,
		DedupeThreshold: 0.88,
		UpdateThreshold: 0.94,
	}
}

func normalizeRankingOptions(in RankingOptions) RankingOptions {
	out := in
	if strings.TrimSpace(out.Algorithm) == "" {
		out.Algorithm = defaultRankingOptions().Algorithm
	}
	out.Algorithm = strings.ToLower(strings.TrimSpace(out.Algorithm))
	if out.Algorithm != "match" {
		out.Algorithm = "wal"
	}
	if out.WAL.Recency+out.WAL.Relevance+out.WAL.Importance == 0 {
		out.WAL = defaultRankingOptions().WAL
	}
	if out.Match.Recency+out.Match.Relevance+out.Match.Importance+out.Match.QueryOverlap+out.Match.Routing == 0 {
		out.Match = defaultRankingOptions().Match
	}
	return out
}

func normalizeParserOptions(in ParserOptions) ParserOptions {
	out := in
	out.Provider = strings.ToLower(strings.TrimSpace(out.Provider))
	if out.Provider == "" {
		out.Provider = defaultParserOptions().Provider
	}
	if out.MaxFacts <= 0 {
		out.MaxFacts = defaultParserOptions().MaxFacts
	}
	if out.DedupeThreshold < 0 {
		out.DedupeThreshold = 0
	}
	if out.DedupeThreshold > 1 {
		out.DedupeThreshold = 1
	}
	if out.UpdateThreshold < 0 {
		out.UpdateThreshold = 0
	}
	if out.UpdateThreshold > 1 {
		out.UpdateThreshold = 1
	}
	if out.DedupeThreshold > out.UpdateThreshold {
		out.DedupeThreshold = out.UpdateThreshold
	}
	return out
}

func NewService(
	repo domain.MemoryRepository,
	tenantRepo domain.TenantRepository,
	vector domain.VectorStore,
	embedder domain.Embedder,
	scorer domain.ImportanceScorer,
	options ...ServiceOption,
) *Service {
	svc := &Service{
		repo:       repo,
		tenantRepo: tenantRepo,
		vector:     vector,
		embedder:   embedder,
		scorer:     scorer,
		structured: defaultStructuredMemoryOptions(),
		ranking:    defaultRankingOptions(),
		parser:     defaultParserOptions(),
	}
	for _, opt := range options {
		if opt == nil {
			continue
		}
		opt.apply(svc)
	}
	svc.ranking = normalizeRankingOptions(svc.ranking)
	svc.parser = normalizeParserOptions(svc.parser)
	return svc
}
