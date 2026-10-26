#!/usr/bin/env bash
# Monthly refresh of the visitor geography database (docs/SPEC.md §4i, Change 35): DB-IP City Lite
# (CC BY 4.0) into SMOTRYASHCHIY_GEOIP_DIR, which the server container mounts read-only. The new
# file replaces the old one atomically in the same directory; the server notices the new
# modification time within a minute, no restart needed. A failed download keeps the previous file;
# no file at all only means analytics shows no country or region.
set -euo pipefail
umask 022

target_dir=${SMOTRYASHCHIY_GEOIP_DIR:-/var/lib/smotryashchiy/geoip}
target="$target_dir/dbip-city-lite.mmdb"
# DB-IP publishes a month's file early in that month; before it exists the previous one is used.
month=${SMOTRYASHCHIY_GEOIP_MONTH:-$(date -u +%Y-%m)}
previous=$(date -u -d "$month-01 -1 month" +%Y-%m)

work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
install -d -m 755 "$target_dir"
fetch() {
  curl --fail --silent --show-error --location --max-time 600 --proto '=https' \
    --output "$work/city.mmdb.gz" "https://download.db-ip.com/free/dbip-city-lite-$1.mmdb.gz"
}
if ! fetch "$month"; then
  echo "smotryashchiy-geoip: $month not published yet, trying $previous" >&2
  fetch "$previous"
  month=$previous
fi
gunzip -c "$work/city.mmdb.gz" >"$work/city.mmdb"
# A real database is about 120 MB; anything small is an error page, not a database.
size=$(stat -c %s "$work/city.mmdb")
((size > 10 * 1024 * 1024)) || {
  echo "smotryashchiy-geoip: unexpected size $size, keeping the current file" >&2
  exit 1
}
# The MMDB metadata marker sits near the end of every valid file.
tail -c 131072 "$work/city.mmdb" | LC_ALL=C grep -aqF $'\xab\xcd\xefMaxMind.com' || {
  echo "smotryashchiy-geoip: not an MMDB file, keeping the current file" >&2
  exit 1
}
install -m 644 "$work/city.mmdb" "$target.new"
mv -f "$target.new" "$target"
echo "smotryashchiy-geoip: $month installed ($size bytes)"
