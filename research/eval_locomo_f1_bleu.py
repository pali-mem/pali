#!/usr/bin/env python3
# =============================================================================
# INTEGRITY NOTICE — DO NOT ADD LOCOMO-SPECIFIC TINKERING
# =============================================================================
# This eval measures pali's real retrieval and answer quality.
# It must NOT contain any logic that is aware of specific LOCOMO questions,
# characters, story facts, or ground-truth answers.
#
# Forbidden patterns:
#   - Hardcoded keywords from LOCOMO stories (names, places, phrases)
#   - Query rewrites targeting specific known eval questions
#   - Scoring bonuses keyed to LOCOMO ground-truth answer text
#   - Any regex, constant, or branch that only makes sense for LOCOMO content
#
# Why: eval-side tinkering inflates benchmark numbers without improving the
# actual product. Users get the real pali; the benchmark should measure that —
# nothing more. We do not optimise for benchmarks.
# =============================================================================

"""Evaluate LOCOMO QA metrics (F1, BLEU-1) with retrieval + optional generation.

Research-only approximation of paper protocol:
- store fixture memories into fresh local Pali server
- run retrieval for each LOCOMO question
- score lexical metrics against reference answers
- optional generated answer from local Ollama model (no LLM judge)
"""

from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
import hashlib
import json
import math
import os
import re
import signal
import sqlite3
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request
from collections import Counter
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any


TOKEN_RE = re.compile(r"[a-z0-9]+")
SENTENCE_SPLIT_RE = re.compile(r"(?<=[.!?])\s+")
DIALOG_ID_RE = re.compile(r"\[dialog:([^\]]+)\]")
TURN_TAG_RE = re.compile(r"\[(\w+):([^\]]+)\]")
TURN_SPEAKER_RE = re.compile(r"^\s*([A-Za-z][A-Za-z0-9 .'\-]{0,80}):\s*(.+)\s*$")
TEMPORAL_QUERY_RE = re.compile(r"\b(when|date|time|day|month|year|before|after|first|last|earlier|later|yesterday|today|tomorrow)\b")
PERSON_QUERY_RE = re.compile(r"\b(who|name|which person|whose)\b")
MULTIHOP_QUERY_RE = re.compile(r"\b(before|after|first|last|both|either|then|and)\b")
AGGREGATION_QUERY_RE = re.compile(
    r"\b(what all|list|activities?|events?|things?|places?|books?|hobbies?|interests?|participated|attended|done)\b"
)
THINK_BLOCK_RE = re.compile(r"(?is)<think>.*?</think>")
THINK_TAG_RE = re.compile(r"(?i)</?think>")
ANSWER_PREFIX_RE = re.compile(r"(?i)^\s*(?:answer|final answer)\s*:\s*")
MONTH_NAME_RE = r"(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)"
RELATIVE_DATE_RE = re.compile(rf"\b(?:the\s+)?(?:week|month|year|day|monday|tuesday|wednesday|thursday|friday|saturday|sunday)\s+(?:before|after)\s+\d{{1,2}}\s+{MONTH_NAME_RE}\s*,?\s*\d{{4}}\b", re.IGNORECASE)
FULL_DATE_RE = re.compile(rf"\b\d{{1,2}}\s+{MONTH_NAME_RE}\s*,?\s*\d{{4}}\b", re.IGNORECASE)
MONTH_DAY_YEAR_RE = re.compile(rf"\b{MONTH_NAME_RE}\s+\d{{1,2}},?\s*\d{{4}}\b", re.IGNORECASE)
MONTH_YEAR_RE = re.compile(rf"\b{MONTH_NAME_RE}\s+\d{{4}}\b", re.IGNORECASE)
YEAR_RE = re.compile(r"\b(?:19|20)\d{2}\b")
DURATION_RE = re.compile(r"\b\d+\s+(?:years?|months?|weeks?|days?)\b", re.IGNORECASE)
TEMPORAL_SIGNAL_RE = re.compile(
    r"\b(yesterday|today|tomorrow|last\s+(?:week|month|year)|next\s+(?:week|month|year)|\d+\s+(?:years?|months?|weeks?|days?)\s+ago)\b",
    re.IGNORECASE,
)
SPEAKER_PREFIX_RE = re.compile(r"^\s*[A-Za-z][A-Za-z0-9 .'\-]{0,80}(?:\s*\([^)]+\))?:\s*")
ACK_LINE_RE = re.compile(r"^(?:hey|hi|hello|wow|thanks|thank you|cool|awesome|great|nice)\b", re.IGNORECASE)
LOW_SIGNAL_LINE_RE = re.compile(
    r"^(?:absolutely|definitely|totally|exactly|sure|yep|yeah|yup|ok|okay|alright|sounds good|no worries|got it|i see)\b",
    re.IGNORECASE,
)
SOURCE_STAMP_RE = re.compile(r"^eval_row_(\d+)(?::.*)?$")
QUESTION_LIKE_RE = re.compile(r"(?i)^(?:what|who|when|where|why|how|which|whose|did|does|do|is|are|was|were|can|could|would|should|have|has|had|will)\b")
LEADING_DATE_PREFIX_RE = re.compile(rf"(?i)^on\s+\d{{1,2}}\s+{MONTH_NAME_RE}\s+\d{{4}},\s*")
SAID_THAT_PREFIX_RE = re.compile(r"(?i)^[A-Z][A-Za-z0-9 .'\-]{0,80}\s+said that\s+")
LIKELY_QUERY_RE = re.compile(r"\b(would|could|likely|probably|might|considered)\b", re.IGNORECASE)
PROFILE_SIGNAL_RE = re.compile(
    r"\b(i am|i'm|i was|i have|i've|i want|i plan|i hope|i love|i enjoy|i work|i study|i read|i collect|i volunteer|i joined|i chose|i decided|my goal|my dream|because|that's why)\b",
    re.IGNORECASE,
)
WHY_CLAUSE_RE = re.compile(r"(?i)\b(?:because|that's why|so that)\b(.+)$")
QUESTION_ENTITY_RE = re.compile(r"\b[A-Z][a-z]+(?:\s+[A-Z][a-z]+)?\b")

STOPWORDS = {
    "a",
    "an",
    "and",
    "are",
    "as",
    "at",
    "be",
    "by",
    "did",
    "do",
    "does",
    "for",
    "from",
    "had",
    "has",
    "have",
    "how",
    "in",
    "is",
    "it",
    "of",
    "on",
    "or",
    "that",
    "the",
    "their",
    "there",
    "to",
    "was",
    "were",
    "what",
    "when",
    "where",
    "which",
    "who",
    "why",
    "with",
}

QUESTION_ENTITY_STOPWORDS = {
    "What",
    "Who",
    "When",
    "Where",
    "Why",
    "How",
    "Which",
    "Whose",
    "Would",
    "Could",
    "Should",
    "Can",
    "Did",
    "Does",
    "Do",
    "Is",
    "Are",
    "Was",
    "Were",
    "Will",
    "Have",
    "Has",
    "Had",
}

STORE_BATCH_SIZE = 64
STORE_BATCH_TIMEOUT_SECONDS = 90.0
STORE_SINGLE_TIMEOUT_SECONDS = 45.0
OPENROUTER_MAX_INFLIGHT = 12
OPENROUTER_REQUEST_SEMAPHORE = threading.Semaphore(OPENROUTER_MAX_INFLIGHT)

LOCOMO_CATEGORY_LABELS = {
    "1": "Multi-hop",
    "2": "Temporal",
    "3": "Open-domain",
    "4": "Single-hop",
    "5": "Adversarial",
}


def category_label(category: Any) -> str:
    key = str(category).strip()
    if not key:
        return "Unknown Category"
    return LOCOMO_CATEGORY_LABELS.get(key, f"Unknown Category ({key})")


def category_sort_key(category: Any) -> tuple[int, str]:
    key = str(category).strip()
    if key.isdigit():
        return (0, f"{int(key):06d}")
    return (1, key)


def normalize_tokens(text: str) -> list[str]:
    return TOKEN_RE.findall((text or "").lower())


def token_f1(pred: str, ref: str) -> float:
    p = normalize_tokens(pred)
    r = normalize_tokens(ref)
    if not p or not r:
        return 0.0
    cp = Counter(p)
    cr = Counter(r)
    common = sum((cp & cr).values())
    if common == 0:
        return 0.0
    precision = common / len(p)
    recall = common / len(r)
    return (2 * precision * recall) / (precision + recall)


def bleu1(pred: str, ref: str) -> float:
    p = normalize_tokens(pred)
    r = normalize_tokens(ref)
    if not p or not r:
        return 0.0

    cp = Counter(p)
    cr = Counter(r)
    overlap = sum((cp & cr).values())
    precision = overlap / len(p)
    if precision <= 0:
        return 0.0

    if len(p) >= len(r):
        bp = 1.0
    else:
        bp = math.exp(1 - (len(r) / len(p)))
    return bp * precision


def token_f1_no_stopwords(pred: str, ref: str) -> float:
    p = [t for t in normalize_tokens(pred) if t not in STOPWORDS]
    r = [t for t in normalize_tokens(ref) if t not in STOPWORDS]
    if not p or not r:
        return 0.0
    cp = Counter(p)
    cr = Counter(r)
    common = sum((cp & cr).values())
    if common == 0:
        return 0.0
    precision = common / len(p)
    recall = common / len(r)
    return (2 * precision * recall) / (precision + recall)


def normalized_exact_match(pred: str, ref: str) -> float:
    p = " ".join(normalize_tokens(pred))
    r = " ".join(normalize_tokens(ref))
    if not p or not r:
        return 0.0
    return 1.0 if p == r else 0.0


def build_run_stamp() -> str:
    # Unique per eval execution; used to isolate source stamps in persistent DBs.
    nonce = hashlib.sha1(os.urandom(16)).hexdigest()[:8]
    return f"{int(time.time() * 1000)}_{os.getpid()}_{nonce}"


def fixture_source_stamp(idx: int, run_stamp: str = "") -> str:
    base = f"eval_row_{idx}"
    if run_stamp:
        return f"{base}:run_{run_stamp}"
    return base


def compute_config_fingerprint(fixture: list[dict[str, Any]], args: argparse.Namespace) -> str:
    fixture_digest = hashlib.sha256(
        json.dumps(fixture, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    ).hexdigest()
    payload = {
        "fixture_digest": fixture_digest,
        "entity_fact_backend": str(args.entity_fact_backend),
        "parser": {
            "enabled": bool(args.parser_enabled),
            "provider": str(args.parser_provider),
            "store_raw_turn": bool(args.parser_store_raw_turn),
            "max_facts": int(args.parser_max_facts),
            "dedupe_threshold": float(args.parser_dedupe_threshold),
            "update_threshold": float(args.parser_update_threshold),
            "ollama_url": str(args.parser_ollama_url),
            "ollama_model": str(args.parser_ollama_model),
            "openrouter_model": str(args.parser_openrouter_model),
            "ollama_timeout_ms": int(args.parser_ollama_timeout_ms),
        },
        "structured_memory": {
            "enabled": bool(args.structured_memory_enabled),
            "dual_write_observations": bool(args.structured_dual_write_observations),
            "dual_write_events": bool(args.structured_dual_write_events),
            "query_routing_enabled": bool(args.structured_query_routing_enabled),
            "max_observations": int(args.structured_max_observations),
        },
    }
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def parse_index_map_payload(raw: Any) -> tuple[dict[int, set[str]], str, int]:
    if not isinstance(raw, dict):
        return {}, "", 0
    if isinstance(raw.get("index_to_ids"), dict):
        mapped: dict[int, set[str]] = {}
        for key, value in raw.get("index_to_ids", {}).items():
            try:
                idx = int(key)
            except (TypeError, ValueError):
                continue
            ids: set[str] = set()
            if isinstance(value, list):
                for item in value:
                    if isinstance(item, str) and item.strip():
                        ids.add(item.strip())
            elif isinstance(value, str) and value.strip():
                ids.add(value.strip())
            if ids:
                mapped[idx] = ids
        fingerprint = str(raw.get("config_fingerprint", "")).strip()
        schema_version = int(raw.get("schema_version", 2))
        return mapped, fingerprint, schema_version
    mapped: dict[int, set[str]] = {}
    for key, value in raw.items():
        try:
            idx = int(key)
        except (TypeError, ValueError):
            continue
        ids: set[str] = set()
        if isinstance(value, str) and value.strip():
            ids.add(value.strip())
        elif isinstance(value, list):
            for item in value:
                if isinstance(item, str) and item.strip():
                    ids.add(item.strip())
        if ids:
            mapped[idx] = ids
    return mapped, "", 1


def collect_index_map_from_db(db: Path, run_stamp: str = "") -> dict[int, set[str]]:
    out: dict[int, set[str]] = {}
    db = db.absolute()
    print(f"collect_index_map_from_db: reading {db} (exists={db.exists()}, size={db.stat().st_size if db.exists() else 'N/A'})", flush=True)
    if not db.exists():
        return out
    conn = sqlite3.connect(str(db))
    try:
        cur = conn.cursor()
        if run_stamp:
            cur.execute(
                "SELECT id, source FROM memories WHERE source LIKE ?",
                # Include parser/derived writes whose source appends suffixes
                # (e.g. `:run_<stamp>:parser`), not only the raw-turn rows.
                (f"eval_row_%:run_{run_stamp}%",),
            )
        else:
            cur.execute("SELECT id, source FROM memories WHERE source LIKE 'eval_row_%'")
        for memory_id, source in cur.fetchall():
            if not isinstance(memory_id, str) or not isinstance(source, str):
                continue
            m = SOURCE_STAMP_RE.match(source.strip())
            if not m:
                continue
            idx = int(m.group(1))
            out.setdefault(idx, set()).add(memory_id.strip())
    finally:
        conn.close()
    return out


def count_existing_profile_memories(db: Path) -> tuple[int, int]:
    db = db.absolute()
    if not db.exists():
        return 0, 0
    conn = sqlite3.connect(str(db))
    try:
        cur = conn.cursor()
        cur.execute(
            """
            SELECT source
            FROM memories
            WHERE kind = 'summary'
              AND source LIKE 'profile_summary:%'
            """
        )
        rows = [str(row[0] or "") for row in cur.fetchall()]
    finally:
        conn.close()

    entities: set[str] = set()
    for source in rows:
        parts = source.split(":")
        if len(parts) >= 3 and parts[1]:
            entities.add(parts[1])
    return len(rows), len(entities)


def json_request(url: str, payload: Any = None, timeout_s: float = 30.0) -> tuple[int, Any]:
    method = "POST" if payload is not None else "GET"
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json"},
        method=method,
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout_s) as resp:
            body = resp.read().decode("utf-8")
            return resp.getcode(), json.loads(body) if body else {}
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8")
        try:
            return e.code, json.loads(body) if body else {}
        except json.JSONDecodeError:
            return e.code, {"raw": body}
    except urllib.error.URLError as e:
        return 0, {"error": str(e)}


def split_sentences(text: str) -> list[str]:
    raw = (text or "").strip()
    if not raw:
        return []
    parts = [p.strip() for p in SENTENCE_SPLIT_RE.split(raw) if p.strip()]
    return parts if parts else [raw]


def compact_query(query: str) -> str:
    toks = normalize_tokens(query)
    if not toks:
        return query.strip()
    keep = [t for t in toks if t not in STOPWORDS]
    if len(keep) < 3:
        keep = toks[: min(8, len(toks))]
    return " ".join(keep)


def build_query_variants(query: str, max_variants: int) -> list[str]:
    base = query.strip()
    variants = [base]
    compact = compact_query(base)
    if compact and compact != base:
        variants.append(compact)

    toks = normalize_tokens(base)
    if toks:
        tail = " ".join(toks[-min(8, len(toks)):])
        if tail and tail not in variants:
            variants.append(tail)

    # Keep stable order, dedupe, and cap count.
    out: list[str] = []
    seen: set[str] = set()
    for v in variants:
        v = v.strip()
        if not v or v in seen:
            continue
        out.append(v)
        seen.add(v)
        if len(out) >= max_variants:
            break
    return out


def merge_query_variants(*groups: list[str], max_variants: int) -> list[str]:
    out: list[str] = []
    seen: set[str] = set()
    for group in groups:
        for value in group:
            text = (value or "").strip()
            key = text.lower()
            if not text or key in seen:
                continue
            seen.add(key)
            out.append(text)
            if len(out) >= max_variants:
                return out
    return out


def parse_query_rewrite_response(raw: str, max_queries: int) -> list[str]:
    text = (raw or "").strip()
    if not text:
        return []

    def clamp_queries(values: list[str]) -> list[str]:
        out: list[str] = []
        seen: set[str] = set()
        for value in values:
            query = " ".join(str(value or "").split()).strip()
            key = query.lower()
            if not query or key in seen:
                continue
            seen.add(key)
            out.append(query)
            if len(out) >= max_queries:
                break
        return out

    try:
        payload = json.loads(text)
        if isinstance(payload, dict) and isinstance(payload.get("queries"), list):
            return clamp_queries([str(v) for v in payload["queries"] if isinstance(v, str)])
        if isinstance(payload, list):
            return clamp_queries([str(v) for v in payload if isinstance(v, str)])
    except Exception:
        pass

    return clamp_queries([line.strip(" -0123456789.") for line in text.splitlines() if line.strip()])


def parse_profile_summary_response(raw: str, entity: str, max_lines: int) -> list[str]:
    text = (raw or "").strip()
    if not text:
        return []

    def clamp_lines(values: list[str]) -> list[str]:
        out: list[str] = []
        seen: set[str] = set()
        for value in values:
            line = " ".join(str(value or "").split()).strip()
            key = line.lower()
            if not line or key in seen:
                continue
            seen.add(key)
            out.append(line)
            if len(out) >= max_lines:
                break
        return out

    try:
        payload = json.loads(text)
        if isinstance(payload, dict) and isinstance(payload.get("summary_lines"), list):
            return clamp_lines([str(v) for v in payload["summary_lines"] if isinstance(v, str)])
        if isinstance(payload, list):
            return clamp_lines([str(v) for v in payload if isinstance(v, str)])
    except Exception:
        pass

    return clamp_lines([line.strip(" -0123456789.") for line in text.splitlines() if line.strip()])


def parse_profile_facets_response(raw: str, max_items: int) -> dict[str, list[str]]:
    text = (raw or "").strip()
    if not text:
        return {}

    def clamp_items(values: Any) -> list[str]:
        if not isinstance(values, list):
            return []
        out: list[str] = []
        seen: set[str] = set()
        for value in values:
            item = " ".join(str(value or "").split()).strip()
            key = item.lower()
            if not item or key in seen:
                continue
            seen.add(key)
            out.append(item)
            if len(out) >= max_items:
                break
        return out

    try:
        payload = json.loads(text)
    except Exception:
        return {}
    if not isinstance(payload, dict):
        return {}
    facets = payload.get("facets")
    if not isinstance(facets, dict):
        return {}
    out: dict[str, list[str]] = {}
    for key in PROFILE_FACET_LABELS:
        values = clamp_items(facets.get(key))
        if values:
            out[key] = values
    return out


