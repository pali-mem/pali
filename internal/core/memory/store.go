package memory

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pali-mem/pali/internal/domain"
)

const (
	// DualWriteDedupeThreshold is the semantic similarity cutoff used to suppress
	// near-duplicate legacy structured dual-write entries.
	DualWriteDedupeThreshold = 0.92

	dualWriteLexicalSafetyThreshold = 0.50
	dualWriteCandidateTopK          = 12
	minParsedFactContentLength      = 25
)

var dualWriteNegationTokens = map[string]struct{}{
	"not":   {},
	"no":    {},
	"never": {},
}

var factCompoundSplitPattern = regexp.MustCompile(`(?i)\s+and\s+`)

type StoreInput struct {
	TenantID       string
	Content        string
	Tier           domain.MemoryTier
	Kind           domain.MemoryKind
	Tags           []string
	Source         string
	CreatedBy      domain.MemoryCreatedBy
	AnswerMetadata domain.MemoryAnswerMetadata
}

func (s *Service) Store(ctx context.Context, in StoreInput) (domain.Memory, error) {
	stored, err := s.StoreBatch(ctx, []StoreInput{in})
	if err != nil {
		return domain.Memory{}, err
	}
	if len(stored) != 1 {
		return domain.Memory{}, fmt.Errorf("store batch returned %d records for single input", len(stored))
	}
	return stored[0], nil
}

type preparedStoreInput struct {
	idx          int
	input        StoreInput
	resolvedTier domain.MemoryTier
	resolvedKind domain.MemoryKind
}

func (s *Service) StoreBatch(ctx context.Context, inputs []StoreInput) ([]domain.Memory, error) {
	if len(inputs) == 0 {
		return []domain.Memory{}, nil
	}

	prepared, err := s.prepareStoreInputs(ctx, inputs)
	if err != nil {
		return nil, err
	}

	results := make([]domain.Memory, len(prepared))
	nonParser := make([]preparedStoreInput, 0, len(prepared))
	parserItems := make([]preparedStoreInput, 0, len(prepared))
	for _, item := range prepared {
		if s.parser.Enabled && item.resolvedKind == domain.MemoryKindRawTurn && s.infoParser != nil {
			parserItems = append(parserItems, item)
			continue
		}
		nonParser = append(nonParser, item)
	}

	if len(parserItems) > 0 {
		stored, err := s.storeBatchWithParser(ctx, parserItems)
		if err != nil {
			return nil, err
		}
		for i := range parserItems {
			results[parserItems[i].idx] = stored[i]
		}
	}

	if len(nonParser) > 0 {
		stored, err := s.storeBatchWithoutParser(ctx, nonParser)
		if err != nil {
			return nil, err
		}
		for i := range nonParser {
			results[nonParser[i].idx] = stored[i]
		}
	}

	return results, nil
}

func (s *Service) prepareStoreInputs(ctx context.Context, inputs []StoreInput) ([]preparedStoreInput, error) {
	if s.scorer == nil || s.embedder == nil || s.vector == nil {
		return nil, fmt.Errorf("memory service dependencies are not initialized")
	}

	prepared := make([]preparedStoreInput, 0, len(inputs))
	tenantChecked := make(map[string]struct{}, len(inputs))
	for i := range inputs {
		in := inputs[i]
		tenantID := strings.TrimSpace(in.TenantID)
		if tenantID == "" || strings.TrimSpace(in.Content) == "" {
			return nil, domain.ErrInvalidInput
		}
		if _, ok := tenantChecked[tenantID]; !ok {
			if err := s.ensureTenantExists(ctx, tenantID); err != nil {
				return nil, err
			}
			tenantChecked[tenantID] = struct{}{}
		}
		in.TenantID = tenantID
		prepared = append(prepared, preparedStoreInput{
			idx:          i,
			input:        in,
			resolvedTier: resolveTier(in),
			resolvedKind: resolveKind(in.Kind),
		})
	}
	return prepared, nil
}

