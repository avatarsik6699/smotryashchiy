#!/usr/bin/env bash
# Builds the web UI, then static linux/amd64 and linux/arm64 binaries with the UI embedded and the
# release SHA stamped in, plus SHA256SUMS, into release/ (docs/SPEC.md §4f).
set -Eeuo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
out_dir="${RELEASE_DIR:-$repo_dir/release}"
release="${RELEASE_SHA:-$(git -C "$repo_dir" rev-parse HEAD)}"

if ! [[ "$release" =~ ^[0-9a-f]{40}$ ]]; then
  echo "build-release: RELEASE_SHA must be a 40-character lowercase git SHA, got '$release'" >&2
  exit 1
fi

echo "build-release: release $release"
if [ "${SKIP_WEB:-0}" != "1" ]; then
  npm --prefix "$repo_dir/web" ci
  npm --prefix "$repo_dir/web" run build
fi
[ -f "$repo_dir/web/dist/index.html" ] || { echo "build-release: web/dist/index.html is missing; the UI must be built first" >&2; exit 1; }

rm -rf -- "$out_dir"
mkdir -p "$out_dir"
for arch in amd64 arm64; do
  name="smotryashchiy-linux-$arch"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w -X main.release=$release" -o "$out_dir/$name" "$repo_dir/cmd/smotryashchiy"
  echo "build-release: built $name"
done

# Static means no dynamic loader and no shared libraries: the binary must run on any Linux (and in distroless).
for arch in amd64 arm64; do
  bin="$out_dir/smotryashchiy-linux-$arch"
  if readelf -l "$bin" 2>/dev/null | grep -q INTERP; then
    echo "build-release: $bin is dynamically linked" >&2
    exit 1
  fi
  if readelf -d "$bin" 2>/dev/null | grep -q NEEDED; then
    echo "build-release: $bin needs shared libraries" >&2
    exit 1
  fi
done

if [ "$(go env GOARCH)" = "amd64" ] && [ "$(go env GOOS)" = "linux" ]; then
  version_line=$("$out_dir/smotryashchiy-linux-amd64" version)
  case "$version_line" in
    *"release=$release "*) echo "build-release: amd64 binary reports: $version_line" ;;
    *) echo "build-release: unexpected version output: $version_line" >&2; exit 1 ;;
  esac
else
  echo "build-release: SKIPPED running the amd64 binary (not on linux/amd64)"
fi
echo "build-release: SKIPPED running the arm64 binary (cross-compiled only; verified static)"

(cd "$out_dir" && sha256sum smotryashchiy-linux-* > SHA256SUMS && cat SHA256SUMS)
