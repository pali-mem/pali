package memory

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/pali-mem/pali/internal/domain"
	"github.com/schollz/progressbar/v2"
)

// parserBatchConcurrency is the max number of parallel parser calls issued
// when processing a batch. Network-bound parsers (OpenRouter, Ollama) benefit
// from high concurrency; the heuristic parser is CPU-bound but fast enough
// that 8 concurrent goroutines won't cause contention.
const parserBatchConcurrency = 8

type parserBatchItem struct {
	item             preparedStoreInput
	facts            []ParsedFact
	extractor        string
	extractorVersion string
	parseErr         error
}

type parserPendingWrite struct {
	memory           domain.Memory
	embedding        []float32
	relationTupleKey string
	dropped          bool
	replacedBy       int
}

type parserBatchResultRef struct {
	pendingIdx  int
	existing    domain.Memory
	hasExisting bool
}

type parserPendingEntityFact struct {
	pendingIdx  int
	existing    domain.Memory
	hasExisting bool
	fact        ParsedFact
}

func (s *Service) storeBatchWithParser(ctx context.Context, items []preparedStoreInput) ([]domain.Memory, error) {
	if len(items) == 0 {
		return []domain.Memory{}, nil
	}
	if len(items) == 1 {
		s.logDebugf("[pali-store] batch=1 fallback=sequential reason=single_item")
		return s.storeBatchWithParserSequential(ctx, items)
	}

	batchEmbedder, ok := s.embedder.(domain.BatchEmbedder)
	if !ok || batchEmbedder == nil {
		s.logInfof("[pali-store] batch=%d FALLBACK code=500 reason=batch_embedder_unavailable", len(items))
		return s.storeBatchWithParserSequential(ctx, items)
	}

	// Phase 1: parse all items in parallel, up to parserBatchConcurrency at once.
	// Each goroutine writes to its own pre-allocated slot so no mutex is needed
	// on the slice itself.
	parsed := make([]parserBatchItem, len(items))
	var parseProgress *progressbar.ProgressBar
	if s.devVerbose && s.progress && len(items) > 1 && isatty.IsTerminal(os.Stdout.Fd()) {
		parseProgress = progressbar.NewOptions(
			len(items),
			progressbar.OptionSetWriter(os.Stdout),
			progressbar.OptionSetDescription("[pali-store] parse"),
			progressbar.OptionSetWidth(18),
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
			progressbar.OptionClearOnFinish(),
		)
	}
	{
		sem := make(chan struct{}, parserBatchConcurrency)
		var wg sync.WaitGroup
		for i, item := range items {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int, item preparedStoreInput) {
				defer wg.Done()
				defer func() { <-sem }()
				parseStart := time.Now()
				parseResult, parseErr := s.parseFactsWithFallback(ctx, item.input.Content, i+1)
				parserMS := time.Since(parseStart).Milliseconds()
				facts := parseResult.Facts
				entityTriples := 0
				for _, fact := range facts {
					if parsedFactHasEntityTriple(fact) {
						entityTriples++
					}
				}
				s.logInfof(
					"[pali-store] turn=%d raw_turns=1 facts=%d entity_triples=%d",
					i+1,
					len(facts),
					entityTriples,
				)
				s.logDebugf("[pali-store] turn=%d parser_ms=%d facts=%d", i+1, parserMS, len(facts))
				for _, fact := range facts {
					s.logDebugf("[pali-parser] turn=%d fact=%q", i+1, sanitizeLogSnippet(fact.Content, 220))
				}
				if parseProgress != nil {
					_ = parseProgress.Add(1)
				}
				parsed[i] = parserBatchItem{
					item:             item,
					facts:            facts,
					extractor:        parseResult.Extractor,
					extractorVersion: parseResult.ExtractorVersion,
					parseErr:         parseErr,
				}
			}(i, item)
		}
		wg.Wait()
	}

	// Phase 2: collect unique embedding texts in original order (sequential, no locking needed).
	uniqueTexts := make([]string, 0, len(items)*(s.parser.MaxFacts+1))
	seenTexts := make(map[string]struct{}, len(uniqueTexts))
	pushText := func(text string) {
		if text == "" {
			return
		}
		if _, ok := seenTexts[text]; ok {
			return
		}
		seenTexts[text] = struct{}{}
		uniqueTexts = append(uniqueTexts, text)
	}
	for _, p := range parsed {
		pushText(p.item.input.Content)
		if p.parseErr != nil {
			continue
		}
		for _, fact := range p.facts {
			pushText(embeddingTextForParsedFact(fact))
		}
	}

	embedStart := time.Now()
	embeddings, err := batchEmbedder.BatchEmbed(ctx, uniqueTexts)
	embedDur := time.Since(embedStart)
	if err != nil {
		s.logInfof("[pali-store] batch=%d FALLBACK code=500 reason=batch_embed_error err=%v", len(items), err)
		return s.storeBatchWithParserSequential(ctx, items)
	}
	if len(embeddings) != len(uniqueTexts) {
		s.logInfof(
			"[pali-store] batch=%d FALLBACK code=500 reason=batch_embed_mismatch embeddings=%d texts=%d",
			len(items),
			len(embeddings),
			len(uniqueTexts),
		)
		return s.storeBatchWithParserSequential(ctx, items)
	}

	embeddingByContent := make(map[string][]float32, len(uniqueTexts))
	for i := range uniqueTexts {
		if len(embeddings[i]) == 0 {
			return s.storeBatchWithParserSequential(ctx, items)
		}
		embeddingByContent[uniqueTexts[i]] = append([]float32{}, embeddings[i]...)
	}

	pending := make([]parserPendingWrite, 0, len(uniqueTexts))
	pendingEntityFacts := make([]parserPendingEntityFact, 0, len(uniqueTexts))
	resultRefs := make([]parserBatchResultRef, len(items))
	for i := range resultRefs {
		resultRefs[i].pendingIdx = -1
	}

	stageMemory := func(memory domain.Memory) (int, error) {
		embedding, ok := embeddingByContent[memory.Content]
		if !ok || len(embedding) == 0 {
			return -1, fmt.Errorf("missing precomputed embedding for parser batch content")
		}
		pending = append(pending, parserPendingWrite{
			memory:     memory,
			embedding:  append([]float32{}, embedding...),
			replacedBy: -1,
		})
		return len(pending) - 1, nil
	}

	stageRawMemory := func(item preparedStoreInput) (int, error) {
		return stageMemory(applyIdentityToMemory(domain.Memory{
			TenantID:  item.input.TenantID,
			Content:   item.input.Content,
			Tier:      item.resolvedTier,
			Kind:      item.resolvedKind,
			Tags:      item.input.Tags,
			Source:    item.input.Source,
			CreatedBy: item.input.CreatedBy,
		}, buildRawTurnIdentity(item.input.Content)))
	}

	for i := range parsed {
		current := parsed[i]
		if current.parseErr != nil {
			rawIdx, err := stageRawMemory(current.item)
			if err != nil {
				return nil, err
			}
			resultRefs[i].pendingIdx = rawIdx
			continue
		}

		if s.parser.StoreRawTurn {
			rawIdx, err := stageRawMemory(current.item)
			if err != nil {
				return nil, err
			}
			resultRefs[i].pendingIdx = rawIdx
		}

		firstFound := false
		firstPendingIdx := -1
		var firstExisting domain.Memory
		for factIdx, fact := range current.facts {
			memory, pendingIdx, err := s.applyParsedFactWithPending(
				ctx,
				current.item.input.TenantID,
				current.item.input.Content,
				current.item.input.Tags,
				current.item.input.Source,
				fact,
				factIdx,
				current.extractor,
				current.extractorVersion,
				embeddingByContent,
				&pending,
			)
			if err != nil {
				return nil, err
			}
			if memory == nil || firstFound {
				if memory != nil && parsedFactHasEntityTriple(fact) {
					ref := parserPendingEntityFact{
						pendingIdx: pendingIdx,
						fact:       fact,
					}
					if pendingIdx < 0 {
						ref.existing = *memory
						ref.hasExisting = true
					}
					pendingEntityFacts = append(pendingEntityFacts, ref)
				}
				continue
			}
			firstFound = true
			if pendingIdx >= 0 {
				firstPendingIdx = pendingIdx
			} else {
				firstExisting = *memory
			}
			if parsedFactHasEntityTriple(fact) {
				ref := parserPendingEntityFact{
					pendingIdx: pendingIdx,
					fact:       fact,
				}
				if pendingIdx < 0 {
					ref.existing = *memory
					ref.hasExisting = true
				}
				pendingEntityFacts = append(pendingEntityFacts, ref)
			}
		}

		if s.parser.StoreRawTurn {
			continue
		}
		if firstFound {
			if firstPendingIdx >= 0 {
				resultRefs[i].pendingIdx = firstPendingIdx
			} else {
				resultRefs[i].existing = firstExisting
				resultRefs[i].hasExisting = true
			}
			continue
		}

		rawIdx, err := stageRawMemory(current.item)
		if err != nil {
			return nil, err
		}
		resultRefs[i].pendingIdx = rawIdx
	}

	pendingToStored := make(map[int]domain.Memory, len(pending))
	memoriesToStore := make([]domain.Memory, 0, len(pending))
	embeddingsToStore := make([][]float32, 0, len(pending))
	pendingToStoreIdx := make(map[int]int, len(pending))
	writeStart := time.Now()
	pendingContents := make([]string, 0, len(pending))
	pendingIdxOrder := make([]int, 0, len(pending))
	for i := range pending {
		if pending[i].dropped {
			continue
		}
		pendingContents = append(pendingContents, pending[i].memory.Content)
		pendingIdxOrder = append(pendingIdxOrder, i)
	}
	importanceScores, err := s.scoreContents(ctx, pendingContents)
	if err != nil {
		return nil, err
	}
	if len(importanceScores) != len(pendingIdxOrder) {
		return nil, fmt.Errorf("parser batch importance score count mismatch: got %d for %d memories", len(importanceScores), len(pendingIdxOrder))
	}
	for orderIdx, pendingIdx := range pendingIdxOrder {
		pending[pendingIdx].memory.Importance = importanceScores[orderIdx]
		pendingToStoreIdx[pendingIdx] = len(memoriesToStore)
		memoriesToStore = append(memoriesToStore, pending[pendingIdx].memory)
		embeddingsToStore = append(embeddingsToStore, pending[pendingIdx].embedding)
	}

	if len(memoriesToStore) > 0 {
		stored, err := s.storeInRepo(ctx, memoriesToStore)
		if err != nil {
			s.logInfof("[pali-store] batch=%d FALLBACK code=500 reason=repo_store_error err=%v", len(items), err)
			return nil, err
		}
		if len(stored) != len(embeddingsToStore) {
			return nil, fmt.Errorf("parser batch store result mismatch: stored=%d embeddings=%d", len(stored), len(embeddingsToStore))
		}
		if err := s.upsertStoredEmbeddings(ctx, stored, embeddingsToStore); err != nil {
			return nil, err
		}
		for pendingIdx, storedIdx := range pendingToStoreIdx {
			pendingToStored[pendingIdx] = stored[storedIdx]
		}
	}
	s.logInfof(
		"[pali-store] batch=%d embed_ms=%d write_ms=%d",
		len(items),
		embedDur.Milliseconds(),
		time.Since(writeStart).Milliseconds(),
	)

	entityFacts := make([]domain.EntityFact, 0, len(pendingEntityFacts))
	for _, pendingFact := range pendingEntityFacts {
		var memory domain.Memory
		hasMemory := false
		if pendingFact.pendingIdx >= 0 {
			resolved := resolveParserPendingIndex(pending, pendingFact.pendingIdx)
			if resolved >= 0 {
				if stored, ok := pendingToStored[resolved]; ok {
					memory = stored
					hasMemory = true
				}
			}
		} else if pendingFact.hasExisting {
			memory = pendingFact.existing
			hasMemory = true
		}
		if !hasMemory {
			continue
		}
		if entityFact, ok := buildEntityFactRecord(memory, pendingFact.fact); ok {
			entityFacts = append(entityFacts, entityFact)
		}
	}
	if err := s.storeEntityFacts(ctx, entityFacts); err != nil {
		return nil, err
	}

	out := make([]domain.Memory, len(items))
	for i := range resultRefs {
		if resultRefs[i].pendingIdx >= 0 {
			resolved := resolveParserPendingIndex(pending, resultRefs[i].pendingIdx)
			if resolved >= 0 {
				if memory, ok := pendingToStored[resolved]; ok {
					out[i] = memory
					continue
				}
			}
		}
		if resultRefs[i].hasExisting {
			out[i] = resultRefs[i].existing
			continue
		}
		return nil, fmt.Errorf("parser batch result resolution failed at index %d", i)
	}

	return out, nil
}

