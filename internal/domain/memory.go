package domain

import "time"

type MemoryTier string

const (
	MemoryTierWorking  MemoryTier = "working"
	MemoryTierEpisodic MemoryTier = "episodic"
	MemoryTierSemantic MemoryTier = "semantic"
	MemoryTierAuto     MemoryTier = "auto"
)

type MemoryCreatedBy string

const (
	MemoryCreatedByAuto   MemoryCreatedBy = "auto"
	MemoryCreatedByUser   MemoryCreatedBy = "user"
	MemoryCreatedBySystem MemoryCreatedBy = "system"
)

type MemoryKind string

const (
	MemoryKindRawTurn     MemoryKind = "raw_turn"
	MemoryKindObservation MemoryKind = "observation"
	MemoryKindSummary     MemoryKind = "summary"
	MemoryKindEvent       MemoryKind = "event"
)

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

type EntityFactPathQuery struct {
	SeedEntities     []string
	RelationHints    []string
	MaxHops          int
	Limit            int
	TemporalValidity bool
}

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
