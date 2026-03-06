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

type Memory struct {
	ID             string
	TenantID       string
	Content        string
	Tier           MemoryTier
	Tags           []string
	Source         string
	CreatedBy      MemoryCreatedBy
	Kind           MemoryKind
	Importance     float64
	RecallCount    int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastAccessedAt time.Time
	LastRecalledAt time.Time
}

type EntityFact struct {
	ID        string
	TenantID  string
	Entity    string
	Relation  string
	Value     string
	MemoryID  string
	CreatedAt time.Time
}
