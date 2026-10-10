#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf '%s\n' \
    'Usage: bash build.sh' \
    'Builds a Linux amd64 binary in dist/emby-go-linux-amd64.' \
    'Requires Go 1.25+, Node 24.14.1, npm 11.11.0. Optional: VERSION, OUT_DIR.'
}

if (( $# > 0 )); then
  if (( $# == 1 )) && [[ "$1" == '-h' || "$1" == '--help' ]]; then
    usage
    exit 0
  fi
  usage >&2
  exit 2
fi

if ! command -v go >/dev/null 2>&1; then
  printf '%s\n' 'Error: Go 1.25 or newer must be installed and available in PATH.' >&2
  exit 1
fi

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd -- "$root"

if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
  printf '%s\n' 'Error: Node and npm are required on the build machine.' >&2
  exit 1
fi
node frontend/scripts/check-toolchain.mjs
npm --prefix frontend ci
npm --prefix frontend run typecheck
npm --prefix frontend run test:unit -- --run
npm --prefix frontend run test:build
npm --prefix frontend run build

version="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || printf 'dev')}"
commit="$(git rev-parse --short HEAD 2>/dev/null || printf 'unknown')"
build_time="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
if [[ "$version" == *[!a-zA-Z0-9._+/-]* ]]; then
  printf '%s\n' 'Error: VERSION may contain only letters, digits, dot, underscore, plus, slash or hyphen.' >&2
  exit 2
fi

out_dir="${OUT_DIR:-$root/dist}"
mkdir -p -- "$out_dir"
out_dir="$(cd -- "$out_dir" && pwd)"
binary="$out_dir/emby-go-linux-amd64"
temporary="$(mktemp "$out_dir/.emby-go-linux-amd64.XXXXXX")"
trap 'rm -f -- "$temporary"' EXIT

printf 'Building linux/amd64: version=%s commit=%s built=%s\n' "$version" "$commit" "$build_time"
# Match release builds without CGO; v1 also runs on older x86-64 CPUs.
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build \
  -tags embedui -mod=readonly -trimpath -buildvcs=false \
  -ldflags "-s -w -X main.version=$version -X main.commit=$commit -X main.buildTime=$build_time" \
  -o "$temporary" ./cmd/metatube

# A failed build must not replace the last successful binary.
chmod 0755 "$temporary"
mv -f -- "$temporary" "$binary"
trap - EXIT
printf 'Built: %s\n' "$binary"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$binary"
fi
