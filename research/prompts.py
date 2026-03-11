"""Prompts used by the LOCOMO eval harness.

Keep all LLM-facing prompt strings here so they can be iterated on without
touching the evaluation logic in eval_locomo_f1_bleu.py.
"""

from __future__ import annotations


def build_generation_prompt(
    question: str,
    contexts: list[str],
    candidate_answers: list[str] | None = None,
    allow_inference: bool = False,
) -> str:
    """Build the RAG answer-generation prompt.

    Instructions are intentionally terse: the model should produce a short
    factual answer (name / date / number / phrase) derived solely from the
    supplied evidence lines, mirroring the LOCOMO paper's answer format.

    Key design choices informed by mem0/evaluation/prompts.py:
    - Explicit instruction to prioritise the most recent memory when evidence
      conflicts (temporal grounding).
    - Numbered evidence lines make it easy for the model to cite or scan.
    - Hard upper-bound on answer length kept implicit via "short factual answer"
      rather than a token count, which is model-agnostic.
    """
    joined = "\n".join(f"{idx + 1}. {c}" for idx, c in enumerate(contexts))
    candidate_block = ""
    cleaned_candidates = [c.strip() for c in (candidate_answers or []) if c and c.strip()]
    if cleaned_candidates:
        rendered = "\n".join(f"{idx + 1}. {c}" for idx, c in enumerate(cleaned_candidates))
        candidate_block = f"Candidate answers:\n{rendered}\n\n"

    inference_rule = (
        "  - For likely/reasoning questions, return the most supported short inference from the evidence.\n"
        if allow_inference
        else ""
    )

    return (
        "You are a memory assistant. Answer the question using ONLY the numbered evidence lines below.\n"
        "Rules:\n"
        "  - Return a short factual answer: a name, date, number, yes/no, or brief phrase.\n"
        "  - If candidate answers are provided, prefer the shortest candidate that directly answers the question when it clearly matches the evidence.\n"
        "  - If the candidates are weak, incomplete, or do not directly answer the question, ignore them and return a concise grounded answer from the evidence.\n"
        "  - Do not mix multiple weak candidates into a new unsupported answer.\n"
        f"{inference_rule}"
        "  - If evidence lines conflict, use the most recent one.\n"
        "  - Do NOT explain your reasoning or repeat the question.\n"
        "  - If the answer is not present in the evidence, reply exactly: Unknown\n\n"
        f"Question: {question}\n\n"
        f"{candidate_block}"
        f"Evidence:\n{joined}\n\n"
        "Answer:"
    )


def build_open_domain_resolution_prompt(
    question: str,
    contexts: list[str],
    candidate_answers: list[str] | None = None,
) -> str:
    joined = "\n".join(f"{idx + 1}. {c}" for idx, c in enumerate(contexts))
    cleaned_candidates = [c.strip() for c in (candidate_answers or []) if c and c.strip()]
    candidate_block = ""
    if cleaned_candidates:
        rendered = "\n".join(f"{idx + 1}. {c}" for idx, c in enumerate(cleaned_candidates))
        candidate_block = f"Candidate answers:\n{rendered}\n\n"

    return (
        "You are resolving a non-binary open-domain memory question from evidence.\n"
        "Rules:\n"
        "  - Choose the shortest answer that still matches the evidence: a label, noun phrase, short comma-separated list, or concise role.\n"
        "  - Preserve specific fields, majors, roles, locations, nicknames, and titles when evidence supports them.\n"
        "  - If the exact answer is not stated, infer the shortest plausible answer from repeated goals, values, preferences, or behavior.\n"
        "  - You may use lightweight common knowledge only when directly anchored by the evidence, such as mapping a named game to its console or volunteer/work patterns to a likely field.\n"
        "  - Reject answers contradicted by stronger evidence.\n"
        "  - If the evidence is insufficient, reply exactly: Unknown.\n"
        "  - Do not include chain-of-thought.\n\n"
        f"Question: {question}\n\n"
        f"{candidate_block}"
        f"Evidence:\n{joined}\n\n"
        "Answer:"
    )