func (s *Service) storeBatchWithoutParser(ctx context.Context, items []preparedStoreInput) ([]domain.Memory, error) {
	if len(items) == 0 {
		return []domain.Memory{}, nil
	}

	contents := make([]string, 0, len(items))
	for _, item := range items {
		contents = append(contents, item.input.Content)
	}
	importanceScores, err := s.scoreContents(ctx, contents)
	if err != nil {
		return nil, err
	}
	if len(importanceScores) != len(items) {
		return nil, fmt.Errorf("importance score count mismatch: got %d for %d inputs", len(importanceScores), len(items))
	}

	memories := make([]domain.Memory, 0, len(items))
	for i, item := range items {
		memories = append(memories, domain.Memory{
			TenantID:   item.input.TenantID,
			Content:    item.input.Content,
			Tier:       item.resolvedTier,
			Kind:       item.resolvedKind,
			Tags:       item.input.Tags,
			Source:     item.input.Source,
			CreatedBy:  item.input.CreatedBy,
			Importance: importanceScores[i],
		})
	}

	embeddings, err := s.embedContents(ctx, contents)
	if err != nil {
		return nil, err
	}
	stored, err := s.storeInRepo(ctx, memories)
	if err != nil {
		return nil, err
	}
	if len(stored) != len(embeddings) {
		return nil, fmt.Errorf("store result mismatch: stored=%d embeddings=%d", len(stored), len(embeddings))
	}
	if err := s.upsertStoredEmbeddings(ctx, stored, embeddings); err != nil {
		return nil, err
	}

	for _, memory := range stored {
		if err := s.writeCanonicalStructuredDerived(ctx, memory); err != nil {
			return nil, err
		}
	}
	return stored, nil
}

func (s *Service) storeWithParser(
	ctx context.Context,
	in StoreInput,
	resolvedTier domain.MemoryTier,
	resolvedKind domain.MemoryKind,
) (domain.Memory, error) {
	parsed, err := s.parseFactsWithFallback(ctx, in.Content, 1)
	if err != nil {
		if s.parser.StoreRawTurn {
			return s.storeOne(ctx, domain.Memory{
				TenantID:       in.TenantID,
				Content:        in.Content,
				Tier:           resolvedTier,
				Kind:           resolvedKind,
				Tags:           in.Tags,
				Source:         in.Source,
				CreatedBy:      in.CreatedBy,
				AnswerMetadata: in.AnswerMetadata,
			})
		}
		// Parser errors should not drop writes; fallback to raw turn store.
		return s.storeOne(ctx, domain.Memory{
			TenantID:       in.TenantID,
			Content:        in.Content,
			Tier:           resolvedTier,
			Kind:           resolvedKind,
			Tags:           in.Tags,
			Source:         in.Source,
			CreatedBy:      in.CreatedBy,
			AnswerMetadata: in.AnswerMetadata,
		})
	}
	entityTriples := 0
	for _, fact := range parsed.Facts {
		if parsedFactHasEntityTriple(fact) {
			entityTriples++
		}
	}
	s.logInfof("[pali-store] turn=1 raw_turns=1 facts=%d entity_triples=%d", len(parsed.Facts), entityTriples)
	for _, fact := range parsed.Facts {
		s.logDebugf("[pali-parser] turn=1 fact=%q", sanitizeLogSnippet(fact.Content, 220))
	}

	embeddingByContent, err := s.precomputeParserEmbeddings(ctx, in.Content, parsed.Facts)
	if err != nil {
		// Batch precompute is an optimization; fallback preserves existing behavior.
		embeddingByContent = nil
	}

	var storedRaw domain.Memory
	if s.parser.StoreRawTurn {
		storedRaw, err = s.storeOneWithOptionalEmbedding(ctx, applyIdentityToMemory(domain.Memory{
			TenantID:       in.TenantID,
			Content:        in.Content,
			Tier:           resolvedTier,
			Kind:           resolvedKind,
			Tags:           in.Tags,
			Source:         in.Source,
			CreatedBy:      in.CreatedBy,
			AnswerMetadata: in.AnswerMetadata,
		}, buildRawTurnIdentity(in.Content)), embeddingByContent)
		if err != nil {
			return domain.Memory{}, err
		}
	}

	var firstParsed *domain.Memory
	for factIdx, fact := range parsed.Facts {
		m, err := s.applyParsedFact(
			ctx,
			in.TenantID,
			in.Content,
			in.Tags,
			in.Source,
			fact,
			factIdx,
			parsed.Extractor,
			parsed.ExtractorVersion,
			embeddingByContent,
		)
		if err != nil {
			return domain.Memory{}, err
		}
		if m != nil {
			if entityFact, ok := buildEntityFactRecord(*m, fact); ok {
				if err := s.storeEntityFacts(ctx, []domain.EntityFact{entityFact}); err != nil {
					return domain.Memory{}, err
				}
			}
		}
		if firstParsed == nil && m != nil {
			firstParsed = m
		}
	}

	if s.parser.StoreRawTurn {
		return storedRaw, nil
	}
	if firstParsed != nil {
		return *firstParsed, nil
	}
	// Parser yielded no storable facts; preserve reliability by storing raw input.
	return s.storeOneWithOptionalEmbedding(ctx, domain.Memory{
		TenantID:       in.TenantID,
		Content:        in.Content,
		Tier:           resolvedTier,
		Kind:           resolvedKind,
		Tags:           in.Tags,
		Source:         in.Source,
		CreatedBy:      in.CreatedBy,
		AnswerMetadata: in.AnswerMetadata,
	}, embeddingByContent)
}

