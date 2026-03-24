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

// NewOpenRouterInfoParser constructs an OpenRouter-backed info parser.
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
	parsed, err := parseParserFacts(raw, maxFacts)
	if err != nil {
		p.debugf("[pali-parser] provider=openrouter model=%s PARSE_ERROR raw_response=%q err=%v", p.client.Model(), sanitizeLogSnippet(raw, 260), err)
		return nil, err
	}
	p.debugf("[pali-parser] provider=openrouter model=%s status=ok ms=%d facts=%d", p.client.Model(), time.Since(start).Milliseconds(), len(parsed))
	return parsed, nil
}

func (p *openRouterInfoParser) debugf(format string, args ...any) {
	if p == nil || p.logger == nil || !p.verbose {
		return
	}
	p.logger.Printf(format, args...)
}
