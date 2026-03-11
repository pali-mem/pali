#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

PYTHON_BIN="${PYTHON_BIN:-}"
if [[ -z "$PYTHON_BIN" ]]; then
  if command -v python3 >/dev/null 2>&1; then
    PYTHON_BIN="python3"
  elif command -v python >/dev/null 2>&1; then
    PYTHON_BIN="python"
  else
    echo "ERROR: Python interpreter not found (python3 or python required)"
    exit 1
  fi
fi

"$PYTHON_BIN" - <<'PY'
from __future__ import annotations

import os
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path.cwd()
DOC_FILES = [
    "README.md",
    "cmd/setup/README.md",
    "docs/README.md",
    "docs/api.md",
    "docs/architecture.md",
    "docs/configuration.md",
    "docs/deployment.md",
    "docs/mcp.md",
    "docs/onnx.md",
    "docs/operations.md",
    "docs/client/README.md",
    "test/benchmarks/trends/README.md",
    "test/benchmarks/profiles/README.md",
    "CONTRIBUTING.md",
    "CODE_OF_CONDUCT.md",
    "CHANGELOG.md",
]

link_re = re.compile(r"\[[^\]]*\]\(([^)]+)\)")
code_re = re.compile(r"`([^`\n]+)`")
path_hint_re = re.compile(r"^(?:\.{0,2}/)?[A-Za-z0-9_.-][A-Za-z0-9_./-]*(?:\.[A-Za-z0-9_.-]+)?$")
file_like_suffixes = (
    ".md",
    ".go",
    ".sh",
    ".ps1",
    ".json",
    ".yaml",
    ".yml",
    ".txt",
    ".png",
)
repo_roots = (
    "README.md",
    "BENCHMARKS.MD",
    "TODO.md",
    "CHANGELOG.md",
    "CONTRIBUTING.md",
    "CODE_OF_CONDUCT.md",
    "cmd/",
    "docs/",
    "internal/",
    "pkg/",
    "scripts/",
    "test/",
    "testdata/",
    "research/",
    "models/",
    "web/",
    "pali.yaml.example",
)

missing: list[tuple[str, str, str]] = []

def resolve_path(doc: Path, target: str, kind: str) -> Path | None:
    if not target or target.startswith(("http://", "https://", "mailto:", "#")):
        return None
    clean = target.split("#", 1)[0].strip()
    if not clean:
        return None
    if clean.startswith("/"):
        candidate = (ROOT / clean.lstrip("/")).resolve()
    elif kind == "link":
        candidate = (doc.parent / clean).resolve()
    elif clean.startswith("."):
        candidate = (doc.parent / clean).resolve()
    else:
        candidate = (ROOT / clean).resolve()
    return candidate


def add_missing(doc: Path, kind: str, target: str) -> None:
    resolved = resolve_path(doc, target, kind)
    if resolved is None:
        return
    try:
        resolved.relative_to(ROOT)
    except ValueError:
        missing.append((doc.as_posix(), kind, target))
        return
    if not resolved.exists():
        missing.append((doc.as_posix(), kind, target))


for rel in DOC_FILES:
    doc = ROOT / rel
    if not doc.exists():
        missing.append((rel, "doc", rel))
        continue
    text = doc.read_text(encoding="utf-8")
    for match in link_re.finditer(text):
        add_missing(doc, "link", match.group(1).strip())
    for match in code_re.finditer(text):
        token = match.group(1).strip()
        if token.startswith(("-", "http://", "https://")):
            continue
        if token.startswith(("/health", "/api/", "/etc/")):
            continue
        if token.startswith("models/"):
            continue
        if not path_hint_re.match(token):
            continue
        if not token.startswith(repo_roots) and not token.startswith(("./", "../")):
            continue
        add_missing(doc, "path", token.rstrip("/\\"))

if missing:
    print("Docs freshness check failed:")
    for doc, kind, target in missing:
        print(f"  [{kind}] {doc}: {target}")
    sys.exit(1)

print("Docs freshness check passed.")
PY
