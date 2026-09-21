#!/usr/bin/env bash
# Fails when the gzip size of the built JavaScript exceeds the budget (docs/SPEC.md §4d).
set -euo pipefail

BUDGET_KB="${BUNDLE_BUDGET_KB:-200}"
DIST="$(cd "$(dirname "$0")/.." && pwd)/web/dist/assets"

if ! compgen -G "$DIST/*.js" >/dev/null; then
  echo "bundle-budget: no built JavaScript in $DIST (run 'npm run build' in web/ first)" >&2
  exit 1
fi

total=0
for f in "$DIST"/*.js; do
  size=$(gzip -9 -c "$f" | wc -c)
  printf '  %7d B gzip  %s\n' "$size" "$(basename "$f")"
  total=$((total + size))
done

limit=$((BUDGET_KB * 1024))
printf 'bundle-budget: %d B gzip total, budget %d B (%d KB)\n' "$total" "$limit" "$BUDGET_KB"
if [ "$total" -gt "$limit" ]; then
  echo "bundle-budget: FAIL, JavaScript exceeds the budget" >&2
  exit 1
fi
echo "bundle-budget: PASS"
