package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

const (
	heuristicExtractorName    = "heuristic"
	heuristicExtractorVersion = "v1"
	rawTurnExtractorName      = "raw_turn"
	rawTurnExtractorVersion   = "v1"
)

type parserParseResult struct {
	Facts            []ParsedFact
	Extractor        string
	ExtractorVersion string
}

type parserFactIdentity struct {
	CanonicalKey     string
	SourceTurnHash   string
	SourceFactIndex  int
	Extractor        string
	ExtractorVersion string
}

func buildRawTurnIdentity(content string) parserFactIdentity {
	turnHash := buildSourceTurnHash(content)
	return parserFactIdentity{
		CanonicalKey:     hashIdentityParts("raw_turn", turnHash),
		SourceTurnHash:   turnHash,
		SourceFactIndex:  -1,
		Extractor:        rawTurnExtractorName,
		ExtractorVersion: rawTurnExtractorVersion,
	}
}

func buildParsedFactIdentity(
	sourceContent string,
	factIndex int,
	fact ParsedFact,
	extractor string,
	extractorVersion string,
) parserFactIdentity {
	turnHash := buildSourceTurnHash(sourceContent)
	return parserFactIdentity{
		CanonicalKey: hashIdentityParts(
			"parsed_fact",
			turnHash,
			strconv.Itoa(factIndex),
			string(resolveKind(fact.Kind)),
			normalizeFactContent(fact.Content),
			normalizeEntityFactEntity(fact.Entity),
			normalizeEntityFactRelation(fact.Relation),
			normalizeEntityFactValue(fact.Value),
			normalizeExtractorName(extractor),
			normalizeExtractorVersion(extractorVersion),
		),
		SourceTurnHash:   turnHash,
		SourceFactIndex:  factIndex,
		Extractor:        normalizeExtractorName(extractor),
		ExtractorVersion: normalizeExtractorVersion(extractorVersion),
	}
}

func applyIdentityToMemory(memory domain.Memory, identity parserFactIdentity) domain.Memory {
	memory.CanonicalKey = identity.CanonicalKey
	memory.SourceTurnHash = identity.SourceTurnHash
	memory.SourceFactIndex = identity.SourceFactIndex
	memory.Extractor = identity.Extractor
	memory.ExtractorVersion = identity.ExtractorVersion
	return memory
}

func buildSourceTurnHash(content string) string {
	return hashIdentityParts("turn", normalizeFactContent(content))
}

func hashIdentityParts(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(strings.TrimSpace(part)))
		h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:16])
}

func normalizeExtractorName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "parser"
	}
	return value
}

func normalizeExtractorVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "v1"
	}
	return value
}
