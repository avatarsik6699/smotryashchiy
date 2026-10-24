#!/usr/bin/env bash
# Contract test for deploy/backup/smotryashchiy-backup.sh (Change 29): docker, systemctl and df are
# fakes; the Restic repository is a directory the fake restic keeps its snapshots in.
set -euo pipefail
script=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)/smotryashchiy-backup.sh
root=$(mktemp -d)
trap 'rm -rf -- "$root"' EXIT
mkdir -p "$root/bin" "$root/state" "$root/repo"
export FAKE="$root" PATH="$root/bin:$PATH"
export SMOTRYASHCHIY_BACKUP_ENV="$root/backup.env" SMOTRYASHCHIY_BACKUP_STATE="$root/state"
SMOTRYASHCHIY_BACKUP_ENV_OWNER=$(id -u)
export SMOTRYASHCHIY_BACKUP_ENV_OWNER
secret='synthetic-password-must-not-leak'
cat >"$root/backup.env" <<ENV
RESTIC_REPOSITORY=s3:https://storage.example/bucket
RESTIC_PASSWORD=$secret
AWS_ACCESS_KEY_ID=synthetic-key-id
AWS_SECRET_ACCESS_KEY=synthetic-secret-key
COMPOSE_FILE=/opt/smotryashchiy/docker-compose.yml
BACKUP_HOST=monitor
ENV
chmod 600 "$root/backup.env"

cat >"$root/bin/docker" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$FAKE/argv"
if [[ $1 == compose ]]; then
  case " $* " in
    *' admin backup --out - '*) [[ ${FAKE_BACKUP_FAIL:-0} == 0 ]] || exit 3; printf 'bundle-%s' "$(date +%s%N)" ;;
    *' ps -q server '*) echo cid ;;
    *) exit 99 ;;
  esac
  exit 0
fi
case $1 in
  inspect) echo ghcr.io/example/smotryashchiy:v0.4.0; exit 0 ;;
  volume) exit 0 ;;
esac
[[ $1 == run ]] || exit 99
shift
args=("$@")
image=''
for i in "${!args[@]}"; do
  if [[ ${args[$i]} == restic/restic:* || ${args[$i]} == ghcr.io/* ]]; then image=${args[$i]}; rest=("${args[@]:$((i + 1))}"); break; fi
done
if [[ $image == ghcr.io/* ]]; then
  [[ ${rest[*]} == 'admin restore --from -' ]] || exit 98
  grep -q '^bundle-' || exit 4
  exit 0
fi
[[ ${RESTIC_PASSWORD:-} == synthetic-password-must-not-leak ]] || exit 97
repo=$FAKE/repo
case ${rest[0]} in
  cat) [[ -f $repo/config ]] ;;
  init) touch "$repo/config" ;;
  backup)
    id=$(date +%s%N)
    cat >"$repo/snap-$id"
    printf '{"message_type":"status"}\n{"message_type":"summary","snapshot_id":"%s"}\n' "$id"
    ;;
  forget)
    if [[ ${rest[1]} =~ ^[0-9]+$ ]]; then rm -- "$repo/snap-${rest[1]}"; else ls "$repo"/snap-* | sort | head -n -2 | xargs -r rm --; fi
    ;;
  snapshots) n=$(ls "$repo"/snap-* 2>/dev/null | wc -l); jq -n --argjson n "$n" '[range($n)] | map({id:"x"})' ;;
  stats) echo '{"total_size":2048}' ;;
  check) [[ ${FAKE_CHECK_FAIL:-0} == 0 ]] ;;
  dump) cat "$(ls "$repo"/snap-* | sort | tail -1)" ;;
  *) exit 96 ;;
esac
FAKE
cat >"$root/bin/systemctl" <<'FAKE'
#!/usr/bin/env bash
# Like systemd 255: a unit pattern needs the list-units verb, or the call fails (Change 31).
[[ ${1:-} == list-units && " $* " == *" --failed "* ]] || {
  echo "Unknown command verb '${*: -1}'." >&2
  exit 1
}
cat "$FAKE/failed-units" 2>/dev/null || true
FAKE
cat >"$root/bin/df" <<'FAKE'
#!/usr/bin/env bash
printf 'Mounted on Avail Size\n/ 3000 20000\n'
FAKE
chmod +x "$root/bin/"*

summary() { jq -r "$1" "$root/state/backup.json"; }
snapshots() { find "$root/repo" -name 'snap-*' | wc -l; }
job() { summary ".jobs[] | select(.name == \"$1\") | $2"; }

# A fresh host: the summary exists before any job ran, and every job reads as never completed.
bash "$script" summary
[[ $(summary .schema) == smotryashchiy.backup/1 ]]
[[ $(job backup .completed_at) == null ]]
[[ $(summary '.disk.free_bytes') == 3000 ]]

# Three daily backups keep only the last two; the backup job is the primary (first) job.
for _ in 1 2 3; do bash "$script" backup; done
[[ $(snapshots) == 2 ]]
[[ $(summary '.jobs[0].name') == backup && $(job backup .status) == success ]]
[[ $(summary '.repositories[0].snapshots') == 2 && $(summary '.repositories[0].size_bytes') == 2048 ]]

bash "$script" check
bash "$script" drill
[[ $(job drill .status) == success && $(job drill .source) == offsite ]]

# A failed `admin backup` stores nothing, records the failure and fails the unit.
before=$(snapshots)
if FAKE_BACKUP_FAIL=1 bash "$script" backup 2>/dev/null; then echo 'failed backup reported success' >&2; exit 1; fi
[[ $(job backup .status) == failed ]]
[[ $(snapshots) == "$before" ]]
# The surviving copies are both real bundles, so the next drill restores a good one.
if grep -L '^bundle-' "$root/repo"/snap-* | grep -q .; then echo 'an empty snapshot survived' >&2; exit 1; fi
if FAKE_CHECK_FAIL=1 bash "$script" check 2>/dev/null; then echo 'failed check reported success' >&2; exit 1; fi
[[ $(job check .status) == failed ]]

# Failed units reach the summary.
echo 'smotryashchiy-backup@backup.service loaded failed failed backup' >"$root/failed-units"
bash "$script" summary
[[ $(summary '.failed_units[0]') == smotryashchiy-backup@backup.service ]]

# Secrets travel as environment names only, never as argument values.
if grep -q -e "$secret" -e synthetic-secret-key "$root/argv"; then echo 'a secret reached argv' >&2; exit 1; fi

# A loose env file is refused before anything runs.
chmod 644 "$root/backup.env"
if bash "$script" backup 2>/dev/null; then echo 'loose env file accepted' >&2; exit 1; fi
echo 'smotryashchiy-backup: all checks passed'
