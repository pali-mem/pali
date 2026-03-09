package memory

import (
	"context"
	"log"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

type Service struct {
	repo       domain.MemoryRepository
	entityRepo domain.EntityFactRepository
	tenantRepo domain.TenantRepository
	vector     domain.VectorStore
	embedder   domain.Embedder
	scorer     domain.ImportanceScorer
	queryCache *queryEmbeddingCache

	structured                 StructuredMemoryOptions
	ranking                    RankingOptions
	rerank                     RerankOptions
	multiHop                   MultiHopOptions
	parser                     ParserOptions
	preferCanonicalEntityKinds bool
	infoParser                 InfoParser
	queryDecomposer            MultiHopQueryDecomposer
	logger                     *log.Logger
	devVerbose                 bool
	progress                   bool
}

type StructuredMemoryOptions struct {
	Enabled               bool
	DualWriteObservations bool
	DualWriteEvents       bool
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

type RerankOptions struct {
	Enabled  bool
	Provider string
	Window   int
	Blend    float64
}

type MultiHopOptions struct {
	EntityFactBridgeEnabled bool
	LLMDecompositionEnabled bool
	MaxDecompositionQueries int
	EnablePairwiseRerank    bool
	TokenExpansionFallback  bool
}

type ParserOptions struct {
	Enabled         bool
	Provider        string
	Model           string
	StoreRawTurn    bool
	MaxFacts        int
	DedupeThreshold float64
	UpdateThreshold float64
}

type ParsedFact struct {
	Content       string
	QueryViewText string
	Kind          domain.MemoryKind
	Tags          []string
	Entity        string
	Relation      string
	Value         string
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

type decomposerOption struct {
	decomposer MultiHopQueryDecomposer
}

type entityFactRepoOption struct {
	repo domain.EntityFactRepository
}

type loggerOption struct {
	logger *log.Logger
}

type debugOptions struct {
	verbose  bool
	progress bool
}

type canonicalEntityKindsOption struct {
	enabled bool
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

func (o RerankOptions) apply(s *Service) {
	if s == nil {
		return
	}
	s.rerank = normalizeRerankOptions(o)
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

func (o decomposerOption) apply(s *Service) {
	if s == nil {
		return
	}
	s.queryDecomposer = o.decomposer
}

func (o entityFactRepoOption) apply(s *Service) {
	if s == nil {
		return
	}
	s.entityRepo = o.repo
}

func (o loggerOption) apply(s *Service) {
	if s == nil {
		return
	}
	s.logger = o.logger
}

func (o debugOptions) apply(s *Service) {
	if s == nil {
		return
	}
	s.devVerbose = o.verbose
	s.progress = o.progress
}

func (o canonicalEntityKindsOption) apply(s *Service) {
	if s == nil {
		return
	}
	s.preferCanonicalEntityKinds = o.enabled
}

func WithInfoParser(parser InfoParser) ServiceOption {
	return parserImplOption{parser: parser}
}

func WithMultiHopQueryDecomposer(decomposer MultiHopQueryDecomposer) ServiceOption {
	return decomposerOption{decomposer: decomposer}
}

func WithEntityFactRepository(repo domain.EntityFactRepository) ServiceOption {
	return entityFactRepoOption{repo: repo}
}

func WithLogger(logger *log.Logger) ServiceOption {
	return loggerOption{logger: logger}
}

func WithDebug(verbose bool, progress bool) ServiceOption {
	return debugOptions{
		verbose:  verbose,
		progress: progress,
	}
}

func WithImplicitCanonicalKindsForEntityFacts(enabled bool) ServiceOption {
	return canonicalEntityKindsOption{enabled: enabled}
}

func defaultStructuredMemoryOptions() StructuredMemoryOptions {
	return StructuredMemoryOptions{
		Enabled:               false,
		DualWriteObservations: false,
		DualWriteEvents:       false,
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
		Model:           "",
		StoreRawTurn:    true,
		MaxFacts:        4,
		DedupeThreshold: 0.88,
		UpdateThreshold: 0.94,
	}
}

func defaultMultiHopOptions() MultiHopOptions {
	return MultiHopOptions{
		EntityFactBridgeEnabled: true,
		LLMDecompositionEnabled: false,
		MaxDecompositionQueries: 3,
		EnablePairwiseRerank:    true,
		TokenExpansionFallback:  true,
	}
}

func defaultRerankOptions() RerankOptions {
	return RerankOptions{
		Enabled:  true,
		Provider: "pairwise",
		Window:   50,
		Blend:    0.40,
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
	out.Model = strings.TrimSpace(out.Model)
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

func (o MultiHopOptions) apply(s *Service) {
	if s == nil {
		return
	}
	s.multiHop = normalizeMultiHopOptions(o)
}

func normalizeMultiHopOptions(in MultiHopOptions) MultiHopOptions {
	def := defaultMultiHopOptions()
	out := in
	if out.MaxDecompositionQueries <= 0 {
		out.MaxDecompositionQueries = def.MaxDecompositionQueries
	}
	return out
}

func normalizeRerankOptions(in RerankOptions) RerankOptions {
	def := defaultRerankOptions()
	out := in
	out.Enabled = true
	out.Provider = def.Provider
	if out.Window <= 0 {
		out.Window = def.Window
	}
	if out.Blend < 0 {
		out.Blend = 0
	}
	if out.Blend > 1 {
		out.Blend = 1
	}
	if out.Blend == 0 {
		out.Blend = def.Blend
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
		queryCache: newQueryEmbeddingCache(defaultQueryEmbeddingCacheCapacity),
		structured: defaultStructuredMemoryOptions(),
		ranking:    defaultRankingOptions(),
		rerank:     defaultRerankOptions(),
		multiHop:   defaultMultiHopOptions(),
		parser:     defaultParserOptions(),
	}
	for _, opt := range options {
		if opt == nil {
			continue
		}
		opt.apply(svc)
	}
	svc.ranking = normalizeRankingOptions(svc.ranking)
	svc.rerank = normalizeRerankOptions(svc.rerank)
	svc.multiHop = normalizeMultiHopOptions(svc.multiHop)
	svc.parser = normalizeParserOptions(svc.parser)
	return svc
}
