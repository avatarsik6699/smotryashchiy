#!/usr/bin/env bash
# Fails when the JavaScript the server sends exceeds the budget (docs/SPEC.md §4d). Since Change 38
# the build precompresses every asset and the server sends the .br file, so the budget counts
# brotli bytes: the entry chunk with its modulepreloads (what the sign-in screen downloads) and all
# JavaScript together (everything a session can load).
set -euo pipefail

ENTRY_KB="${BUNDLE_ENTRY_BUDGET_KB:-80}"
TOTAL_KB="${BUNDLE_BUDGET_KB:-200}"
DIST="$(cd "$(dirname "$0")/.." && pwd)/web/dist"

if ! compgen -G "$DIST/assets/*.js.br" >/dev/null; then
  echo "bundle-budget: no precompressed JavaScript in $DIST/assets (run 'npm run build' in web/ first)" >&2
  exit 1
fi

# sent_size is what the server sends for a file: its .br variant, or the file itself when it was too
# small to precompress.
sent_size() {
  if [ -f "$1.br" ]; then stat -c %s "$1.br"; else stat -c %s "$1"; fi
}

entry=0
while read -r f; do
  size=$(sent_size "$DIST/$f")
  printf '  %7d B br  entry  %s\n' "$size" "$(basename "$f")"
  entry=$((entry + size))
done < <(grep -oE '(src|href)="/assets/[^"]+\.js"' "$DIST/index.html" | sed -E 's/.*"\/(assets\/[^"]+)"/\1/')

total=0
for f in "$DIST"/assets/*.js; do
  total=$((total + $(sent_size "$f")))
done

status=0
printf 'bundle-budget: entry %d B br, budget %d KB\n' "$entry" "$ENTRY_KB"
if [ "$entry" -gt $((ENTRY_KB * 1024)) ]; then
  echo "bundle-budget: FAIL, the entry chunk exceeds its budget" >&2
  status=1
fi
printf 'bundle-budget: all JavaScript %d B br, budget %d KB\n' "$total" "$TOTAL_KB"
if [ "$total" -gt $((TOTAL_KB * 1024)) ]; then
  echo "bundle-budget: FAIL, JavaScript exceeds the budget" >&2
  status=1
fi
[ "$status" -eq 0 ] && echo "bundle-budget: PASS"
exit "$status"