def parse_open_domain_verification_response(raw: str) -> dict[str, Any]:
    text = (raw or "").strip()
    if not text:
        return {}
    try:
        payload = json.loads(text)
    except Exception:
        return {}
    if not isinstance(payload, dict):
        return {}
    final_answer = " ".join(str(payload.get("final_answer", "")).split()).strip()
    verdict = " ".join(str(payload.get("verdict", "")).split()).strip().lower()
    best_candidate = " ".join(str(payload.get("best_candidate", "")).split()).strip()
    supporting_lines = payload.get("supporting_lines", [])
    if not isinstance(supporting_lines, list):
        supporting_lines = []
    supporting_lines = [int(v) for v in supporting_lines if isinstance(v, int)]
    return {
        "final_answer": final_answer,
        "verdict": verdict,
        "best_candidate": best_candidate,
        "supporting_lines": supporting_lines,
    }


def parse_open_domain_resolution_response(raw: str) -> dict[str, Any]:
    text = (raw or "").strip()
    if not text:
        return {}
    try:
        payload = json.loads(text)
    except Exception:
        return {}
    if not isinstance(payload, dict):
        return {}
    final_answer = " ".join(str(payload.get("final_answer", "")).split()).strip()
    supporting_lines = payload.get("supporting_lines", [])
    if not isinstance(supporting_lines, list):
        supporting_lines = []
    supporting_lines = [int(v) for v in supporting_lines if isinstance(v, int)]
    focused_facts = payload.get("focused_facts", [])
    if not isinstance(focused_facts, list):
        focused_facts = []
    focused_facts = [
        " ".join(str(value or "").split()).strip()
        for value in focused_facts
        if str(value or "").strip()
    ][:3]
    return {
        "final_answer": final_answer,
        "supporting_lines": supporting_lines,
        "focused_facts": focused_facts,
    }


def parse_dialog_id(content: str) -> str:
    m = DIALOG_ID_RE.search(content or "")
    if not m:
        return ""
    return m.group(1).strip()


def parse_dialog_session_index(dialog_id: str) -> tuple[str, int]:
    # D12:3 -> ("D12", 3)
    if ":" not in dialog_id:
        return "", -1
    left, right = dialog_id.split(":", 1)
    if not left or not right.isdigit():
        return "", -1
    return left, int(right)


def scoped_session_id(tenant_id: str, session_id: str) -> str:
    return f"{tenant_id}::{session_id}"


def scoped_dialog_id(tenant_id: str, dialog_id: str) -> str:
    return f"{tenant_id}::{dialog_id}"


def build_dialog_context_index(fixture: list[dict[str, Any]]) -> tuple[dict[str, list[str]], dict[str, str]]:
    by_session: dict[str, list[tuple[int, str]]] = {}
    by_dialog_id: dict[str, str] = {}
    for row in fixture:
        tenant_id = str(row.get("tenant_id", "")).strip()
        if not tenant_id:
            continue
        content = str(row.get("content", ""))
        did = parse_dialog_id(content)
        if not did:
            continue
        sess, idx = parse_dialog_session_index(did)
        if not sess or idx < 0:
            continue
        session_key = scoped_session_id(tenant_id, sess)
        dialog_key = scoped_dialog_id(tenant_id, did)
        by_session.setdefault(session_key, []).append((idx, did))
        by_dialog_id[dialog_key] = content

    ordered_by_session: dict[str, list[str]] = {}
    for session_key, pairs in by_session.items():
        pairs.sort(key=lambda x: x[0])
        ordered_by_session[session_key] = [did for _, did in pairs]

    return ordered_by_session, by_dialog_id


def expand_context_with_neighbors(
    selected_contents: list[str],
    ordered_by_session: dict[str, list[str]],
    by_dialog_id: dict[str, str],
    tenant_id: str,
    window: int,
    max_context_items: int,
) -> list[str]:
    if window <= 0:
        return selected_contents[:max_context_items]

    out: list[str] = []
    seen: set[str] = set()

    def add_text(text: str) -> None:
        t = text.strip()
        if not t or t in seen:
            return
        seen.add(t)
        out.append(t)

    for c in selected_contents:
        add_text(c)
        did = parse_dialog_id(c)
        if not did:
            continue
        sess, idx = parse_dialog_session_index(did)
        if not sess or idx < 0:
            continue
        session_key = scoped_session_id(tenant_id, sess)
        if session_key not in ordered_by_session:
            continue
        # Collect neighbor dialog IDs by numeric index.
        for offset in range(-window, window + 1):
            if offset == 0:
                continue
            neighbor_id = f"{sess}:{idx + offset}"
            neighbor_key = scoped_dialog_id(tenant_id, neighbor_id)
            if neighbor_key in by_dialog_id:
                add_text(by_dialog_id[neighbor_key])
            if len(out) >= max_context_items:
                return out
        if len(out) >= max_context_items:
            return out

    return out[:max_context_items]


def has_temporal_signal(text: str) -> bool:
    raw = (text or "").strip()
    if not raw:
        return False
    return bool(
        RELATIVE_DATE_RE.search(raw)
        or FULL_DATE_RE.search(raw)
        or MONTH_DAY_YEAR_RE.search(raw)
        or MONTH_YEAR_RE.search(raw)
        or YEAR_RE.search(raw)
        or DURATION_RE.search(raw)
        or TEMPORAL_SIGNAL_RE.search(raw)
    )


def parse_annotated_turn(content: str) -> dict[str, str]:
    tags: dict[str, str] = {}
    raw = content or ""
    for key, val in TURN_TAG_RE.findall(raw):
        tags[key.strip().lower()] = val.strip()

    stripped = TURN_TAG_RE.sub(" ", raw)
    stripped = " ".join(stripped.split()).strip()

    speaker = ""
    utterance = stripped
    m = TURN_SPEAKER_RE.match(stripped)
    if m:
        speaker = m.group(1).strip()
        utterance = m.group(2).strip()

    return {
        "time": tags.get("time", ""),
        "speaker_a": tags.get("speaker_a", ""),
        "speaker_b": tags.get("speaker_b", ""),
        "speaker": speaker,
        "utterance": utterance,
    }


def normalize_context_line(content: str) -> str:
    turn = parse_annotated_turn(content)
    utterance = (turn.get("utterance") or "").strip()
    if not utterance:
        return " ".join((content or "").split()).strip()

    speaker = (turn.get("speaker") or "").strip()
    tval = (turn.get("time") or "").strip()
    if speaker and tval:
        return f"{speaker} ({tval}): {utterance}"
    if speaker:
        return f"{speaker}: {utterance}"
    return utterance


