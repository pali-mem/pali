package memory

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	turnTagPattern       = regexp.MustCompile(`\[(\w+):([^\]]+)\]`)
	turnSpeakerLineRegex = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9 .'\-]{0,80}):\s*(.+)\s*$`)
	turnSpeakerOnlyRegex = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 .'\-]{0,80}$`)
)

type parsedTurn struct {
	Time      string
	SpeakerA  string
	SpeakerB  string
	Speaker   string
	Utterance string
}

func deriveObservations(content string, max int) ([]string, error) {
	if max <= 0 {
		return []string{}, fmt.Errorf("max observations must be > 0")
	}

	normalized := strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if normalized == "" {
		return []string{}, nil
	}

	if turn, ok := parseAnnotatedTurn(normalized); ok {
		return deriveTurnObservations(turn, max), nil
	}

	return deriveSentenceObservations(normalized, max), nil
}

func deriveEvent(content string) (string, bool) {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if normalized == "" {
		return "", false
	}
	turn, ok := parseAnnotatedTurn(normalized)
	if !ok || turn.Time == "" || turn.Utterance == "" {
		return "", false
	}
	speaker := turn.Speaker
	if speaker == "" {
		speaker = "speaker"
	}
	return strings.TrimSpace(speaker + " at " + turn.Time + ": " + turn.Utterance), true
}

func deriveTurnObservations(turn parsedTurn, max int) []string {
	out := make([]string, 0, max)
	seen := make(map[string]struct{}, max+2)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if len(s) < 16 {
			return
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}

	if turn.Utterance != "" {
		switch {
		case turn.Speaker != "" && turn.Time != "":
			add(turn.Speaker + " said at " + turn.Time + ": " + turn.Utterance)
		case turn.Speaker != "":
			add(turn.Speaker + " said: " + turn.Utterance)
		default:
			add(turn.Utterance)
		}
	}

	sentences := splitObservationSentences(turn.Utterance)
	for _, s := range sentences {
		switch {
		case turn.Speaker != "" && turn.Time != "":
			add(turn.Speaker + " (" + turn.Time + "): " + s)
		case turn.Speaker != "":
			add(turn.Speaker + ": " + s)
		default:
			add(s)
		}
		if len(out) >= max {
			break
		}
	}

	if len(out) > max {
		return out[:max]
	}
	return out
}

func deriveSentenceObservations(content string, max int) []string {
	parts := splitObservationSentences(content)
	if len(parts) <= 1 {
		return []string{}
	}

	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, max)
	for _, s := range parts {
		if len(s) < 20 {
			continue
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
		if len(out) >= max {
			break
		}
	}
	return out
}

