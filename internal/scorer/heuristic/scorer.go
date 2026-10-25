// Package heuristic provides a lightweight, non-LLM importance scorer.
package heuristic

import (
	"context"
	"math"
	"regexp"
	"strings"
)

// Scorer scores memories using simple lexical heuristics.
type Scorer struct{}

// NewScorer returns a heuristic scorer.
func NewScorer() *Scorer { return &Scorer{} }

var (
	scorerTokens = regexp.MustCompile(`[a-zA-Z0-9_]+`)
	scorerDigits = regexp.MustCompile(`\d`)

	// keySignals mark content that is explicitly about durable preferences
	// or obligations.
	keySignals = []string{"prefer", "always", "important", "never", "remember", "must"}

	// scorerStopwords are tokens that carry no memory-worthy information on
	// their own. Kept deliberately small; the goal is to stop filler from
	// counting as content, not to do full stopword removal.
	scorerStopwords = map[string]struct{}{
		"a": {}, "an": {}, "the": {}, "and": {}, "or": {}, "but": {}, "so": {},
		"of": {}, "to": {}, "in": {}, "on": {}, "at": {}, "for": {}, "with": {},
		"as": {}, "by": {}, "from": {}, "is": {}, "are": {}, "was": {}, "were": {},
		"be": {}, "been": {}, "it": {}, "its": {}, "this": {}, "that": {}, "these": {},
		"those": {}, "i": {}, "me": {}, "my": {}, "we": {}, "you": {}, "your": {},
		"he": {}, "she": {}, "they": {}, "them": {}, "his": {}, "her": {}, "their": {},
		"ok": {}, "okay": {}, "yes": {}, "no": {}, "yeah": {}, "wow": {}, "hi": {},
		"hello": {}, "hey": {}, "thanks": {}, "thank": {}, "cool": {}, "great": {},
		"nice": {}, "sure": {}, "good": {}, "sounds": {}, "lol": {}, "haha": {},
	}
)

// informativeTokenSaturation is the number of distinct informative tokens at
// which the base score reaches 1.0.
const informativeTokenSaturation = 12

// Score returns an importance estimate for a single memory in [0,1].
//
// The base signal is the number of distinct informative (non-stopword) tokens
// on a log scale, so short chatter scores near zero and dense factual content
// scores high. Explicit preference/obligation words and numeric anchors add a
// small boost.
func (s *Scorer) Score(ctx context.Context, text string) (float64, error) {
	_ = ctx
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return 0, nil
	}

	tokens := scorerTokens.FindAllString(text, -1)
	if len(tokens) == 0 {
		return 0, nil
	}

	informative := map[string]struct{}{}
	for _, token := range tokens {
		if _, stop := scorerStopwords[token]; stop {
			continue
		}
		if len(token) < 3 && !scorerDigits.MatchString(token) {
			continue
		}
		informative[token] = struct{}{}
	}

	base := math.Log1p(float64(len(informative))) / math.Log1p(informativeTokenSaturation)

	boost := 0.0
	for _, signal := range keySignals {
		if strings.Contains(text, signal) {
			boost += 0.08
		}
	}
	if scorerDigits.MatchString(text) {
		boost += 0.05
	}

	score := base + boost
	if score > 1 {
		score = 1
	}
	if score < 0 {
		score = 0
	}
	return score, nil
}

// BatchScore scores each input memory independently.
func (s *Scorer) BatchScore(ctx context.Context, texts []string) ([]float64, error) {
	out := make([]float64, 0, len(texts))
	for _, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		score, err := s.Score(ctx, text)
		if err != nil {
			return nil, err
		}
		out = append(out, score)
	}
	return out, nil
}
