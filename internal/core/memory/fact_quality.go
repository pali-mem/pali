package memory

import (
	"regexp"
	"strings"

	"github.com/vein05/pali/internal/domain"
)

var (
	vagueFactValuePattern  = regexp.MustCompile(`(?i)\b(?:it|this|that|these|those|something|anything|everything|nothing|stuff|things?|one|ones|there|here|them)\b`)
	vagueFactTailPattern   = regexp.MustCompile(`(?i)\b(?:do|did|does|doing|done)\s+(?:it|that|this|something|anything)\b`)
	offerFactPattern       = regexp.MustCompile(`(?i)\b(?:let me know|need any help|happy to help|hope that helps|glad you agree|sounds good)\b`)
	genericSpeechPattern   = regexp.MustCompile(`(?i)\bsaid that\b`)
	reactionFactPattern    = regexp.MustCompile(`(?i)\b(?:that sounds like fun|sounds great|looks great|looks amazing|looks awesome|take a look|look at this|look at that|check this out|you both look|you look|love the red and blue)\b`)
	chatterLeadPattern     = regexp.MustCompile(`(?i)^(?:yeah|yep|yup|wow|oh|ah|hey|hi|hello|thanks|thank you|cool|nice|great|awesome|amazing|absolutely|definitely|totally)\b`)
	bareEmotionFactPattern = regexp.MustCompile(`(?i)\b(?:is|was|feels?|felt|seems?|seemed|became)\b`)
	relativeTimePattern    = regexp.MustCompile(`(?i)\b(?:yesterday|today|tomorrow|tonight|last|next|earlier|later|before|after|soon|eventually|someday)\b`)
	futurePlanPattern      = regexp.MustCompile(`(?i)\b(?:will|plan(?:s|ning)? to|going to)\b`)
	subjectPronounPattern  = regexp.MustCompile(`(?i)^(?:i|you|we|they|he|she|it)\b`)
	predicateSignalPattern = regexp.MustCompile(`(?i)\b(?:is|was|has|had|lives? in|moved to|relocat(?:ed|es)|works? as|stud(?:y|ies|ied)|read(?:s|ing)?|likes?|loves?|enjoys?|prefers?|avoid(?:s)?|plans? to|going to|will|attended|participated in|joined|went to|visited|met|married|dating|supports?)\b`)
	identityValuePattern   = regexp.MustCompile(`(?i)\b(?:gay|lesbian|bisexual|queer|asexual|straight|heterosexual|non-binary|transgender(?:\s+(?:man|woman))?|genderqueer|genderfluid|agender|intersex)\b`)
	roleValuePattern       = regexp.MustCompile(`(?i)\b(?:teacher|student|engineer|developer|designer|doctor|nurse|lawyer|manager|writer|artist|researcher|photographer|chef|therapist|architect|consultant|analyst|accountant|counselor)\b`)
	emotionWordPattern     = regexp.MustCompile(`(?i)\b(?:happy|thankful|grateful|excited|proud|glad|thrilled|relieved|nervous|sad|upset|angry|emotional|empowered|liberated|accepted|inspired|motivated|fulfilled|comforted)\b`)
	emotionAnchorPattern   = regexp.MustCompile(`(?i)\b(?:about|for|because|after|during|when|while|to)\b`)
)

var weakSingleTokenValues = map[string]struct{}{
	"good": {}, "great": {}, "nice": {}, "fine": {}, "okay": {}, "ok": {}, "sure": {}, "absolutely": {}, "definitely": {},
	"yes": {}, "no": {}, "maybe": {}, "later": {}, "soon": {}, "there": {}, "here": {}, "something": {}, "anything": {},
}

func passesCanonicalFactAdmission(sourceContent string, fact ParsedFact) bool {
	content := normalizeFactContent(fact.Content)
	if content == "" || !isInformativeFact(content) {
		return false
	}
	if offerFactPattern.MatchString(content) {
		return false
	}
	if isSpeechOrReactionFact(content) {
		return false
	}
	if isBareEmotionFact(content) {
		return false
	}
	entity := strings.TrimSpace(fact.Entity)
	if entity == "" {
		entity = inferEntityFromFact(content)
	}
	entity = strings.Join(strings.Fields(entity), " ")
	if !hasResolvedFactSubject(sourceContent, content, entity) {
		return false
	}
	if !hasSpecificFactPredicate(content, fact.Relation) {
		return false
	}
	if !hasSpecificFactValue(content, fact.Value) {
		return false
	}
	if isTemporalFact(content, fact) && !hasAbsoluteOrAnchoredTime(sourceContent, content) {
		return false
	}
	return true
}