def canonical_entity_tag(entity: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", (entity or "").strip().lower()).strip("-")


PROFILE_FACET_LABELS = {
    "identity_roles": "Identity and roles",
    "preferences_interests": "Preferences and interests",
    "goals_plans": "Goals and plans",
    "values_beliefs": "Values and beliefs",
    "relationships": "Relationships",
    "traits_tendencies": "Traits and tendencies",
}


def classify_query(query: str) -> tuple[bool, bool, bool]:
    q = (query or "").strip().lower()
    if not q:
        return False, False, False
    temporal = bool(TEMPORAL_QUERY_RE.search(q))
    person = bool(PERSON_QUERY_RE.search(q))
    multihop = bool(MULTIHOP_QUERY_RE.search(q))
    return temporal, person, multihop


def is_inference_query(query: str) -> bool:
    q = (query or "").strip().lower()
    if not q:
        return False
    if LIKELY_QUERY_RE.search(q):
        return True
    if q.startswith("why ") or q.startswith("how "):
        return True
    if "personality trait" in q or "personality traits" in q:
        return True
    if "what fields" in q or "career option" in q:
        return True
    if "what does" in q and "think" in q:
        return True
    return False


def allow_inference_generation(query: str) -> bool:
    q = (query or "").strip().lower()
    if not q:
        return False
    return q.startswith("why ") or q.startswith("how ")


def is_booleanish_query(query: str) -> bool:
    q = (query or "").strip().lower()
    if not q:
        return False
    prefixes = ("is ", "are ", "was ", "were ", "do ", "does ", "did ", "can ", "could ", "would ", "should ", "has ", "have ", "had ")
    return q.startswith(prefixes)


def has_profile_signal(text: str) -> bool:
    return bool(PROFILE_SIGNAL_RE.search((text or "").strip()))


def extract_question_entities(question: str) -> list[str]:
    entities: list[str] = []
    seen: set[str] = set()
    for match in QUESTION_ENTITY_RE.finditer(question or ""):
        value = match.group(0).strip()
        if not value or value in QUESTION_ENTITY_STOPWORDS:
            continue
        key = value.lower()
        if key in seen:
            continue
        seen.add(key)
        entities.append(value)
    return entities


def infer_open_domain_focus_terms(question: str) -> set[str]:
    q = (question or "").strip().lower()
    focus: set[str] = set()
    groups: list[tuple[tuple[str, ...], tuple[str, ...]]] = [
        (("career", "job", "work", "profession", "field", "education", "study", "school", "college", "counsel"), ("career", "job", "work", "study", "education", "school", "college", "counsel", "therapy", "psychology", "mentor", "certification")),
        (("book", "bookshelf", "read", "author", "library"), ("book", "books", "bookshelf", "library", "read", "reading", "collect", "children", "classic")),
        (("music", "song", "artist", "band", "singer"), ("music", "song", "artist", "band", "singer", "classical", "modern")),
        (("park", "outdoors", "outdoor", "theme park", "camp", "hike", "nature"), ("park", "camp", "hike", "outdoors", "forest", "mountain", "nature", "trail")),
        (("political", "politics", "leaning"), ("rights", "equality", "justice", "acceptance", "community", "support", "activism", "lgbt", "transgender")),
        (("religious", "religion", "faith", "spiritual"), ("religious", "religion", "faith", "spiritual", "church", "pray", "god", "belief")),
        (("personality", "trait", "traits", "describe"), ("kind", "thoughtful", "authentic", "driven", "courage", "brave", "supportive", "creative", "caring")),
        (("move back", "home country", "country", "home"), ("home", "country", "move", "adopt", "kids", "children", "family")),
        (("pet", "pets", "dog", "cat"), ("pet", "pets", "dog", "cat", "kitty", "pup", "puppy")),
    ]
    for triggers, terms in groups:
        if any(trigger in q for trigger in triggers):
            focus.update(terms)
    if is_booleanish_query(question) or bool(LIKELY_QUERY_RE.search(q)):
        focus.update({"want", "goal", "dream", "plan", "hope", "love", "enjoy", "important", "believe", "support"})
    return focus


def infer_profile_facets_for_question(question: str) -> list[str]:
    q = (question or "").strip().lower()
    scores: dict[str, int] = {key: 0 for key in PROFILE_FACET_LABELS}
    if re.search(r"\b(job|career|profession|work|degree|study|education|field|school|college|role)\b", q):
        scores["identity_roles"] += 3
        scores["goals_plans"] += 1
    if re.search(r"\b(like|enjoy|prefer|favorite|favourite|hobby|interest|book|music|song|park|game|travel|vacation)\b", q):
        scores["preferences_interests"] += 3
    if re.search(r"\b(plan|goal|dream|want|pursue|future|soon|move|adopt|career option|consider)\b", q):
        scores["goals_plans"] += 3
    if re.search(r"\b(value|belief|religious|religion|faith|political|leaning|patriotic|support|justice|equality)\b", q):
        scores["values_beliefs"] += 3
    if re.search(r"\b(friend|partner|girlfriend|boyfriend|wife|husband|family|relationship|nickname|who is)\b", q):
        scores["relationships"] += 3
    if re.search(r"\b(personality|trait|traits|describe|kind of person|likely)\b", q):
        scores["traits_tendencies"] += 3
    ordered = sorted(scores.items(), key=lambda item: (-item[1], item[0]))
    facets = [key for key, score in ordered if score > 0]
    if not facets:
        facets = ["goals_plans", "preferences_interests", "values_beliefs"]
    return facets[:3]


def build_open_domain_profile_queries(question: str) -> list[str]:
    entities = extract_question_entities(question)
    if not entities:
        return []
    entity = entities[0]
    queries: list[str] = []
    for facet in infer_profile_facets_for_question(question):
        label = PROFILE_FACET_LABELS.get(facet, facet).lower()
        queries.append(f"{entity} profile {label}")
    return queries


def extract_question_alternatives(question: str) -> list[str]:
    q = " ".join((question or "").split()).strip().rstrip("?")
    if not q or " or " not in q.lower():
        return []
    quoted = re.findall(r'"([^"]+)"', q)
    if len(quoted) >= 2:
        return [quoted[0].strip(), quoted[1].strip()]
    matches = list(re.finditer(r"(?i)(?:a|an|the)?\s*([A-Za-z0-9 .'\-]{2,40}?)\s+or\s+(?:a|an|the)?\s*([A-Za-z0-9 .'\-]{2,40})", q))
    for match in reversed(matches):
        left = match.group(1).strip(" ,.;:")
        right = match.group(2).strip(" ,.;:")
        if left and right:
            return [left, right]
    parts = re.split(r"(?i)\bor\b", q, maxsplit=1)
    if len(parts) != 2:
        return []
    left = re.sub(r"(?i)^.*(?:be|is|are|was|were|to)\s+", "", parts[0]).strip(" ,.;:")
    right = re.sub(r"(?i)^(?:to|a|an|the)\s+", "", parts[1]).strip(" ,.;:")
    return [left, right] if left and right else []


def open_domain_evidence_score(question: str, line: str) -> float:
    stripped = strip_non_temporal_prefixes(line)
    lowered = stripped.lower()
    score = 0.30 * evidence_score(question, stripped)
    if lowered.startswith("profile for ") or "profile facet:" in lowered:
        score += 0.20
    if has_profile_signal(stripped):
        score += 0.28
    if re.search(r"(?i)\b(?:bad|badly|difficult|hard|setback|stress|stressed|afraid|allerg|pain|couldn't|could not|wouldn't|would not|won't|cannot|can't)\b", stripped):
        score += 0.12
    if re.search(r"(?i)\b(?:want|goal|dream|plan|hope|decid(?:e|ed)|choose|care about|believe|value|important)\b", stripped):
        score += 0.18
    if re.search(r"(?i)\b(?:love|enjoy|prefer|fan of|passionate|inspired)\b", stripped):
        score += 0.14

    focus_terms = infer_open_domain_focus_terms(question)
    focus_hits = sum(1 for term in focus_terms if term in lowered)
    score += min(0.32, focus_hits * 0.08)

    question_entities = extract_question_entities(question)
    if question_entities:
        primary = question_entities[0].lower()
        if primary in lowered:
            score += 0.18
        speaker_match = TURN_SPEAKER_RE.match(stripped)
        if speaker_match and primary in speaker_match.group(1).strip().lower():
            score += 0.20

    if re.search(r"(?i)\b(?:glad|amazing|awesome|sweet|proud of you|so happy for you)\b", stripped):
        score -= 0.18
    if ACK_LINE_RE.match(stripped) or LOW_SIGNAL_LINE_RE.match(stripped):
        score -= 0.30
    return score


def profile_source_score(entity: str, line: str) -> float:
    normalized = normalize_context_line(line)
    stripped = strip_non_temporal_prefixes(normalized)
    lowered = stripped.lower()
    entity_key = (entity or "").strip().lower()
    score = 0.0
    if not stripped:
        return score
    if has_profile_signal(stripped):
        score += 0.45
    if re.search(r"(?i)\b(?:want|goal|dream|plan|hope|decid(?:e|ed)|choose|care about|believe|value|important)\b", stripped):
        score += 0.25
    if re.search(r"(?i)\b(?:love|enjoy|prefer|fan of|passionate|inspired|collect|volunteer|study|work)\b", stripped):
        score += 0.20
    if re.search(r"(?i)\b(?:bad|badly|difficult|hard|setback|stress|afraid|allerg|pain|couldn't|wouldn't|won't)\b", stripped):
        score += 0.18
    speaker_match = TURN_SPEAKER_RE.match(normalized)
    if speaker_match and speaker_match.group(1).strip().lower() == entity_key:
        score += 0.30
    elif entity_key and entity_key in lowered:
        score += 0.18
    if ACK_LINE_RE.match(stripped) or LOW_SIGNAL_LINE_RE.match(stripped):
        score -= 0.35
    if len(normalize_tokens(stripped)) <= 4:
        score -= 0.12
    return score


def build_profile_source_index(fixture: list[dict[str, Any]]) -> dict[str, dict[str, list[str]]]:
    by_tenant: dict[str, dict[str, list[str]]] = {}
    for row in fixture:
        tenant_id = str(row.get("tenant_id", "")).strip()
        if not tenant_id:
            continue
        content = str(row.get("content", ""))
        turn = parse_annotated_turn(content)
        normalized = normalize_context_line(content)
        if not normalized:
            continue
        tenant_profiles = by_tenant.setdefault(tenant_id, {})
        speaker_names = [turn.get("speaker_a", ""), turn.get("speaker_b", ""), turn.get("speaker", "")]
        for name in speaker_names:
            entity = " ".join(str(name or "").split()).strip()
            if not entity:
                continue
            tenant_profiles.setdefault(entity, []).append(normalized)
    return by_tenant


def select_profile_source_lines(entity: str, lines: list[str], max_lines: int) -> list[str]:
    scored: list[tuple[float, int, str]] = []
    seen: set[str] = set()
    for idx, line in enumerate(lines):
        text = " ".join((line or "").split()).strip()
        key = text.lower()
        if not text or key in seen:
            continue
        seen.add(key)
        score = profile_source_score(entity, text) - (0.002 * idx)
        scored.append((score, idx, text))
    if not scored:
        return []
    scored.sort(key=lambda item: (-item[0], item[1]))
    return [text for _, _, text in scored[:max_lines]]


def select_answer_contexts(question: str, returned_contents: list[str], max_contexts: int, open_domain: bool = False) -> list[str]:
    if not returned_contents:
        return []
    temporal, _, _ = classify_query(question)
    reasoning = is_inference_query(question)
    scored: list[tuple[float, int, str]] = []
    for idx, content in enumerate(returned_contents):
        norm = normalize_context_line(content)
        if not norm:
            continue
        best = -10**9
        seen_signal = False
        for sent in split_sentences(norm):
            s = sent.strip()
            if not s or is_low_signal_sentence(question, s, temporal):
                continue
            seen_signal = True
            score = open_domain_evidence_score(question, s) if open_domain else evidence_score(question, s)
            if (reasoning or open_domain) and has_profile_signal(s):
                score += 0.16
            if temporal and has_temporal_signal(s):
                score += 0.12
            best = max(best, score)
        if not seen_signal:
            continue
        context_score = best - (0.01 * idx)
        if open_domain:
            context_score += 0.35 * open_domain_evidence_score(question, norm)
        if (reasoning or open_domain) and has_profile_signal(norm):
            context_score += 0.10
        scored.append((context_score, idx, content))
    if not scored:
        return returned_contents[:max_contexts]
    scored.sort(key=lambda x: (-x[0], x[1]))
    return [content for _, _, content in scored[:max_contexts]]


def build_open_domain_candidates(question: str, candidate_answers: list[str]) -> list[str]:
    out: list[str] = []
    seen: set[str] = set()

    def add(value: str) -> None:
        v = (value or "").strip()
        key = v.lower()
        if not v or key in seen:
            return
        seen.add(key)
        out.append(v)

    alternatives = extract_question_alternatives(question)
    if alternatives:
        for value in alternatives:
            add(value)
    elif is_booleanish_query(question):
        for value in ("Likely yes", "Likely no", "Yes", "No", "Unknown"):
            add(value)

    for value in candidate_answers:
        add(value)
        if len(out) >= 8:
            break
    return out[:8]


def parse_line_number_selection(raw: str, max_index: int, max_lines: int) -> list[int]:
    text = (raw or "").strip()
    if not text:
        return []

    def clamp_indices(values: list[int]) -> list[int]:
        out: list[int] = []
        seen: set[int] = set()
        for value in values:
            if value < 1 or value > max_index or value in seen:
                continue
            seen.add(value)
            out.append(value)
            if len(out) >= max_lines:
                break
        return out

    try:
        payload = json.loads(text)
        if isinstance(payload, dict) and isinstance(payload.get("line_numbers"), list):
            return clamp_indices([int(v) for v in payload["line_numbers"] if isinstance(v, int)])
        if isinstance(payload, list):
            return clamp_indices([int(v) for v in payload if isinstance(v, int)])
    except Exception:
        pass

    matches = [int(value) for value in re.findall(r"\b\d+\b", text)]
    return clamp_indices(matches)


def parse_open_domain_candidate_response(raw: str, max_candidates: int) -> list[str]:
    text = (raw or "").strip()
    if not text:
        return []
    try:
        payload = json.loads(text)
    except Exception:
        return []
    if not isinstance(payload, dict):
        return []
    values = payload.get("candidates", [])
    if not isinstance(values, list):
        return []
    out: list[str] = []
    seen: set[str] = set()
    for value in values:
        item = " ".join(str(value or "").split()).strip()
        key = item.lower()
        if not item or key in seen:
            continue
        seen.add(key)
        out.append(item)
        if len(out) >= max_candidates:
            break
    return out


def is_aggregation_query(query: str) -> bool:
    q = (query or "").strip().lower()
    if not q:
        return False
    return bool(AGGREGATION_QUERY_RE.search(q))


def build_retrieval_routes(
    query: str,
    structured_enabled: bool,
    category: Any = "",
    temporal_route_raw_turn: bool = True,
    open_domain_profile_route: bool = False,
    profile_layer_enabled: bool = False,
) -> list[tuple[str, list[str] | None, float]]:
    cat = str(category).strip()
    base_kinds: list[str] | None = None
    if profile_layer_enabled and cat == "1":
        # Keep profile summaries from dominating multi-hop retrieval.
        base_kinds = ["raw_turn", "observation", "event"]
    routes: list[tuple[str, list[str] | None, float]] = [("vector", base_kinds, 1.0)]
    if cat == "1" and structured_enabled and is_aggregation_query(query):
        # Entity route hits a separate server code path (entity_facts table)
        # that aggregates across relational facts — genuinely different from
        # the vector/BM25 path and worth a second call.
        routes.append(("entity", None, 1.25))
    elif cat == "2" and temporal_route_raw_turn:
        # Temporal questions benefit from direct event/raw-turn evidence.
        routes.append(("vector", ["raw_turn", "event"], 1.12))
    elif open_domain_profile_route and cat == "3":
        # Open-domain questions are mostly profile-style. Querying the cleaner
        # canonical kinds separately lets summary/observation memories surface
        # without being buried by conversational raw turns.
        routes.append(("vector", ["summary"], 1.18))
        if not is_booleanish_query(query):
            # Non-binary profile questions still need concrete raw turns and
            # events so specific labels are not lost in profile abstraction.
            routes.append(("vector", ["raw_turn", "event"], 1.10))
    elif cat == "4":
        # Single-hop attribute questions are hurt by observation chatter.
        # A second pass over answer-bearing kinds keeps raw turns in play for
        # rich attributes while shrinking the candidate pool away from noise.
        routes.append(("vector", ["raw_turn", "event", "summary"], 1.10))

    return routes


def extract_anchor_from_top_results(query: str, top_results: list[str], top_k: int = 3) -> str:
    """M3: Extract key entity or phrase from top results to seed second-pass query."""
    if not top_results:
        return ""

    query_tokens = set(normalize_tokens(query))

    # Try to extract speaker/entity prefixes from normalized lines.
    for result in top_results[:top_k]:
        line = normalize_context_line(result)
        m = re.match(r"^\s*([A-Za-z][A-Za-z0-9 .'\-]{0,48})(?:\s*\([^)]+\))?:", line)
        if not m:
            continue
        prefix_tokens = [
            t
            for t in normalize_tokens(m.group(1))
            if t not in STOPWORDS and t not in query_tokens and len(t) >= 3
        ]
        if 1 <= len(prefix_tokens) <= 3:
            return " ".join(prefix_tokens)

    # Fallback: pick frequent novel content token from pass-1 evidence.
    all_tokens: dict[str, int] = {}
    for result in top_results[:top_k]:
        for token in normalize_tokens(normalize_context_line(result)):
            if token in STOPWORDS or token in query_tokens:
                continue
            if len(token) < 3 or token.isdigit():
                continue
            all_tokens[token] = all_tokens.get(token, 0) + 1

    if all_tokens:
        top_token = max(all_tokens.keys(), key=lambda t: (all_tokens[t], len(t), t))
        return top_token

    return ""


def build_two_pass_query(original_query: str, anchor: str) -> str:
    """M3: Build second-pass query combining original query with extracted anchor.
    
    Example:
      original: "When did Caroline go to the LGBTQ support group?"
      anchor: "Caroline"
      result: "Caroline LGBTQ support group"
    """
    if not anchor:
        return original_query
    
    # Remove the anchor from the original query to avoid duplication
    stripped = original_query.lower().replace(anchor.lower(), "").strip()
    
    # Rebuild: anchor + remaining significant words
    if stripped:
        return f"{anchor} {stripped}"
    return anchor


def evidence_score(question: str, line: str) -> float:
    q_tokens = [t for t in normalize_tokens(question) if t not in STOPWORDS]
    l_tokens = normalize_tokens(line)
    if not l_tokens:
        return 0.0

    q_set = set(q_tokens)
    l_set = set(l_tokens)
    overlap = len(q_set & l_set)
    score = overlap / max(1, len(q_set))

    temporal, person, _ = classify_query(question)
    ll = line.lower()
    if temporal and re.search(r"\b\d{4}\b|am|pm|jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec", ll):
        score += 0.2
    if temporal and has_temporal_signal(line):
        score += 0.15
    if temporal and not has_temporal_signal(line):
        score -= 0.20
    if not temporal:
        stripped = strip_non_temporal_prefixes(line)
        if is_question_like_text(stripped):
            score -= 0.45
        focus_tokens = {
            t for t in q_tokens
            if len(t) > 3 and t not in {"what", "which", "where", "when", "why", "does", "did", "from"}
        }
        focus_overlap = len(focus_tokens & l_set)
        score += focus_overlap * 0.08
    if person and ":" in line:
        score += 0.08
    stripped_line = strip_non_temporal_prefixes(line)
    if ACK_LINE_RE.match(stripped_line) or LOW_SIGNAL_LINE_RE.match(stripped_line):
        score -= 0.35
    if len(l_tokens) <= 18:
        score += 0.05
    return score


def is_low_signal_sentence(question: str, sentence: str, temporal: bool) -> bool:
    stripped = strip_non_temporal_prefixes(sentence)
    if not stripped:
        return True
    if ACK_LINE_RE.match(stripped) or LOW_SIGNAL_LINE_RE.match(stripped):
        return True
    toks = normalize_tokens(stripped)
    if not toks:
        return True
    if len(toks) <= 3 and not has_temporal_signal(stripped):
        return True
    if sentence.strip().endswith("!") and len(toks) <= 5 and not has_temporal_signal(stripped):
        return True
    if not temporal:
        q_tokens = {t for t in normalize_tokens(question) if t not in STOPWORDS}
        overlap = len(q_tokens & set(toks))
        if overlap == 0 and len(toks) <= 5:
            return True
    return False


def token_jaccard_similarity(a: str, b: str) -> float:
    a_tokens = {t for t in normalize_tokens(a) if t not in STOPWORDS}
    b_tokens = {t for t in normalize_tokens(b) if t not in STOPWORDS}
    if not a_tokens or not b_tokens:
        return 0.0
    overlap = len(a_tokens & b_tokens)
    union = len(a_tokens | b_tokens)
    if union <= 0:
        return 0.0
    return overlap / union


def select_evidence_contexts(question: str, returned_contents: list[str], max_lines: int, open_domain: bool = False) -> list[str]:
    candidates: list[tuple[float, str]] = []
    seen: set[str] = set()
    temporal, _, _ = classify_query(question)
    for context_rank, content in enumerate(returned_contents):
        # Prefer earlier retrieved contexts as tie-break signal.
        rank_bonus = 0.08 / (1 + context_rank)
        norm = normalize_context_line(content)
        for sent in split_sentences(norm):
            s = sent.strip()
            if not s:
                continue
            if is_low_signal_sentence(question, s, temporal):
                continue
            key = s.lower()
            if key in seen:
                continue
            seen.add(key)
            score = (open_domain_evidence_score(question, s) if open_domain else evidence_score(question, s)) + rank_bonus
            if temporal and has_temporal_signal(s):
                score += 0.08
            candidates.append((score, s))

    candidates.sort(key=lambda x: (-x[0], len(x[1]), x[1]))
    out: list[str] = []
    # MMR-like greedy rerank for relevance + diversity.
    for _ in range(max(1, max_lines)):
        if not candidates:
            break
        best_idx = -1
        best_score = -10**9
        for idx, (base_score, line) in enumerate(candidates):
            redundancy = 0.0
            if out:
                redundancy = max(token_jaccard_similarity(line, chosen) for chosen in out)
            adjusted = (0.88 * base_score) - (0.12 * redundancy)
            if adjusted > best_score:
                best_score = adjusted
                best_idx = idx
        if best_idx < 0:
            break
        out.append(candidates[best_idx][1])
        candidates.pop(best_idx)
        if len(out) >= max_lines:
            break
    if out:
        return out

    fallback = [normalize_context_line(c) for c in returned_contents if normalize_context_line(c)]
    return fallback[:max_lines]


def is_unknown_answer(text: str) -> bool:
    cleaned = (text or "").strip().lower()
    return cleaned in {"", "unknown", "n/a", "na"}


def normalize_month_token(token: str) -> str:
    key = token.strip().lower()[:3]
    mapping = {
        "jan": "January",
        "feb": "February",
        "mar": "March",
        "apr": "April",
        "may": "May",
        "jun": "June",
        "jul": "July",
        "aug": "August",
        "sep": "September",
        "oct": "October",
        "nov": "November",
        "dec": "December",
    }
    return mapping.get(key, token.strip().title())


def normalize_date_phrase(phrase: str) -> str:
    value = (phrase or "").strip(" \t\r\n.,;:")
    if not value:
        return ""

    md = re.match(rf"(?i)^({MONTH_NAME_RE})\s+(\d{{1,2}}),?\s*(\d{{4}})$", value)
    if md:
        month = normalize_month_token(md.group(1))
        day = str(int(md.group(2)))
        year = md.group(3)
        return f"{day} {month} {year}"

    dm = re.match(rf"(?i)^(\d{{1,2}})\s+({MONTH_NAME_RE}),?\s*(\d{{4}})$", value)
    if dm:
        day = str(int(dm.group(1)))
        month = normalize_month_token(dm.group(2))
        year = dm.group(3)
        return f"{day} {month} {year}"

    my = re.match(rf"(?i)^({MONTH_NAME_RE})\s+(\d{{4}})$", value)
    if my:
        month = normalize_month_token(my.group(1))
        year = my.group(2)
        return f"{month} {year}"

    return value


def extract_temporal_phrase(text: str, question: str) -> str:
    source = (text or "").strip()
    if not source:
        return ""
    q = (question or "").lower()

    candidates: list[tuple[float, str]] = []

    def collect(pattern: re.Pattern[str], base_score: float) -> None:
        for m in pattern.finditer(source):
            phrase = normalize_date_phrase(m.group(0))
            if not phrase:
                continue
            score = base_score + (len(normalize_tokens(phrase)) * 0.01)
            candidates.append((score, phrase))

    if "how long" in q:
        collect(DURATION_RE, 1.35)
    collect(RELATIVE_DATE_RE, 1.30)
    collect(FULL_DATE_RE, 1.20)
    collect(MONTH_DAY_YEAR_RE, 1.15)
    collect(MONTH_YEAR_RE, 0.95)
    collect(YEAR_RE, 0.90)
    if "how long" not in q:
        collect(DURATION_RE, 0.80)

    if not candidates:
        return ""

    candidates.sort(key=lambda x: (-x[0], len(x[1]), x[1].lower()))
    return candidates[0][1]


def compact_extractive_phrase(text: str) -> str:
    value = strip_non_temporal_prefixes(text)
    if not value:
        return "Unknown"

    value = value.strip(" \"'")
    value = re.split(r"[.;]", value, maxsplit=1)[0].strip()
    value = re.split(r"\b(?:because|however|although)\b", value, maxsplit=1, flags=re.IGNORECASE)[0].strip()
    words = value.split()
    if len(words) > 14:
        value = " ".join(words[:14]).strip()
    return value if value else "Unknown"


def is_question_like_text(text: str) -> bool:
    value = (text or "").strip()
    if not value:
        return False
    return value.endswith("?") or bool(QUESTION_LIKE_RE.match(value))


def strip_non_temporal_prefixes(text: str) -> str:
    value = SPEAKER_PREFIX_RE.sub("", (text or "").strip()).strip()
    value = LEADING_DATE_PREFIX_RE.sub("", value).strip()
    value = SAID_THAT_PREFIX_RE.sub("", value).strip()
    return value


def extract_non_temporal_phrase(question: str, text: str) -> str:
    value = strip_non_temporal_prefixes(text)
    if not value:
        return "Unknown"
    if is_question_like_text(value):
        return "Unknown"
    if (question or "").strip().lower().startswith("why "):
        m = WHY_CLAUSE_RE.search(value)
        if m:
            reason = m.group(1).strip(" ,.;:")
            if reason:
                return compact_extractive_phrase(reason)
    return compact_extractive_phrase(value)


def collect_extractive_candidates(
    question: str,
    evidence_lines: list[str],
    max_candidates: int = 8,
    open_domain: bool = False,
) -> list[tuple[float, str, str]]:
    temporal, _, _ = classify_query(question)
    reasoning = is_inference_query(question)
    q_tokens = {t for t in normalize_tokens(question) if t not in STOPWORDS}

    candidates: list[tuple[float, str, str]] = []
    for line in evidence_lines:
        for sentence in split_sentences(line):
            s = sentence.strip()
            if not s:
                continue
            if is_low_signal_sentence(question, s, temporal):
                continue
            score = evidence_score(question, s)
            s_tokens = set(normalize_tokens(s))
            overlap = len(q_tokens & s_tokens)
            if not temporal and overlap == 0 and not reasoning and not open_domain:
                continue
            if not temporal and is_question_like_text(strip_non_temporal_prefixes(s)):
                continue
            if ACK_LINE_RE.match(SPEAKER_PREFIX_RE.sub("", s)):
                score -= 0.25
            if temporal:
                temporal_phrase = extract_temporal_phrase(s, question)
                if temporal_phrase:
                    answer = temporal_phrase
                    score += 0.25
                else:
                    answer = compact_extractive_phrase(s)
            else:
                answer = extract_non_temporal_phrase(question, s)

            if is_unknown_answer(answer):
                continue
            if open_domain and has_profile_signal(s):
                score += 0.10
            if reasoning and has_profile_signal(s):
                score += 0.14
            if (question or "").strip().lower().startswith("why ") and re.search(r"(?i)\b(?:because|that's why|so that)\b", s):
                score += 0.18
            a_tokens = normalize_tokens(answer)
            novel = [t for t in a_tokens if t not in q_tokens]
            if not novel:
                score -= 0.15
            if len(a_tokens) <= 8:
                score += 0.05
            if len(a_tokens) > 18:
                score -= 0.10
            candidates.append((score, answer, s))

    if not candidates:
        return []

    # Dedupe by answer string while keeping highest-scoring sentence support.
    deduped: list[tuple[float, str, str]] = []
    seen_answers: set[str] = set()
    candidates.sort(key=lambda x: (-x[0], len(normalize_tokens(x[1])), x[1].lower()))
    for item in candidates:
        key = item[1].strip().lower()
        if not key or key in seen_answers:
            continue
        seen_answers.add(key)
        deduped.append(item)
        if len(deduped) >= max_candidates:
            break
    return deduped


def extractive_answer(question: str, evidence_lines: list[str], open_domain: bool = False) -> tuple[str, float, str]:
    candidates = collect_extractive_candidates(question, evidence_lines, max_candidates=8, open_domain=open_domain)
    if not candidates:
        return "Unknown", 0.0, ""
    best_score, best_answer, best_sentence = candidates[0]
    confidence = max(0.0, min(1.0, best_score))
    return best_answer, confidence, best_sentence


def repair_answer_spacing(text: str) -> str:
    value = (text or "").strip()
    if not value:
        return ""
    value = re.sub(r"(?<=[A-Za-z])(?=\d)|(?<=\d)(?=[A-Za-z])", " ", value)
    value = re.sub(r"(?<=[a-z])(?=[A-Z])", " ", value)
    value = re.sub(r"\s+", " ", value).strip()
    return value


def compact_answer_key(text: str) -> str:
    return re.sub(r"[^a-z0-9]+", "", (text or "").lower())


def canonicalize_boolean_answer(text: str) -> str:
    compact = compact_answer_key(text)
    if not compact:
        return "Unknown"
    mapping = {
        "yes": "Yes",
        "no": "No",
        "unknown": "Unknown",
        "likelyyes": "Likely yes",
        "likelyno": "Likely no",
        "probablyyes": "Likely yes",
        "probablyno": "Likely no",
    }
    for key, value in mapping.items():
        if compact == key:
            return value
    for key, value in mapping.items():
        if compact.startswith(key) or compact.endswith(key):
            return value
    return repair_answer_spacing(text)


def snap_generated_answer_to_candidates(
    question: str,
    generated_answer: str,
    candidate_answers: list[str],
    extractive_answer_value: str,
    extractive_confidence: float,
    open_domain: bool,
) -> str:
    answer = repair_answer_spacing(generated_answer)
    if is_unknown_answer(answer):
        return "Unknown"
    if open_domain and is_booleanish_query(question):
        return canonicalize_boolean_answer(answer)

    compact_generated = compact_answer_key(answer)
    if not compact_generated:
        return answer

    pool: list[str] = []
    seen: set[str] = set()
    for value in [extractive_answer_value, *candidate_answers]:
        v = repair_answer_spacing(value)
        key = compact_answer_key(v)
        if not v or not key or key in seen or is_unknown_answer(v):
            continue
        seen.add(key)
        pool.append(v)

    if open_domain and is_booleanish_query(question):
        for value in ("Likely yes", "Likely no", "Yes", "No", "Unknown"):
            key = compact_answer_key(value)
            if key not in seen:
                seen.add(key)
                pool.append(value)

    for candidate in pool:
        if compact_generated == compact_answer_key(candidate):
            return candidate

    if extractive_confidence >= 0.45:
        for candidate in pool:
            compact_candidate = compact_answer_key(candidate)
            if compact_candidate and compact_candidate in compact_generated:
                candidate_tokens = set(normalize_tokens(candidate))
                generated_tokens = set(normalize_tokens(answer))
                if candidate_tokens and candidate_tokens.issubset(generated_tokens):
                    return candidate

    return answer


def open_domain_extract_is_safe_fallback(question: str, answer: str, confidence: float) -> bool:
    value = repair_answer_spacing(answer)
    if is_unknown_answer(value) or confidence < 0.42:
        return False
    if is_booleanish_query(question):
        compact = compact_answer_key(value)
        if compact in {"yes", "no", "likelyyes", "likelyno", "unknown"}:
            return True
        return False
    stripped = strip_non_temporal_prefixes(value)
    if re.search(r"(?i)\b(?:i|i'm|i've|i want|i hope|i dream|that's why|because)\b", stripped):
        return False
    return len(normalize_tokens(stripped)) <= 8


def normalize_open_domain_label_answer(question: str, answer: str) -> str:
    q = (question or "").strip().lower()
    a = repair_answer_spacing(answer)
    lowered = a.lower()
    if "political" in q or "leaning" in q:
        if any(term in lowered for term in ("left-leaning", "left leaning", "progressive")):
            return "Liberal"
        if any(term in lowered for term in ("right-leaning", "right leaning")):
            return "Conservative"
        if "moderate" in lowered or "centrist" in lowered:
            return "Moderate"
    if any(term in q for term in ("religious", "religion", "faith", "spiritual")):
        if "somewhat" in lowered or "not extremely" in lowered or "moderately" in lowered:
            return "Somewhat religious"
        if "not religious" in lowered or "nonreligious" in lowered or "secular" in lowered:
            return "Not religious"
        if "religious" in lowered or "faith" in lowered or "spiritual" in lowered:
            return "Religious"
    if "financial status" in q:
        if "middle" in lowered and "class" in lowered:
            return "Middle-class"
        if "wealth" in lowered or "affluent" in lowered or "rich" in lowered:
            return "Wealthy"
        if "low" in lowered and "income" in lowered:
            return "Low-income"
    return a


def build_support_clause(question: str, evidence_lines: list[str], supporting_lines: list[int]) -> str:
    candidate_indexes = [(line_no - 1) for line_no in supporting_lines]
    if not candidate_indexes:
        candidate_indexes = list(range(min(len(evidence_lines), 3)))
    for idx in candidate_indexes:
        if idx < 0 or idx >= len(evidence_lines):
            continue
        line = evidence_lines[idx]
        stripped = strip_non_temporal_prefixes(line)
        if not stripped:
            continue
        reason = extract_non_temporal_phrase(question, stripped)
        reason = repair_answer_spacing(reason)
        if is_unknown_answer(reason):
            continue
        reason = re.sub(r"(?i)^(?:yes|no|likely yes|likely no)\b[;,:]?\s*", "", reason).strip()
        if not reason:
            continue
        tokens = normalize_tokens(reason)
        if len(tokens) > 12:
            reason = " ".join(reason.split()[:12]).strip()
        return reason
    return ""


def clean_generated_answer(raw: str) -> str:
    text = (raw or "").replace("\r", "\n")
    text = THINK_BLOCK_RE.sub(" ", text)
    text = THINK_TAG_RE.sub(" ", text)
    text = re.sub(r"(?is)```.*?```", " ", text)
    text = re.sub(r"\n{3,}", "\n\n", text).strip()
    if not text:
        return "Unknown"

    lines = [line.strip() for line in text.splitlines() if line.strip()]
    filtered: list[str] = []
    for line in lines:
        low = line.lower()
        if low in {"answer:", "final answer:"}:
            continue
        if low.startswith("reasoning:") or low.startswith("thought:"):
            continue
        filtered.append(line)

    candidate = filtered[-1] if filtered else lines[-1]
    candidate = ANSWER_PREFIX_RE.sub("", candidate).strip()
    candidate = re.sub(r"^\s*\d+\s*[\.\)]\s*", "", candidate)
    candidate = candidate.strip(" \"'")
    candidate = repair_answer_spacing(candidate)
    if not candidate:
        return "Unknown"
    if is_unknown_answer(candidate):
        return "Unknown"
    return candidate


def normalize_answer_for_scoring(answer: str, question: str) -> str:
    value = repair_answer_spacing(answer)
    if not value:
        return "Unknown"
    temporal, person, _ = classify_query(question)
    if temporal:
        temporal_phrase = extract_temporal_phrase(value, question)
        if temporal_phrase:
            value = normalize_date_phrase(temporal_phrase)
    value = strip_non_temporal_prefixes(value)
    if person:
        # Collapse noisy person answers to a compact phrase for fair token scoring.
        value = re.split(r"[.;]", value, maxsplit=1)[0].strip()
        words = value.split()
        if len(words) > 6:
            value = " ".join(words[:6]).strip()
    value = re.sub(r"\s+", " ", value).strip(" \"'")
    if is_unknown_answer(value):
        return "Unknown"
    return value if value else "Unknown"


def dcg_binary(rels: list[int]) -> float:
    score = 0.0
    for i, rel in enumerate(rels):
        if rel:
            score += 1.0 / (math.log(i + 2, 2))
    return score


def compute_rank_metrics(returned_ids: list[str], expected_ids: set[str], k: int) -> tuple[int, int, int, float, float, float]:
    top = returned_ids[:k]
    rels = [1 if mid in expected_ids else 0 for mid in top]
    hits = sum(rels)
    relevant = len(expected_ids)
    top1 = 1 if top and top[0] in expected_ids else 0
    hit_at_k = 1 if hits > 0 else 0
    recall = hits / relevant if relevant > 0 else 0.0
    first = next((idx for idx, rel in enumerate(rels) if rel), None)
    mrr = 1.0 / (first + 1) if first is not None else 0.0
    dcg = dcg_binary(rels)
    ideal = [1] * min(relevant, k)
    idcg = dcg_binary(ideal)
    ndcg = dcg / idcg if idcg > 0 else 0.0
    return top1, hit_at_k, hits, recall, mrr, ndcg


def compute_group_rank_metrics(returned_ids: list[str], expected_groups: list[set[str]], k: int) -> tuple[int, int, int, float, float, float]:
    top = returned_ids[:k]
    if not expected_groups:
        return 0, 0, 0, 0.0, 0.0, 0.0

    matched: set[int] = set()
    rels: list[int] = []
    for mid in top:
        matched_now = False
        for group_idx, group in enumerate(expected_groups):
            if group_idx in matched:
                continue
            if mid in group:
                matched.add(group_idx)
                matched_now = True
        rels.append(1 if matched_now else 0)

    hits = len(matched)
    relevant = len(expected_groups)
    top1 = 1 if rels and rels[0] == 1 else 0
    hit_at_k = 1 if hits > 0 else 0
    recall = hits / relevant if relevant > 0 else 0.0
    first = next((idx for idx, rel in enumerate(rels) if rel), None)
    mrr = 1.0 / (first + 1) if first is not None else 0.0
    dcg = dcg_binary(rels)
    ideal = [1] * min(relevant, k)
    idcg = dcg_binary(ideal)
    ndcg = dcg / idcg if idcg > 0 else 0.0
    return top1, hit_at_k, hits, recall, mrr, ndcg


from prompts import (  # noqa: E402
    build_open_domain_candidate_prompt,
    build_generation_prompt,
    build_open_domain_evidence_selection_prompt,
    build_open_domain_hyde_prompt,
    build_open_domain_query_rewrite_prompt,
    build_open_domain_verification_prompt,
    build_profile_facets_prompt,
    build_open_domain_resolution_prompt,
    build_profile_summary_prompt,
)


def ollama_generate(
    base_url: str,
    model: str,
    prompt: str,
    temperature: float = 0.0,
    timeout_s: float = 45.0,
    max_tokens: int = 96,
    clean_output: bool = True,
) -> tuple[bool, str]:
    payload = {
        "model": model,
        "prompt": prompt,
        "stream": False,
        "options": {"temperature": temperature, "num_predict": max_tokens},
    }
    code, body = json_request(base_url.rstrip("/") + "/api/generate", payload, timeout_s=timeout_s)
    if code != 200 or not isinstance(body, dict):
        return False, "Unknown"
    raw = str(body.get("response", "")).strip()
    if not raw:
        return False, "Unknown"
    if not clean_output:
        return True, raw
    text = clean_generated_answer(raw)
    if not text:
        return False, "Unknown"
    return True, text


def openrouter_generate(
    base_url: str,
    api_key: str,
    model: str,
    prompt: str,
    temperature: float = 0.0,
    timeout_s: float = 45.0,
    max_tokens: int = 96,
    clean_output: bool = True,
) -> tuple[bool, str]:
    key = (api_key or "").strip()
    if not key:
        return False, "Unknown"
    model_name = (model or "").strip()
    if not model_name:
        return False, "Unknown"

    payload = {
        "model": model_name,
        "messages": [{"role": "user", "content": prompt}],
        "temperature": temperature,
        "max_tokens": max_tokens,
    }
    if "gpt-oss" in model_name.lower():
        # GPT-OSS models require reasoning enabled; keep it low so we still get final content.
        payload["reasoning"] = {"effort": "low"}
    data = json.dumps(payload).encode("utf-8")
    url = base_url.rstrip("/") + "/chat/completions"
    body: dict[str, Any] | None = None
    retries = 3
    for attempt in range(retries):
        req = urllib.request.Request(
            url,
            data=data,
            headers={
                "Content-Type": "application/json",
                "Authorization": f"Bearer {key}",
                "Accept": "application/json",
            },
            method="POST",
        )
        try:
            with OPENROUTER_REQUEST_SEMAPHORE:
                with urllib.request.urlopen(req, timeout=timeout_s) as resp:
                    body_raw = resp.read().decode("utf-8")
                    body = json.loads(body_raw) if body_raw else {}
                    break
        except urllib.error.HTTPError as e:
            transient = e.code in {408, 409, 429, 500, 502, 503, 504}
            if transient and attempt + 1 < retries:
                time.sleep(0.5 * (2 ** attempt))
                continue
            return False, "Unknown"
        except Exception:
            if attempt + 1 < retries:
                time.sleep(0.5 * (2 ** attempt))
                continue
            return False, "Unknown"

    if not isinstance(body, dict):
        return False, "Unknown"
    choices = body.get("choices", [])
    if not isinstance(choices, list) or not choices:
        return False, "Unknown"
    first = choices[0] if isinstance(choices[0], dict) else {}
    message = first.get("message", {}) if isinstance(first, dict) else {}
    content = message.get("content", "") if isinstance(message, dict) else ""
    if isinstance(content, list):
        parts: list[str] = []
        for item in content:
            if isinstance(item, dict):
                text_part = item.get("text", "")
                if isinstance(text_part, str) and text_part.strip():
                    parts.append(text_part.strip())
        content = "\n".join(parts)
    raw = str(content or "").strip()
    if not raw:
        alt_text = first.get("text", "") if isinstance(first, dict) else ""
        if isinstance(alt_text, str) and alt_text.strip():
            raw = alt_text.strip()
    if not raw:
        return False, "Unknown"
    if not clean_output:
        return True, raw
    text = clean_generated_answer(raw)
    if not text:
        return False, "Unknown"
    return True, text


@dataclass
class QAAcc:
    count: int = 0
    f1_sum: float = 0.0
    bleu_sum: float = 0.0

    def add(self, f1: float, bleu: float) -> None:
        self.count += 1
        self.f1_sum += f1
        self.bleu_sum += bleu

    def mean_f1(self) -> float:
        return self.f1_sum / self.count if self.count else 0.0

    def mean_bleu(self) -> float:
        return self.bleu_sum / self.count if self.count else 0.0


@dataclass
class EvalAcc:
    queries: int = 0
    query_failures: int = 0
    generation_failures: int = 0
    top1_hit: int = 0
    hit_at_k: int = 0
    total_hits: int = 0
    total_relevant: int = 0
    mrr_sum: float = 0.0
    recall_sum: float = 0.0
    ndcg_sum: float = 0.0
    id_top1_hit: int = 0
    id_hit_at_k: int = 0
    id_total_hits: int = 0
    id_total_relevant: int = 0
    id_mrr_sum: float = 0.0
    id_recall_sum: float = 0.0
    id_ndcg_sum: float = 0.0

    # Extractive proxies
    f1_top1_sum: float = 0.0
    bleu1_top1_sum: float = 0.0
    f1_concat3_sum: float = 0.0
    bleu1_concat3_sum: float = 0.0
    f1_oracle_sentence_sum: float = 0.0
    bleu1_oracle_sentence_sum: float = 0.0

    # Generated answer metrics
    f1_generated_sum: float = 0.0
    bleu1_generated_sum: float = 0.0
    em_generated_sum: float = 0.0
    em_extractive_sum: float = 0.0
    f1_generated_no_stopwords_sum: float = 0.0

    by_category_generated: dict[str, QAAcc] = field(default_factory=dict)
    answer_path_counts: Counter[str] = field(default_factory=Counter)
    top1_text_counts: Counter[str] = field(default_factory=Counter)
    expected_groups_total: int = 0
    expected_group_items_total: int = 0

    def avg(self, value: float) -> float:
        return value / self.queries if self.queries else 0.0

    def add_category_generated(self, category: Any, f1: float, bleu: float) -> None:
        key = str(category).strip() or "unknown"
        if key not in self.by_category_generated:
            self.by_category_generated[key] = QAAcc()
        self.by_category_generated[key].add(f1, bleu)

    def add_answer_path(self, path: str) -> None:
        key = (path or "").strip() or "unknown"
        self.answer_path_counts[key] += 1


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--fixture", required=True)
    p.add_argument("--eval-set", required=True)
    p.add_argument("--embedding-provider", required=True, choices=["ollama", "lexical", "mock", "onnx", "openrouter"])
    p.add_argument("--embedding-model", default="all-minilm")
    p.add_argument("--ollama-url", default="http://127.0.0.1:11434")
    p.add_argument("--importance-scorer", choices=["heuristic", "ollama", "openrouter"], default="heuristic")
    p.add_argument("--importance-ollama-url", default="http://127.0.0.1:11434")
    p.add_argument("--importance-ollama-model", default="deepseek-r1:7b")
    p.add_argument("--importance-ollama-timeout-ms", type=int, default=20000)
    p.add_argument("--openrouter-base-url", default="https://openrouter.ai/api/v1")
    p.add_argument("--openrouter-api-key", default=os.getenv("OPENROUTER_API_KEY", ""))
    p.add_argument("--openrouter-embedding-model", default="sentence-transformers/all-minilm-l12-v2:nitro")
    p.add_argument("--openrouter-scoring-model", default="openai/gpt-oss-120b:nitro")
    p.add_argument("--openrouter-timeout-ms", type=int, default=10000)
    p.add_argument("--vector-backend", choices=["sqlite", "qdrant"], default="sqlite")
    p.add_argument("--qdrant-url", default="http://127.0.0.1:6333")
    p.add_argument("--qdrant-api-key", default="")
    p.add_argument("--qdrant-collection", default="pali_memories")
    p.add_argument("--qdrant-timeout-ms", type=int, default=2000)
    p.add_argument("--top-k", type=int, default=60)
    p.add_argument("--max-queries", type=int, default=-1)
    p.add_argument("--host", default="127.0.0.1")
    p.add_argument("--port", type=int, default=18080)
    p.add_argument("--server-start-timeout-seconds", type=float, default=120.0)
    p.add_argument("--answer-mode", choices=["extractive", "generate", "hybrid"], default="hybrid")
    p.add_argument("--answer-provider", choices=["ollama", "openrouter"], default="ollama")
    p.add_argument("--answer-model", default="qwen2.5:7b")
    p.add_argument("--answer-openrouter-model", default="google/gemini-2.0-flash-001")
    p.add_argument("--answer-top-docs", type=int, default=8)
    p.add_argument("--answer-ollama-url", default="http://127.0.0.1:11434")
    p.add_argument("--answer-timeout-seconds", type=float, default=45.0)
    p.add_argument("--answer-max-tokens", type=int, default=96)
    p.add_argument("--answer-temperature", type=float, default=0.0)
    p.add_argument("--extractive-confidence-threshold", type=float, default=0.42)
    p.add_argument("--prefer-extractive-for-temporal", action="store_true")
    p.add_argument("--retrieval-query-variants", type=int, default=1)
    p.add_argument("--retrieval-rrf-k", type=float, default=60.0)
    p.add_argument("--retrieval-kind-routing", action="store_true")
    p.add_argument("--multihop-entity-fact-bridge-enabled", action=argparse.BooleanOptionalAction, default=True)
    p.add_argument("--multihop-llm-decomposition-enabled", action=argparse.BooleanOptionalAction, default=False)
    p.add_argument("--multihop-decomposition-provider", choices=["openrouter", "ollama", "none"], default="openrouter")
    p.add_argument("--multihop-openrouter-model", default="openai/gpt-oss-120b:nitro")
    p.add_argument("--multihop-ollama-url", default="http://127.0.0.1:11434")
    p.add_argument("--multihop-ollama-model", default="qwen2.5:7b")
    p.add_argument("--multihop-ollama-timeout-ms", type=int, default=20000)
    p.add_argument("--multihop-max-decomposition-queries", type=int, default=3)
    p.add_argument("--multihop-enable-pairwise-rerank", action=argparse.BooleanOptionalAction, default=True)
    p.add_argument("--multihop-token-expansion-fallback", action=argparse.BooleanOptionalAction, default=True)
    p.add_argument("--temporal-route-raw-turn", action=argparse.BooleanOptionalAction, default=True)
    p.add_argument("--context-neighbor-window", type=int, default=1)
    p.add_argument("--context-max-items", type=int, default=24)
    p.add_argument("--evidence-max-lines", type=int, default=10)
    p.add_argument("--open-domain-llm-evidence-select", action=argparse.BooleanOptionalAction, default=True)
    p.add_argument("--open-domain-query-rewrite", action=argparse.BooleanOptionalAction, default=False)
    p.add_argument("--open-domain-query-rewrite-count", type=int, default=3)
    p.add_argument("--open-domain-hyde", action=argparse.BooleanOptionalAction, default=False)
    p.add_argument("--open-domain-profile-route", action=argparse.BooleanOptionalAction, default=False)
    p.add_argument("--profile-layer-enabled", action=argparse.BooleanOptionalAction, default=False)
    p.add_argument("--profile-layer-mode", choices=["summary", "facets"], default="summary")
    p.add_argument("--profile-layer-provider", choices=["openrouter", "ollama"], default="openrouter")
    p.add_argument("--profile-layer-openrouter-model", default="openai/gpt-oss-120b:nitro")
    p.add_argument("--profile-layer-ollama-url", default="http://127.0.0.1:11434")
    p.add_argument("--profile-layer-ollama-model", default="qwen2.5:7b")
    p.add_argument("--profile-layer-timeout-seconds", type=float, default=45.0)
    p.add_argument("--profile-layer-max-source-lines", type=int, default=80)
    p.add_argument("--profile-layer-max-summary-lines", type=int, default=8)
    p.add_argument("--profile-layer-workers", type=int, default=8)
    p.add_argument("--structured-memory-enabled", action="store_true")
    p.add_argument("--structured-dual-write-observations", action="store_true")
    p.add_argument("--structured-dual-write-events", action="store_true")
    p.add_argument("--structured-query-routing-enabled", action="store_true")
    p.add_argument("--structured-max-observations", type=int, default=3)
    p.add_argument("--parser-enabled", action=argparse.BooleanOptionalAction, default=True)
    p.add_argument("--parser-provider", choices=["heuristic", "ollama", "openrouter"], default="heuristic")
    p.add_argument("--parser-store-raw-turn", action=argparse.BooleanOptionalAction, default=True)
    p.add_argument("--parser-max-facts", type=int, default=5)
    p.add_argument("--parser-dedupe-threshold", type=float, default=0.88)
    p.add_argument("--parser-update-threshold", type=float, default=0.94)
    p.add_argument("--parser-ollama-url", default="http://127.0.0.1:11434")
    p.add_argument("--parser-ollama-model", default="qwen2.5:7b")
    p.add_argument("--parser-openrouter-model", default="openai/gpt-oss-120b:nitro")
    p.add_argument("--parser-ollama-timeout-ms", type=int, default=20000)
    p.add_argument("--trace-jsonl", default="", help="Optional per-query trace output JSONL path")
    p.add_argument("--trace-top-k", type=int, default=12, help="How many ranked items to keep in per-query traces")
    p.add_argument("--store-batch-size", type=int, default=STORE_BATCH_SIZE, help="Batch size for /v1/memory/batch ingestion")
    p.add_argument("--store-batch-timeout-seconds", type=float, default=STORE_BATCH_TIMEOUT_SECONDS, help="Timeout for each batch ingest request")
    p.add_argument("--store-single-timeout-seconds", type=float, default=STORE_SINGLE_TIMEOUT_SECONDS, help="Timeout for each single /v1/memory ingest request")
    p.add_argument("--eval-workers", type=int, default=50, help="Parallel workers for eval query processing")
    p.add_argument("--entity-fact-backend", choices=["sqlite", "neo4j"], default="sqlite")
    p.add_argument("--neo4j-uri", default="bolt://127.0.0.1:7687")
    p.add_argument("--neo4j-username", default="neo4j")
    p.add_argument("--neo4j-password", default="")
    p.add_argument("--neo4j-database", default="neo4j")
    p.add_argument("--neo4j-timeout-ms", type=int, default=2000)
    p.add_argument("--neo4j-batch-size", type=int, default=256)
    p.add_argument("--db-path", default="", help="Optional persistent sqlite db path")
    p.add_argument("--index-map-path", default="", help="Optional fixture-index to memory-id JSON path")
    p.add_argument("--reuse-existing-store", action="store_true", help="Skip tenant/store and reuse existing db + index map")
    p.add_argument("--reset-db", action="store_true", help="Delete existing --db-path file before storing")
    p.add_argument("--override-fingerprint", action="store_true", help="Proceed with reuse store even when index-map fingerprint mismatches")
    p.add_argument("--out-json", required=True)
    p.add_argument("--out-summary", required=True)
    args = p.parse_args()
    if args.extractive_confidence_threshold < 0 or args.extractive_confidence_threshold > 1:
        raise SystemExit("--extractive-confidence-threshold must be in [0,1]")
    if args.parser_max_facts <= 0:
        raise SystemExit("--parser-max-facts must be > 0")
    if args.parser_dedupe_threshold < 0 or args.parser_dedupe_threshold > 1:
        raise SystemExit("--parser-dedupe-threshold must be in [0,1]")
    if args.parser_update_threshold < 0 or args.parser_update_threshold > 1:
        raise SystemExit("--parser-update-threshold must be in [0,1]")
    if args.parser_dedupe_threshold > args.parser_update_threshold:
        raise SystemExit("--parser-dedupe-threshold must be <= --parser-update-threshold")
    if args.parser_ollama_timeout_ms < 0:
        raise SystemExit("--parser-ollama-timeout-ms must be >= 0")
    if args.store_batch_size <= 0:
        raise SystemExit("--store-batch-size must be > 0")
    if args.store_batch_timeout_seconds <= 0:
        raise SystemExit("--store-batch-timeout-seconds must be > 0")
    if args.store_single_timeout_seconds <= 0:
        raise SystemExit("--store-single-timeout-seconds must be > 0")
    if args.eval_workers <= 0:
        raise SystemExit("--eval-workers must be > 0")
    if args.open_domain_query_rewrite_count <= 0:
        raise SystemExit("--open-domain-query-rewrite-count must be > 0")
    if args.profile_layer_timeout_seconds <= 0:
        raise SystemExit("--profile-layer-timeout-seconds must be > 0")
    if args.profile_layer_max_source_lines <= 0:
        raise SystemExit("--profile-layer-max-source-lines must be > 0")
    if args.profile_layer_max_summary_lines <= 0:
        raise SystemExit("--profile-layer-max-summary-lines must be > 0")
    if args.profile_layer_workers <= 0:
        raise SystemExit("--profile-layer-workers must be > 0")
    if args.multihop_ollama_timeout_ms < 0:
        raise SystemExit("--multihop-ollama-timeout-ms must be >= 0")
    if args.multihop_max_decomposition_queries <= 0:
        raise SystemExit("--multihop-max-decomposition-queries must be > 0")
    if args.neo4j_timeout_ms < 0:
        raise SystemExit("--neo4j-timeout-ms must be >= 0")
    if args.neo4j_batch_size <= 0:
        raise SystemExit("--neo4j-batch-size must be > 0")
    if args.importance_ollama_timeout_ms < 0:
        raise SystemExit("--importance-ollama-timeout-ms must be >= 0")
    if args.openrouter_timeout_ms < 0:
        raise SystemExit("--openrouter-timeout-ms must be >= 0")
    if args.qdrant_timeout_ms < 0:
        raise SystemExit("--qdrant-timeout-ms must be >= 0")
    # Match the shell benchmark scripts and keep lexical vectors out of the
    # shared dense-model collection to avoid dimension mismatches.
    if (
        args.vector_backend == "qdrant"
        and args.embedding_provider in {"lexical", "mock"}
        and args.qdrant_collection == "pali_memories"
    ):
        args.qdrant_collection = "pali_memories_lexical"
    if args.embedding_provider == "openrouter" and not str(args.openrouter_api_key or "").strip():
        raise SystemExit(
            "--openrouter-api-key is required when --embedding-provider=openrouter "
            "(or set OPENROUTER_API_KEY)"
        )
    if args.importance_scorer == "openrouter" and not str(args.openrouter_api_key or "").strip():
        raise SystemExit(
            "--openrouter-api-key is required when --importance-scorer=openrouter "
            "(or set OPENROUTER_API_KEY)"
        )
    if args.profile_layer_enabled and args.profile_layer_provider == "openrouter" and not str(args.openrouter_api_key or "").strip():
        raise SystemExit(
            "--openrouter-api-key is required when --profile-layer-enabled "
            "and --profile-layer-provider=openrouter (or set OPENROUTER_API_KEY)"
        )
    if args.answer_mode in {"generate", "hybrid"} and args.answer_provider == "openrouter":
        if not str(args.openrouter_api_key or "").strip():
            raise SystemExit(
                "--openrouter-api-key is required when --answer-provider=openrouter "
                "(or set OPENROUTER_API_KEY)"
            )
        if not str(args.answer_openrouter_model or "").strip():
            raise SystemExit("--answer-openrouter-model is required when --answer-provider=openrouter")
    if args.parser_enabled and args.parser_provider == "openrouter":
        if not str(args.openrouter_api_key or "").strip():
            raise SystemExit(
                "--openrouter-api-key is required when --parser-provider=openrouter "
                "(or set OPENROUTER_API_KEY)"
            )
        if not str(args.parser_openrouter_model or "").strip():
            raise SystemExit("--parser-openrouter-model is required when --parser-provider=openrouter")
    if args.multihop_llm_decomposition_enabled:
        if args.multihop_decomposition_provider == "openrouter":
            if not str(args.openrouter_api_key or "").strip():
                raise SystemExit(
                    "--openrouter-api-key is required when --multihop-llm-decomposition-enabled "
                    "and --multihop-decomposition-provider=openrouter"
                )
            if not str(args.multihop_openrouter_model or "").strip():
                raise SystemExit(
                    "--multihop-openrouter-model is required when "
                    "--multihop-llm-decomposition-enabled and "
                    "--multihop-decomposition-provider=openrouter"
                )
        elif args.multihop_decomposition_provider == "ollama":
            if not str(args.multihop_ollama_url or "").strip():
                raise SystemExit(
                    "--multihop-ollama-url is required when "
                    "--multihop-llm-decomposition-enabled and "
                    "--multihop-decomposition-provider=ollama"
                )
            if not str(args.multihop_ollama_model or "").strip():
                raise SystemExit(
                    "--multihop-ollama-model is required when "
                    "--multihop-llm-decomposition-enabled and "
                    "--multihop-decomposition-provider=ollama"
                )
        elif args.multihop_decomposition_provider == "none":
            raise SystemExit(
                "--multihop-decomposition-provider=none cannot be used with "
                "--multihop-llm-decomposition-enabled"
            )

    fixture = json.loads(Path(args.fixture).read_text(encoding="utf-8"))
    eval_set = json.loads(Path(args.eval_set).read_text(encoding="utf-8"))
    if not isinstance(fixture, list) or not fixture:
        raise SystemExit("fixture must be a non-empty JSON array")
    if not isinstance(eval_set, list) or not eval_set:
        raise SystemExit("eval-set must be a non-empty JSON array")
    config_fingerprint = compute_config_fingerprint(fixture, args)
    run_stamp = build_run_stamp()

    base_url = f"http://{args.host}:{args.port}"
    out_json = Path(args.out_json)
    out_summary = Path(args.out_summary)
    out_json.parent.mkdir(parents=True, exist_ok=True)
    out_summary.parent.mkdir(parents=True, exist_ok=True)
    trace_path = Path(args.trace_jsonl) if args.trace_jsonl else None
    if trace_path:
        trace_path.parent.mkdir(parents=True, exist_ok=True)
    store_batch_size = int(args.store_batch_size)
    store_batch_timeout_s = float(args.store_batch_timeout_seconds)
    store_single_timeout_s = float(args.store_single_timeout_seconds)

    preflight_code, _ = json_request(base_url + "/health", None, timeout_s=2)
    if preflight_code == 200:
        raise SystemExit(
            f"Refusing to start eval server at {base_url}: /health already responds. "
            "Stop the stale server or use a different --port."
        )

    if args.embedding_provider == "ollama":
        code, _ = json_request(args.ollama_url.rstrip("/") + "/api/version", None, timeout_s=10)
        if code != 200:
            raise SystemExit(f"Ollama embedder endpoint not reachable: {args.ollama_url}")
    if args.embedding_provider == "openrouter":
        if not str(args.openrouter_api_key or "").strip():
            raise SystemExit(
                "OpenRouter API key missing. Set --openrouter-api-key or OPENROUTER_API_KEY."
            )
    if args.answer_mode in {"generate", "hybrid"}:
        if args.answer_provider == "ollama":
            code, _ = json_request(args.answer_ollama_url.rstrip("/") + "/api/version", None, timeout_s=10)
            if code != 200:
                raise SystemExit(f"Ollama answer endpoint not reachable: {args.answer_ollama_url}")
        elif args.answer_provider == "openrouter":
            if not str(args.openrouter_api_key or "").strip():
                raise SystemExit(
                    "OpenRouter API key missing. Set --openrouter-api-key or OPENROUTER_API_KEY."
                )
    if args.parser_enabled and args.parser_provider == "ollama":
        code, _ = json_request(args.parser_ollama_url.rstrip("/") + "/api/version", None, timeout_s=10)
        if code != 200:
            raise SystemExit(f"Ollama parser endpoint not reachable: {args.parser_ollama_url}")
    if args.parser_enabled and args.parser_provider == "openrouter":
        if not str(args.openrouter_api_key or "").strip():
            raise SystemExit(
                "OpenRouter API key missing. Set --openrouter-api-key or OPENROUTER_API_KEY."
            )
    if args.importance_scorer == "ollama":
        code, _ = json_request(args.importance_ollama_url.rstrip("/") + "/api/version", None, timeout_s=10)
        if code != 200:
            raise SystemExit(f"Ollama importance scorer endpoint not reachable: {args.importance_ollama_url}")
    if args.importance_scorer == "openrouter":
        if not str(args.openrouter_api_key or "").strip():
            raise SystemExit(
                "OpenRouter API key missing. Set --openrouter-api-key or OPENROUTER_API_KEY."
            )
    if args.vector_backend == "qdrant":
        code, _ = json_request(args.qdrant_url.rstrip("/") + "/collections", None, timeout_s=10)
        if code != 200:
            raise SystemExit(f"Qdrant endpoint not reachable: {args.qdrant_url}")

    ordered_by_session, by_dialog_id = build_dialog_context_index(fixture)
    resolved_answer_model = ""
    if args.answer_mode in {"generate", "hybrid"}:
        if args.answer_provider == "openrouter":
            resolved_answer_model = args.answer_openrouter_model
        else:
            resolved_answer_model = args.answer_model

    def generate_answer(prompt: str) -> tuple[bool, str]:
        attempts = 3
        for _ in range(attempts):
            if args.answer_provider == "openrouter":
                ok, text = openrouter_generate(
                    base_url=args.openrouter_base_url,
                    api_key=args.openrouter_api_key,
                    model=args.answer_openrouter_model,
                    prompt=prompt,
                    temperature=args.answer_temperature,
                    timeout_s=args.answer_timeout_seconds,
                    max_tokens=args.answer_max_tokens,
                )
            else:
                ok, text = ollama_generate(
                    base_url=args.answer_ollama_url,
                    model=args.answer_model,
                    prompt=prompt,
                    temperature=args.answer_temperature,
                    timeout_s=args.answer_timeout_seconds,
                    max_tokens=args.answer_max_tokens,
                )
            if ok:
                return ok, text
        return False, "Unknown"

    def generate_structured_output(
        prompt: str,
        provider: str,
        model: str,
        timeout_s: float,
        max_tokens: int = 256,
    ) -> tuple[bool, str]:
        attempts = 3
        for _ in range(attempts):
            if provider == "openrouter":
                ok, text = openrouter_generate(
                    base_url=args.openrouter_base_url,
                    api_key=args.openrouter_api_key,
                    model=model,
                    prompt=prompt,
                    temperature=0.0,
                    timeout_s=timeout_s,
                    max_tokens=max_tokens,
                    clean_output=False,
                )
            else:
                ok, text = ollama_generate(
                    base_url=args.profile_layer_ollama_url if provider == "ollama" and model == args.profile_layer_ollama_model else args.answer_ollama_url,
                    model=model,
                    prompt=prompt,
                    temperature=0.0,
                    timeout_s=timeout_s,
                    max_tokens=max_tokens,
                    clean_output=False,
                )
            if ok:
                return ok, text
        return False, "Unknown"

    def select_open_domain_evidence(question: str, candidate_lines: list[str], max_lines: int) -> list[str]:
        if len(candidate_lines) <= max_lines:
            return candidate_lines
        prompt = build_open_domain_evidence_selection_prompt(question, candidate_lines, max_lines)
        ok, raw = generate_structured_output(
            prompt,
            args.answer_provider,
            args.answer_openrouter_model if args.answer_provider == "openrouter" else args.answer_model,
            args.answer_timeout_seconds,
            max_tokens=160,
        )
        if not ok:
            return candidate_lines[:max_lines]
        selected = parse_line_number_selection(raw, len(candidate_lines), max_lines)
        if not selected:
            return candidate_lines[:max_lines]
        return [candidate_lines[idx - 1] for idx in selected]

    def build_open_domain_query_rewrites(question: str) -> list[str]:
        if not args.open_domain_query_rewrite:
            return []
        prompt = build_open_domain_query_rewrite_prompt(question, args.open_domain_query_rewrite_count)
        ok, raw = generate_structured_output(
            prompt,
            args.answer_provider,
            args.answer_openrouter_model if args.answer_provider == "openrouter" else args.answer_model,
            args.answer_timeout_seconds,
            max_tokens=220,
        )
        if not ok:
            return []
        rewrites = parse_query_rewrite_response(raw, args.open_domain_query_rewrite_count)
        filtered: list[str] = []
        base_key = compact_query(question).lower()
        for rewrite in rewrites:
            key = compact_query(rewrite).lower()
            if not key or key == base_key:
                continue
            filtered.append(rewrite)
        return filtered

    def build_open_domain_hyde_query(question: str) -> list[str]:
        if not args.open_domain_hyde:
            return []
        prompt = build_open_domain_hyde_prompt(question)
        ok, raw = generate_answer(prompt)
        if not ok:
            return []
        text = " ".join(str(raw or "").split()).strip()
        if not text:
            return []
        base_key = compact_query(question).lower()
        if compact_query(text).lower() == base_key:
            return []
        return [text]

    def build_open_domain_llm_candidates(question: str, evidence_lines: list[str], max_candidates: int = 5) -> list[str]:
        prompt = build_open_domain_candidate_prompt(question, evidence_lines, max_candidates)
        ok, raw = generate_structured_output(
            prompt,
            args.answer_provider,
            args.answer_openrouter_model if args.answer_provider == "openrouter" else args.answer_model,
            args.answer_timeout_seconds,
            max_tokens=220,
        )
        if not ok:
            return []
        return parse_open_domain_candidate_response(raw, max_candidates)

    def verify_open_domain_answer(
        question: str,
        evidence_lines: list[str],
        candidate_pool: list[str],
        extractive_answer_value: str,
        extractive_confidence: float,
    ) -> tuple[bool, str, dict[str, Any]]:
        prompt = build_open_domain_verification_prompt(
            question,
            evidence_lines,
            candidate_answers=candidate_pool,
            extractive_answer=extractive_answer_value,
        )
        ok, raw = generate_structured_output(
            prompt,
            args.answer_provider,
            args.answer_openrouter_model if args.answer_provider == "openrouter" else args.answer_model,
            args.answer_timeout_seconds,
            max_tokens=260,
        )
        if not ok:
            return False, "Unknown", {}
        parsed = parse_open_domain_verification_response(raw)
        answer = parsed.get("final_answer", "") if parsed else ""
        if not answer:
            return False, "Unknown", parsed
        if parsed.get("verdict") == "insufficient":
            return True, "Unknown", parsed
        answer = snap_generated_answer_to_candidates(
            question,
            answer,
            candidate_pool,
            extractive_answer_value,
            extractive_confidence,
            True,
        )
        answer = normalize_open_domain_label_answer(question, answer)
        if (is_booleanish_query(question) or extract_question_alternatives(question)) and ";" not in answer:
            clause = build_support_clause(question, evidence_lines, parsed.get("supporting_lines", []))
            if clause:
                answer = f"{answer}; {clause}"
        return True, answer, parsed

    def resolve_open_domain_answer(
        question: str,
        evidence_lines: list[str],
        candidate_pool: list[str],
        extractive_answer_value: str,
        extractive_confidence: float,
    ) -> tuple[bool, str, dict[str, Any]]:
        prompt = build_open_domain_resolution_prompt(
            question,
            evidence_lines,
            candidate_answers=candidate_pool,
        )
        ok, raw = generate_answer(prompt)
        if not ok:
            return False, "Unknown", {}
        answer = raw
        if not answer:
            return False, "Unknown", {}
        answer = snap_generated_answer_to_candidates(
            question,
            answer,
            candidate_pool,
            extractive_answer_value,
            extractive_confidence,
            True,
        )
        answer = normalize_open_domain_label_answer(question, answer)
        return True, answer, {}

    def generate_profile_facets(entity: str, source_lines: list[str]) -> dict[str, list[str]]:
        prompt = build_profile_facets_prompt(entity, source_lines, args.profile_layer_max_source_lines)
        ok, raw = generate_structured_output(
            prompt,
            args.profile_layer_provider,
            args.profile_layer_openrouter_model if args.profile_layer_provider == "openrouter" else args.profile_layer_ollama_model,
            args.profile_layer_timeout_seconds,
            max_tokens=520,
        )
        if not ok:
            return {}
        return parse_profile_facets_response(raw, args.profile_layer_max_summary_lines)

    def generate_profile_summary(entity: str, source_lines: list[str]) -> list[str]:
        prompt = build_profile_summary_prompt(entity, source_lines, args.profile_layer_max_source_lines)
        ok, raw = generate_structured_output(
            prompt,
            args.profile_layer_provider,
            args.profile_layer_openrouter_model if args.profile_layer_provider == "openrouter" else args.profile_layer_ollama_model,
            args.profile_layer_timeout_seconds,
            max_tokens=320,
        )
        if not ok:
            return []
        return parse_profile_summary_response(raw, entity, args.profile_layer_max_summary_lines)

    tmpdir = tempfile.TemporaryDirectory(ignore_cleanup_errors=True)
    tmp = Path(tmpdir.name)
    cfg = tmp / "qa_eval.yaml"
    if args.db_path:
        db = Path(args.db_path)
        db.parent.mkdir(parents=True, exist_ok=True)
        if args.reset_db and db.exists():
            db.unlink()
    else:
        db = tmp / "qa_eval.sqlite"
    server_log = tmp / "server.log"
    server_log_fixed = Path("research/cache/server_last_run.log")  # readable after run
    server_log_fixed.parent.mkdir(parents=True, exist_ok=True)
    # YAML double-quoted scalars treat backslashes as escapes; normalize to URI-style path.
    db_uri_path = db.resolve().as_posix()
    cfg.write_text(
        (
            "server:\n"
            f"  host: \"{args.host}\"\n"
            f"  port: {args.port}\n"
            f"vector_backend: \"{args.vector_backend}\"\n"
            f"entity_fact_backend: \"{args.entity_fact_backend}\"\n"
            "default_tenant_id: \"\"\n"
            f"importance_scorer: \"{args.importance_scorer}\"\n"
            "postprocess:\n"
            "  enabled: true\n"
            "  poll_interval_ms: 250\n"
            "  batch_size: 32\n"
            "  worker_count: 2\n"
            "  lease_ms: 30000\n"
            "  max_attempts: 5\n"
            "  retry_base_ms: 500\n"
            "  retry_max_ms: 60000\n"
            "retrieval:\n"
            "  scoring:\n"
            "    algorithm: \"wal\"\n"
            "    wal:\n"
            "      recency: 0.1\n"
            "      relevance: 0.8\n"
            "      importance: 0.1\n"
            "    match:\n"
            "      recency: 0.05\n"
            "      relevance: 0.70\n"
            "      importance: 0.10\n"
            "      query_overlap: 0.10\n"
            "      routing: 0.05\n"
            "  multi_hop:\n"
            f"    entity_fact_bridge_enabled: {'true' if args.multihop_entity_fact_bridge_enabled else 'false'}\n"
            f"    llm_decomposition_enabled: {'true' if args.multihop_llm_decomposition_enabled else 'false'}\n"
            f"    decomposition_provider: \"{args.multihop_decomposition_provider}\"\n"
            f"    openrouter_model: \"{args.multihop_openrouter_model}\"\n"
            f"    ollama_base_url: \"{args.multihop_ollama_url}\"\n"
            f"    ollama_model: \"{args.multihop_ollama_model}\"\n"
            f"    ollama_timeout_ms: {args.multihop_ollama_timeout_ms}\n"
            f"    max_decomposition_queries: {args.multihop_max_decomposition_queries}\n"
            f"    enable_pairwise_rerank: {'true' if args.multihop_enable_pairwise_rerank else 'false'}\n"
            f"    token_expansion_fallback: {'true' if args.multihop_token_expansion_fallback else 'false'}\n"
            "structured_memory:\n"
            f"  enabled: {'true' if args.structured_memory_enabled else 'false'}\n"
            f"  dual_write_observations: {'true' if args.structured_dual_write_observations else 'false'}\n"
            f"  dual_write_events: {'true' if args.structured_dual_write_events else 'false'}\n"
            f"  max_observations: {args.structured_max_observations}\n"
            "parser:\n"
            f"  enabled: {'true' if args.parser_enabled else 'false'}\n"
            f"  provider: \"{args.parser_provider}\"\n"
            f"  ollama_base_url: \"{args.parser_ollama_url}\"\n"
            f"  ollama_model: \"{args.parser_ollama_model}\"\n"
            f"  openrouter_model: \"{args.parser_openrouter_model}\"\n"
            f"  ollama_timeout_ms: {args.parser_ollama_timeout_ms}\n"
            f"  store_raw_turn: {'true' if args.parser_store_raw_turn else 'false'}\n"
            f"  max_facts: {args.parser_max_facts}\n"
            f"  dedupe_threshold: {args.parser_dedupe_threshold}\n"
            f"  update_threshold: {args.parser_update_threshold}\n"
            "database:\n"
            f"  sqlite_dsn: \"file:{db_uri_path}?cache=shared\"\n"
            "qdrant:\n"
            f"  base_url: \"{args.qdrant_url}\"\n"
            f"  api_key: \"{args.qdrant_api_key}\"\n"
            f"  collection: \"{args.qdrant_collection}\"\n"
            f"  timeout_ms: {args.qdrant_timeout_ms}\n"
            "neo4j:\n"
            f"  uri: \"{args.neo4j_uri}\"\n"
            f"  username: \"{args.neo4j_username}\"\n"
            f"  password: \"{args.neo4j_password}\"\n"
            f"  database: \"{args.neo4j_database}\"\n"
            f"  timeout_ms: {args.neo4j_timeout_ms}\n"
            f"  batch_size: {args.neo4j_batch_size}\n"
            "embedding:\n"
            f"  provider: \"{args.embedding_provider}\"\n"
            "  fallback_provider: \"lexical\"\n"
            f"  ollama_base_url: \"{args.ollama_url}\"\n"
            f"  ollama_model: \"{args.embedding_model}\"\n"
            "  ollama_timeout_seconds: 10\n"
            "  model_path: \"./models/all-MiniLM-L6-v2/model.onnx\"\n"
            "  tokenizer_path: \"./models/all-MiniLM-L6-v2/tokenizer.json\"\n"
            "openrouter:\n"
            f"  base_url: \"{args.openrouter_base_url}\"\n"
            f"  api_key: \"{args.openrouter_api_key}\"\n"
            f"  embedding_model: \"{args.openrouter_embedding_model}\"\n"
            f"  scoring_model: \"{args.openrouter_scoring_model}\"\n"
            f"  timeout_ms: {args.openrouter_timeout_ms}\n"
            "ollama:\n"
            f"  base_url: \"{args.importance_ollama_url}\"\n"
            f"  model: \"{args.importance_ollama_model}\"\n"
            f"  timeout_ms: {args.importance_ollama_timeout_ms}\n"
            "auth:\n"
            "  enabled: false\n"
            "  jwt_secret: \"\"\n"
            "  issuer: \"pali\"\n"
            "logging:\n"
            "  dev_verbose: false\n"
            "  progress: true\n"
        ),
        encoding="utf-8",
    )
    print(f"server db path : {db.absolute()}", flush=True)
    print(f"server cfg path: {cfg}", flush=True)

    env = os.environ.copy()
    env["GOCACHE"] = env.get("GOCACHE", str(tmp / "gocache"))
    logf = server_log_fixed.open("w", encoding="utf-8", buffering=1)
    tracef = trace_path.open("w", encoding="utf-8") if trace_path else None
    proc = subprocess.Popen(
        ["go", "run", "./cmd/pali", "-config", str(cfg)],
        stdout=logf,
        stderr=subprocess.STDOUT,
        env=env,
    )

    try:
        start = time.time()
        while time.time() - start < args.server_start_timeout_seconds:
            if proc.poll() is not None:
                logs = server_log_fixed.read_text(encoding="utf-8")
                raise RuntimeError(f"pali server exited early (code={proc.returncode})\n{logs}")
            code, _ = json_request(base_url + "/health", None, timeout_s=5)
            if code == 200:
                break
            time.sleep(0.2)
        else:
            logs = server_log_fixed.read_text(encoding="utf-8")
            raise RuntimeError(f"server health check timed out: {base_url}/health\n{logs}")
        if proc.poll() is not None:
            logs = server_log_fixed.read_text(encoding="utf-8")
            raise RuntimeError(
                "pali server process exited right after health became reachable; "
                f"refusing to continue (code={proc.returncode})\n{logs}"
            )

        idx_to_ids: dict[int, set[str]] = {}
        reuse_store = False
        store_mode = "single"
        store_batch_supported = False
        store_batch_fallbacks = 0
        store_single_writes = 0
        index_map_schema = 2
        stored_index_fingerprint = ""
        index_map_file = Path(args.index_map_path) if args.index_map_path else None
        if args.db_path and db.exists() and not args.reset_db and not args.reuse_existing_store:
            raise SystemExit(
                f"ERROR: --db-path already exists ({db}) and this run is not in reuse mode. "
                "Use --reset-db for a clean run or --reuse-existing-store with --index-map-path."
            )
        if args.reuse_existing_store and (not index_map_file or not index_map_file.exists()):
            raise SystemExit(
                "ERROR: --reuse-existing-store requires an existing --index-map-path file."
            )
        if args.reuse_existing_store and db.exists() and index_map_file and index_map_file.exists():
            raw = json.loads(index_map_file.read_text(encoding="utf-8"))
            idx_to_ids, stored_index_fingerprint, index_map_schema = parse_index_map_payload(raw)
            if stored_index_fingerprint and stored_index_fingerprint != config_fingerprint:
                msg = (
                    "index-map fingerprint mismatch: "
                    f"stored={stored_index_fingerprint} current={config_fingerprint}"
                )
                if args.override_fingerprint:
                    print(f"WARNING: {msg} (proceeding due to --override-fingerprint)", flush=True)
                else:
                    raise SystemExit(f"ERROR: {msg}. Rebuild cache or pass --override-fingerprint.")
            elif not stored_index_fingerprint:
                print("WARNING: index map has no fingerprint (legacy format); reusing with caution", flush=True)
            if idx_to_ids:
                reuse_store = True
                store_mode = "reuse_existing_store"
                print(
                    f"reusing existing store: db={db} index_map={index_map_file} schema=v{index_map_schema}",
                    flush=True,
                )

        # Ensure tenants exist for both fresh-store and reuse-store modes.
        # Reuse mode can skip writes entirely, so tenant rows may not exist yet.
        tenant_ids = sorted({str(row.get("tenant_id", "")).strip() for row in fixture if str(row.get("tenant_id", "")).strip()})
        for tid in tenant_ids:
            json_request(base_url + "/v1/tenants", {"id": tid, "name": tid}, timeout_s=20)

        if not reuse_store:
            batch_probe_code, _ = json_request(base_url + "/v1/memory/batch", {"items": []}, timeout_s=20)
            batch_supported = batch_probe_code not in (404, 405)
            store_batch_supported = batch_supported

            if batch_supported:
                store_mode = "batch"
                for start in range(0, len(fixture), store_batch_size):
                    items: list[dict[str, Any]] = []
                    indexes: list[int] = []
                    for offset, row in enumerate(fixture[start:start + store_batch_size]):
                        idx = start + offset
                        payload = dict(row)
                        payload["source"] = fixture_source_stamp(idx, run_stamp=run_stamp)
                        items.append(payload)
                        indexes.append(idx)

                    code, body = json_request(
                        base_url + "/v1/memory/batch",
                        {"items": items},
                        timeout_s=store_batch_timeout_s,
                    )
                    if start == 0:
                        print(f"[diag] first batch: code={code} body={str(body)[:300]}", flush=True)
                    if code == 201 and isinstance(body, dict):
                        returned = body.get("items", [])
                        if isinstance(returned, list):
                            for idx, item in zip(indexes, returned):
                                if not isinstance(item, dict):
                                    continue
                                mid = str(item.get("id", "")).strip()
                                if mid:
                                    idx_to_ids.setdefault(idx, set()).add(mid)
                    else:
                        # Fallback to one-by-one writes if batch endpoint is unavailable or fails.
                        store_batch_fallbacks += 1
                        store_mode = "batch_with_single_fallback"
                        for idx, payload in zip(indexes, items):
                            store_single_writes += 1
                            single_code, single_body = json_request(
                                base_url + "/v1/memory",
                                payload,
                                timeout_s=store_single_timeout_s,
                            )
                            if single_code == 201 and isinstance(single_body, dict):
                                mid = str(single_body.get("id", "")).strip()
                                if mid:
                                    idx_to_ids.setdefault(idx, set()).add(mid)

                    stored_count = min(start + store_batch_size, len(fixture))
                    if stored_count % 200 == 0 or stored_count == len(fixture):
                        print(f"stored {stored_count}/{len(fixture)}", flush=True)
            else:
                store_mode = "single"
                for idx, row in enumerate(fixture):
                    payload = dict(row)
                    payload["source"] = fixture_source_stamp(idx, run_stamp=run_stamp)
                    store_single_writes += 1
                    code, body = json_request(base_url + "/v1/memory", payload, timeout_s=store_single_timeout_s)
                    if code == 201 and isinstance(body, dict):
                        mid = str(body.get("id", "")).strip()
                        if mid:
                            idx_to_ids.setdefault(idx, set()).add(mid)
                    if (idx + 1) % 200 == 0 or (idx + 1) == len(fixture):
                        print(f"stored {idx + 1}/{len(fixture)}", flush=True)

            # Dump any error lines from server log to help diagnose 500s
            try:
                log_lines = server_log_fixed.read_text(encoding="utf-8").splitlines()
                err_lines = [l for l in log_lines if any(k in l for k in ("error", "Error", "ERROR", "panic", "PANIC", "fatal", "FATAL"))]
                print(f"[diag] server log errors ({len(err_lines)} lines):", flush=True)
                for l in err_lines[:15]:
                    print(f"  {l}", flush=True)
            except Exception as e:
                print(f"[diag] could not read server log: {e}", flush=True)

            print(f"idx_to_ids from API responses: {len(idx_to_ids)} fixture rows mapped", flush=True)
            try:
                db_idx_to_ids = collect_index_map_from_db(db.absolute(), run_stamp=run_stamp)
                for idx, memory_ids in db_idx_to_ids.items():
                    if not memory_ids:
                        continue
                    idx_to_ids.setdefault(idx, set()).update(memory_ids)
                print(f"idx_to_ids after db supplement: {len(idx_to_ids)} fixture rows mapped", flush=True)
            except Exception as e:
                print(f"WARNING: collect_index_map_from_db failed ({e}); using API-response index only", flush=True)

            if index_map_file:
                index_map_file.parent.mkdir(parents=True, exist_ok=True)
                payload = {
                    "schema_version": 2,
                    "config_fingerprint": config_fingerprint,
                    "index_to_ids": {
                        str(k): sorted(list(v))
                        for k, v in sorted(idx_to_ids.items(), key=lambda kv: kv[0])
                        if v
                    },
                }
                index_map_file.write_text(
                    json.dumps(payload, indent=2) + "\n",
                    encoding="utf-8",
                )
                print(f"wrote index map: {index_map_file}", flush=True)

        profile_memory_count = 0
        profile_entities_count = 0
        profile_facet_count = 0
        if args.profile_layer_enabled:
            if reuse_store:
                profile_memory_count, profile_entities_count = count_existing_profile_memories(db)
                profile_facet_count = profile_memory_count if args.profile_layer_mode == "facets" else 0
                print(
                    f"profile layer: reusing {profile_memory_count} stored profile memories across {profile_entities_count} entities",
                    flush=True,
                )
            else:
                profile_source_index = build_profile_source_index(fixture)
                profile_jobs: list[tuple[str, str, list[str]]] = []
                for tenant_id, entity_map in profile_source_index.items():
                    for entity, raw_lines in entity_map.items():
                        selected = select_profile_source_lines(entity, raw_lines, args.profile_layer_max_source_lines)
                        if len(selected) < 4:
                            continue
                        profile_jobs.append((tenant_id, entity, selected))

                if profile_jobs:
                    print(f"profile layer: generating {len(profile_jobs)} entity profiles", flush=True)
                    if args.profile_layer_mode == "facets":
                        generated_profiles: list[tuple[str, str, dict[str, list[str]]]] = []
                        if args.profile_layer_workers > 1 and len(profile_jobs) > 1:
                            with ThreadPoolExecutor(max_workers=min(args.profile_layer_workers, len(profile_jobs))) as executor:
                                future_to_job = {
                                    executor.submit(generate_profile_facets, entity, source_lines): (tenant_id, entity)
                                    for tenant_id, entity, source_lines in profile_jobs
                                }
                                for future in as_completed(future_to_job):
                                    tenant_id, entity = future_to_job[future]
                                    try:
                                        facet_map = future.result()
                                    except Exception:
                                        facet_map = {}
                                    if facet_map:
                                        generated_profiles.append((tenant_id, entity, facet_map))
                        else:
                            for tenant_id, entity, source_lines in profile_jobs:
                                facet_map = generate_profile_facets(entity, source_lines)
                                if facet_map:
                                    generated_profiles.append((tenant_id, entity, facet_map))

                        for tenant_id, entity, facet_map in generated_profiles:
                            entity_tag = canonical_entity_tag(entity)
                            for facet_key, items in facet_map.items():
                                label = PROFILE_FACET_LABELS.get(facet_key, facet_key)
                                content = f"Profile for {entity}. Profile facet: {label}. " + " ".join(items).strip()
                                payload = {
                                    "tenant_id": tenant_id,
                                    "content": content,
                                    "tags": ["profile", f"entity:{entity_tag}", f"facet:{facet_key}"],
                                    "tier": "semantic",
                                    "kind": "summary",
                                    "source": f"profile_summary:{entity_tag}:{facet_key}:run_{run_stamp}",
                                    "created_by": "system",
                                }
                                code, _ = json_request(base_url + "/v1/memory", payload, timeout_s=store_single_timeout_s)
                                if code == 201:
                                    profile_memory_count += 1
                                    profile_facet_count += 1
                        profile_entities_count = len(generated_profiles)
                    else:
                        generated_profiles: list[tuple[str, str, list[str]]] = []
                        if args.profile_layer_workers > 1 and len(profile_jobs) > 1:
                            with ThreadPoolExecutor(max_workers=min(args.profile_layer_workers, len(profile_jobs))) as executor:
                                future_to_job = {
                                    executor.submit(generate_profile_summary, entity, source_lines): (tenant_id, entity)
                                    for tenant_id, entity, source_lines in profile_jobs
                                }
                                for future in as_completed(future_to_job):
                                    tenant_id, entity = future_to_job[future]
                                    try:
                                        summary_lines = future.result()
                                    except Exception:
                                        summary_lines = []
                                    if summary_lines:
                                        generated_profiles.append((tenant_id, entity, summary_lines))
                        else:
                            for tenant_id, entity, source_lines in profile_jobs:
                                summary_lines = generate_profile_summary(entity, source_lines)
                                if summary_lines:
                                    generated_profiles.append((tenant_id, entity, summary_lines))

                        for tenant_id, entity, summary_lines in generated_profiles:
                            entity_tag = canonical_entity_tag(entity)
                            content = "Profile for " + entity + ". " + " ".join(summary_lines).strip()
                            payload = {
                                "tenant_id": tenant_id,
                                "content": content,
                                "tags": ["profile", f"entity:{entity_tag}"],
                                "tier": "semantic",
                                "kind": "summary",
                                "source": f"profile_summary:{entity_tag}:run_{run_stamp}",
                                "created_by": "system",
                            }
                            code, _ = json_request(base_url + "/v1/memory", payload, timeout_s=store_single_timeout_s)
                            if code == 201:
                                profile_memory_count += 1
                        profile_entities_count = len(generated_profiles)
                print(
                    f"profile layer: stored {profile_memory_count} {args.profile_layer_mode} memories across {profile_entities_count} entities",
                    flush=True,
                )

        eval_rows: list[dict[str, Any]] = []
        for row in eval_set:
            q = str(row.get("query", "")).strip()
            tenant_id = str(row.get("tenant_id", "")).strip()
            ref = str(row.get("reference_answer", "")).strip()
            if not q or not tenant_id or not ref:
                continue
            expected_groups: list[set[str]] = []
            for i in row.get("expected_fixture_indexes", []) or []:
                if isinstance(i, int):
                    group_ids = idx_to_ids.get(i, set())
                    if group_ids:
                        expected_groups.append(set(group_ids))
            for mid in row.get("expected_memory_ids", []) or []:
                if isinstance(mid, str) and mid.strip():
                    expected_groups.append({mid.strip()})
            expected_ids: set[str] = set()
            for group in expected_groups:
                expected_ids.update(group)
            if not expected_ids:
                continue
            eval_rows.append(
                {
                    "tenant_id": tenant_id,
                    "query": q,
                    "reference_answer": ref,
                    "expected_groups": expected_groups,
                    "expected_ids": expected_ids,
                    "expected_fixture_indexes": [i for i in (row.get("expected_fixture_indexes", []) or []) if isinstance(i, int)],
                    "expected_memory_ids": [m.strip() for m in (row.get("expected_memory_ids", []) or []) if isinstance(m, str) and m.strip()],
                    "category": row.get("category"),
                    "category_label": category_label(row.get("category")),
                }
            )

        if args.max_queries > 0:
            eval_rows = eval_rows[: args.max_queries]

        acc = EvalAcc()
        for i, row in enumerate(eval_rows, start=1):
            query_variants = build_query_variants(row["query"], max(1, args.retrieval_query_variants))
            if str(row.get("category", "")).strip() == "3":
                extra_profile_queries = build_open_domain_profile_queries(row["query"]) if (args.profile_layer_enabled and args.open_domain_profile_route and args.profile_layer_mode == "facets") else []
                hyde_queries = build_open_domain_hyde_query(row["query"])
                rewrite_budget = max(
                    1,
                    args.retrieval_query_variants
                    + args.open_domain_query_rewrite_count
                    + len(extra_profile_queries)
                    + len(hyde_queries),
                )
                query_variants = merge_query_variants(
                    query_variants,
                    extra_profile_queries,
                    hyde_queries,
                    build_open_domain_query_rewrites(row["query"]),
                    max_variants=rewrite_budget,
                )

            # Multi-query retrieval + RRF fusion.
            fused_score: dict[str, float] = {}
            best_rank: dict[str, int] = {}
            content_by_id: dict[str, str] = {}
            route_calls: list[dict[str, Any]] = []
            any_success = False
            route_jobs: list[tuple[str, str, list[str], float, dict[str, Any]]] = []
            for qv in query_variants:
                routes = build_retrieval_routes(
                    qv,
                    args.retrieval_kind_routing and (args.structured_memory_enabled or args.parser_enabled),
                    row.get("category"),
                    args.temporal_route_raw_turn,
                    args.open_domain_profile_route,
                    args.profile_layer_enabled,
                )
                for retrieval_kind, route_kinds, route_weight in routes:
                    payload: dict[str, Any] = {
                        "tenant_id": row["tenant_id"],
                        "query": qv,
                        "top_k": args.top_k,
                        "disable_touch": True,
                    }
                    kinds = route_kinds or []
                    if kinds:
                        payload["kinds"] = kinds
                    route_jobs.append((qv, retrieval_kind, kinds, route_weight, payload))

            route_results: list[tuple[str, str, list[str], float, int, Any]] = []
            if args.eval_workers > 1 and len(route_jobs) > 1:
                max_workers = min(args.eval_workers, len(route_jobs))
                with ThreadPoolExecutor(max_workers=max_workers) as executor:
                    future_to_job = {
                        executor.submit(json_request, base_url + "/v1/memory/search", payload, 45): (qv, retrieval_kind, kinds, route_weight)
                        for qv, retrieval_kind, kinds, route_weight, payload in route_jobs
                    }
                    for future in as_completed(future_to_job):
                        qv, retrieval_kind, kinds, route_weight = future_to_job[future]
                        try:
                            code, body = future.result()
                        except Exception as e:
                            code, body = 599, {"error": str(e)}
                        route_results.append((qv, retrieval_kind, kinds, route_weight, code, body))
            else:
                for qv, retrieval_kind, kinds, route_weight, payload in route_jobs:
                    code, body = json_request(
                        base_url + "/v1/memory/search",
                        payload,
                        timeout_s=45,
                    )
                    route_results.append((qv, retrieval_kind, kinds, route_weight, code, body))

            for qv, retrieval_kind, kinds, route_weight, code, body in route_results:
                items = body.get("items", []) if isinstance(body, dict) else []
                route_calls.append(
                    {
                        "query_variant": qv,
                        "retrieval_kind": retrieval_kind,
                        "kinds": kinds,
                        "route_weight": route_weight,
                        "status_code": code,
                        "items": len(items) if isinstance(items, list) else 0,
                    }
                )
                if code != 200 or not isinstance(body, dict):
                    continue
                any_success = True
                if not isinstance(items, list):
                    continue
                rank = 0
                for it in items:
                    mid = str(it.get("id", "")).strip()
                    if not mid:
                        continue
                    content_by_id[mid] = str(it.get("content", ""))
                    rank += 1
                    score = route_weight * (1.0 / (args.retrieval_rrf_k + rank))
                    fused_score[mid] = fused_score.get(mid, 0.0) + score
                    prev = best_rank.get(mid, 10**9)
                    if rank < prev:
                        best_rank[mid] = rank

            if not any_success or not fused_score:
                acc.query_failures += 1
                continue

            ranked_ids = sorted(
                fused_score.keys(),
                key=lambda mid: (-fused_score[mid], best_rank.get(mid, 10**9), mid),
            )
            
            # M3: Two-pass retrieval for multi-hop queries
            pass1_ids = ranked_ids[:args.top_k]
            pass1_contents = [normalize_context_line(content_by_id[mid]) for mid in pass1_ids if mid in content_by_id]
            pass2_performed = False
            pass2_anchor = ""
            pass2_trigger = ""
            
            # Trigger two-pass if: multi-hop query detected
            _, _, is_multihop = classify_query(row["query"])
            category_multihop = str(row.get("category", "")).strip() == "1"
            should_two_pass = bool(pass1_contents) and (is_multihop or category_multihop)
            if should_two_pass:
                if is_multihop and category_multihop:
                    pass2_trigger = "query_and_category"
                elif category_multihop:
                    pass2_trigger = "category"
                else:
                    pass2_trigger = "query"
                # Extract anchor from pass1 results
                pass2_anchor = extract_anchor_from_top_results(row["query"], pass1_contents, top_k=3)
                if pass2_anchor:
                    # Build second-pass query
                    pass2_query = build_two_pass_query(row["query"], pass2_anchor)
                    
                    # Execute second-pass retrieval with same routes + anchor query
                    pass2_fused: dict[str, float] = {}
                    pass2_best_rank: dict[str, int] = {}
                    pass2_success = False
                    
                    pass2_variants = build_query_variants(
                        pass2_query,
                        max(1, min(args.retrieval_query_variants, 2)),
                    )
                    pass2_jobs: list[tuple[str, str, list[str], float, dict[str, Any]]] = []
                    for qv in pass2_variants:
                        routes = build_retrieval_routes(
                            qv,
                            args.retrieval_kind_routing and (args.structured_memory_enabled or args.parser_enabled),
                            row.get("category"),
                            args.temporal_route_raw_turn,
                            args.open_domain_profile_route,
                            args.profile_layer_enabled,
                        )
                        for retrieval_kind, route_kinds, route_weight in routes:
                            payload: dict[str, Any] = {
                                "tenant_id": row["tenant_id"],
                                "query": qv,
                                "top_k": args.top_k,
                                "disable_touch": True,
                            }
                            kinds = route_kinds or []
                            if kinds:
                                payload["kinds"] = kinds
                            pass2_jobs.append((qv, retrieval_kind, kinds, route_weight, payload))

                    pass2_results: list[tuple[str, str, list[str], float, int, Any]] = []
                    if args.eval_workers > 1 and len(pass2_jobs) > 1:
                        max_workers = min(args.eval_workers, len(pass2_jobs))
                        with ThreadPoolExecutor(max_workers=max_workers) as executor:
                            future_to_job = {
                                executor.submit(json_request, base_url + "/v1/memory/search", payload, 45): (qv, retrieval_kind, kinds, route_weight)
                                for qv, retrieval_kind, kinds, route_weight, payload in pass2_jobs
                            }
                            for future in as_completed(future_to_job):
                                qv, retrieval_kind, kinds, route_weight = future_to_job[future]
                                try:
                                    code, body = future.result()
                                except Exception as e:
                                    code, body = 599, {"error": str(e)}
                                pass2_results.append((qv, retrieval_kind, kinds, route_weight, code, body))
                    else:
                        for qv, retrieval_kind, kinds, route_weight, payload in pass2_jobs:
                            code, body = json_request(
                                base_url + "/v1/memory/search",
                                payload,
                                timeout_s=45,
                            )
                            pass2_results.append((qv, retrieval_kind, kinds, route_weight, code, body))

                    for qv, retrieval_kind, kinds, route_weight, code, body in pass2_results:
                        items = body.get("items", []) if isinstance(body, dict) else []
                        route_calls.append(
                            {
                                "query_variant": qv,
                                "retrieval_kind": retrieval_kind,
                                "kinds": kinds,
                                "route_weight": route_weight,
                                "status_code": code,
                                "items": len(items) if isinstance(items, list) else 0,
                                "two_pass": True,
                            }
                        )
                        if code != 200 or not isinstance(body, dict) or not isinstance(items, list):
                            continue
                        pass2_success = True
                        rank = 0
                        for it in items:
                            mid = str(it.get("id", "")).strip()
                            if not mid:
                                continue
                            if mid not in content_by_id:
                                content_by_id[mid] = str(it.get("content", ""))
                            rank += 1
                            score = route_weight * (1.0 / (args.retrieval_rrf_k + rank))
                            pass2_fused[mid] = pass2_fused.get(mid, 0.0) + score
                            prev = pass2_best_rank.get(mid, 10**9)
                            if rank < prev:
                                pass2_best_rank[mid] = rank
                    
                    if pass2_success and pass2_fused:
                        pass2_performed = True
                        # Merge pass1 + pass2 using RRF: weight pass1 and pass2 equally
                        for mid in pass2_fused:
                            # Pass2 results get half weight so pass1 + pass2 contribute equally
                            fused_score[mid] = fused_score.get(mid, 0.0) + (0.5 * pass2_fused[mid])
                            prev = best_rank.get(mid, args.top_k + 1)
                            if pass2_best_rank[mid] < prev:
                                best_rank[mid] = pass2_best_rank[mid]
                        
                        # Re-rank merged results
                        ranked_ids = sorted(
                            fused_score.keys(),
                            key=lambda mid: (-fused_score[mid], best_rank.get(mid, 10**9), mid),
                        )
            
            returned_ids = ranked_ids[: args.top_k]
            returned_contents = [content_by_id[mid] for mid in returned_ids if mid in content_by_id]
            normalized_contents: list[str] = []
            for content in returned_contents:
                normalized = normalize_context_line(content)
                if normalized:
                    normalized_contents.append(normalized)

            top1_text = normalized_contents[0] if normalized_contents else ""
            concat3_text = " ".join(normalized_contents[:3]).strip()
            candidate_sentences: list[str] = []
            for c in normalized_contents:
                candidate_sentences.extend(split_sentences(c))
            if candidate_sentences:
                oracle_text = max(candidate_sentences, key=lambda s: token_f1(s, row["reference_answer"]))
            else:
                oracle_text = ""

            # Retrieval metrics
            top1, hitk, hits, recall, mrr, ndcg = compute_group_rank_metrics(returned_ids, row["expected_groups"], args.top_k)
            id_top1, id_hitk, id_hits, id_recall, id_mrr, id_ndcg = compute_rank_metrics(returned_ids, row["expected_ids"], args.top_k)
            acc.queries += 1
            acc.top1_hit += top1
            acc.hit_at_k += hitk
            acc.total_hits += hits
            acc.total_relevant += len(row["expected_groups"])
            acc.recall_sum += recall
            acc.mrr_sum += mrr
            acc.ndcg_sum += ndcg
            acc.id_top1_hit += id_top1
            acc.id_hit_at_k += id_hitk
            acc.id_total_hits += id_hits
            acc.id_total_relevant += len(row["expected_ids"])
            acc.id_recall_sum += id_recall
            acc.id_mrr_sum += id_mrr
            acc.id_ndcg_sum += id_ndcg
            acc.expected_groups_total += len(row["expected_groups"])
            acc.expected_group_items_total += sum(len(group) for group in row["expected_groups"])
            if top1_text:
                acc.top1_text_counts[top1_text.lower()] += 1

            ref = row["reference_answer"]
            ref_norm = normalize_answer_for_scoring(ref, row["query"])
            # Extractive proxies
            top1_norm = normalize_answer_for_scoring(top1_text, row["query"])
            concat3_norm = normalize_answer_for_scoring(concat3_text, row["query"])
            oracle_norm = normalize_answer_for_scoring(oracle_text, row["query"])
            acc.f1_top1_sum += token_f1(top1_norm, ref_norm)
            acc.bleu1_top1_sum += bleu1(top1_norm, ref_norm)
            acc.f1_concat3_sum += token_f1(concat3_norm, ref_norm)
            acc.bleu1_concat3_sum += bleu1(concat3_norm, ref_norm)
            acc.f1_oracle_sentence_sum += token_f1(oracle_norm, ref_norm)
            acc.bleu1_oracle_sentence_sum += bleu1(oracle_norm, ref_norm)

            open_domain_query = str(row.get("category")).strip() == "3"
            open_domain_binary = open_domain_query and (
                is_booleanish_query(row["query"]) or bool(extract_question_alternatives(row["query"]))
            )
            answer_top_docs = max(1, args.answer_top_docs)
            evidence_max_lines = max(1, args.evidence_max_lines)
            evidence_candidate_lines = evidence_max_lines
            if open_domain_query:
                answer_top_docs = max(answer_top_docs, 16)
                evidence_max_lines = max(evidence_max_lines, 12)
                evidence_candidate_lines = max(evidence_max_lines * 2, 18)

            base_contexts = select_answer_contexts(
                row["query"],
                returned_contents,
                answer_top_docs,
                open_domain=open_domain_query,
            )
            contexts = expand_context_with_neighbors(
                selected_contents=base_contexts,
                ordered_by_session=ordered_by_session,
                by_dialog_id=by_dialog_id,
                tenant_id=row["tenant_id"],
                window=max(0, args.context_neighbor_window),
                max_context_items=max(1, args.context_max_items),
            )
            evidence = select_evidence_contexts(
                row["query"],
                contexts,
                evidence_candidate_lines,
                open_domain=open_domain_query,
            )
            if open_domain_query and args.open_domain_llm_evidence_select:
                evidence = select_open_domain_evidence(row["query"], evidence, evidence_max_lines)

            extractive_candidates = collect_extractive_candidates(
                row["query"],
                evidence,
                max_candidates=6,
                open_domain=open_domain_query,
            )
            candidate_answers = [ans for _, ans, _ in extractive_candidates]
            if open_domain_query and not open_domain_binary:
                llm_candidates = build_open_domain_llm_candidates(row["query"], evidence, max_candidates=5)
                merged_candidates: list[str] = []
                seen_candidate_keys: set[str] = set()
                for value in llm_candidates + candidate_answers:
                    item = " ".join(str(value or "").split()).strip()
                    key = item.lower()
                    if not item or key in seen_candidate_keys:
                        continue
                    seen_candidate_keys.add(key)
                    merged_candidates.append(item)
                    if len(merged_candidates) >= 8:
                        break
                candidate_answers = merged_candidates
            open_domain_candidates = build_open_domain_candidates(row["query"], candidate_answers)
            extractive_ans, extractive_conf, extractive_sentence = extractive_answer(
                row["query"],
                evidence,
                open_domain=open_domain_query,
            )
            temporal_query, _, _ = classify_query(row["query"])
            generator_answer = "Unknown"
            gen_answer = extractive_ans
            answer_path = "extractive"
            allow_inference = allow_inference_generation(row["query"])

            if args.answer_mode == "generate":
                if open_domain_query:
                    if open_domain_binary:
                        ok, generator_answer, verification = verify_open_domain_answer(
                            row["query"],
                            evidence,
                            open_domain_candidates,
                            extractive_ans,
                            extractive_conf,
                        )
                    else:
                        ok, generator_answer, verification = resolve_open_domain_answer(
                            row["query"],
                            evidence,
                            open_domain_candidates,
                            extractive_ans,
                            extractive_conf,
                        )
                else:
                    prompt = build_generation_prompt(
                        row["query"],
                        evidence,
                        candidate_answers=candidate_answers,
                        allow_inference=allow_inference,
                    )
                    ok, generator_answer = generate_answer(prompt)
                if not ok:
                    acc.generation_failures += 1
                elif not is_unknown_answer(generator_answer):
                    generator_answer = snap_generated_answer_to_candidates(
                        row["query"],
                        generator_answer,
                        open_domain_candidates if open_domain_query else candidate_answers,
                        extractive_ans,
                        extractive_conf,
                        open_domain_query,
                    )
                if (not ok or is_unknown_answer(generator_answer)) and not is_unknown_answer(extractive_ans):
                    if open_domain_query and not open_domain_extract_is_safe_fallback(row["query"], extractive_ans, extractive_conf):
                        gen_answer = generator_answer if not is_unknown_answer(generator_answer) else "Unknown"
                        answer_path = "open_domain_unknown_generate"
                    else:
                        gen_answer = extractive_ans
                        answer_path = "extractive_fallback_generate"
                else:
                    gen_answer = generator_answer
                    answer_path = "generator_only"
            elif args.answer_mode == "hybrid":
                use_extractive = extractive_conf >= args.extractive_confidence_threshold and not is_unknown_answer(extractive_ans)
                if temporal_query:
                    temporal_has_signal = has_temporal_signal(extractive_sentence) or has_temporal_signal(extractive_ans)
                    temporal_threshold = max(args.extractive_confidence_threshold, 0.60)
                    if not temporal_has_signal or extractive_conf < temporal_threshold:
                        use_extractive = False
                    elif args.prefer_extractive_for_temporal and not is_unknown_answer(extractive_ans):
                        use_extractive = True
                if use_extractive:
                    gen_answer = extractive_ans
                    answer_path = "extractive_primary"
                else:
                    if open_domain_query:
                        if open_domain_binary:
                            ok, generator_answer, verification = verify_open_domain_answer(
                                row["query"],
                                evidence,
                                open_domain_candidates,
                                extractive_ans,
                                extractive_conf,
                            )
                        else:
                            ok, generator_answer, verification = resolve_open_domain_answer(
                                row["query"],
                                evidence,
                                open_domain_candidates,
                                extractive_ans,
                                extractive_conf,
                            )
                    else:
                        prompt = build_generation_prompt(
                            row["query"],
                            evidence,
                            candidate_answers=candidate_answers,
                            allow_inference=allow_inference,
                        )
                        ok, generator_answer = generate_answer(prompt)
                    if not ok:
                        acc.generation_failures += 1
                    elif not is_unknown_answer(generator_answer):
                        generator_answer = snap_generated_answer_to_candidates(
                            row["query"],
                            generator_answer,
                            open_domain_candidates if open_domain_query else candidate_answers,
                            extractive_ans,
                            extractive_conf,
                            open_domain_query,
                        )
                    if is_unknown_answer(generator_answer) and not is_unknown_answer(extractive_ans):
                        if open_domain_query and not open_domain_extract_is_safe_fallback(row["query"], extractive_ans, extractive_conf):
                            gen_answer = "Unknown"
                            answer_path = "open_domain_unknown_fallback"
                        else:
                            gen_answer = extractive_ans
                            answer_path = "extractive_fallback"
                    else:
                        gen_answer = generator_answer
                        answer_path = "generator_fallback"

            extractive_norm = normalize_answer_for_scoring(extractive_ans, row["query"])
            generated_norm = normalize_answer_for_scoring(gen_answer, row["query"])
            f1_gen = token_f1(generated_norm, ref_norm)
            bleu_gen = bleu1(generated_norm, ref_norm)
            acc.f1_generated_sum += f1_gen
            acc.bleu1_generated_sum += bleu_gen
            acc.em_generated_sum += normalized_exact_match(generated_norm, ref_norm)
            acc.em_extractive_sum += normalized_exact_match(extractive_norm, ref_norm)
            acc.f1_generated_no_stopwords_sum += token_f1_no_stopwords(generated_norm, ref_norm)
            acc.add_answer_path(answer_path)
            acc.add_category_generated(row.get("category"), f1_gen, bleu_gen)

            if tracef:
                expected_id_set = set(row["expected_ids"])
                ranked_preview: list[dict[str, Any]] = []
                for rank, mid in enumerate(returned_ids[: max(1, args.trace_top_k)], start=1):
                    content = content_by_id.get(mid, "")
                    ranked_preview.append(
                        {
                            "rank": rank,
                            "memory_id": mid,
                            "is_expected": mid in expected_id_set,
                            "rrf_score": fused_score.get(mid, 0.0),
                            "normalized_content": normalize_context_line(content),
                        }
                    )
                hit_ranks = [
                    rank
                    for rank, mid in enumerate(returned_ids, start=1)
                    if mid in expected_id_set
                ]
                trace_row = {
                    "query_index": i,
                    "tenant_id": row["tenant_id"],
                    "query": row["query"],
                    "category": row.get("category"),
                    "category_label": row.get("category_label"),
                    "reference_answer": ref,
                    "expected_fixture_indexes": row.get("expected_fixture_indexes", []),
                    "expected_memory_ids": sorted(list(expected_id_set)),
                    "expected_group_count": len(row.get("expected_groups", [])),
                    "expected_group_sizes": [len(g) for g in row.get("expected_groups", [])],
                    "query_variants": query_variants,
                    "route_calls": route_calls,
                    "two_pass_performed": pass2_performed,
                    "two_pass_anchor": pass2_anchor,
                    "two_pass_trigger": pass2_trigger,
                    "returned_ids_topk": returned_ids[: max(1, args.trace_top_k)],
                    "hit_ranks": hit_ranks,
                    "top1_text": top1_text,
                    "concat3_text": concat3_text,
                    "oracle_text": oracle_text,
                    "candidate_answers": candidate_answers,
                    "open_domain_candidates": open_domain_candidates,
                    "evidence_lines": evidence,
                    "extractive_answer": extractive_ans,
                    "extractive_confidence": extractive_conf,
                    "extractive_sentence": extractive_sentence,
                    "generator_answer": generator_answer,
                    "answer_path": answer_path,
                    "generated_answer": gen_answer,
                    "f1_generated": f1_gen,
                    "bleu1_generated": bleu_gen,
                    "ranked_preview": ranked_preview,
                }
                tracef.write(json.dumps(trace_row, ensure_ascii=False) + "\n")
                tracef.flush()

            if i % 50 == 0 or i == len(eval_rows):
                print(f"evaluated {i}/{len(eval_rows)}", flush=True)

        if acc.queries == 0:
            raise SystemExit("no eval queries completed")

        top1_unique_rate = (len(acc.top1_text_counts) / acc.queries) if acc.queries else 0.0
        top1_most_common_share = (max(acc.top1_text_counts.values()) / acc.queries) if acc.top1_text_counts and acc.queries else 0.0
        avg_expected_group_size = (
            acc.expected_group_items_total / acc.expected_groups_total if acc.expected_groups_total else 0.0
        )
        answer_path_counts = dict(sorted(acc.answer_path_counts.items(), key=lambda kv: kv[0]))
        answer_path_rates = {
            key: (value / acc.queries if acc.queries else 0.0)
            for key, value in answer_path_counts.items()
        }

        result = {
            "timestamp_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "run_stamp": run_stamp,
            "fixture": args.fixture,
            "eval_set": args.eval_set,
            "config_fingerprint": config_fingerprint,
            "vector_backend": args.vector_backend,
            "embedding_provider": args.embedding_provider,
            "embedding_model": args.embedding_model,
            "importance_scorer": args.importance_scorer,
            "openrouter": {
                "base_url": args.openrouter_base_url,
                "embedding_model": args.openrouter_embedding_model,
                "scoring_model": args.openrouter_scoring_model,
                "timeout_ms": args.openrouter_timeout_ms,
            },
            "qdrant": {
                "base_url": args.qdrant_url,
                "collection": args.qdrant_collection,
                "timeout_ms": args.qdrant_timeout_ms,
            },
            "eval_workers": args.eval_workers,
            "top_k": args.top_k,
            "answer_mode": args.answer_mode,
            "answer_provider": args.answer_provider,
            "answer_model": resolved_answer_model,
            "answer_top_docs": args.answer_top_docs,
            "extractive_confidence_threshold": args.extractive_confidence_threshold,
            "prefer_extractive_for_temporal": args.prefer_extractive_for_temporal,
            "retrieval_query_variants": args.retrieval_query_variants,
            "retrieval_rrf_k": args.retrieval_rrf_k,
            "retrieval_kind_routing": args.retrieval_kind_routing,
            "entity_fact_backend": args.entity_fact_backend,
            "multihop_config": {
                "entity_fact_bridge_enabled": args.multihop_entity_fact_bridge_enabled,
                "llm_decomposition_enabled": args.multihop_llm_decomposition_enabled,
                "decomposition_provider": args.multihop_decomposition_provider,
                "openrouter_model": args.multihop_openrouter_model,
                "ollama_url": args.multihop_ollama_url,
                "ollama_model": args.multihop_ollama_model,
                "ollama_timeout_ms": args.multihop_ollama_timeout_ms,
                "max_decomposition_queries": args.multihop_max_decomposition_queries,
                "enable_pairwise_rerank": args.multihop_enable_pairwise_rerank,
                "token_expansion_fallback": args.multihop_token_expansion_fallback,
            },
            "temporal_route_raw_turn": args.temporal_route_raw_turn,
            "context_neighbor_window": args.context_neighbor_window,
            "context_max_items": args.context_max_items,
            "evidence_max_lines": args.evidence_max_lines,
            "open_domain_llm_evidence_select": args.open_domain_llm_evidence_select,
            "open_domain_hyde": args.open_domain_hyde,
            "profile_layer": {
                "enabled": args.profile_layer_enabled,
                "mode": args.profile_layer_mode,
                "provider": args.profile_layer_provider,
                "model": args.profile_layer_openrouter_model if args.profile_layer_provider == "openrouter" else args.profile_layer_ollama_model,
                "timeout_seconds": args.profile_layer_timeout_seconds,
                "max_source_lines": args.profile_layer_max_source_lines,
                "max_summary_lines": args.profile_layer_max_summary_lines,
                "workers": args.profile_layer_workers,
                "stored_profiles": profile_memory_count,
                "stored_facets": profile_facet_count,
                "generated_entities": profile_entities_count,
            },
            "structured_memory_enabled": args.structured_memory_enabled,
            "structured_dual_write_observations": args.structured_dual_write_observations,
            "structured_dual_write_events": args.structured_dual_write_events,
            "structured_query_routing_enabled": args.structured_query_routing_enabled,
            "parser_config": {
                "enabled": args.parser_enabled,
                "provider": args.parser_provider,
                "store_raw_turn": args.parser_store_raw_turn,
                "max_facts": args.parser_max_facts,
                "dedupe_threshold": args.parser_dedupe_threshold,
                "update_threshold": args.parser_update_threshold,
                "ollama_url": args.parser_ollama_url,
                "ollama_model": args.parser_ollama_model,
                "openrouter_model": args.parser_openrouter_model,
                "ollama_timeout_ms": args.parser_ollama_timeout_ms,
            },
            "trace_jsonl": str(trace_path) if trace_path else "",
            "db_path": str(db),
            "index_map_path": str(index_map_file) if index_map_file else "",
            "index_map_schema_version": index_map_schema,
            "index_map_fingerprint": stored_index_fingerprint,
            "reuse_existing_store": reuse_store,
            "store_diagnostics": {
                "mode": store_mode,
                "batch_endpoint_supported": store_batch_supported,
                "batch_size": store_batch_size if store_batch_supported else 1,
                "batch_fallbacks": store_batch_fallbacks,
                "single_write_calls": store_single_writes,
                "batch_timeout_seconds": store_batch_timeout_s,
                "single_timeout_seconds": store_single_timeout_s,
            },
            "eval_cases": len(eval_rows),
            "eval_success": acc.queries,
            "eval_failures": acc.query_failures,
            "generation_failures": acc.generation_failures,
            "retrieval_metrics": {
                "top1_hit_rate": acc.avg(acc.top1_hit),
                "hit_rate_at_k": acc.avg(acc.hit_at_k),
                "recall_at_k": acc.avg(acc.recall_sum),
                "mrr": acc.avg(acc.mrr_sum),
                "ndcg_at_k": acc.avg(acc.ndcg_sum),
                "micro_recall_at_k": (acc.total_hits / acc.total_relevant) if acc.total_relevant else 0.0,
                "total_hits": acc.total_hits,
                "total_relevant": acc.total_relevant,
            },
            "retrieval_metrics_id_level": {
                "top1_hit_rate": acc.avg(acc.id_top1_hit),
                "hit_rate_at_k": acc.avg(acc.id_hit_at_k),
                "recall_at_k": acc.avg(acc.id_recall_sum),
                "mrr": acc.avg(acc.id_mrr_sum),
                "ndcg_at_k": acc.avg(acc.id_ndcg_sum),
                "micro_recall_at_k": (acc.id_total_hits / acc.id_total_relevant) if acc.id_total_relevant else 0.0,
                "total_hits": acc.id_total_hits,
                "total_relevant": acc.id_total_relevant,
            },
            "qa_metrics": {
                "f1_generated": acc.avg(acc.f1_generated_sum),
                "bleu1_generated": acc.avg(acc.bleu1_generated_sum),
                "f1_top1": acc.avg(acc.f1_top1_sum),
                "bleu1_top1": acc.avg(acc.bleu1_top1_sum),
                "f1_concat3": acc.avg(acc.f1_concat3_sum),
                "bleu1_concat3": acc.avg(acc.bleu1_concat3_sum),
                "f1_oracle_sentence_topk": acc.avg(acc.f1_oracle_sentence_sum),
                "bleu1_oracle_sentence_topk": acc.avg(acc.bleu1_oracle_sentence_sum),
            },
            "qa_metrics_paper_scale": {
                "f1_generated": acc.avg(acc.f1_generated_sum) * 100.0,
                "bleu1_generated": acc.avg(acc.bleu1_generated_sum) * 100.0,
            },
            "qa_metrics_companion": {
                "em_generated_normalized": acc.avg(acc.em_generated_sum),
                "em_extractive_normalized": acc.avg(acc.em_extractive_sum),
                "f1_generated_no_stopwords": acc.avg(acc.f1_generated_no_stopwords_sum),
                "oracle_gap_f1": acc.avg(acc.f1_oracle_sentence_sum) - acc.avg(acc.f1_generated_sum),
            },
            "answer_path_metrics": {
                "counts": answer_path_counts,
                "rates": answer_path_rates,
            },
            "retrieval_diagnostics": {
                "top1_unique_rate": top1_unique_rate,
                "top1_most_common_share": top1_most_common_share,
                "avg_ids_per_expected_group": avg_expected_group_size,
                "expected_group_count_total": acc.expected_groups_total,
            },
            "category_metrics_generated": {
                category_label(cat): {
                    "category_id": cat,
                    "count": m.count,
                    "f1": m.mean_f1(),
                    "bleu1": m.mean_bleu(),
                    "f1_paper_scale": m.mean_f1() * 100.0,
                    "bleu1_paper_scale": m.mean_bleu() * 100.0,
                }
                for cat, m in sorted(acc.by_category_generated.items(), key=lambda kv: category_sort_key(kv[0]))
            },
        }

        out_json.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")

        lines = [
            "LOCOMO QA Metric Summary (Paper-aligned Lite, No LLM Judge)",
            "============================================================",
            f"Vector backend   : {args.vector_backend}",
            f"Provider         : {args.embedding_provider}",
            f"Embed model      : {args.embedding_model}",
            f"Importance scorer: {args.importance_scorer}",
            f"OpenRouter embed : {args.openrouter_embedding_model}",
            f"OpenRouter score : {args.openrouter_scoring_model}",
            f"Answer mode      : {args.answer_mode}",
            f"Answer provider  : {args.answer_provider}",
            f"Answer model     : {resolved_answer_model if resolved_answer_model else '(extractive)'}",
            f"Extractive thr   : {args.extractive_confidence_threshold:.2f}",
            f"Temporal prefer  : {'on' if args.prefer_extractive_for_temporal else 'off'}",
            f"Kind routing     : {'on' if args.retrieval_kind_routing else 'off'}",
            f"Open-domain sel  : {'on' if args.open_domain_llm_evidence_select else 'off'}",
            f"Open-domain qrw  : {'on' if args.open_domain_query_rewrite else 'off'} ({args.open_domain_query_rewrite_count})",
            f"Open-domain hyde : {'on' if args.open_domain_hyde else 'off'}",
            f"Open-domain prof : {'on' if args.open_domain_profile_route else 'off'}",
            f"Profile layer    : {'on' if args.profile_layer_enabled else 'off'} ({args.profile_layer_mode}, {args.profile_layer_provider})",
            f"Profile model    : {args.profile_layer_openrouter_model if args.profile_layer_provider == 'openrouter' else args.profile_layer_ollama_model}",
            f"Profile stored   : {profile_memory_count} facets / {profile_entities_count} entities",
            f"Entity backend   : {args.entity_fact_backend}",
            f"Multi-hop bridge : {'on' if args.multihop_entity_fact_bridge_enabled else 'off'}",
            f"Multi-hop decomp : {'on' if args.multihop_llm_decomposition_enabled else 'off'}",
            f"Multi-hop prov   : {args.multihop_decomposition_provider}",
            f"Multi-hop model  : {args.multihop_openrouter_model if args.multihop_decomposition_provider == 'openrouter' else args.multihop_ollama_model}",
            f"Temporal raw_turn: {'on' if args.temporal_route_raw_turn else 'off'}",
            f"Structured memory: {'on' if args.structured_memory_enabled else 'off'}",
            f"Parser profile   : {'on' if args.parser_enabled else 'off'} ({args.parser_provider})",
            f"Eval workers     : {args.eval_workers}",
            f"Store mode       : {result['store_diagnostics']['mode']}",
            f"Store batch size : {result['store_diagnostics']['batch_size']}",
            f"Store fallbacks  : {result['store_diagnostics']['batch_fallbacks']}",
            f"Store batch t/o  : {result['store_diagnostics']['batch_timeout_seconds']:.1f}s",
            f"Store single t/o : {result['store_diagnostics']['single_timeout_seconds']:.1f}s",
            f"Run stamp        : {run_stamp}",
            f"Fixture          : {args.fixture}",
            f"Eval set         : {args.eval_set}",
            f"Top K            : {args.top_k}",
            f"Eval success/fail: {acc.queries}/{acc.query_failures}",
            f"Gen failures     : {acc.generation_failures}",
            "",
            "QA Metrics (overall)",
            "--------------------",
            f"F1 generated     : {result['qa_metrics']['f1_generated']:.6f} ({result['qa_metrics_paper_scale']['f1_generated']:.2f})",
            f"BLEU-1 generated : {result['qa_metrics']['bleu1_generated']:.6f} ({result['qa_metrics_paper_scale']['bleu1_generated']:.2f})",
            f"EM generated     : {result['qa_metrics_companion']['em_generated_normalized']:.6f}",
            f"F1 no-stopwords  : {result['qa_metrics_companion']['f1_generated_no_stopwords']:.6f}",
            "",
            "Retrieval Metrics",
            "-----------------",
            f"Recall@{args.top_k}       : {result['retrieval_metrics']['recall_at_k']:.6f}",
            f"nDCG@{args.top_k}         : {result['retrieval_metrics']['ndcg_at_k']:.6f}",
            f"MRR              : {result['retrieval_metrics']['mrr']:.6f}",
            f"Top1 unique rate : {result['retrieval_diagnostics']['top1_unique_rate']:.6f}",
            "",
            "Answer Path Distribution",
            "------------------------",
        ]
        for key, value in result["answer_path_metrics"]["counts"].items():
            rate = result["answer_path_metrics"]["rates"].get(key, 0.0)
            lines.append(f"{key}: count={value} rate={rate:.6f}")
        lines.extend(
            [
                "",
            "Generated QA by Category",
            "------------------------",
            ]
        )
        for cat, m in result["category_metrics_generated"].items():
            lines.append(
                f"{cat}: count={m['count']} F1={m['f1']:.6f} ({m['f1_paper_scale']:.2f}) "
                f"BLEU-1={m['bleu1']:.6f} ({m['bleu1_paper_scale']:.2f})"
            )
        final_score = (
            result["qa_metrics_paper_scale"]["f1_generated"]
            + result["qa_metrics_paper_scale"]["bleu1_generated"]
        ) / 2.0
        lines.extend(
            [
                "",
                f"Final score (avg F1/BLEU, paper scale): {final_score:.2f}",
                f"JSON result: {out_json}",
                f"Summary    : {out_summary}",
                f"Trace JSONL: {trace_path}" if trace_path else "Trace JSONL: (disabled)",
            ]
        )
        out_summary.write_text("\n".join(lines) + "\n", encoding="utf-8")

        print(f"JSON: {out_json}")
        print(f"Summary: {out_summary}")

    finally:
        proc.send_signal(signal.SIGTERM)
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
        if tracef:
            tracef.close()
        logf.close()
        tmpdir.cleanup()


if __name__ == "__main__":
    main()
