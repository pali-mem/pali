#!/usr/bin/env python3
"""benchmark_suite.py

Config-driven benchmark orchestrator for Pali.

It composes existing benchmark entrypoints into a modular suite:
- scripts/benchmark.sh (ingest + search API speed)
- scripts/retrieval_quality.sh (retrieval quality metrics)
- research/eval_locomo_f1_bleu.py (optional LoCoMo score lane)

Configuration is JSON to avoid extra dependencies.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import shutil
import subprocess
import sys
import threading
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any


RUNNER_BENCHMARK = "benchmark"
RUNNER_RETRIEVAL = "retrieval_quality"
RUNNER_LOCOMO = "locomo_qa"
RUNNER_TYPES = {RUNNER_BENCHMARK, RUNNER_RETRIEVAL, RUNNER_LOCOMO}

DEFAULT_OUT_ROOT = "test/benchmarks/results/suites"
DEFAULT_P_WEIGHTS = {"p50": 0.2, "p95": 0.5, "p99": 0.3}
PROGRESS_HEARTBEAT_SECONDS = 20


@dataclass
class ScenarioResult:
    scenario_id: str
    name: str
    runner: str
    enabled: bool
    status: str
    duration_seconds: float = 0.0
    command: list[str] = field(default_factory=list)
    result_json: str = ""
    metrics: dict[str, Any] = field(default_factory=dict)
    scoring: dict[str, Any] = field(default_factory=dict)
    logs: dict[str, str] = field(default_factory=dict)
    error: str = ""


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run config-driven benchmark suites.")
    parser.add_argument(
        "--config",
        required=True,
        help="Path to suite JSON config.",
    )
    parser.add_argument(
        "--only",
        default="",
        help="Comma-separated scenario ids to run (optional).",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print resolved commands without executing them.",
    )
    return parser.parse_args()


def utc_now_iso() -> str:
    return dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def utc_run_id() -> str:
    return dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def slugify(text: str) -> str:
    out = []
    for ch in text.lower():
        if ch.isalnum():
            out.append(ch)
        elif ch in {"-", "_"}:
            out.append("-")
        elif ch.isspace():
            out.append("-")
    slug = "".join(out).strip("-")
    return slug or "suite"


def read_json(path: Path) -> dict[str, Any]:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise SystemExit(f"ERROR: invalid JSON in {path}: {exc}") from exc


def merge_args(common: dict[str, Any], local: dict[str, Any]) -> dict[str, Any]:
    merged = dict(common)
    for key, value in local.items():
        merged[key] = value
    return merged


def to_cli_flags(args_map: dict[str, Any]) -> list[str]:
    flags: list[str] = []
    for key, value in args_map.items():
        if value is None:
            continue
        flag = "--" + key.replace("_", "-")
        if isinstance(value, list):
            for item in value:
                flags.extend([flag, str(item)])
            continue
        if isinstance(value, bool):
            flags.extend([flag, "true" if value else "false"])
            continue
        flags.extend([flag, str(value)])
    return flags


def flatten_get(data: dict[str, Any], path: str) -> Any:
    cursor: Any = data
    for part in path.split("."):
        if not isinstance(cursor, dict):
            return None
        if part not in cursor:
            return None
        cursor = cursor[part]
    return cursor


def safe_float(value: Any) -> float | None:
    if value is None:
        return None
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def percentile_score(latencies: dict[str, float], slo_ms: dict[str, float], weights: dict[str, float]) -> float | None:
    weighted_sum = 0.0
    weight_total = 0.0
    for pct, weight in weights.items():
        target = safe_float(slo_ms.get(pct))
        actual = safe_float(latencies.get(pct))
        if target is None or actual is None or actual <= 0:
            continue
        score = min(1.0, target / actual)
        weighted_sum += score * weight
        weight_total += weight
    if weight_total <= 0:
        return None
    return round((weighted_sum / weight_total) * 100.0, 2)


def throughput_score(actual: float | None, target: float | None) -> float | None:
    if actual is None or target is None or target <= 0:
        return None
    return round(min(1.0, actual / target) * 100.0, 2)


def compute_scoring(metrics: dict[str, Any], scoring_cfg: dict[str, Any]) -> dict[str, Any]:
    if not scoring_cfg:
        return {}

    output: dict[str, Any] = {}
    latency_cfg = scoring_cfg.get("latency_slo_ms", {})
    throughput_cfg = scoring_cfg.get("throughput_targets_ops_sec", {})
    weight_cfg = scoring_cfg.get("weights", {})
    pct_weights = scoring_cfg.get("percentile_weights", DEFAULT_P_WEIGHTS)

    search_p = percentile_score(
        {
            "p50": safe_float(metrics.get("search_p50_ms")) or 0.0,
            "p95": safe_float(metrics.get("search_p95_ms")) or 0.0,
            "p99": safe_float(metrics.get("search_p99_ms")) or 0.0,
        },
        latency_cfg.get("search", {}),
        pct_weights,
    )
    store_p = percentile_score(
        {
            "p50": safe_float(metrics.get("store_p50_ms")) or 0.0,
            "p95": safe_float(metrics.get("store_p95_ms")) or 0.0,
            "p99": safe_float(metrics.get("store_p99_ms")) or 0.0,
        },
        latency_cfg.get("store", {}),
        pct_weights,
    )
    search_t = throughput_score(
        safe_float(metrics.get("search_ops_sec")),
        safe_float(throughput_cfg.get("search")),
    )
    store_t = throughput_score(
        safe_float(metrics.get("store_ops_sec")),
        safe_float(throughput_cfg.get("store")),
    )

    if search_p is not None:
        output["search_p_score"] = search_p
    if store_p is not None:
        output["store_p_score"] = store_p
    if search_t is not None:
        output["search_throughput_score"] = search_t
    if store_t is not None:
        output["store_throughput_score"] = store_t

    latency_scores = [s for s in (search_p, store_p) if s is not None]
    throughput_scores = [s for s in (search_t, store_t) if s is not None]
    latency_avg = round(sum(latency_scores) / len(latency_scores), 2) if latency_scores else None
    throughput_avg = round(sum(throughput_scores) / len(throughput_scores), 2) if throughput_scores else None
    if latency_avg is not None:
        output["latency_score"] = latency_avg
    if throughput_avg is not None:
        output["throughput_score"] = throughput_avg

    lat_w = safe_float(weight_cfg.get("latency")) or 0.7
    thr_w = safe_float(weight_cfg.get("throughput")) or 0.3
    if latency_avg is not None and throughput_avg is not None:
        denom = lat_w + thr_w if (lat_w + thr_w) > 0 else 1.0
        output["performance_score"] = round(((latency_avg * lat_w) + (throughput_avg * thr_w)) / denom, 2)
    elif latency_avg is not None:
        output["performance_score"] = latency_avg
    elif throughput_avg is not None:
        output["performance_score"] = throughput_avg

    return output


def benchmark_metrics(payload: dict[str, Any]) -> dict[str, Any]:
    store = payload.get("store", {})
    search = payload.get("search", {})
    store_lat = store.get("latency_ms", {})
    search_lat = search.get("latency_ms", {})
    return {
        "backend": payload.get("backend", ""),
        "embedding_provider": payload.get("embedding_provider", ""),
        "fixture": payload.get("fixture", ""),
        "eval_set": payload.get("eval_set", ""),
        "config_profile": payload.get("config_profile", ""),
        "store_ops_sec": safe_float(store.get("throughput_ops_sec")),
        "search_ops_sec": safe_float(search.get("throughput_ops_sec")),
        "store_p50_ms": safe_float(store_lat.get("p50")),
        "store_p95_ms": safe_float(store_lat.get("p95")),
        "store_p99_ms": safe_float(store_lat.get("p99")),
        "search_p50_ms": safe_float(search_lat.get("p50")),
        "search_p95_ms": safe_float(search_lat.get("p95")),
        "search_p99_ms": safe_float(search_lat.get("p99")),
        "store_success": store.get("success"),
        "store_failures": store.get("failures"),
        "search_success": search.get("success"),
        "search_failures": search.get("failures"),
        "top_k": search.get("top_k"),
    }


def retrieval_metrics(payload: dict[str, Any]) -> dict[str, Any]:
    metrics = payload.get("metrics", {})
    return {
        "backend": payload.get("backend", ""),
        "embedding_provider": payload.get("embedding_provider", ""),
        "fixture": payload.get("fixture", ""),
        "eval_set": payload.get("eval_set", ""),
        "config_profile": payload.get("config_profile", ""),
        "top1_hit_rate": safe_float(metrics.get("top1_hit_rate")),
        "topk_accuracy": safe_float(metrics.get("topk_accuracy")),
        "recall_at_k": safe_float(metrics.get("recall_at_k")),
        "ndcg_at_k": safe_float(metrics.get("ndcg_at_k")),
        "mrr": safe_float(metrics.get("mrr")),
        "eval_success": payload.get("eval_success"),
        "eval_failures": payload.get("eval_failures"),
        "top_k": payload.get("top_k"),
    }


def locomo_metrics(payload: dict[str, Any]) -> dict[str, Any]:
    retrieval = payload.get("retrieval_metrics", {})
    qa = payload.get("qa_metrics_paper_scale", {})
    return {
        "backend": payload.get("vector_backend", ""),
        "embedding_provider": payload.get("embedding_provider", ""),
        "fixture": payload.get("fixture", ""),
        "eval_set": payload.get("eval_set", ""),
        "top_k": payload.get("top_k"),
        "retrieval_recall_at_k": safe_float(retrieval.get("recall_at_k")),
        "retrieval_ndcg_at_k": safe_float(retrieval.get("ndcg_at_k")),
        "retrieval_mrr": safe_float(retrieval.get("mrr")),
        "f1_generated_paper": safe_float(qa.get("f1_generated")),
        "bleu1_generated_paper": safe_float(qa.get("bleu1_generated")),
        "eval_success": payload.get("eval_success"),
        "eval_failures": payload.get("eval_failures"),
    }


def extract_metrics(runner: str, payload: dict[str, Any]) -> dict[str, Any]:
    if runner == RUNNER_BENCHMARK:
        return benchmark_metrics(payload)
    if runner == RUNNER_RETRIEVAL:
        return retrieval_metrics(payload)
    if runner == RUNNER_LOCOMO:
        return locomo_metrics(payload)
    return {}


def resolve_metric(result: ScenarioResult, metric: str) -> float | None:
    if metric.startswith("scoring."):
        return safe_float(flatten_get(result.scoring, metric[len("scoring."):]))
    return safe_float(flatten_get(result.metrics, metric))


def build_comparisons(comparison_defs: list[dict[str, Any]], results: list[ScenarioResult]) -> list[dict[str, Any]]:
    by_id = {r.scenario_id: r for r in results}
    out: list[dict[str, Any]] = []
    for comp in comparison_defs:
        comp_id = str(comp.get("id", "comparison"))
        baseline_id = str(comp.get("baseline", ""))
        candidate_id = str(comp.get("candidate", ""))
        metric_list = comp.get("metrics", [])
        if not baseline_id or not candidate_id or not isinstance(metric_list, list):
            out.append(
                {
                    "id": comp_id,
                    "status": "invalid",
                    "error": "comparison requires baseline, candidate, and metrics[]",
                }
            )
            continue
        base = by_id.get(baseline_id)
        cand = by_id.get(candidate_id)
        if base is None or cand is None:
            out.append(
                {
                    "id": comp_id,
                    "status": "invalid",
                    "error": "baseline/candidate scenario not found",
                    "baseline": baseline_id,
                    "candidate": candidate_id,
                }
            )
            continue
        if base.status != "ok" or cand.status != "ok":
            out.append(
                {
                    "id": comp_id,
                    "status": "skipped",
                    "baseline": baseline_id,
                    "candidate": candidate_id,
                    "reason": "one or both scenarios not successful",
                }
            )
            continue

        metric_deltas: list[dict[str, Any]] = []
        for metric in metric_list:
            metric_name = str(metric)
            base_value = resolve_metric(base, metric_name)
            cand_value = resolve_metric(cand, metric_name)
            if base_value is None or cand_value is None:
                metric_deltas.append(
                    {
                        "metric": metric_name,
                        "baseline": base_value,
                        "candidate": cand_value,
                        "delta": None,
                        "delta_pct": None,
                    }
                )
                continue
            delta = cand_value - base_value
            delta_pct = None if base_value == 0 else (delta / base_value) * 100.0
            metric_deltas.append(
                {
                    "metric": metric_name,
                    "baseline": round(base_value, 6),
                    "candidate": round(cand_value, 6),
                    "delta": round(delta, 6),
                    "delta_pct": None if delta_pct is None else round(delta_pct, 4),
                }
            )

        context_warnings: list[str] = []
        for field in ("fixture", "eval_set", "top_k"):
            b = base.metrics.get(field)
            c = cand.metrics.get(field)
            if b and c and b != c:
                context_warnings.append(f"mismatched {field}: baseline={b} candidate={c}")

        out.append(
            {
                "id": comp_id,
                "status": "ok",
                "baseline": baseline_id,
                "candidate": candidate_id,
                "warnings": context_warnings,
                "metrics": metric_deltas,
            }
        )
    return out


def build_command(repo_root: Path, runner: str, args_map: dict[str, Any]) -> list[str]:
    flags = to_cli_flags(args_map)
    if runner == RUNNER_BENCHMARK:
        bash = resolve_bash()
        if not bash:
            raise RuntimeError("bash not found in PATH; required for scripts/benchmark.sh")
        return [bash, str(repo_root / "scripts" / "benchmark.sh"), *flags]
    if runner == RUNNER_RETRIEVAL:
        bash = resolve_bash()
        if not bash:
            raise RuntimeError("bash not found in PATH; required for scripts/retrieval_quality.sh")
        return [bash, str(repo_root / "scripts" / "retrieval_quality.sh"), *flags]
    if runner == RUNNER_LOCOMO:
        return [sys.executable, str(repo_root / "research" / "eval_locomo_f1_bleu.py"), *flags]
    raise RuntimeError(f"unsupported runner: {runner}")


def resolve_bash() -> str | None:
    # Prefer Git Bash on Windows because system32\bash.exe targets WSL.
    preferred: list[str] = []
    if sys.platform.startswith("win"):
        preferred.extend(
            [
                r"C:\Program Files\Git\bin\bash.exe",
                r"C:\Program Files\Git\usr\bin\bash.exe",
            ]
        )

    found = shutil.which("bash")
    if found:
        low = found.lower()
        if sys.platform.startswith("win") and low.endswith(r"\windows\system32\bash.exe"):
            pass
        else:
            return found

    for path in preferred:
        p = Path(path)
        if p.exists():
            return str(p)
    return found


def find_result_json(runner: str, args_map: dict[str, Any], before: set[Path], out_hint: Path) -> Path:
    if runner == RUNNER_BENCHMARK:
        target_name = "benchmark.json"
    elif runner == RUNNER_RETRIEVAL:
        target_name = "retrieval_quality.json"
    elif runner == RUNNER_LOCOMO:
        explicit = args_map.get("out_json")
        if explicit:
            p = Path(str(explicit))
            if p.exists():
                return p
        target_name = ""
    else:
        raise RuntimeError(f"unsupported runner: {runner}")

    if runner in {RUNNER_BENCHMARK, RUNNER_RETRIEVAL}:
        candidates = sorted(
            [p for p in out_hint.rglob(target_name) if p.is_file() and p not in before],
            key=lambda p: p.stat().st_mtime,
            reverse=True,
        )
        if candidates:
            return candidates[0]
        fallback = sorted(
            [p for p in out_hint.rglob(target_name) if p.is_file()],
            key=lambda p: p.stat().st_mtime,
            reverse=True,
        )
        if fallback:
            return fallback[0]
    raise RuntimeError(f"could not find result json for runner '{runner}'")


def _pump_stream_to_log(stream: Any, log_handle: Any, console_handle: Any) -> None:
    while True:
        chunk = stream.read(1)
        if chunk == "":
            break
        log_handle.write(chunk)
        log_handle.flush()
        console_handle.write(chunk)
        console_handle.flush()


def run_command_with_live_logs(
    command: list[str],
    cwd: Path,
    stdout_path: Path,
    stderr_path: Path,
    scenario_id: str,
    scenario_index: int,
    scenario_total: int,
) -> tuple[int, float]:
    print(f"\n==> [{scenario_index}/{scenario_total}] scenario={scenario_id} status=starting")
    print(f"    command: {' '.join(command)}")
    start = time.perf_counter()
    start_heartbeat = time.monotonic()
    with stdout_path.open("w", encoding="utf-8") as stdout_log, stderr_path.open("w", encoding="utf-8") as stderr_log:
        proc = subprocess.Popen(  # pylint: disable=consider-using-with
            command,
            cwd=str(cwd),
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            encoding="utf-8",
            errors="replace",
        )

        if proc.stdout is None or proc.stderr is None:
            raise RuntimeError("failed to start process stream capture")

        stdout_thread = threading.Thread(
            target=_pump_stream_to_log,
            args=(proc.stdout, stdout_log, sys.stdout),
            daemon=True,
        )
        stderr_thread = threading.Thread(
            target=_pump_stream_to_log,
            args=(proc.stderr, stderr_log, sys.stderr),
            daemon=True,
        )
        stdout_thread.start()
        stderr_thread.start()

        last_heartbeat = time.monotonic()
        while proc.poll() is None:
            now = time.monotonic()
            if now - last_heartbeat >= PROGRESS_HEARTBEAT_SECONDS:
                elapsed = now - start_heartbeat
                print(f"\n... scenario={scenario_id} still running ({elapsed:.0f}s elapsed)")
                last_heartbeat = now
            time.sleep(0.5)

        stdout_thread.join()
        stderr_thread.join()
        rc = int(proc.returncode or 0)

    duration = round(time.perf_counter() - start, 3)
    outcome = "ok" if rc == 0 else f"failed(code={rc})"
    print(f"\n==> [{scenario_index}/{scenario_total}] scenario={scenario_id} status={outcome} duration={duration:.3f}s")
    return rc, duration


def scenario_filter(only_raw: str) -> set[str] | None:
    if not only_raw.strip():
        return None
    return {token.strip() for token in only_raw.split(",") if token.strip()}


def markdown_table_row(columns: list[str]) -> str:
    return "| " + " | ".join(columns) + " |"


def render_summary_markdown(
    suite_meta: dict[str, Any],
    results: list[ScenarioResult],
    comparisons: list[dict[str, Any]],
) -> str:
    lines: list[str] = []
    lines.append(f"# Benchmark Suite Summary: {suite_meta['suite_name']}")
    lines.append("")
    lines.append(f"- run_id: `{suite_meta['run_id']}`")
    lines.append(f"- started_utc: `{suite_meta['started_utc']}`")
    lines.append(f"- completed_utc: `{suite_meta['completed_utc']}`")
    lines.append(f"- duration_seconds: `{suite_meta['duration_seconds']:.2f}`")
    lines.append("")
    lines.append("## Scorecard")
    lines.append("")
    lines.append(
        markdown_table_row(
            [
                "Scenario",
                "Runner",
                "Status",
                "Store ops/s",
                "Search ops/s",
                "Search p95 (ms)",
                "Top1",
                "Recall",
                "Perf Score",
            ]
        )
    )
    lines.append(markdown_table_row(["---"] * 9))
    for res in results:
        m = res.metrics
        s = res.scoring
        lines.append(
            markdown_table_row(
                [
                    res.scenario_id,
                    res.runner,
                    res.status,
                    f"{m.get('store_ops_sec', '')}",
                    f"{m.get('search_ops_sec', '')}",
                    f"{m.get('search_p95_ms', '')}",
                    f"{m.get('top1_hit_rate', '')}",
                    f"{m.get('recall_at_k', m.get('retrieval_recall_at_k', ''))}",
                    f"{s.get('performance_score', '')}",
                ]
            )
        )
    lines.append("")
    lines.append("## Artifacts")
    lines.append("")
    lines.append(f"- suite_json: `{suite_meta['suite_json']}`")
    lines.append(f"- suite_markdown: `{suite_meta['suite_markdown']}`")
    lines.append("")
    if comparisons:
        lines.append("## Profile Comparisons")
        lines.append("")
        lines.append(markdown_table_row(["Comparison", "Baseline", "Candidate", "Metric", "Baseline Val", "Candidate Val", "Delta", "Delta %"]))
        lines.append(markdown_table_row(["---"] * 8))
        for comp in comparisons:
            if comp.get("status") != "ok":
                lines.append(
                    markdown_table_row(
                        [
                            str(comp.get("id", "")),
                            str(comp.get("baseline", "")),
                            str(comp.get("candidate", "")),
                            comp.get("reason", comp.get("error", comp.get("status", ""))),
                            "",
                            "",
                            "",
                            "",
                        ]
                    )
                )
                continue
            comp_id = str(comp.get("id", ""))
            baseline = str(comp.get("baseline", ""))
            candidate = str(comp.get("candidate", ""))
            for metric in comp.get("metrics", []):
                lines.append(
                    markdown_table_row(
                        [
                            comp_id,
                            baseline,
                            candidate,
                            str(metric.get("metric", "")),
                            str(metric.get("baseline", "")),
                            str(metric.get("candidate", "")),
                            str(metric.get("delta", "")),
                            str(metric.get("delta_pct", "")),
                        ]
                    )
                )
            for warning in comp.get("warnings", []):
                lines.append(markdown_table_row([comp_id, baseline, candidate, f"WARNING: {warning}", "", "", "", ""]))
        lines.append("")
    return "\n".join(lines) + "\n"


def detect_repo_root(anchor: Path) -> Path:
    for parent in [anchor, *anchor.parents]:
        if (parent / ".git").exists():
            return parent
    # Fallback for unusual packaging contexts.
    if len(anchor.parents) >= 2:
        return anchor.parents[2]
    return anchor.parent


def main() -> int:
    args = parse_args()
    repo_root = detect_repo_root(Path(__file__).resolve())
    config_path = (repo_root / args.config).resolve() if not Path(args.config).is_absolute() else Path(args.config)
    if not config_path.exists():
        raise SystemExit(f"ERROR: config file not found: {config_path}")

    cfg = read_json(config_path)
    suite_name = str(cfg.get("name", config_path.stem))
    out_root = (repo_root / cfg.get("out_root", DEFAULT_OUT_ROOT)).resolve()
    run_id = utc_run_id()
    suite_dir = out_root / f"{run_id}-{slugify(suite_name)}"
    raw_dir = suite_dir / "raw"
    logs_dir = suite_dir / "logs"
    suite_dir.mkdir(parents=True, exist_ok=True)
    raw_dir.mkdir(parents=True, exist_ok=True)
    logs_dir.mkdir(parents=True, exist_ok=True)

    started_utc = utc_now_iso()
    start_monotonic = time.perf_counter()

    only_ids = scenario_filter(args.only)
    fail_fast = bool(cfg.get("fail_fast", False))
    common_cfg = cfg.get("common", {})
    default_scoring = cfg.get("scoring_defaults", {})
    scenario_defs = cfg.get("scenarios", [])
    if not isinstance(scenario_defs, list) or not scenario_defs:
        raise SystemExit("ERROR: config must define a non-empty 'scenarios' array")

    results: list[ScenarioResult] = []
    had_failure = False
    comparison_defs = cfg.get("comparisons", [])
    if not isinstance(comparison_defs, list):
        comparison_defs = []

    for idx, scenario in enumerate(scenario_defs, start=1):
        scenario_id = str(scenario.get("id", f"scenario_{idx}"))
        if only_ids is not None and scenario_id not in only_ids:
            continue

        name = str(scenario.get("name", scenario_id))
        runner = str(scenario.get("runner", "")).strip()
        enabled = bool(scenario.get("enabled", True))
        if runner not in RUNNER_TYPES:
            results.append(
                ScenarioResult(
                    scenario_id=scenario_id,
                    name=name,
                    runner=runner,
                    enabled=enabled,
                    status="failed",
                    error=f"unsupported runner: {runner}",
                )
            )
            had_failure = True
            if fail_fast:
                break
            continue

        if not enabled:
            results.append(
                ScenarioResult(
                    scenario_id=scenario_id,
                    name=name,
                    runner=runner,
                    enabled=False,
                    status="skipped",
                )
            )
            continue

        scenario_args = scenario.get("args", {})
        if not isinstance(scenario_args, dict):
            results.append(
                ScenarioResult(
                    scenario_id=scenario_id,
                    name=name,
                    runner=runner,
                    enabled=True,
                    status="failed",
                    error="scenario.args must be an object",
                )
            )
            had_failure = True
            if fail_fast:
                break
            continue

        common_runner_args = common_cfg.get(runner, {})
        if not isinstance(common_runner_args, dict):
            common_runner_args = {}
        merged_args = merge_args(common_runner_args, scenario_args)

        if runner in {RUNNER_BENCHMARK, RUNNER_RETRIEVAL} and "out_dir" not in merged_args:
            merged_args["out_dir"] = raw_dir.as_posix()
        if runner == RUNNER_LOCOMO:
            merged_args.setdefault("out_json", (suite_dir / f"{scenario_id}.locomo.json").as_posix())
            merged_args.setdefault("out_summary", (suite_dir / f"{scenario_id}.locomo.summary.txt").as_posix())

        try:
            command = build_command(repo_root, runner, merged_args)
        except Exception as exc:  # pylint: disable=broad-except
            results.append(
                ScenarioResult(
                    scenario_id=scenario_id,
                    name=name,
                    runner=runner,
                    enabled=True,
                    status="failed",
                    error=str(exc),
                )
            )
            had_failure = True
            if fail_fast:
                break
            continue

        result = ScenarioResult(
            scenario_id=scenario_id,
            name=name,
            runner=runner,
            enabled=True,
            status="pending",
            command=command,
        )
        results.append(result)

        if args.dry_run:
            result.status = "dry_run"
            continue

        out_dir_hint = Path(str(merged_args.get("out_dir", raw_dir)))
        before = set(out_dir_hint.rglob("benchmark.json")) | set(out_dir_hint.rglob("retrieval_quality.json"))

        stdout_path = logs_dir / f"{scenario_id}.stdout.log"
        stderr_path = logs_dir / f"{scenario_id}.stderr.log"
        result.logs = {"stdout": str(stdout_path), "stderr": str(stderr_path)}
        return_code, duration = run_command_with_live_logs(
            command=command,
            cwd=repo_root,
            stdout_path=stdout_path,
            stderr_path=stderr_path,
            scenario_id=scenario_id,
            scenario_index=idx,
            scenario_total=len(scenario_defs),
        )
        result.duration_seconds = duration

        if return_code != 0:
            result.status = "failed"
            result.error = f"command exited with code {return_code}"
            had_failure = True
            if fail_fast:
                break
            continue

        try:
            json_path = find_result_json(runner, merged_args, before, out_dir_hint)
            payload = read_json(json_path)
            result.result_json = str(json_path)
            result.metrics = extract_metrics(runner, payload)
            scoring_cfg = merge_args(default_scoring, scenario.get("scoring", {}))
            result.scoring = compute_scoring(result.metrics, scoring_cfg)
            result.status = "ok"
        except Exception as exc:  # pylint: disable=broad-except
            result.status = "failed"
            result.error = f"result parse failed: {exc}"
            had_failure = True
            if fail_fast:
                break

    completed_utc = utc_now_iso()
    duration_seconds = round(time.perf_counter() - start_monotonic, 3)
    comparisons = build_comparisons(comparison_defs, results)

    suite_json_path = suite_dir / "suite.json"
    suite_md_path = suite_dir / "suite.summary.md"
    suite_meta = {
        "suite_name": suite_name,
        "run_id": run_id,
        "config": str(config_path),
        "started_utc": started_utc,
        "completed_utc": completed_utc,
        "duration_seconds": duration_seconds,
        "suite_json": str(suite_json_path),
        "suite_markdown": str(suite_md_path),
    }

    serializable_results = [
        {
            "scenario_id": r.scenario_id,
            "name": r.name,
            "runner": r.runner,
            "enabled": r.enabled,
            "status": r.status,
            "duration_seconds": r.duration_seconds,
            "command": r.command,
            "result_json": r.result_json,
            "metrics": r.metrics,
            "scoring": r.scoring,
            "logs": r.logs,
            "error": r.error,
        }
        for r in results
    ]
    suite_json_path.write_text(
        json.dumps({"suite": suite_meta, "scenarios": serializable_results, "comparisons": comparisons}, indent=2) + "\n",
        encoding="utf-8",
    )

    summary = render_summary_markdown(suite_meta, results, comparisons)
    suite_md_path.write_text(summary, encoding="utf-8")

    print(summary)
    print(f"Suite JSON: {suite_json_path}")
    print(f"Suite markdown: {suite_md_path}")

    return 1 if had_failure else 0


if __name__ == "__main__":
    raise SystemExit(main())