func (s *Service) storeBatchWithParserSequential(ctx context.Context, items []preparedStoreInput) ([]domain.Memory, error) {
	out := make([]domain.Memory, 0, len(items))
	for _, item := range items {
		stored, err := s.storeWithParser(ctx, item.input, item.resolvedTier, item.resolvedKind)
		if err != nil {
			return nil, err
		}
		out = append(out, stored)
	}
	return out, nil
}

func (s *Service) applyParsedFactWithPending(
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
	pending *[]parserPendingWrite,
) (*domain.Memory, int, error) {
	content := normalizeFactContent(fact.Content)
	if !shouldStoreParsedFactContent(content) {
		return nil, -1, nil
	}
	kind := resolveKind(fact.Kind)
	if kind != domain.MemoryKindEvent && kind != domain.MemoryKindObservation {
		kind = domain.MemoryKindObservation
	}
	identity := buildParsedFactIdentity(sourceContent, factIndex, fact, extractor, extractorVersion)

	if exactMatch, pendingIdx := findPendingMemoryByCanonicalKey(*pending, tenantID, identity.CanonicalKey); exactMatch != nil {
		return exactMatch, pendingIdx, nil
	}
	if tupleKey, ok := normalizedRelationTupleKey(fact); ok {
		if exactMatch, pendingIdx := findPendingMemoryByRelationTuple(*pending, tenantID, tupleKey); exactMatch != nil {
			return exactMatch, pendingIdx, nil
		}
	}
	exactMatch, err := s.findMemoryByCanonicalKey(ctx, tenantID, identity.CanonicalKey)
	if err != nil {
		return nil, -1, err
	}
	if exactMatch != nil {
		return exactMatch, -1, nil
	}
	exactMatch, err = s.findMemoryByRelationTuple(ctx, tenantID, fact)
	if err != nil {
		return nil, -1, err
	}
	if exactMatch != nil {
		return exactMatch, -1, nil
	}

	embedding, ok := embeddingByContent[embeddingTextForParsedFact(fact)]
	if !ok || len(embedding) == 0 {
		return nil, -1, fmt.Errorf("missing precomputed embedding for parsed fact content")
	}

	tupleKey, _ := normalizedRelationTupleKey(fact)
	*pending = append(*pending, parserPendingWrite{
		memory: applyIdentityToMemory(domain.Memory{
			TenantID:      tenantID,
			Content:       content,
			QueryViewText: fact.QueryViewText,
			Tier:          domain.MemoryTierSemantic,
			Kind:          kind,
			Tags:          mergeTags(baseTags, append(append([]string{}, fact.Tags...), "memory_op:add", "memory_state:active")...),
			Source:        appendDerivedSource(baseSource, "parser"),
			CreatedBy:     domain.MemoryCreatedBySystem,
		}, identity),
		embedding:        append([]float32{}, embedding...),
		relationTupleKey: tupleKey,
		replacedBy:       -1,
	})
	newPendingIdx := len(*pending) - 1
	staged := (*pending)[newPendingIdx].memory
	return &staged, newPendingIdx, nil
}

