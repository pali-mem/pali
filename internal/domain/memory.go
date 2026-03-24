package domain

import "time"

// MemoryTier identifies how a memory should be retained.
type MemoryTier string

// Memory tier values.
const (
	MemoryTierWorking  MemoryTier = "working"
	MemoryTierEpisodic MemoryTier = "episodic"
	MemoryTierSemantic MemoryTier = "semantic"
	MemoryTierAuto     MemoryTier = "auto"
)

// MemoryCreatedBy identifies which actor created a memory.
type MemoryCreatedBy string

// Memory creator values.
const (
	MemoryCreatedByAuto   MemoryCreatedBy = "auto"
	MemoryCreatedByUser   MemoryCreatedBy = "user"
	MemoryCreatedBySystem MemoryCreatedBy = "system"
)

// MemoryKind classifies the shape of a memory record.
type MemoryKind string

// Memory kind values.
const (
	MemoryKindRawTurn     MemoryKind = "raw_turn"
	MemoryKindObservation MemoryKind = "observation"
	MemoryKindSummary     MemoryKind = "summary"
	MemoryKindEvent       MemoryKind = "event"
)

// MemoryAnswerMetadata carries answer-specific metadata.
type MemoryAnswerMetadata struct {
	AnswerKind         string
	SourceSentence     string
	SurfaceSpan        string
	TemporalAnchor     string
	RelativeTimePhrase string
	ResolvedTimeStart  string
	ResolvedTimeEnd    string
	TimeGranularity    string
	SupportMemoryIDs   []string
	SupportLines       []string
}

// Memory is the core persisted memory record.
type Memory struct {
	ID               string
	TenantID         string
	Content          string
	QueryViewText    string
	Tier             MemoryTier
	Tags             []string
	Source           string
	CreatedBy        MemoryCreatedBy
	Kind             MemoryKind
	CanonicalKey     string
	SourceTurnHash   string
	SourceFactIndex  int
	Extractor        string
	ExtractorVersion string
	Importance       float64
	RecallCount      int
	AnswerMetadata   MemoryAnswerMetadata
	CreatedAt        time.Time
	UpdatedAt        time.Time
	LastAccessedAt   time.Time
	LastRecalledAt   time.Time
}

// EntityFact is a normalized fact anchored to a memory.
type EntityFact struct {
	ID                  string
	TenantID            string
	Entity              string
	Relation            string
	RelationRaw         string
	Value               string
	MemoryID            string
	CreatedAt           time.Time
	ObservedAt          time.Time
	ValidFrom           time.Time
	ValidTo             *time.Time
	InvalidatedByFactID string
	Confidence          float64
}

// EntityFactPathQuery requests graph traversal candidates.
type EntityFactPathQuery struct {
	SeedEntities     []string
	RelationHints    []string
	MaxHops          int
	Limit            int
	TemporalValidity bool
}

// EntityFactPathCandidate is a candidate returned by a path search.
type EntityFactPathCandidate struct {
	MemoryID       string
	FactIDs        []string
	Entities       []string
	Relations      []string
	PathLength     int
	SupportCount   int
	TemporalValid  bool
	TraversalScore float64
}
