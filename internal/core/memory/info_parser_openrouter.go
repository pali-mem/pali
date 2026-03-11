package memory

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	coreprompts "github.com/pali-mem/pali/internal/core/prompts"
	openrouterclient "github.com/pali-mem/pali/internal/scorer/openrouter"
)

type openRouterGenerator interface {
	Generate(ctx context.Context, prompt string) (string, error)
	Model() string
}

type openRouterInfoParser struct {
	client  openRouterGenerator
	logger  *log.Logger
	verbose bool
}

func NewOpenRouterInfoParser(client *openrouterclient.Client, logger *log.Logger, verbose bool) InfoParser {
	return &openRouterInfoParser{
		client:  client,
		logger:  logger,
		verbose: verbose,
	}
}

func (p *openRouterInfoParser) Parse(ctx context.Context, content string, maxFacts int) ([]ParsedFact, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("openrouter parser client is nil")
	}
	if maxFacts <= 0 {
		return nil, fmt.Errorf("max facts must be > 0")
	}
	content = strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if content == "" {
		return []ParsedFact{}, nil
	}

	prompt := coreprompts.Parser(content, maxFacts)
	start := time.Now()
	raw, err := p.client.Generate(ctx, prompt)
	if err != nil {
		p.debugf("[pali-parser] provider=openrouter model=%s status=error ms=%d err=%v", p.client.Model(), time.Since(start).Milliseconds(), err)
		return nil, err
	}
	parsed, err := decodeParserJSON(raw)
	if err != nil {
		p.debugf("[pali-parser] provider=openrouter model=%s PARSE_ERROR raw_response=%q err=%v", p.client.Model(), sanitizeLogSnippet(raw, 260), err)
		return nil, err
	}

	out := make([]ParsedFact, 0, maxFacts)
	seen := make(map[string]struct{}, maxFacts*2)
	for _, f := range parsed.Facts {
		text := strings.Join(strings.Fields(strings.TrimSpace(f.Content)), " ")
		if !isInformativeFact(text) {
			continue
		}
		kind := normalizeFactKind(f.Kind)
		entity := strings.Join(strings.Fields(strings.TrimSpace(f.Entity)), " ")
		relation := strings.Join(strings.Fields(strings.TrimSpace(f.Relation)), " ")
		value := strings.Join(strings.Fields(strings.TrimSpace(f.Value)), " ")
		if entity == "" || relation == "" || value == "" {
			inferredEntity, inferredRelation, inferredValue := inferEntityRelationValue(text, kind)
			if entity == "" {
				entity = inferredEntity
			}
			if relation == "" {
				relation = inferredRelation
			}
			if value == "" {
				value = inferredValue
			}
		}
		if entity == "" && (relation != "" || value != "") {
			entity = inferEntityFromFact(text)
			if entity == "" {
				entity = "user"
			}
		}
		key := strings.ToLower(text)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ParsedFact{
			Content:  text,
			Kind:     kind,
			Tags:     normalizeFactTags(f.Tags, kind),
			Entity:   entity,
			Relation: relation,
			Value:    value,
		})
		if len(out) >= maxFacts {
			break
		}
	}
	p.debugf("[pali-parser] provider=openrouter model=%s status=ok ms=%d facts=%d", p.client.Model(), time.Since(start).Milliseconds(), len(out))
	return out, nil
}

func (p *openRouterInfoParser) debugf(format string, args ...any) {
	if p == nil || p.logger == nil || !p.verbose {
		return
	}
	p.logger.Printf(format, args...)
}
