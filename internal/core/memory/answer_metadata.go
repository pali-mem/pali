package memory

import (
	"regexp"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

var (
	answerTagPattern          = regexp.MustCompile(`\[[^\]]+\]`)
	answerSpeakerPrefix       = regexp.MustCompile(`^\s*[A-Za-z][A-Za-z0-9 .'\-]{0,80}(?:\s*\([^)]+\))?:\s*`)
	answerQuotedSpanPattern   = regexp.MustCompile(`"([^"]+)"|'([^']+)'`)
	answerRelativeTimePattern = regexp.MustCompile(`(?i)\b(?:yesterday|today|tomorrow|last\s+\w+|next\s+\w+|\d+\s+(?:years?|months?|weeks?|days?)\s+ago|week before|month before|year before)\b`)
	answerDurationPattern     = regexp.MustCompile(`(?i)\b\d+\s+(?:years?|months?|weeks?|days?)\b`)
	answerYearPattern         = regexp.MustCompile(`\b(?:19|20)\d{2}\b`)
	answerMonthPattern        = regexp.MustCompile(`(?i)\b(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\b`)
)

func inferAnswerMetadata(sourceContent string, fact ParsedFact) domain.MemoryAnswerMetadata {
	sourceSentence := bestSourceSentence(sourceContent, fact)
	metadata := domain.MemoryAnswerMetadata{
		AnswerKind:         inferAnswerKind(fact, sourceSentence),
		SourceSentence:     sourceSentence,
		SurfaceSpan:        inferSurfaceSpan(fact, sourceSentence),
		TemporalAnchor:     inferTemporalAnchor(sourceContent, fact, sourceSentence),
		RelativeTimePhrase: inferRelativeTimePhrase(fact.Content, sourceSentence),
		TimeGranularity:    inferTimeGranularity(fact.Content, sourceSentence),
	}
	metadata.ResolvedTimeStart = inferResolvedTimeStart(metadata, fact, sourceSentence)
	if metadata.TimeGranularity == "duration" {
		metadata.ResolvedTimeEnd = metadata.ResolvedTimeStart
	}
	return metadata
}

func bestSourceSentence(sourceContent string, fact ParsedFact) string {
	content := strings.TrimSpace(stripAnswerSurfaceNoise(sourceContent))
	if content == "" {
		return ""
	}
	sentences := splitObservationSentences(content)
	if len(sentences) == 0 {
		return normalizeFactContent(content)
	}
	factTokens := normalizedRankingTokens(strings.Join([]string{fact.Content, fact.Value, fact.QueryViewText}, " "))
	bestSentence := ""
	bestScore := -1.0
	for _, sentence := range sentences {
		score := queryOverlapScore(factTokens, normalizedRankingTokens(sentence))
		if score > bestScore {
			bestScore = score
			bestSentence = sentence
		}
	}
	if bestSentence != "" {
		return normalizeFactContent(bestSentence)
	}
	return normalizeFactContent(content)
}

func stripAnswerSurfaceNoise(raw string) string {
	value := answerTagPattern.ReplaceAllString(strings.TrimSpace(raw), " ")
	value = answerSpeakerPrefix.ReplaceAllString(value, "")
	return strings.Join(strings.Fields(value), " ")
}

func inferAnswerKind(fact ParsedFact, sourceSentence string) string {
	lowered := strings.ToLower(strings.TrimSpace(fact.Content + " " + sourceSentence))
	relation := strings.ToLower(strings.TrimSpace(fact.Relation))
	switch {
	case answerQuotedSpanPattern.MatchString(sourceSentence):
		return "quote"
	case relation == "reason" || relation == "outcome" || strings.Contains(lowered, "because ") || strings.Contains(lowered, " so ") || strings.Contains(lowered, "made ") || strings.Contains(lowered, "realize"):
		return "reason"
	case relation == "family" || relation == "relationship" || relation == "relationship status" || relation == "place" || relation == "role" || relation == "identity" || relation == "artifact" || relation == "organization" || relation == "media":
		return "entity"
	case relation == "event" || strings.TrimSpace(fact.Value) != "" && len(strings.Fields(fact.Value)) <= 5:
		return "entity"
	case answerRelativeTimePattern.MatchString(lowered) || timeTagPattern.MatchString(lowered):
		return "time"
	case strings.Contains(lowered, " yes ") || strings.Contains(lowered, " no ") || strings.HasPrefix(lowered, "yes ") || strings.HasPrefix(lowered, "no "):
		return "boolean"
	default:
		return "span"
	}
}

func inferSurfaceSpan(fact ParsedFact, sourceSentence string) string {
	if sourceSentence != "" {
		if matches := answerQuotedSpanPattern.FindStringSubmatch(sourceSentence); len(matches) > 0 {
			for _, match := range matches[1:] {
				match = strings.TrimSpace(match)
				if match != "" {
					return normalizeFactContent(match)
				}
			}
		}
	}
	for _, candidate := range []string{fact.Value, fact.Content, sourceSentence} {
		value := normalizeFactContent(candidate)
		if value == "" {
			continue
		}
		return value
	}
	return ""
}

func inferTemporalAnchor(sourceContent string, fact ParsedFact, sourceSentence string) string {
	if anchor, ok := sourceTimeAnchor(sourceContent); ok {
		return anchor
	}
	for _, candidate := range []string{sourceSentence, fact.Content} {
		candidate = normalizeFactContent(candidate)
		if candidate == "" {
			continue
		}
		if timeTagPattern.MatchString(strings.ToLower(candidate)) {
			return candidate
		}
	}
	return ""
}

func inferRelativeTimePhrase(factContent, sourceSentence string) string {
	for _, candidate := range []string{sourceSentence, factContent} {
		match := answerRelativeTimePattern.FindString(candidate)
		if strings.TrimSpace(match) != "" {
			return normalizeFactContent(match)
		}
	}
	return ""
}

func inferTimeGranularity(factContent, sourceSentence string) string {
	lowered := strings.ToLower(strings.Join([]string{factContent, sourceSentence}, " "))
	switch {
	case answerDurationPattern.MatchString(lowered):
		return "duration"
	case answerYearPattern.MatchString(lowered) && !answerMonthPattern.MatchString(lowered):
		return "year"
	case answerMonthPattern.MatchString(lowered):
		return "month"
	case answerRelativeTimePattern.MatchString(lowered):
		return "relative"
	case timeTagPattern.MatchString(lowered):
		return "date"
	default:
		return ""
	}
}

func inferResolvedTimeStart(metadata domain.MemoryAnswerMetadata, fact ParsedFact, sourceSentence string) string {
	for _, candidate := range []string{
		metadata.TemporalAnchor,
		metadata.RelativeTimePhrase,
		sourceSentence,
		fact.Content,
		fact.Value,
	} {
		value := normalizeFactContent(candidate)
		if value == "" {
			continue
		}
		if answerDurationPattern.MatchString(value) || answerYearPattern.MatchString(value) || answerMonthPattern.MatchString(value) || timeTagPattern.MatchString(strings.ToLower(value)) {
			return value
		}
	}
	return ""
}
