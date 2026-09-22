#!/usr/bin/env bash
# Scans the image for known vulnerabilities; fails on fixable HIGH/CRITICAL findings (docs/SPEC.md §4f).
# Trivy is pinned by digest (0.74.0). Usage: scripts/image-scan.sh [image]
set -Eeuo pipefail

image="${1:-smotryashchiy:smoke}"
trivy='aquasec/trivy@sha256:62b1e65e8869bc4b4c6aa4fa2b21595256c7c2f6018a9d9ad61caf87187c1969'
cache="${TRIVY_CACHE_DIR:-$HOME/.cache/trivy}"
mkdir -p "$cache"

docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$cache:/root/.cache/trivy" \
  "$trivy" image \
    --severity HIGH,CRITICAL \
    --ignore-unfixed \
    --exit-code 1 \
    --no-progress \
    "$image"
echo "image-scan: PASS (no fixable HIGH/CRITICAL findings in $image)"
