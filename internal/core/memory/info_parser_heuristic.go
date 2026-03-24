package memory

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

var (
	lowSignalFactPattern    = regexp.MustCompile(`(?i)^(hi|hello|hey|thanks|thank you|ok|okay|cool|great|nice|wow|got it|sure|sounds good|no problem)[.!]?$`)
	questionLikeFactPattern = regexp.MustCompile(`(?i)^(what|who|when|where|why|how|which|whose|did|does|do|is|are|was|were|can|could|would|should|have|has|had|will)\b`)
	shortChatterFactPattern = regexp.MustCompile(`(?i)^(phew|wow|woah|whoa|awesome|amazing|lovely|beautiful|gorgeous|super stoked|so excited|really appreciate it|appreciate it|love it|glad you agree|totally agree|absolutely|yeah(?:,)?(?: definitely| that's true| for sure)?|wow,? great pic.*|thank goodness.*)$`)

	shortNumericPattern             = regexp.MustCompile(`(?i)^\d+(?:\.\d+)?(?:%|k|m|bn)?$`)
	shortDurationPattern            = regexp.MustCompile(`(?i)^\d+\s*(?:year|month|week|day|hour)s?$`)
	shortDateFragmentPattern        = regexp.MustCompile(`(?i)^(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?\s*(?:\d{1,2},?\s*)?(?:\d{2,4})?$|^\d{1,2}[/-]\d{2,4}(?:[/-]\d{2,4})?$`)
	shortStatusPattern              = regexp.MustCompile(`(?i)^(single|married|divorced|widowed|engaged|retired|deceased)$`)
	shortIdentityGenderPattern      = regexp.MustCompile(`(?i)^(non-binary|transgender(?:\s*(?:man|woman))?|cisgender|genderqueer|genderfluid|agender|intersex)$`)
	shortIdentityOrientationPattern = regexp.MustCompile(`(?i)^(gay|lesbian|bisexual|queer|asexual|heterosexual|straight)$`)
)

var shortRoleGazetteer = map[string]struct{}{
	"accountant":   {},
	"analyst":      {},
	"architect":    {},
	"artist":       {},
	"chef":         {},
	"consultant":   {},
	"counselor":    {},
	"designer":     {},
	"developer":    {},
	"doctor":       {},
	"engineer":     {},
	"lawyer":       {},
	"manager":      {},
	"nurse":        {},
	"photographer": {},
	"researcher":   {},
	"student":      {},
	"teacher":      {},
	"therapist":    {},
	"writer":       {},
}

type heuristicInfoParser struct{}

// NewHeuristicInfoParser returns the heuristic parser implementation.
func NewHeuristicInfoParser() InfoParser {
	return heuristicInfoParser{}
}

func (heuristicInfoParser) Parse(_ context.Context, content string, maxFacts int) ([]ParsedFact, error) {
	if maxFacts <= 0 {
		return nil, fmt.Errorf("max facts must be > 0")
	}
	normalized := strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if normalized == "" {
		return []ParsedFact{}, nil
	}

	out := make([]ParsedFact, 0, maxFacts)
	seen := make(map[string]struct{}, maxFacts*2)
	add := func(kind domain.MemoryKind, text string, tags ...string) {
		text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
		if !isInformativeFact(text) {
			return
		}
		key := strings.ToLower(text)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		entity, relation, value := inferEntityRelationValue(text, kind)
		out = append(out, ParsedFact{
			Content:  text,
			Kind:     kind,
			Tags:     tags,
			Entity:   entity,
			Relation: relation,
			Value:    value,
		})
	}

	if eventText, ok := deriveEvent(normalized); ok {
		add(domain.MemoryKindEvent, eventText, "event", "parser")
	}

	observations, err := deriveObservations(normalized, maxFacts)
	if err != nil {
		return nil, err
	}
	for _, obs := range observations {
		add(domain.MemoryKindObservation, obs, "observation", "parser")
		if len(out) >= maxFacts {
			break
		}
	}

	if len(out) == 0 && isInformativeFact(normalized) {
		add(domain.MemoryKindObservation, normalized, "observation", "parser")
	}
	if len(out) > maxFacts {
		return out[:maxFacts], nil
	}
	return out, nil
}

func isInformativeFact(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if lowSignalFactPattern.MatchString(text) {
		return false
	}
	normalized := strings.ToLower(text)
	if strings.HasSuffix(normalized, "?") || questionLikeFactPattern.MatchString(normalized) {
		return false
	}
	if len(strings.Fields(normalized)) <= 6 && shortChatterFactPattern.MatchString(normalized) {
		return false
	}
	tokens := strings.Fields(normalized)
	if len(tokens) >= 3 {
		return true
	}
	return isHighSignalShortFact(normalized)
}

func isHighSignalShortFact(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	if shortNumericPattern.MatchString(text) {
		return true
	}
	if shortDurationPattern.MatchString(text) {
		return true
	}
	if shortDateFragmentPattern.MatchString(text) {
		return true
	}
	if shortStatusPattern.MatchString(text) {
		return true
	}
	if shortIdentityGenderPattern.MatchString(text) {
		return true
	}
	if shortIdentityOrientationPattern.MatchString(text) {
		return true
	}
	if _, ok := shortRoleGazetteer[text]; ok {
		return true
	}
	return false
}