func (s *Service) parseFactsWithFallback(ctx context.Context, content string, turn int) (parserParseResult, error) {
	parserStart := time.Now()
	primaryFacts, primaryErr := s.infoParser.Parse(ctx, content, s.parser.MaxFacts)
	if primaryErr == nil && len(primaryFacts) > 0 {
		prepared := prepareParsedFactsForStore(content, primaryFacts, s.parser.AnswerSpanRetentionEnabled)
		maxPreparedFacts := s.parser.MaxFacts
		if strings.EqualFold(strings.TrimSpace(s.parser.Provider), heuristicExtractorName) && s.parser.StoreRawTurn && maxPreparedFacts > 1 {
			maxPreparedFacts = 1
		}
		prepared = optimizePreparedFactsForParser(content, prepared, s.parser.Provider, maxPreparedFacts)
		if len(prepared) > 0 {
			s.logDebugf(
				"[pali-parser] turn=%d provider=%s model=%s status=ok ms=%d facts=%d",
				turn,
				s.parser.Provider,
				s.parser.Model,
				time.Since(parserStart).Milliseconds(),
				len(prepared),
			)
			return parserParseResult{
				Facts:            prepared,
				Extractor:        s.parser.Provider,
				ExtractorVersion: s.parser.Model,
			}, nil
		}
	}

	fallback := NewHeuristicInfoParser()
	fallbackFacts, fallbackErr := fallback.Parse(ctx, content, s.parser.MaxFacts)
	if fallbackErr == nil && len(fallbackFacts) > 0 {
		prepared := prepareParsedFactsForStore(content, fallbackFacts, s.parser.AnswerSpanRetentionEnabled)
		maxPreparedFacts := s.parser.MaxFacts
		if s.parser.StoreRawTurn && maxPreparedFacts > 1 {
			maxPreparedFacts = 1
		}
		prepared = optimizePreparedFactsForParser(content, prepared, heuristicExtractorName, maxPreparedFacts)
		if len(prepared) > 0 {
			s.logDebugf(
				"[pali-parser] turn=%d provider=%s model=%s status=fallback_heuristic ms=%d facts=%d",
				turn,
				s.parser.Provider,
				s.parser.Model,
				time.Since(parserStart).Milliseconds(),
				len(prepared),
			)
			return parserParseResult{
				Facts:            prepared,
				Extractor:        heuristicExtractorName,
				ExtractorVersion: heuristicExtractorVersion,
			}, nil
		}
	}

	if primaryErr != nil {
		s.logDebugf(
			"[pali-parser] turn=%d provider=%s model=%s status=error ms=%d err=%v",
			turn,
			s.parser.Provider,
			s.parser.Model,
			time.Since(parserStart).Milliseconds(),
			primaryErr,
		)
		return parserParseResult{}, primaryErr
	}
	if fallbackErr != nil {
		s.logDebugf(
			"[pali-parser] turn=%d provider=%s model=%s status=fallback_error ms=%d err=%v",
			turn,
			s.parser.Provider,
			s.parser.Model,
			time.Since(parserStart).Milliseconds(),
			fallbackErr,
		)
		return parserParseResult{}, fallbackErr
	}
	s.logDebugf(
		"[pali-parser] turn=%d provider=%s model=%s status=empty ms=%d facts=0",
		turn,
		s.parser.Provider,
		s.parser.Model,
		time.Since(parserStart).Milliseconds(),
	)
	return parserParseResult{
		Facts:            []ParsedFact{},
		Extractor:        s.parser.Provider,
		ExtractorVersion: s.parser.Model,
	}, nil
}

func (s *Service) precomputeParserEmbeddings(
	ctx context.Context,
	rawContent string,
	facts []ParsedFact,
) (map[string][]float32, error) {
	batchEmbedder, ok := s.embedder.(domain.BatchEmbedder)
	if !ok || batchEmbedder == nil {
		return nil, nil
	}

	texts := make([]string, 0, len(facts)+1)
	seen := make(map[string]struct{}, len(facts)+1)
	push := func(text string) {
		if text == "" {
			return
		}
		if _, ok := seen[text]; ok {
			return
		}
		seen[text] = struct{}{}
		texts = append(texts, text)
	}

	push(rawContent)
	for _, fact := range facts {
		push(embeddingTextForParsedFact(fact))
	}
	if len(texts) == 0 {
		return nil, nil
	}

	embeddings, err := batchEmbedder.BatchEmbed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(embeddings) != len(texts) {
		return nil, fmt.Errorf("batch embedding count mismatch: got %d embeddings for %d texts", len(embeddings), len(texts))
	}

	out := make(map[string][]float32, len(texts))
	for i := range texts {
		if len(embeddings[i]) == 0 {
			return nil, fmt.Errorf("batch embedding vector is empty at index %d", i)
		}
		out[texts[i]] = append([]float32{}, embeddings[i]...)
	}
	return out, nil
}

