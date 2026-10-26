#!/usr/bin/env bash
# Contract test for deploy/geoip/smotryashchiy-geoip.sh (Change 35): curl is a fake that serves
# synthetic files, no network.
set -euo pipefail
script=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)/smotryashchiy-geoip.sh
root=$(mktemp -d)
trap 'rm -rf -- "$root"' EXIT
mkdir -p "$root/bin" "$root/files"
export FAKE="$root" PATH="$root/bin:$PATH" SMOTRYASHCHIY_GEOIP_DIR="$root/geoip"

# A valid-looking database: 11 MB of zeros, then the MMDB metadata marker.
{ head -c $((11 * 1024 * 1024)) /dev/zero; printf '\xab\xcd\xefMaxMind.com'; } | gzip -c >"$root/files/valid.gz"
printf 'tiny' | gzip -c >"$root/files/small.gz"
head -c $((11 * 1024 * 1024)) /dev/zero | gzip -c >"$root/files/nommdb.gz"

cat >"$root/bin/curl" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
out='' url=''
while (($#)); do
  case $1 in
    --output) out=$2; shift 2 ;;
    --max-time | --proto) shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
printf '%s\n' "$url" >>"$FAKE/urls"
month=${url##*dbip-city-lite-}; month=${month%%.mmdb.gz}
[[ -f "$FAKE/serve-$month" ]] || exit 22
cp "$FAKE/files/$(cat "$FAKE/serve-$month")" "$out"
FAKE
chmod +x "$root/bin/curl"

run() { SMOTRYASHCHIY_GEOIP_MONTH=$1 bash "$script" >"$root/out" 2>&1; }

# 1. the month's file is installed
echo valid.gz >"$root/serve-2026-11"
run 2026-11
[[ -s "$root/geoip/dbip-city-lite.mmdb" ]]
grep -q '2026-11 installed' "$root/out"
first=$(stat -c %i "$root/geoip/dbip-city-lite.mmdb")

# 2. not published yet: the previous month's file
rm "$root/serve-2026-11"
echo valid.gz >"$root/serve-2026-11"
rm -f "$root/serve-2026-12"
run 2026-12
grep -q 'trying 2026-11' "$root/out"
grep -q '2026-11 installed' "$root/out"
[[ $(stat -c %i "$root/geoip/dbip-city-lite.mmdb") != "$first" ]] # replaced by a rename

# 3. an error page or a non-MMDB file is refused and the current file stays
before=$(sha256sum "$root/geoip/dbip-city-lite.mmdb")
for bad in small.gz nommdb.gz; do
  echo "$bad" >"$root/serve-2027-01"
  if run 2027-01; then
    echo "accepted $bad" >&2
    exit 1
  fi
  grep -q 'keeping the current file' "$root/out"
  [[ $(sha256sum "$root/geoip/dbip-city-lite.mmdb") == "$before" ]]
done
[[ ! -e "$root/geoip/dbip-city-lite.mmdb.new" ]]

# 4. nothing published at all: a failure, the current file stays
rm -f "$root"/serve-*
if run 2027-02; then
  echo 'succeeded without a download' >&2
  exit 1
fi
[[ $(sha256sum "$root/geoip/dbip-city-lite.mmdb") == "$before" ]]
echo 'smotryashchiy-geoip: PASS'