def build_open_domain_evidence_selection_prompt(
    question: str,
    candidate_lines: list[str],
    max_lines: int,
) -> str:
    joined = "\n".join(f"{idx + 1}. {line}" for idx, line in enumerate(candidate_lines))
    return (
        "You are selecting the strongest evidence lines for an open-domain memory question.\n"
        "Rules:\n"
        f"  - Return JSON only using this exact schema: {{\"line_numbers\":[...]}}.\n"
        f"  - Select between 1 and {max_lines} line numbers.\n"
        "  - Prefer lines that directly reveal personal goals, values, preferences, plans, beliefs, history, or repeated behavior.\n"
        "  - Exclude greetings, compliments, acknowledgements, and lines that do not materially help answer the question.\n"
        "  - If several lines say the same thing, keep the strongest and most specific ones.\n\n"
        f"Question: {question}\n\n"
        f"Candidate evidence:\n{joined}\n\n"
        "JSON:"
    )


def build_open_domain_candidate_prompt(
    question: str,
    contexts: list[str],
    max_candidates: int,
) -> str:
    joined = "\n".join(f"{idx + 1}. {c}" for idx, c in enumerate(contexts))
    return (
        "You are generating short candidate answers for an open-domain memory question.\n"
        "Rules:\n"
        '  - Return JSON only using this exact schema: {"candidates":["...", "..."]}.\n'
        f"  - Return between 1 and {max_candidates} candidates.\n"
        "  - Each candidate must be a short answer phrase, label, noun phrase, or comma-separated list, not a sentence.\n"
        "  - Preserve specific majors, fields, nicknames, locations, roles, titles, and possessions when evidence supports them.\n"
        "  - You may use lightweight common knowledge only when directly anchored by the evidence, such as mapping a named game to its console.\n"
        "  - Do not include explanations or line numbers.\n"
        "  - If the evidence is insufficient, return an empty list.\n\n"
        f"Question: {question}\n\n"
        f"Evidence:\n{joined}\n\n"
        "JSON:"
    )


def build_open_domain_hyde_prompt(question: str) -> str:
    return (
        "You are writing one short hypothetical evidence line for retrieval.\n"
        "Rules:\n"
        "  - Return exactly one line of plain text, not JSON.\n"
        "  - Preserve the named person and the likely answer space implied by the question.\n"
        "  - Write it like a memory/profile statement that could appear in conversation notes.\n"
        "  - Do not mention uncertainty words like maybe, likely, probably.\n"
        "  - Do not add extra explanation.\n\n"
        f"Question: {question}\n\n"
        "Hypothetical evidence:"
    )


def build_open_domain_verification_prompt(
    question: str,
    contexts: list[str],
    candidate_answers: list[str] | None = None,
    extractive_answer: str | None = None,
) -> str:
    joined = "\n".join(f"{idx + 1}. {c}" for idx, c in enumerate(contexts))
    cleaned_candidates = [c.strip() for c in (candidate_answers or []) if c and c.strip()]
    candidate_block = ""
    if cleaned_candidates:
        rendered = "\n".join(f"{idx + 1}. {c}" for idx, c in enumerate(cleaned_candidates))
        candidate_block = f"Candidate answers:\n{rendered}\n\n"
    extractive_block = f"Extractive fallback: {extractive_answer.strip()}\n\n" if extractive_answer and extractive_answer.strip() else ""
    return (
        "You are verifying and finalizing an open-domain answer from memory evidence.\n"
        "Use FEVER-style reasoning over the evidence: support, contradiction, or insufficient evidence.\n"
        "Rules:\n"
        '  - Return JSON only using this exact schema: {"final_answer":"...","verdict":"supported|weakly_supported|contradicted|insufficient","best_candidate":"...","supporting_lines":[...]}.\n'
        "  - First weigh which evidence lines support or contradict the candidate answers.\n"
        "  - Prefer the answer best supported by multiple lines or by strong profile evidence.\n"
        "  - For yes/no or likely questions, return exactly one of: Yes, No, Likely yes, Likely no, Unknown.\n"
        "  - For explicit A-or-B choice questions, return one of the choices, not yes/no.\n"
        "  - For label questions such as politics, religion, traits, or financial status, use a conventional short label or short phrase.\n"
        "  - For counterfactual questions, check whether the current goal or preference depends on the mentioned prior support, event, or condition.\n"
        "  - For future-choice questions, strong conflicting plans or clearly negative prior experiences count against repeating that choice.\n"
        "  - For stable-profile questions, prefer the answer implied by repeated goals, values, preferences, or behavior over one-off chatter.\n"
        "  - If a brief grounded clause is needed, include it after a semicolon in final_answer.\n"
        "  - If evidence is insufficient overall, final_answer must be Unknown.\n"
        "  - Do not include chain-of-thought.\n\n"
        f"Question: {question}\n\n"
        f"{candidate_block}"
        f"{extractive_block}"
        f"Evidence:\n{joined}\n\n"
        "JSON:"
    )