func embeddingTextForParsedFact(fact ParsedFact) string {
	content := normalizeFactContent(fact.Content)
	queryView := filterSpecificQueryViewText(normalizeFactContent(fact.QueryViewText))
	if queryView == "" {
		return content
	}
	return content + "\n" + queryView
}

func embeddingLookupTextForMemory(m domain.Memory) string {
	content := normalizeFactContent(m.Content)
	queryView := normalizeFactContent(m.QueryViewText)
	if queryView == "" {
		return content
	}
	return content + "\n" + queryView
}

func normalizeFactContent(content string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
}

func (s *Service) applyParsedFact(
	ctx context.Context,
	tenantID string,
	sourceContent string,
	baseTags []string,
	baseSource string,
	fact ParsedFact,
	factIndex int,
	extractor string,
	extractorVersion string,
	embeddingByContent map[string][]float32,
) (*domain.Memory, error) {
	content := normalizeFactContent(fact.Content)
	if !shouldStoreParsedFactContent(content) {
		return nil, nil
	}
	kind := resolveKind(fact.Kind)
	if kind != domain.MemoryKindEvent && kind != domain.MemoryKindObservation {
		kind = domain.MemoryKindObservation
	}
	identity := buildParsedFactIdentity(sourceContent, factIndex, fact, extractor, extractorVersion)

	exactMatch, err := s.findMemoryByCanonicalKey(ctx, tenantID, identity.CanonicalKey)
	if err != nil {
		return nil, err
	}
	if exactMatch != nil {
		return exactMatch, nil
	}
	exactMatch, err = s.findMemoryByRelationTuple(ctx, tenantID, fact)
	if err != nil {
		return nil, err
	}
	if exactMatch != nil {
		return exactMatch, nil
	}

	stored, err := s.storeOneWithOptionalEmbedding(ctx, applyIdentityToMemory(domain.Memory{
		TenantID:       tenantID,
		Content:        content,
		QueryViewText:  fact.QueryViewText,
		Tier:           domain.MemoryTierSemantic,
		Kind:           kind,
		Tags:           mergeTags(baseTags, append(append([]string{}, fact.Tags...), "memory_op:add", "memory_state:active")...),
		Source:         appendDerivedSource(baseSource, "parser"),
		CreatedBy:      domain.MemoryCreatedBySystem,
		AnswerMetadata: fact.AnswerMetadata,
	}, identity), embeddingByContent)
	if err != nil {
		return nil, err
	}
	return &stored, nil
}

func shouldReplaceMemory(existing domain.Memory, nextContent string) bool {
	existingLen := len(strings.TrimSpace(existing.Content))
	nextLen := len(strings.TrimSpace(nextContent))
	if nextLen == 0 {
		return false
	}
	return nextLen >= existingLen+12
}

func (s *Service) writeCanonicalStructuredDerived(ctx context.Context, stored domain.Memory) error {
	if !s.structured.Enabled || stored.Kind != domain.MemoryKindRawTurn {
		return nil
	}
	if !s.structured.DualWriteObservations && !s.structured.DualWriteEvents {
		return nil
	}
	heuristic := NewHeuristicInfoParser()
	facts, err := heuristic.Parse(ctx, stored.Content, structuredCanonicalParseLimit(s.structured))
	if err != nil {
		return err
	}
	facts = filterCanonicalStructuredFacts(prepareParsedFactsForStore(stored.Content, facts, s.parser.AnswerSpanRetentionEnabled), s.structured)
	if len(facts) == 0 {
		return nil
	}
	embeddingByContent, err := s.precomputeParserEmbeddings(ctx, stored.Content, facts)
	if err != nil {
		embeddingByContent = nil
	}

	for factIdx, fact := range facts {
		memory, err := s.applyParsedFact(
			ctx,
			stored.TenantID,
			stored.Content,
			stored.Tags,
			stored.Source,
			fact,
			factIdx,
			heuristicExtractorName,
			heuristicExtractorVersion,
			embeddingByContent,
		)
		if err != nil {
			return err
		}
		if memory == nil {
			continue
		}
		if entityFact, ok := buildEntityFactRecord(*memory, fact); ok {
			if err := s.storeEntityFacts(ctx, []domain.EntityFact{entityFact}); err != nil {
				return err
			}
		}
	}
	return nil
}

