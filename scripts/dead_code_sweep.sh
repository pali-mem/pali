#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> Dead-code and artifact sweep"

tracked_artifacts="$(git ls-files \
  '*.exe' '*.exe~' '*.db' '*.db-shm' '*.db-wal' '*.sqlite' '*.sqlite-shm' '*.sqlite-wal' \
  'test/benchmarks/results/**' 'test/benchmarks/generated/**' 'research/results/**' 'research/cache/**' \
  'research/__pycache__/**' 'pali' 'genfix')"

if [[ -n "$tracked_artifacts" ]]; then
  echo "Tracked generated artifacts found:"
  printf '  %s\n' $tracked_artifacts
  exit 1
fi

echo "Tracked artifact policy: clean"

echo ""
echo "Potential orphan references (manual review list):"
python - <<'PY'
from __future__ import annotations

import subprocess
from pathlib import Path

ROOT = Path.cwd()
watch_dirs = [
    ROOT / "docs" / "internal",
    ROOT / "scripts",
]

reported = False
allowed_orphans = {
    "scripts/feature_wiring_check.ps1",
    "scripts/feature_wiring_check.sh",
    "scripts/setup.ps1",
    "scripts/setup.sh",
}
for base in watch_dirs:
    if not base.exists():
        continue
    for path in sorted(p for p in base.rglob("*") if p.is_file()):
        rel = path.relative_to(ROOT).as_posix()
        if rel in allowed_orphans:
            continue
        if rel.endswith(".md") and rel.startswith("docs/internal/"):
            if rel in {"docs/internal/sdk-architecture.md", "docs/internal/sqlite.md", "docs/internal/multihop-graph-upgrade-plan.md"}:
                continue
        result = subprocess.run(
            ["git", "grep", "-n", "-F", rel, "--", ".", ":(exclude)" + rel],
            cwd=ROOT,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
        )
        if result.returncode != 0:
            print(f"  {rel}")
            reported = True

if not reported:
    print("  none")
PY
