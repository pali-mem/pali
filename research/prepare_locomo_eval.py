#!/usr/bin/env python3
"""prepare_locomo_eval.py — convert LOCOMO to Pali fixture/eval-set formats."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


def _session_number(key: str) -> int:
    # session_12 -> 12 (ignore keys like session_12_date_time)
    if not key.startswith("session_") or key.endswith("_date_time"):
        return 10**9
    rest = key[len("session_") :]
    if not rest.isdigit():
        return 10**9
    return int(rest)


def _iter_dialogues(conversation: dict[str, Any]) -> list[tuple[str, str, str, str]]:
    out: list[tuple[str, str, str, str]] = []
    session_keys = [k for k in conversation.keys() if _session_number(k) != 10**9]
    session_keys.sort(key=_session_number)
    for session_key in session_keys:
        session_time = str(conversation.get(f"{session_key}_date_time", "")).strip()
        turns = conversation.get(session_key)
        if not isinstance(turns, list):
            continue
        for turn in turns:
            if not isinstance(turn, dict):
                continue
            dia_id = str(turn.get("dia_id", "")).strip()
            speaker = str(turn.get("speaker", "")).strip()
            text = str(turn.get("text", "")).strip()
            if not dia_id or not text:
                continue
            out.append((dia_id, speaker, text, session_time))
    return out


def build(
    locomo: list[dict[str, Any]],
    mode: str = "basic",
    sanitize_percent: bool = False,
) -> tuple[list[dict[str, Any]], list[dict[str, Any]], dict[str, Any]]:
    fixture: list[dict[str, Any]] = []
    eval_set: list[dict[str, Any]] = []
    skipped_no_evidence = 0
    skipped_missing_dialogues = 0

    for sample in locomo:
        sample_id = str(sample.get("sample_id", "")).strip()
        if not sample_id:
            continue
        tenant_id = f"locomo_{sample_id}"

        conversation = sample.get("conversation", {})
        if not isinstance(conversation, dict):
            continue

        speaker_a = str(conversation.get("speaker_a", "")).strip()
        speaker_b = str(conversation.get("speaker_b", "")).strip()

        dia_to_fixture_idx: dict[str, int] = {}
        for dia_id, speaker, text, session_time in _iter_dialogues(conversation):
            if mode == "paperlite":
                content = (
                    f"[sample:{sample_id}] [dialog:{dia_id}] [time:{session_time}] "
                    f"[speaker_a:{speaker_a}] [speaker_b:{speaker_b}] "
                    f"{speaker}: {text}"
                )
            else:
                content = f"{speaker}: {text}" if speaker else text
            if sanitize_percent:
                content = content.replace("%", " percent ")
            idx = len(fixture)
            dia_to_fixture_idx[dia_id] = idx
            fixture.append(
                {
                    "tenant_id": tenant_id,
                    "content": content,
                    "tags": ["locomo", mode],
                    "tier": "episodic",
                }
            )

        qa_items = sample.get("qa", [])
        if not isinstance(qa_items, list):
            continue
        for qa in qa_items:
            if not isinstance(qa, dict):
                continue
            question = str(qa.get("question", "")).strip()
            if not question:
                continue
            evidence = qa.get("evidence", [])
            if not isinstance(evidence, list) or not evidence:
                skipped_no_evidence += 1
                continue

            expected_indexes = sorted(
                {
                    dia_to_fixture_idx[eid]
                    for eid in evidence
                    if isinstance(eid, str) and eid in dia_to_fixture_idx
                }
            )
            if not expected_indexes:
                skipped_missing_dialogues += 1
                continue

            eval_set.append(
                {
                    "tenant_id": tenant_id,
                    "query": question,
                    "expected_fixture_indexes": expected_indexes,
                    "category": qa.get("category"),
                    "reference_answer": qa.get("answer", ""),
                }
            )

    stats = {
        "locomo_samples": len(locomo),
        "fixture_rows": len(fixture),
        "eval_rows": len(eval_set),
        "skipped_no_evidence": skipped_no_evidence,
        "skipped_missing_dialogues": skipped_missing_dialogues,
        "mode": mode,
        "sanitize_percent": sanitize_percent,
    }
    return fixture, eval_set, stats


def main() -> None:
    parser = argparse.ArgumentParser(description="Convert LOCOMO JSON to Pali fixture/eval JSON.")
    parser.add_argument("--locomo-json", required=True, help="Path to locomo10.json (or full LOCOMO split)")
    parser.add_argument("--fixture-out", required=True, help="Output fixture JSON path")
    parser.add_argument("--eval-out", required=True, help="Output eval-set JSON path")
    parser.add_argument("--stats-out", required=True, help="Output stats JSON path")
    parser.add_argument(
        "--mode",
        choices=["basic", "paperlite"],
        default="basic",
        help="Conversion mode (default: basic)",
    )
    parser.add_argument(
        "--sanitize-percent",
        action="store_true",
        help="Replace %% with 'percent' in fixture content",
    )
    parser.add_argument(
        "--max-conversations",
        type=int,
        default=0,
        help="Limit to first N conversations (0 = all). Use for fast dev-loop runs.",
    )
    args = parser.parse_args()

    locomo_path = Path(args.locomo_json)
    if not locomo_path.exists():
        raise SystemExit(f"ERROR: LOCOMO file not found: {locomo_path}")

    data = json.loads(locomo_path.read_text(encoding="utf-8"))
    if not isinstance(data, list):
        raise SystemExit("ERROR: expected LOCOMO file to be a JSON array")

    if args.max_conversations and args.max_conversations > 0:
        data = data[: args.max_conversations]
        print(f"[mini] limited to {len(data)} conversations")

    fixture, eval_set, stats = build(data, mode=args.mode, sanitize_percent=args.sanitize_percent)

    fixture_path = Path(args.fixture_out)
    eval_path = Path(args.eval_out)
    stats_path = Path(args.stats_out)
    fixture_path.parent.mkdir(parents=True, exist_ok=True)
    eval_path.parent.mkdir(parents=True, exist_ok=True)
    stats_path.parent.mkdir(parents=True, exist_ok=True)

    fixture_path.write_text(json.dumps(fixture, indent=2) + "\n", encoding="utf-8")
    eval_path.write_text(json.dumps(eval_set, indent=2) + "\n", encoding="utf-8")
    stats_path.write_text(json.dumps(stats, indent=2) + "\n", encoding="utf-8")

    print(f"Wrote fixture: {fixture_path} ({stats['fixture_rows']} rows)")
    print(f"Wrote eval set: {eval_path} ({stats['eval_rows']} rows)")
    print(f"Wrote stats:   {stats_path}")


if __name__ == "__main__":
    main()