func filterCanonicalStructuredFacts(facts []ParsedFact, opts StructuredMemoryOptions) []ParsedFact {
	if len(facts) == 0 {
		return []ParsedFact{}
	}
	out := make([]ParsedFact, 0, len(facts))
	observations := 0
	events := 0
	maxObservations := max(1, opts.MaxObservations)
	for _, fact := range facts {
		switch fact.Kind {
		case domain.MemoryKindEvent:
			if !opts.DualWriteEvents {
				continue
			}
			if events >= 1 {
				continue
			}
			events++
		default:
			if !opts.DualWriteObservations {
				continue
			}
			if observations >= maxObservations {
				continue
			}
			observations++
		}
		out = append(out, fact)
	}
	return out
}

func structuredCanonicalParseLimit(opts StructuredMemoryOptions) int {
	limit := max(1, opts.MaxObservations)
	if opts.DualWriteObservations {
		limit += max(1, opts.MaxObservations)
	}
	if opts.DualWriteEvents {
		limit++
	}
	return limit
}

func (s *Service) storeOne(ctx context.Context, m domain.Memory) (domain.Memory, error) {
	embedding, err := s.embedder.Embed(ctx, embeddingLookupTextForMemory(m))
	if err != nil {
		return domain.Memory{}, err
	}
	return s.storeOnePrecomputed(ctx, m, embedding)
}

func (s *Service) storeOneWithOptionalEmbedding(
	ctx context.Context,
	m domain.Memory,
	embeddingByContent map[string][]float32,
) (domain.Memory, error) {
	if len(embeddingByContent) > 0 {
		if embedding, ok := embeddingByContent[embeddingLookupTextForMemory(m)]; ok && len(embedding) > 0 {
			return s.storeOnePrecomputed(ctx, m, embedding)
		}
	}
	return s.storeOne(ctx, m)
}

func (s *Service) storeOnePrecomputed(ctx context.Context, m domain.Memory, embedding []float32) (domain.Memory, error) {
	if len(embedding) == 0 {
		return domain.Memory{}, fmt.Errorf("embedding must not be empty")
	}
	importance, err := s.scorer.Score(ctx, m.Content)
	if err != nil {
		return domain.Memory{}, err
	}
	m.Importance = importance

	storedBatch, err := s.storeInRepo(ctx, []domain.Memory{m})
	if err != nil {
		return domain.Memory{}, err
	}
	if len(storedBatch) != 1 {
		return domain.Memory{}, fmt.Errorf("store returned %d records for single memory", len(storedBatch))
	}
	stored := storedBatch[0]
	if err := s.upsertStoredEmbeddings(ctx, []domain.Memory{stored}, [][]float32{embedding}); err != nil {
		return domain.Memory{}, err
	}
	return stored, nil
}

func prepareParsedFactsForStore(sourceContent string, facts []ParsedFact, retainAnswerSpans bool) []ParsedFact {
	if len(facts) == 0 {
		return []ParsedFact{}
	}

	anchor, hasAnchor := sourceTimeAnchor(sourceContent)
	prepared := make([]ParsedFact, 0, len(facts))
	for _, fact := range facts {
		content := normalizeFactContent(canonicalizeTurnStyleFact(sourceContent, fact.Content))
		if !shouldStoreParsedFactContent(content) {
			continue
		}
		if hasAnchor && !timeTagPattern.MatchString(strings.ToLower(content)) {
			// Prepend as natural-language phrase so the date is both embeddable and
			// extractable by the FULL_DATE_RE pattern (e.g. "8 May 2023").
			content = "On " + anchor + ", " + content
		}
		fact.Content = content
		if !parsedFactHasEntityTriple(fact) {
			entity, relation, value := inferEntityRelationValue(content, fact.Kind)
			if strings.TrimSpace(fact.Entity) == "" {
				fact.Entity = entity
			}
			if strings.TrimSpace(fact.Relation) == "" {
				fact.Relation = relation
			}
			if strings.TrimSpace(fact.Value) == "" {
				fact.Value = value
			}
		}
		if strings.TrimSpace(fact.Entity) == "" &&
			(strings.TrimSpace(fact.Relation) != "" || strings.TrimSpace(fact.Value) != "") {
			fact.Entity = inferEntityFromFact(content)
			if strings.TrimSpace(fact.Entity) == "" {
				fact.Entity = "user"
			}
		}
		if !passesCanonicalFactAdmission(sourceContent, fact) {
			continue
		}
		fact.QueryViewText = filterSpecificQueryViewText(buildFactQuestionView(fact))
		if retainAnswerSpans {
			fact.AnswerMetadata = inferAnswerMetadata(sourceContent, fact)
		}
		prepared = append(prepared, expandCompoundPreparedFacts(sourceContent, fact, retainAnswerSpans)...)
	}
	return dedupePreparedFacts(prepared)
}

