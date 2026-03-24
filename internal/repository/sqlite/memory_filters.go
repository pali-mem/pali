package sqlite

import (
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

func appendMemoryFilterClause(
	baseSQL string,
	alias string,
	filters domain.MemorySearchFilters,
) (string, []any) {
	args := make([]any, 0, len(filters.Tiers)+len(filters.Kinds))
	clauses := make([]string, 0, 2)
	column := func(name string) string {
		if strings.TrimSpace(alias) == "" {
			return name
		}
		return alias + "." + name
	}

	kinds := normalizeKindsForFilter(filters.Kinds)
	if len(kinds) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",")
		clauses = append(clauses, column("kind")+" IN ("+placeholders+")")
		for _, kind := range kinds {
			args = append(args, string(kind))
		}
	}
	tiers := normalizeTiersForFilter(filters.Tiers)
	if len(tiers) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tiers)), ",")
		clauses = append(clauses, column("tier")+" IN ("+placeholders+")")
		for _, tier := range tiers {
			args = append(args, string(tier))
		}
	}

	if len(clauses) == 0 {
		return baseSQL, args
	}
	filterClause := "\n  AND " + strings.Join(clauses, "\n  AND ")
	if strings.Contains(baseSQL, "\nORDER BY") {
		return strings.Replace(baseSQL, "\nORDER BY", filterClause+"\nORDER BY", 1), args
	}
	if strings.Contains(baseSQL, " ORDER BY") {
		return strings.Replace(baseSQL, " ORDER BY", filterClause+" ORDER BY", 1), args
	}
	return baseSQL + filterClause, args
}

func normalizeKindsForFilter(kinds []domain.MemoryKind) []domain.MemoryKind {
	if len(kinds) == 0 {
		return []domain.MemoryKind{}
	}
	seen := make(map[domain.MemoryKind]struct{}, len(kinds))
	out := make([]domain.MemoryKind, 0, len(kinds))
	for _, kind := range kinds {
		switch kind {
		case domain.MemoryKindRawTurn, domain.MemoryKindObservation, domain.MemoryKindSummary, domain.MemoryKindEvent:
		default:
			continue
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		out = append(out, kind)
	}
	return out
}

func normalizeTiersForFilter(tiers []domain.MemoryTier) []domain.MemoryTier {
	if len(tiers) == 0 {
		return []domain.MemoryTier{}
	}
	seen := make(map[domain.MemoryTier]struct{}, len(tiers))
	out := make([]domain.MemoryTier, 0, len(tiers))
	for _, tier := range tiers {
		switch tier {
		case domain.MemoryTierWorking, domain.MemoryTierEpisodic, domain.MemoryTierSemantic:
		default:
			continue
		}
		if _, ok := seen[tier]; ok {
			continue
		}
		seen[tier] = struct{}{}
		out = append(out, tier)
	}
	return out
}