func hasSpecificFactPredicate(content, relation string) bool {
	relation = normalizeEntityFactRelation(relation)
	if relation != "" && relation != "unknown" {
		return true
	}
	content = normalizeFactContent(content)
	if content == "" {
		return false
	}
	if genericSpeechPattern.MatchString(content) {
		return false
	}
	return predicateSignalPattern.MatchString(content)
}

func hasSpecificFactValue(content, value string) bool {
	candidate := normalizeEntityFactValue(value)
	if candidate == "" {
		candidate = inferSpecificValueCandidate(content)
	}
	if candidate == "" {
		return false
	}
	lower := strings.ToLower(candidate)
	if vagueFactValuePattern.MatchString(lower) || vagueFactTailPattern.MatchString(lower) {
		return false
	}
	if len(strings.Fields(lower)) >= 2 {
		return true
	}
	if _, weak := weakSingleTokenValues[lower]; weak {
		return false
	}
	if len(lower) >= 3 && !strings.Contains(lower, " ") {
		return true
	}
	if isHighSignalShortFact(lower) {
		return true
	}
	if identityValuePattern.MatchString(lower) {
		return true
	}
	if roleValuePattern.MatchString(lower) {
		return true
	}
	return false
}

func isBareEmotionFact(content string) bool {
	content = normalizeFactContent(content)
	if content == "" {
		return false
	}
	lower := strings.ToLower(content)
	if !bareEmotionFactPattern.MatchString(lower) || !emotionWordPattern.MatchString(lower) {
		return false
	}
	candidate := strings.ToLower(inferSpecificValueCandidate(content))
	if candidate == "" {
		candidate = lower
	}
	if strings.Contains(candidate, "for you") || strings.Contains(candidate, "for u") {
		return true
	}
	if emotionAnchorPattern.MatchString(candidate) {
		switch {
		case strings.Contains(candidate, "to ") && countNonEmotionTokens(candidate) >= 2:
			return false
		case strings.Contains(candidate, "about ") && countNonEmotionTokens(candidate) >= 2:
			return false
		case strings.Contains(candidate, "after ") && countNonEmotionTokens(candidate) >= 2:
			return false
		case strings.Contains(candidate, "because ") && countNonEmotionTokens(candidate) >= 2:
			return false
		case strings.Contains(candidate, "during ") && countNonEmotionTokens(candidate) >= 2:
			return false
		}
	}
	return countNonEmotionTokens(candidate) <= 1
}

func isSpeechOrReactionFact(content string) bool {
	content = normalizeFactContent(content)
	if content == "" {
		return false
	}
	lower := strings.ToLower(content)
	if reactionFactPattern.MatchString(lower) {
		return true
	}
	if genericSpeechPattern.MatchString(lower) {
		candidate := strings.ToLower(inferSpecificValueCandidate(content))
		if candidate == "" {
			return true
		}
		if reactionFactPattern.MatchString(candidate) {
			return true
		}
		if chatterLeadPattern.MatchString(candidate) {
			return true
		}
		if countNonReactionTokens(candidate) <= 1 {
			return true
		}
	}
	return false
}

func countNonReactionTokens(text string) int {
	tokens := strings.Fields(strings.ToLower(strings.TrimSpace(text)))
	count := 0
	for _, token := range tokens {
		token = strings.Trim(token, " \t\r\n.,;:!?\"'")
		if token == "" {
			continue
		}
		switch token {
		case "yeah", "yep", "yup", "wow", "oh", "ah", "hey", "hi", "hello",
			"thanks", "thank", "you", "cool", "nice", "great", "awesome", "amazing",
			"absolutely", "definitely", "totally", "really", "very", "so", "just",
			"that", "this", "it", "look", "take", "a", "the", "at", "on", "to", "and":
			continue
		}
		count++
	}
	return count
}

func countNonEmotionTokens(text string) int {
	tokens := strings.Fields(strings.ToLower(strings.TrimSpace(text)))
	count := 0
	for _, token := range tokens {
		token = strings.Trim(token, " \t\r\n.,;:!?\"'")
		if token == "" {
			continue
		}
		if emotionWordPattern.MatchString(token) {
			continue
		}
		switch token {
		case "is", "was", "feel", "feels", "felt", "seems", "seemed", "became", "become", "am", "are",
			"so", "really", "very", "pretty", "quite", "super", "totally", "extremely", "deeply",
			"and", "but", "just", "kind", "of", "a", "an", "the", "my", "our", "their", "her", "his",
			"for", "to", "about", "because", "after", "during", "when", "while":
			continue
		}
		count++
	}
	return count
}