func optimizePreparedFactsForParser(sourceContent string, facts []ParsedFact, provider string, maxFacts int) []ParsedFact {
	if len(facts) == 0 {
		return []ParsedFact{}
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	filtered := make([]ParsedFact, 0, len(facts))
	for _, fact := range facts {
		if provider == heuristicExtractorName && shouldDropHeuristicSpeechFact(fact) {
			continue
		}
		filtered = append(filtered, fact)
	}
	filtered = dedupePreparedFacts(filtered)
	if provider != heuristicExtractorName || len(filtered) <= 1 {
		return filtered
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		iScore := heuristicFactConfidenceScore(sourceContent, filtered[i])
		jScore := heuristicFactConfidenceScore(sourceContent, filtered[j])
		if iScore != jScore {
			return iScore > jScore
		}
		return len(filtered[i].Content) < len(filtered[j].Content)
	})

	limit := 2
	if maxFacts > 0 && maxFacts < limit {
		limit = maxFacts
	}
	if limit <= 0 || len(filtered) <= limit {
		return filtered
	}
	return filtered[:limit]
}

func shouldDropHeuristicSpeechFact(fact ParsedFact) bool {
	content := strings.ToLower(normalizeFactContent(fact.Content))
	if !genericSpeechPattern.MatchString(content) {
		return false
	}
	if strings.Contains(content, "said that") {
		return true
	}
	candidate := normalizeEntityFactValue(fact.Value)
	if candidate == "" {
		candidate = inferSpecificValueCandidate(fact.Content)
	}
	candidate = strings.ToLower(normalizeFactContent(candidate))
	if candidate == "" {
		return true
	}
	if chatterLeadPattern.MatchString(candidate) {
		return true
	}
	words := strings.Fields(candidate)
	return len(words) < 3
}

func heuristicFactConfidenceScore(sourceContent string, fact ParsedFact) int {
	score := 0
	content := normalizeFactContent(fact.Content)
	if content == "" {
		return -100
	}
	relation := normalizeEntityFactRelation(fact.Relation)
	if relation != "" && relation != "unknown" {
		score += 4
	}
	entity := strings.TrimSpace(fact.Entity)
	if entity == "" {
		entity = inferEntityFromFact(content)
	}
	if entity != "" {
		score += 2
	}
	value := normalizeEntityFactValue(fact.Value)
	if value == "" {
		value = inferSpecificValueCandidate(content)
	}
	if len(strings.Fields(value)) >= 2 {
		score += 2
	}
	if strings.TrimSpace(fact.QueryViewText) != "" {
		score += 1
	}
	if isTemporalFact(content, fact) && hasAbsoluteOrAnchoredTime(sourceContent, content) {
		score += 1
	}
	lowerContent := strings.ToLower(content)
	if genericSpeechPattern.MatchString(lowerContent) {
		score -= 5
	}
	if len(content) > 220 {
		score--
	}
	return score
}

func expandCompoundPreparedFacts(sourceContent string, fact ParsedFact, retainAnswerSpans bool) []ParsedFact {
	entity := strings.TrimSpace(fact.Entity)
	if entity == "" {
		entity = inferEntityFromFact(fact.Content)
	}
	clauses, ok := splitCompoundFactClauses(fact.Content, entity)
	if !ok || len(clauses) < 2 {
		return []ParsedFact{fact}
	}

	out := make([]ParsedFact, 0, len(clauses))
	for _, clause := range clauses {
		candidate := fact
		candidate.Content = clause
		inferredEntity, inferredRelation, inferredValue := inferEntityRelationValue(clause, candidate.Kind)
		if strings.TrimSpace(inferredEntity) != "" {
			candidate.Entity = inferredEntity
		} else if strings.TrimSpace(candidate.Entity) == "" {
			candidate.Entity = inferEntityFromFact(clause)
		}
		if strings.TrimSpace(inferredRelation) != "" {
			candidate.Relation = inferredRelation
		}
		if strings.TrimSpace(inferredValue) != "" {
			candidate.Value = inferredValue
		}
		if strings.TrimSpace(candidate.Entity) == "" &&
			(strings.TrimSpace(candidate.Relation) != "" || strings.TrimSpace(candidate.Value) != "") {
			candidate.Entity = inferEntityFromFact(clause)
			if strings.TrimSpace(candidate.Entity) == "" {
				candidate.Entity = "user"
			}
		}
		if !passesCanonicalFactAdmission(sourceContent, candidate) {
			// If one clause is noisy or malformed, keep the original fact to avoid data loss.
			return []ParsedFact{fact}
		}
		candidate.QueryViewText = filterSpecificQueryViewText(buildFactQuestionView(candidate))
		if retainAnswerSpans {
			candidate.AnswerMetadata = inferAnswerMetadata(sourceContent, candidate)
		}
		out = append(out, candidate)
	}
	if len(out) == 0 {
		return []ParsedFact{fact}
	}
	return out
}