func findPendingMemoryByCanonicalKey(
	pending []parserPendingWrite,
	tenantID, canonicalKey string,
) (*domain.Memory, int) {
	if strings.TrimSpace(canonicalKey) == "" {
		return nil, -1
	}
	for i := len(pending) - 1; i >= 0; i-- {
		if pending[i].dropped {
			continue
		}
		if pending[i].memory.TenantID != tenantID || pending[i].memory.CanonicalKey != canonicalKey {
			continue
		}
		memory := pending[i].memory
		return &memory, i
	}
	return nil, -1
}

func findPendingMemoryByRelationTuple(
	pending []parserPendingWrite,
	tenantID, tupleKey string,
) (*domain.Memory, int) {
	if strings.TrimSpace(tupleKey) == "" {
		return nil, -1
	}
	for i := len(pending) - 1; i >= 0; i-- {
		if pending[i].dropped {
			continue
		}
		if pending[i].memory.TenantID != tenantID || pending[i].relationTupleKey != tupleKey {
			continue
		}
		memory := pending[i].memory
		return &memory, i
	}
	return nil, -1
}

func resolveParserPendingIndex(pending []parserPendingWrite, idx int) int {
	for idx >= 0 && idx < len(pending) {
		if !pending[idx].dropped {
			return idx
		}
		if pending[idx].replacedBy < 0 {
			return -1
		}
		idx = pending[idx].replacedBy
	}
	return -1
}
