package prompts

import (
	"encoding/json"
	"fmt"
)

func Parser(content string, maxFacts int) string {
	return fmt.Sprintf(
		"Extract high-signal factual memories from one dialogue turn.\n"+
			"Rules:\n"+
			"1) Return JSON only, matching the schema exactly.\n"+
			"2) Exclude greetings, acknowledgements, compliments, chit-chat, standalone questions, and style-only text.\n"+
			"3) Keep each fact standalone, explicit, and non-ambiguous; one proposition per fact.\n"+
			"4) Resolve the subject whenever possible; avoid dangling pronouns like 'it', 'that', or 'do that'.\n"+
			"5) Prefer facts with a specific predicate and object/value.\n"+
			"6) If a fact is temporal, anchor it to an absolute or clearly provided time.\n"+
			"7) Preserve negation and constraints (e.g., 'does not', 'avoids').\n"+
			"8) Prefer concrete entities, preferences, commitments, plans, motivations, possessions, relationships, and dated events.\n"+
			"9) Do not invent or infer facts not present in the turn.\n"+
			"10) Output at most %d facts.\n"+
			"11) kind must be either observation or event.\n"+
			"12) If available, include entity/relation/value fields for aggregation lookups.\n"+
			"13) If no high-signal fact exists, return {\"facts\":[]}.\n"+
			"\n"+
			"JSON schema:\n"+
			"{\"facts\":[{\"content\":\"...\",\"kind\":\"observation|event\",\"tags\":[\"...\"],\"entity\":\"...\",\"relation\":\"...\",\"value\":\"...\"}]}\n"+
			"\n"+
			"Example:\n"+
			"Turn: \"Alice: I am vegetarian and avoid dairy\"\n"+
			"Output: {\"facts\":[{\"content\":\"Alice is vegetarian.\",\"kind\":\"observation\",\"tags\":[\"preference\"],\"entity\":\"Alice\",\"relation\":\"identity\",\"value\":\"vegetarian\"},{\"content\":\"Alice avoids dairy.\",\"kind\":\"observation\",\"tags\":[\"preference\"],\"entity\":\"Alice\",\"relation\":\"activity\",\"value\":\"avoids dairy\"}]}\n"+
			"\n"+
			"Turn:\n%s",
		maxFacts,
		content,
	)
}

func MultiHopDecomposition(query string, maxQueries int) string {
	return fmt.Sprintf(
		"Decompose this multi-hop memory query into atomic retrieval sub-queries.\n"+
			"Rules:\n"+
			"1) Return JSON only.\n"+
			"2) Use this exact schema: {\"sub_queries\":[\"...\"]}.\n"+
			"3) Produce 2 to %d short sub-queries.\n"+
			"4) Each sub-query must target one fact/entity relation.\n"+
			"5) Keep names/entities explicit; avoid pronouns when possible.\n"+
			"6) Do not include explanations.\n\n"+
			"Query:\n%s",
		maxQueries,
		query,
	)
}

func Score(content string) string {
	return "You are scoring memory importance for long-term retrieval.\n" +
		"Return only one decimal number between 0 and 1.\n" +
		"0 means disposable or low-value context.\n" +
		"1 means durable user preference/profile or critical instruction.\n\n" +
		"Memory:\n" + content
}

func BatchScore(contents []string) string {
	payload, _ := json.Marshal(contents)
	return "You are scoring memory importance for long-term retrieval.\n" +
		"Return ONLY valid JSON with this exact shape: {\"scores\":[...]}.\n" +
		fmt.Sprintf("The scores array MUST contain exactly %d numbers between 0 and 1 in the same order as the input array.\n", len(contents)) +
		"Do not include explanations or additional keys.\n\n" +
		"Input memories (JSON array):\n" + string(payload)
}