func splitCompoundFactClauses(content, entity string) ([]string, bool) {
	content = normalizeFactContent(content)
	entity = normalizeFactContent(entity)
	if content == "" || entity == "" {
		return nil, false
	}
	parts := factCompoundSplitPattern.Split(content, -1)
	if len(parts) < 2 || len(parts) > 3 {
		return nil, false
	}
	loweredEntity := strings.ToLower(entity)
	clauses := make([]string, 0, len(parts))
	for _, part := range parts {
		clause := normalizeFactContent(strings.Trim(part, " \t\r\n,;"))
		if clause == "" {
			return nil, false
		}
		loweredClause := strings.ToLower(clause)
		if !strings.HasPrefix(loweredClause, loweredEntity+" ") {
			return nil, false
		}
		clauses = append(clauses, clause)
	}
	return clauses, true
}

func dedupePreparedFacts(facts []ParsedFact) []ParsedFact {
	if len(facts) == 0 {
		return []ParsedFact{}
	}
	out := make([]ParsedFact, 0, len(facts))
	seen := make(map[string]struct{}, len(facts))
	for _, fact := range facts {
		key := preparedFactDedupeKey(fact)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, fact)
	}
	return out
}

func preparedFactDedupeKey(fact ParsedFact) string {
	content := strings.ToLower(strings.Trim(strings.TrimSpace(fact.Content), " \t\r\n.,;:!?\"'"))
	if tupleKey, ok := normalizedRelationTupleKey(fact); ok {
		return content + "|" + tupleKey
	}
	return content
}

func shouldStoreParsedFactContent(content string) bool {
	content = normalizeFactContent(content)
	if content == "" {
		return false
	}
	if len([]rune(content)) >= minParsedFactContentLength {
		return true
	}
	return isInformativeFact(content)
}

func (s *Service) storeInRepo(ctx context.Context, memories []domain.Memory) ([]domain.Memory, error) {
	if len(memories) == 0 {
		return []domain.Memory{}, nil
	}
	prepared := make([]domain.Memory, len(memories))
	for i := range memories {
		prepared[i] = applyImplicitMemoryIdentity(memories[i])
	}
	if batchRepo, ok := s.repo.(domain.MemoryBatchRepository); ok && batchRepo != nil {
		return batchRepo.StoreBatch(ctx, prepared)
	}

	stored := make([]domain.Memory, 0, len(prepared))
	for _, memory := range prepared {
		m, err := s.repo.Store(ctx, memory)
		if err != nil {
			return nil, err
		}
		stored = append(stored, m)
	}
	return stored, nil
}

func applyImplicitMemoryIdentity(memory domain.Memory) domain.Memory {
	if memory.Kind != domain.MemoryKindRawTurn {
		return memory
	}
	if strings.TrimSpace(memory.CanonicalKey) != "" {
		if memory.SourceFactIndex == 0 {
			memory.SourceFactIndex = -1
		}
		return memory
	}
	return applyIdentityToMemory(memory, buildRawTurnIdentity(memory.Content))
}

func (s *Service) findMemoryByCanonicalKey(
	ctx context.Context,
	tenantID, canonicalKey string,
) (*domain.Memory, error) {
	if strings.TrimSpace(canonicalKey) == "" {
		return nil, nil
	}
	repo, ok := s.repo.(domain.MemoryCanonicalKeyRepository)
	if !ok || repo == nil {
		return nil, nil
	}
	return repo.FindByCanonicalKey(ctx, tenantID, canonicalKey)
}

func (s *Service) embedContents(ctx context.Context, contents []string) ([][]float32, error) {
	if len(contents) == 0 {
		return [][]float32{}, nil
	}
	if batchEmbedder, ok := s.embedder.(domain.BatchEmbedder); ok && batchEmbedder != nil {
		embeddings, err := batchEmbedder.BatchEmbed(ctx, contents)
		if err == nil {
			if len(embeddings) != len(contents) {
				return nil, fmt.Errorf("batch embedding count mismatch: got %d for %d contents", len(embeddings), len(contents))
			}
			for i := range embeddings {
				if len(embeddings[i]) == 0 {
					return nil, fmt.Errorf("batch embedding vector is empty at index %d", i)
				}
			}
			return embeddings, nil
		}
	}

	embeddings := make([][]float32, 0, len(contents))
	for i := range contents {
		vec, err := s.embedder.Embed(ctx, contents[i])
		if err != nil {
			return nil, err
		}
		if len(vec) == 0 {
			return nil, fmt.Errorf("embedding vector is empty at index %d", i)
		}
		embeddings = append(embeddings, vec)
	}
	return embeddings, nil
}

