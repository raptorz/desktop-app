#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: $0 <version> [linux|darwin] [amd64|arm64] [absolute-output-dir]" >&2
  exit 2
}

[[ $# -ge 1 && $# -le 4 ]] || usage

version="${1#v}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage

script_dir="$(cd "$(dirname "$0")" && pwd)"
desktop_dir="$(cd "$script_dir/.." && pwd)"
source_root="$(cd "$desktop_dir/.." && pwd)"

case "$(uname -s)" in
  Linux) host_platform="linux" ;;
  Darwin) host_platform="darwin" ;;
  *) echo "Unsupported host OS: $(uname -s). Use build-release.ps1 on Windows." >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) host_arch="amd64" ;;
  arm64|aarch64) host_arch="arm64" ;;
  *) echo "Unsupported host architecture: $(uname -m)" >&2; exit 1 ;;
esac

platform="${2:-$host_platform}"
arch="${3:-$host_arch}"
output_dir="${4:-$desktop_dir/release}"

[[ "$platform" == "linux" || "$platform" == "darwin" ]] || usage
[[ "$arch" == "amd64" || "$arch" == "arm64" ]] || usage
if [[ "$platform" != "$host_platform" || "$arch" != "$host_arch" ]]; then
  echo "Wails release builds must be native: host is $host_platform/$host_arch, requested $platform/$arch" >&2
  exit 1
fi
[[ "$output_dir" == /* ]] || { echo "Output directory must be an absolute path: $output_dir" >&2; exit 1; }

command -v go >/dev/null 2>&1 || {
  echo "Required tool not found: go" >&2
  echo "Install Go 1.23 or later from https://go.dev/dl/ and add it to PATH." >&2
  exit 1
}
command -v npm >/dev/null 2>&1 || {
  echo "Required tool not found: npm" >&2
  echo "Install Node.js 22 (including npm) from https://nodejs.org/." >&2
  exit 1
}
command -v wails >/dev/null 2>&1 || {
  go_bin="$(go env GOPATH)/bin"
  echo "Required tool not found: wails" >&2
  echo "Install Wails CLI v2.12.0 with:" >&2
  echo "  go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0" >&2
  echo "Then add $go_bin to PATH and retry." >&2
  exit 1
}
if [[ "$platform" == "darwin" ]]; then
  command -v ditto >/dev/null 2>&1 || { echo "Required tool not found: ditto" >&2; exit 1; }
else
  command -v tar >/dev/null 2>&1 || { echo "Required tool not found: tar" >&2; exit 1; }
  command -v sha256sum >/dev/null 2>&1 || { echo "Required tool not found: sha256sum" >&2; exit 1; }
fi

[[ -f "$source_root/frontend/package-lock.json" ]] || { echo "Shared frontend not found at $source_root/frontend" >&2; exit 1; }
[[ -d "$source_root/public/tinymce" ]] || { echo "TinyMCE assets not found at $source_root/public/tinymce" >&2; exit 1; }

client_version="$(sed -n 's/^const ClientVersion = "\([^"]*\)"/\1/p' "$desktop_dir/api/version.go")"
if [[ -z "$client_version" || "$client_version" != "$version" ]]; then
  echo "Requested version $version does not match api.ClientVersion ${client_version:-<missing>}" >&2
  exit 1
fi

wails_version="$(wails version 2>&1)"
grep -Eq '(^|[^0-9])v?2\.12\.0([^0-9]|$)' <<<"$wails_version" || {
  echo "Wails CLI v2.12.0 is required; got: $wails_version" >&2
  exit 1
}

mkdir -p "$output_dir"

npm ci --prefix "$source_root/frontend"
npm test --prefix "$source_root/frontend" -- --run
npm run build --prefix "$source_root/frontend"

(cd "$desktop_dir" && go test ./...)
(cd "$desktop_dir" && wails build -clean -platform "$platform/$arch")

asset="gemsnote-$version-$platform-$arch"
if [[ "$platform" == "linux" ]]; then
  binary="$desktop_dir/build/bin/gemsnote"
  [[ -f "$binary" ]] || { echo "Wails output not found: $binary" >&2; exit 1; }
  archive="$output_dir/$asset.tar.gz"
  rm -f "$archive"
  tar -C "$desktop_dir/build/bin" -czf "$archive" gemsnote
else
  app="$desktop_dir/build/bin/gemsnote.app"
  [[ -d "$app" ]] || { echo "Wails output not found: $app" >&2; exit 1; }
  archive="$output_dir/$asset.zip"
  rm -f "$archive"
  ditto -c -k --sequesterRsrc --keepParent "$app" "$archive"
fi

checksum_file="$output_dir/SHA256SUMS"
if [[ "$platform" == "linux" ]]; then
  (cd "$output_dir" && sha256sum "gemsnote-$version-"*.tar.gz "gemsnote-$version-"*.zip 2>/dev/null | sort > SHA256SUMS.tmp) || true
else
  (cd "$output_dir" && for file in "gemsnote-$version-"*.tar.gz "gemsnote-$version-"*.zip; do
    [[ -f "$file" ]] && shasum -a 256 "$file"
  done | sort > SHA256SUMS.tmp)
fi
[[ -s "$checksum_file.tmp" ]] || { echo "No release archives found in $output_dir" >&2; exit 1; }
mv "$checksum_file.tmp" "$checksum_file"

echo "Created $archive"
echo "Updated $checksum_file"
