#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

APP_NAME="pali"
MAIN_PKG="./cmd/pali"
OUT_ROOT="dist/releases"
VERSION=""
TARGETS="linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64,windows/arm64"

usage() {
  cat <<'EOF'
Usage:
  scripts/release_assets.sh [flags]

Flags:
  --version <v>   Release version label (default: git tag/commit timestamp)
  --targets <t>   Comma-separated GOOS/GOARCH list
                  (default: linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64,windows/arm64)
  --out-root <p>  Output root directory (default: dist/releases)
  --help          Show help

Examples:
  scripts/release_assets.sh --version v0.1.0
  scripts/release_assets.sh --version v0.1.0 --targets linux/amd64,darwin/arm64,windows/amd64
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      VERSION="$2"
      shift 2
      ;;
    --targets)
      TARGETS="$2"
      shift 2
      ;;
    --out-root)
      OUT_ROOT="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "ERROR: unknown flag: $1"
      usage
      exit 1
      ;;
  esac
done

if [[ -z "$VERSION" ]]; then
  if git describe --tags --exact-match >/dev/null 2>&1; then
    VERSION="$(git describe --tags --exact-match)"
  else
    VERSION="dev-$(date -u +%Y%m%dT%H%M%SZ)-$(git rev-parse --short HEAD 2>/dev/null || echo nogit)"
  fi
fi

if [[ -z "$TARGETS" ]]; then
  echo "ERROR: --targets must not be empty"
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: go command not found"
  exit 1
fi

if command -v shasum >/dev/null 2>&1; then
  HASH_CMD=(shasum -a 256)
elif command -v sha256sum >/dev/null 2>&1; then
  HASH_CMD=(sha256sum)
else
  echo "ERROR: neither shasum nor sha256sum found"
  exit 1
fi

if ! command -v tar >/dev/null 2>&1; then
  echo "ERROR: tar command not found"
  exit 1
fi

if ! command -v zip >/dev/null 2>&1; then
  echo "ERROR: zip command not found"
  exit 1
fi

run_dir="${OUT_ROOT}/${VERSION}"
artifacts_dir="${run_dir}/artifacts"
manifest="${run_dir}/manifest.json"
sha_file="${run_dir}/SHA256SUMS"
latest_file="${OUT_ROOT}/LATEST"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

mkdir -p "$artifacts_dir"
: >"$sha_file"

commit="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
build_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

assets_json="[]"

IFS=',' read -r -a target_list <<<"$TARGETS"

echo "==> Building release assets"
echo "    version : $VERSION"
echo "    targets : $TARGETS"
echo "    out dir : $run_dir"

for target in "${target_list[@]}"; do
  target="$(echo "$target" | xargs)"
  [[ -z "$target" ]] && continue

  goos="${target%%/*}"
  goarch="${target##*/}"
  if [[ "$goos" == "$goarch" ]]; then
    echo "ERROR: invalid target '$target' (expected GOOS/GOARCH)"
    exit 1
  fi

  bin_name="${APP_NAME}"
  if [[ "$goos" == "windows" ]]; then
    bin_name="${bin_name}.exe"
  fi

  stage_dir="${tmp_dir}/${APP_NAME}_${VERSION}_${goos}_${goarch}"
  mkdir -p "$stage_dir"
  out_bin="${stage_dir}/${bin_name}"

  echo "    -> ${goos}/${goarch}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "$out_bin" "$MAIN_PKG"

  archive_name="${APP_NAME}_${VERSION}_${goos}_${goarch}"
  if [[ "$goos" == "windows" ]]; then
    archive_path="${artifacts_dir}/${archive_name}.zip"
    zip -q -9 -j "$archive_path" "$out_bin"
  else
    chmod +x "$out_bin"
    archive_path="${artifacts_dir}/${archive_name}.tar.gz"
    tar -C "$stage_dir" -czf "$archive_path" "$bin_name"
  fi

  asset_sha="$("${HASH_CMD[@]}" "$archive_path" | awk '{print $1}')"
  asset_file="$(basename "$archive_path")"
  printf '%s  %s\n' "$asset_sha" "$asset_file" >>"$sha_file"

  asset_json="$(jq -n \
    --arg file "$asset_file" \
    --arg sha256 "$asset_sha" \
    --arg goos "$goos" \
    --arg goarch "$goarch" \
    '{file:$file, sha256:$sha256, goos:$goos, goarch:$goarch}')"
  assets_json="$(jq -n --argjson arr "$assets_json" --argjson item "$asset_json" '$arr + [$item]')"
done

jq -n \
  --arg name "$APP_NAME" \
  --arg version "$VERSION" \
  --arg commit "$commit" \
  --arg built_at "$build_time" \
  --arg targets "$TARGETS" \
  --argjson assets "$assets_json" \
  '{
    name: $name,
    version: $version,
    commit: $commit,
    built_at_utc: $built_at,
    targets: ($targets | split(",") | map(gsub("^\\s+|\\s+$"; "")) | map(select(length>0))),
    assets: $assets
  }' >"$manifest"

printf '%s\n' "$VERSION" >"$latest_file"

echo ""
echo "Release assets ready:"
echo "  manifest : $manifest"
echo "  checksums: $sha_file"
echo "  assets   : $artifacts_dir"
echo "  latest   : $latest_file"