func (s *Service) scoreContents(ctx context.Context, contents []string) ([]float64, error) {
	if len(contents) == 0 {
		return []float64{}, nil
	}
	if batchScorer, ok := s.scorer.(domain.BatchImportanceScorer); ok && batchScorer != nil {
		scores, err := batchScorer.BatchScore(ctx, contents)
		if err == nil {
			if len(scores) != len(contents) {
				return nil, fmt.Errorf("batch importance score count mismatch: got %d for %d contents", len(scores), len(contents))
			}
			for i := range scores {
				if scores[i] < 0 {
					scores[i] = 0
				}
				if scores[i] > 1 {
					scores[i] = 1
				}
			}
			return scores, nil
		}
	}

	scores := make([]float64, 0, len(contents))
	for i := range contents {
		score, err := s.scorer.Score(ctx, contents[i])
		if err != nil {
			return nil, err
		}
		if score < 0 {
			score = 0
		}
		if score > 1 {
			score = 1
		}
		scores = append(scores, score)
	}
	return scores, nil
}

func (s *Service) upsertStoredEmbeddings(ctx context.Context, stored []domain.Memory, embeddings [][]float32) error {
	if len(stored) != len(embeddings) {
		return fmt.Errorf("upsert embedding mismatch: stored=%d embeddings=%d", len(stored), len(embeddings))
	}
	if len(stored) == 0 {
		return nil
	}
	memoryIDs := make([]string, 0, len(stored))
	for _, memory := range stored {
		memoryIDs = append(memoryIDs, memory.ID)
	}
	tenantID := stored[0].TenantID
	s.markIndexState(ctx, tenantID, memoryIDs, domain.MemoryIndexOperationUpsert, domain.MemoryIndexStatePending, nil)

	if batchVector, ok := s.vector.(domain.VectorBatchStore); ok && batchVector != nil {
		upserts := make([]domain.VectorUpsert, 0, len(stored))
		for i := range stored {
			upserts = append(upserts, domain.VectorUpsert{
				TenantID:  stored[i].TenantID,
				MemoryID:  stored[i].ID,
				Embedding: embeddings[i],
			})
		}
		if err := batchVector.UpsertBatch(ctx, upserts); err != nil {
			s.markIndexState(ctx, tenantID, memoryIDs, domain.MemoryIndexOperationUpsert, domain.MemoryIndexStateFailed, err)
			return err
		}
		s.markIndexState(ctx, tenantID, memoryIDs, domain.MemoryIndexOperationUpsert, domain.MemoryIndexStateIndexed, nil)
		return nil
	}

	indexedIDs := make([]string, 0, len(stored))
	for i := range stored {
		if err := s.vector.Upsert(ctx, stored[i].TenantID, stored[i].ID, embeddings[i]); err != nil {
			failedIDs := make([]string, 0, len(stored)-i)
			failedIDs = append(failedIDs, stored[i].ID)
			for j := i + 1; j < len(stored); j++ {
				failedIDs = append(failedIDs, stored[j].ID)
			}
			if len(indexedIDs) > 0 {
				s.markIndexState(ctx, tenantID, indexedIDs, domain.MemoryIndexOperationUpsert, domain.MemoryIndexStateIndexed, nil)
			}
			s.markIndexState(ctx, tenantID, failedIDs, domain.MemoryIndexOperationUpsert, domain.MemoryIndexStateFailed, err)
			return err
		}
		indexedIDs = append(indexedIDs, stored[i].ID)
	}
	if len(indexedIDs) > 0 {
		s.markIndexState(ctx, tenantID, indexedIDs, domain.MemoryIndexOperationUpsert, domain.MemoryIndexStateIndexed, nil)
	}
	return nil
}

func resolveKind(kind domain.MemoryKind) domain.MemoryKind {
	switch kind {
	case domain.MemoryKindObservation, domain.MemoryKindSummary, domain.MemoryKindEvent:
		return kind
	case "", domain.MemoryKindRawTurn:
		return domain.MemoryKindRawTurn
	default:
		return domain.MemoryKindRawTurn
	}
}

func appendDerivedSource(base, suffix string) string {
	base = strings.TrimSpace(base)
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		return base
	}
	if base == "" {
		return suffix
	}
	return base + ":" + suffix
}

func mergeTags(base []string, extra ...string) []string {
	out := append([]string{}, base...)
	seen := make(map[string]struct{}, len(out))
	for _, tag := range out {
		tag = strings.TrimSpace(strings.ToLower(tag))
		if tag == "" {
			continue
		}
		seen[tag] = struct{}{}
	}
	for _, tag := range extra {
		tag = strings.TrimSpace(strings.ToLower(tag))
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	slices.Sort(out)
	return out
}
