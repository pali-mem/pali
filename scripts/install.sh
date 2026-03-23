#!/usr/bin/env sh
set -eu

REPO="${PALI_REPO:-pali-mem/pali}"
INSTALL_DIR="${PALI_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${PALI_VERSION:-}"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "ERROR: missing required command: $1" >&2
    exit 1
  fi
}

need_cmd tar
need_cmd awk
need_cmd uname
need_cmd mktemp
need_cmd install

fetch() {
  url="$1"
  out="$2"

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
    return
  fi

  if command -v wget >/dev/null 2>&1; then
    wget -qO "$out" "$url"
    return
  fi

  echo "ERROR: missing required downloader: curl or wget" >&2
  exit 1
}

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Linux) goos="linux" ;;
  Darwin) goos="darwin" ;;
  *)
    echo "ERROR: unsupported OS: $os" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64) goarch="amd64" ;;
  arm64|aarch64) goarch="arm64" ;;
  *)
    echo "ERROR: unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

api_url="https://api.github.com/repos/$REPO/releases/latest"
if [ -n "$VERSION" ]; then
  api_url="https://api.github.com/repos/$REPO/releases/tags/$VERSION"
fi

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT INT TERM

release_json="$tmpdir/release.json"
fetch "$api_url" "$release_json"

version="$(awk -F'"' '/"tag_name":/ {print $4; exit}' "$release_json")"
if [ -z "$version" ]; then
  echo "ERROR: failed to resolve release version from GitHub API" >&2
  exit 1
fi

archive_name="pali_${version}_${goos}_${goarch}.tar.gz"
archive_url="$(awk -F'"' -v name="$archive_name" '
  $2 == "name" {asset_name=$4}
  $2 == "browser_download_url" && asset_name == name {print $4; exit}
' "$release_json")"

checksums_url="$(awk -F'"' '
  $2 == "name" {asset_name=$4}
  $2 == "browser_download_url" && asset_name == "SHA256SUMS" {print $4; exit}
' "$release_json")"

if [ -z "$archive_url" ] || [ -z "$checksums_url" ]; then
  echo "ERROR: release assets for $archive_name are missing" >&2
  exit 1
fi

archive_path="$tmpdir/$archive_name"
checksums_path="$tmpdir/SHA256SUMS"

fetch "$archive_url" "$archive_path"
fetch "$checksums_url" "$checksums_path"

expected_sha="$(awk -v name="$archive_name" '$2 == name {print $1; exit}' "$checksums_path")"
if [ -z "$expected_sha" ]; then
  echo "ERROR: checksum for $archive_name not found" >&2
  exit 1
fi

if command -v shasum >/dev/null 2>&1; then
  actual_sha="$(shasum -a 256 "$archive_path" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual_sha="$(sha256sum "$archive_path" | awk '{print $1}')"
else
  echo "ERROR: missing shasum or sha256sum for checksum verification" >&2
  exit 1
fi

if [ "$actual_sha" != "$expected_sha" ]; then
  echo "ERROR: checksum verification failed for $archive_name" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
tar -xzf "$archive_path" -C "$tmpdir"
install -m 0755 "$tmpdir/pali" "$INSTALL_DIR/pali"

shell_name="$(basename "${SHELL:-}")"
rc_file=""
case "$shell_name" in
  zsh) rc_file="$HOME/.zshrc" ;;
  bash) rc_file="$HOME/.bashrc" ;;
  fish) rc_file="$HOME/.config/fish/config.fish" ;;
  *) rc_file="$HOME/.profile" ;;
esac

case ":${PATH:-}:" in
  *:"$INSTALL_DIR":*) path_present="yes" ;;
  *) path_present="no" ;;
esac

if [ "$path_present" = "no" ] && [ "$INSTALL_DIR" = "$HOME/.local/bin" ]; then
  if [ "$shell_name" = "fish" ]; then
    path_line='fish_add_path $HOME/.local/bin'
  else
    path_line='export PATH="$HOME/.local/bin:$PATH"'
  fi

  if [ ! -f "$rc_file" ] || ! grep -F "$path_line" "$rc_file" >/dev/null 2>&1; then
    mkdir -p "$(dirname "$rc_file")"
    printf '\n# Added by Pali installer\n%s\n' "$path_line" >>"$rc_file"
    rc_note="Added $INSTALL_DIR to $rc_file. Open a new shell or source it before running pali."
  else
    rc_note="Open a new shell or source $rc_file before running pali."
  fi
else
  rc_note="pali is ready to use."
fi

cat <<EOF
Installed pali $version to $INSTALL_DIR/pali

Next:
  pali init
  pali serve

$rc_note
Docs:
  https://pali-mem.github.io/pali/
EOF