def build_open_domain_query_rewrite_prompt(question: str, max_queries: int) -> str:
    return (
        "You are rewriting an open-domain personal memory question for retrieval.\n"
        "Rules:\n"
        '  - Return JSON only using this exact schema: {"queries":["...", "..."]}.\n'
        f"  - Return between 2 and {max_queries} queries.\n"
        "  - Each query must be short and semantically different.\n"
        "  - Focus on durable profile signals: goals, values, beliefs, preferences, plans, identity, or repeated behavior.\n"
        "  - Preserve the named person and key entities from the question.\n"
        "  - Do not answer the question.\n"
        "  - Do not invent facts that are not stated in the question.\n"
        "  - Prefer retrieval probes, not explanations.\n\n"
        f"Question: {question}\n\n"
        "JSON:"
    )


def build_profile_summary_prompt(
    entity: str,
    evidence_lines: list[str],
    max_lines: int,
) -> str:
    joined = "\n".join(f"{idx + 1}. {line}" for idx, line in enumerate(evidence_lines[:max_lines]))
    return (
        "You are building a grounded profile memory for a conversational assistant.\n"
        "Rules:\n"
        '  - Return JSON only using this exact schema: {"entity":"...","summary_lines":["...", "..."]}.\n'
        "  - Write 4 to 12 short third-person profile lines about the named person.\n"
        "  - Use only supported information from the evidence.\n"
        "  - Prefer durable information: goals, plans, values, beliefs, preferences, relationships, repeated activities, roles, and stable traits.\n"
        "  - Preserve concrete specifics that can answer later profile questions: majors, fields, job directions, named hobbies/media, locations, nicknames, possessions, pets, organizations, and recurring constraints.\n"
        "  - Include stable dislikes, constraints, setbacks, or aversions only when they plausibly affect future choices or preferences.\n"
        "  - You may include soft inferences only when strongly supported by multiple lines, and phrase them cautiously.\n"
        "  - Do not mention line numbers.\n"
        "  - Do not include unsupported external knowledge.\n"
        "  - Keep each line concise and retrieval-friendly.\n\n"
        f"Entity: {entity}\n\n"
        f"Evidence:\n{joined}\n\n"
        "JSON:"
    )


def build_profile_facets_prompt(
    entity: str,
    evidence_lines: list[str],
    max_lines: int,
) -> str:
    joined = "\n".join(f"{idx + 1}. {line}" for idx, line in enumerate(evidence_lines[:max_lines]))
    return (
        "You are building typed profile memories for a conversational assistant.\n"
        "Rules:\n"
        '  - Return JSON only using this exact schema: {"entity":"...","facets":{"identity_roles":["..."],"preferences_interests":["..."],"goals_plans":["..."],"values_beliefs":["..."],"relationships":["..."],"traits_tendencies":["..."]}}.\n'
        "  - Use only supported information from the evidence.\n"
        "  - Each item must be a short third-person statement about the named person.\n"
        "  - Include only durable or repeated information, not one-off chatter.\n"
        "  - You may include soft inferences only when strongly supported by multiple lines, and phrase them cautiously.\n"
        "  - Omit empty facets instead of inventing content.\n"
        "  - Do not include unsupported external knowledge.\n"
        "  - Keep each item concise and retrieval-friendly.\n\n"
        f"Entity: {entity}\n\n"
        f"Evidence:\n{joined}\n\n"
        "JSON:"
    )