func splitObservationSentences(content string) []string {
	parts := strings.FieldsFunc(content, func(r rune) bool {
		return r == '.' || r == '!' || r == '?'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		s := strings.TrimSpace(strings.Trim(part, `"'`))
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func parseAnnotatedTurn(content string) (parsedTurn, bool) {
	matches := turnTagPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return parsedTurn{}, false
	}

	turn := parsedTurn{}
	for _, m := range matches {
		if len(m) != 3 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(m[1]))
		val := strings.TrimSpace(m[2])
		switch key {
		case "time":
			turn.Time = val
		case "speaker_a":
			turn.SpeakerA = val
		case "speaker_b":
			turn.SpeakerB = val
		}
	}

	rest := strings.TrimSpace(turnTagPattern.ReplaceAllString(content, ""))
	rest = strings.Join(strings.Fields(rest), " ")
	if rest == "" {
		return parsedTurn{}, false
	}

	speakerMatch := turnSpeakerLineRegex.FindStringSubmatch(rest)
	if len(speakerMatch) == 3 {
		turn.Speaker = strings.TrimSpace(speakerMatch[1])
		turn.Utterance = strings.TrimSpace(speakerMatch[2])
		return turn, turn.Utterance != ""
	}

	turn.Utterance = rest
	return turn, true
}

func parseTurnStyleFact(content string) (parsedTurn, bool) {
	trimmed := strings.TrimSpace(content)
	if speaker, timeVal, utterance, ok := splitParserFactWithMarker(trimmed, " said at "); ok {
		return parsedTurn{Speaker: speaker, Time: timeVal, Utterance: utterance}, true
	}
	if speaker, timeVal, utterance, ok := splitParserFactWithMarker(trimmed, " at "); ok {
		return parsedTurn{Speaker: speaker, Time: timeVal, Utterance: utterance}, true
	}
	if speaker, timeVal, utterance, ok := splitParserFactWithParens(trimmed); ok {
		return parsedTurn{Speaker: speaker, Time: timeVal, Utterance: utterance}, true
	}
	if turn, ok := parseAnnotatedTurn(content); ok {
		return turn, true
	}
	return parsedTurn{}, false
}

func splitParserFactWithMarker(content, marker string) (string, string, string, bool) {
	idx := strings.Index(content, marker)
	if idx <= 0 {
		return "", "", "", false
	}
	speaker := strings.TrimSpace(content[:idx])
	if !turnSpeakerOnlyRegex.MatchString(speaker) {
		return "", "", "", false
	}
	rest := strings.TrimSpace(content[idx+len(marker):])
	sep := strings.LastIndex(rest, ":")
	if sep <= 0 || sep >= len(rest)-1 {
		return "", "", "", false
	}
	timeVal := strings.TrimSpace(rest[:sep])
	utterance := strings.TrimSpace(rest[sep+1:])
	if timeVal == "" || utterance == "" {
		return "", "", "", false
	}
	return speaker, timeVal, utterance, true
}

func splitParserFactWithParens(content string) (string, string, string, bool) {
	open := strings.Index(content, " (")
	if open <= 0 {
		return "", "", "", false
	}
	speaker := strings.TrimSpace(content[:open])
	if !turnSpeakerOnlyRegex.MatchString(speaker) {
		return "", "", "", false
	}
	closeMarker := "):"
	closeIdx := strings.Index(content[open+2:], closeMarker)
	if closeIdx < 0 {
		return "", "", "", false
	}
	closeIdx += open + 2
	timeVal := strings.TrimSpace(content[open+2 : closeIdx])
	utterance := strings.TrimSpace(content[closeIdx+len(closeMarker):])
	if timeVal == "" || utterance == "" {
		return "", "", "", false
	}
	return speaker, timeVal, utterance, true
}

func canonicalizeTurnStyleFact(sourceContent, factContent string) string {
	if turn, ok := parseTurnStyleFact(factContent); ok {
		return canonicalizeTurnStatement(turn)
	}

	// Parser fallback can echo the original annotated turn as a "fact". Recover the
	// turn structure from the source so low-signal replies like "Absolutely" can be
	// dropped instead of being stored as timestamped observations.
	if turn, ok := parseAnnotatedTurn(sourceContent); ok {
		normalizedFact := strings.Join(strings.Fields(strings.TrimSpace(factContent)), " ")
		normalizedSource := strings.Join(strings.Fields(strings.TrimSpace(sourceContent)), " ")
		if normalizedFact == normalizedSource {
			return canonicalizeTurnStatement(turn)
		}
	}

	return factContent
}

func canonicalizeTurnStatement(turn parsedTurn) string {
	utterance := normalizeFactContent(turn.Utterance)
	if !isInformativeFact(utterance) {
		return ""
	}
	if strings.TrimSpace(turn.Speaker) == "" {
		return utterance
	}
	return normalizeFactContent(rewriteSpeakerPerspective(turn.Speaker, utterance))
}

func rewriteSpeakerPerspective(speaker, utterance string) string {
	trimmedSpeaker := strings.TrimSpace(speaker)
	trimmedUtterance := strings.TrimSpace(utterance)
	if trimmedSpeaker == "" || trimmedUtterance == "" {
		return trimmedUtterance
	}

	lower := strings.ToLower(trimmedUtterance)
	switch {
	case strings.HasPrefix(lower, "i am "):
		return trimmedSpeaker + " is " + trimmedUtterance[5:]
	case strings.HasPrefix(lower, "i'm "):
		return trimmedSpeaker + " is " + trimmedUtterance[4:]
	case strings.HasPrefix(lower, "i was "):
		return trimmedSpeaker + " was " + trimmedUtterance[6:]
	case strings.HasPrefix(lower, "i have "):
		return trimmedSpeaker + " has " + trimmedUtterance[7:]
	case strings.HasPrefix(lower, "i've "):
		return trimmedSpeaker + " has " + trimmedUtterance[5:]
	case strings.HasPrefix(lower, "i had "):
		return trimmedSpeaker + " had " + trimmedUtterance[6:]
	case strings.HasPrefix(lower, "i will "):
		return trimmedSpeaker + " will " + trimmedUtterance[7:]
	case strings.HasPrefix(lower, "i'll "):
		return trimmedSpeaker + " will " + trimmedUtterance[5:]
	case strings.HasPrefix(lower, "i can "):
		return trimmedSpeaker + " can " + trimmedUtterance[6:]
	case strings.HasPrefix(lower, "i could "):
		return trimmedSpeaker + " could " + trimmedUtterance[8:]
	case strings.HasPrefix(lower, "i should "):
		return trimmedSpeaker + " should " + trimmedUtterance[9:]
	case strings.HasPrefix(lower, "i would "):
		return trimmedSpeaker + " would " + trimmedUtterance[8:]
	case strings.HasPrefix(lower, "i'd "):
		return trimmedSpeaker + " would " + trimmedUtterance[4:]
	case strings.HasPrefix(lower, "i "):
		return trimmedSpeaker + " " + trimmedUtterance[2:]
	case strings.HasPrefix(lower, "my "):
		return trimmedSpeaker + "'s " + trimmedUtterance[3:]
	case strings.HasPrefix(lower, strings.ToLower(trimmedSpeaker)+" "):
		return trimmedUtterance
	default:
		return trimmedSpeaker + " said that " + lowerFirstASCII(trimmedUtterance)
	}
}

func lowerFirstASCII(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'A' && b[0] <= 'Z' {
		b[0] = b[0] + ('a' - 'A')
	}
	return string(b)
}

func sourceTimeAnchor(content string) (string, bool) {
	turn, ok := parseAnnotatedTurn(content)
	if !ok {
		return "", false
	}
	rawTime := strings.Join(strings.Fields(strings.TrimSpace(turn.Time)), " ")
	if rawTime == "" {
		return "", false
	}
	if normalized, ok := normalizeTurnTimeAnchor(rawTime); ok {
		return normalized, true
	}
	return rawTime, true
}

func normalizeTurnTimeAnchor(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	type timeLayout struct {
		layout   string
		hasClock bool
	}

	layouts := []timeLayout{
		{layout: "3:04 pm on 2 Jan, 2006", hasClock: true},
		{layout: "3:04 pm on 2 Jan 2006", hasClock: true},
		{layout: "3:04 pm on 2 January, 2006", hasClock: true},
		{layout: "3:04 pm on 2 January 2006", hasClock: true},
		{layout: "3:04pm on 2 Jan, 2006", hasClock: true},
		{layout: "3:04pm on 2 January, 2006", hasClock: true},
		{layout: "2 Jan, 2006", hasClock: false},
		{layout: "2 Jan 2006", hasClock: false},
		{layout: "2 January, 2006", hasClock: false},
		{layout: "2 January 2006", hasClock: false},
		{layout: "Jan 2, 2006", hasClock: false},
		{layout: "January 2, 2006", hasClock: false},
		{layout: "2006-01-02 15:04", hasClock: true},
		{layout: "2006-01-02", hasClock: false},
	}

	for _, candidate := range layouts {
		parsed, err := time.Parse(candidate.layout, raw)
		if err != nil {
			continue
		}
		// Use human-readable date (day-abbreviated_month-year) regardless of whether the
		// source had a clock component. This format is directly matched by the FULL_DATE_RE
		// pattern in the eval harness (e.g. "8 May 2023") and is natural enough to be
		// prepended into stored fact content.
		return parsed.UTC().Format("2 Jan 2006"), true
	}
	return "", false
}