func hasResolvedFactSubject(sourceContent, content, entity string) bool {
	entity = strings.Join(strings.Fields(strings.TrimSpace(entity)), " ")
	if entity == "" {
		entity = inferEntityFromFact(content)
	}
	entity = strings.Join(strings.Fields(strings.TrimSpace(entity)), " ")
	if entity == "" {
		return false
	}
	if !subjectPronounPattern.MatchString(strings.ToLower(entity)) {
		return true
	}
	_, annotated := parseAnnotatedTurn(sourceContent)
	return !annotated
}

func inferSpecificValueCandidate(content string) string {
	content = normalizeFactContent(content)
	if content == "" {
		return ""
	}
	content = factLeadingDatePattern.ReplaceAllString(content, "")
	entity := inferEntityFromFact(content)
	if entity == "" {
		return ""
	}
	lower := strings.ToLower(content)
	lowerEntity := strings.ToLower(entity)
	if strings.HasPrefix(lower, lowerEntity+" ") {
		content = strings.TrimSpace(content[len(entity):])
	}
	for _, prefix := range []string{
		"is ", "was ", "has ", "had ", "will ", "can ", "could ", "would ", "should ",
		"likes ", "like ", "loves ", "love ", "enjoys ", "enjoy ", "prefers ", "prefer ",
		"plans to ", "plan to ", "going to ", "went to ", "attended ", "joined ",
		"moved to ", "lives in ", "works as ", "read ", "reads ", "met ",
	} {
		if strings.HasPrefix(strings.ToLower(content), prefix) {
			return strings.TrimSpace(content[len(prefix):])
		}
	}
	return cleanupEntityFactValue(content)
}

func isTemporalFact(content string, fact ParsedFact) bool {
	content = normalizeFactContent(content)
	if fact.Kind == domain.MemoryKindEvent {
		return relativeTimePattern.MatchString(strings.ToLower(content)) || futurePlanPattern.MatchString(strings.ToLower(content)) || timeTagPattern.MatchString(strings.ToLower(content))
	}
	lower := strings.ToLower(content)
	return relativeTimePattern.MatchString(lower) || futurePlanPattern.MatchString(lower)
}

func hasAbsoluteOrAnchoredTime(sourceContent, factContent string) bool {
	if timeTagPattern.MatchString(strings.ToLower(factContent)) {
		return true
	}
	_, ok := sourceTimeAnchor(sourceContent)
	return ok
}

func buildFactQuestionView(fact ParsedFact) string {
	entity := strings.TrimSpace(fact.Entity)
	if entity == "" {
		entity = inferEntityFromFact(fact.Content)
	}
	entity = strings.Join(strings.Fields(entity), " ")
	if entity == "" {
		return ""
	}
	relation := normalizeEntityFactRelation(fact.Relation)
	value := normalizeEntityFactValue(fact.Value)
	if value == "" {
		value = inferSpecificValueCandidate(fact.Content)
	}
	parts := make([]string, 0, 6)
	add := func(text string) {
		text = normalizeFactContent(text)
		if text == "" {
			return
		}
		for _, existing := range parts {
			if strings.EqualFold(existing, text) {
				return
			}
		}
		parts = append(parts, text)
	}

	switch relation {
	case "activity", "preference":
		add("what does " + entity + " do")
		add("what activities does " + entity + " do")
		if value != "" {
			add("what does " + entity + " enjoy " + value)
		}
	case "event":
		if value != "" {
			add("when did " + entity + " " + value)
			add("what event did " + entity + " attend " + value)
		}
		add("what did " + entity + " do")
	case "place":
		add("where does " + entity + " live")
		if value != "" {
			add("where did " + entity + " go " + value)
		}
	case "book":
		add("what did " + entity + " read")
	case "identity":
		add("what is " + entity + " identity")
		add("how does " + entity + " identify")
	case "role":
		add("what does " + entity + " do for work")
		add("what is " + entity + " job")
	case "plan":
		add("what is " + entity + " planning")
		if value != "" {
			add("when will " + entity + " " + value)
		}
	default:
		add("what about " + entity)
	}

	if value != "" {
		add(entity + " " + value)
	}
	return strings.Join(parts, "\n")
}
