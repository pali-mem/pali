package memory

import (
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

var semanticTagSignals = map[string]struct{}{
	"preference":  {},
	"preferences": {},
	"profile":     {},
	"identity":    {},
	"always":      {},
	"user":        {},
}

var semanticContentSignals = []string{
	"user prefers",
	"i prefer",
	"always",
	"never",
	"remember this",
	"my name is",
	"i live in",
	"call me",
}

func resolveTier(in StoreInput) domain.MemoryTier {
	switch in.Tier {
	case domain.MemoryTierWorking, domain.MemoryTierEpisodic, domain.MemoryTierSemantic:
		return in.Tier
	case "", domain.MemoryTierAuto:
		if shouldPromoteToSemantic(in) {
			return domain.MemoryTierSemantic
		}
		return domain.MemoryTierEpisodic
	default:
		return in.Tier
	}
}

func shouldPromoteToSemantic(in StoreInput) bool {
	if in.CreatedBy == domain.MemoryCreatedByUser || in.CreatedBy == domain.MemoryCreatedBySystem {
		return true
	}

	for _, tag := range in.Tags {
		if _, ok := semanticTagSignals[strings.ToLower(strings.TrimSpace(tag))]; ok {
			return true
		}
	}

	content := strings.ToLower(strings.TrimSpace(in.Content))
	for _, signal := range semanticContentSignals {
		if strings.Contains(content, signal) {
			return true
		}
	}
	return false
}
