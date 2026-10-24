#!/usr/bin/env bash
# Off-host backup of a running smotryashchiy server (docs/SPEC.md §4f, Change 29).
#
#   smotryashchiy-backup.sh backup   daily: `admin backup --out -` streamed into Restic, keep the last 2
#   smotryashchiy-backup.sh check    weekly: `restic check` reading a sample of the stored data
#   smotryashchiy-backup.sh drill    monthly: restore the latest bundle into a throwaway volume
#   smotryashchiy-backup.sh summary  hourly: rewrite the backup summary only
#
# Nothing is kept on this host: the bundle streams from the server container straight into the
# Restic repository. Every mode rewrites the `smotryashchiy.backup/1` summary that this host's agent
# reports with `--document backup=$STATE_DIR/backup.json`; a failed mode exits non-zero so its systemd
# unit fails. Settings and secrets come from one root-only environment file, never from arguments.
set -euo pipefail

ENV_FILE=${SMOTRYASHCHIY_BACKUP_ENV:-/etc/smotryashchiy/backup.env}
STATE_DIR=${SMOTRYASHCHIY_BACKUP_STATE:-/var/lib/smotryashchiy}
RESTIC_IMAGE=${RESTIC_IMAGE:-restic/restic:0.16.4}
CACHE_VOLUME=smotryashchiy-restic-cache
TAG=smotryashchiy
BUNDLE_NAME=smotryashchiy-backup.tar.gz

fail() {
  echo "smotryashchiy-backup: $*" >&2
  exit 1
}

mode=${1:-}
case $mode in
  backup | check | drill | summary) ;;
  *)
    echo "usage: $0 backup|check|drill|summary" >&2
    exit 64
    ;;
esac
command -v jq >/dev/null || fail 'jq is required'
command -v docker >/dev/null || fail 'docker is required'

# The env file holds the repository password and storage keys: root-owned, mode 600, a real file.
[[ -f $ENV_FILE && ! -L $ENV_FILE ]] || fail "missing $ENV_FILE"
# The owner is root in production; the shell test runs unprivileged and names its own uid.
[[ $(stat -c '%u:%a' "$ENV_FILE") == "${SMOTRYASHCHIY_BACKUP_ENV_OWNER:-0}:600" ]] || fail "$ENV_FILE must be owned by root with mode 600"
set -a
# shellcheck source=/dev/null
source "$ENV_FILE"
set +a
: "${RESTIC_REPOSITORY:?RESTIC_REPOSITORY is required in $ENV_FILE}"
: "${RESTIC_PASSWORD:?RESTIC_PASSWORD is required in $ENV_FILE}"
: "${COMPOSE_FILE:?COMPOSE_FILE (the server compose file) is required in $ENV_FILE}"
SERVICE=${SERVICE:-server}
BACKUP_HOST=${BACKUP_HOST:-$(hostname)}

umask 077
install -d -m 755 "$STATE_DIR"
install -d -m 700 "$STATE_DIR/backup-jobs"
exec 9>"$STATE_DIR/backup.lock"
flock -n 9 || fail 'another backup job is running'

# Restic runs from its pinned image; secrets pass as environment names, never as argument values.
restic() {
  docker run --rm -i \
    --env RESTIC_REPOSITORY --env RESTIC_PASSWORD \
    --env AWS_ACCESS_KEY_ID --env AWS_SECRET_ACCESS_KEY --env AWS_DEFAULT_REGION \
    --env RESTIC_CACHE_DIR=/cache --volume "$CACHE_VOLUME:/cache" \
    "$RESTIC_IMAGE" "$@"
}

now() { date --utc +%FT%TZ; }

# record JOB STATUS SECONDS [SOURCE]: the outcome of the last run of one mode.
record() {
  jq -n --arg status "$2" --arg at "$(now)" --argjson seconds "$3" --arg source "${4:-}" \
    '{status:$status, completed_at:$at, duration_seconds:$seconds} + (if $source == "" then {} else {source:$source} end)' \
    >"$STATE_DIR/backup-jobs/$1.json.tmp"
  mv "$STATE_DIR/backup-jobs/$1.json.tmp" "$STATE_DIR/backup-jobs/$1.json"
}

# The mode functions run under `|| status=$?`, where bash ignores `set -e`: every step returns
# its own failure explicitly, so a failed step never lets the next one run.
repository_facts() {
  local snapshots size
  snapshots=$(restic snapshots --json --tag "$TAG" | jq 'length') || return
  size=$(restic stats --json --mode raw-data | jq '.total_size') || return
  jq -n --argjson snapshots "$snapshots" --argjson size "$size" '{name:"offsite", snapshots:$snapshots, size_bytes:$size}' \
    >"$STATE_DIR/backup-jobs/repository.json"
}

# The summary the agent reports. A job that never ran has no file: it shows as never completed.
write_summary() {
  local job_file disk_line failed
  job() {
    job_file="$STATE_DIR/backup-jobs/$1.json"
    if [[ -f $job_file ]]; then
      jq --arg name "$1" --arg lbl "$2" --argjson max "$3" '{name:$name, "label":$lbl, max_age_hours:$max} + .' "$job_file"
    else
      jq -n --arg name "$1" --arg lbl "$2" --argjson max "$3" \
        '{name:$name, "label":$lbl, status:"success", completed_at:null, max_age_hours:$max}'
    fi
  }
  disk_line=$(df -B1 --output=target,avail,size /var/lib/docker | tail -1)
  # `list-units` is required: systemd 255 reads a bare `--failed PATTERN` as an unknown verb (Change 31).
  failed=$(systemctl list-units --failed --plain --no-legend 'smotryashchiy-backup@*' 2>/dev/null | awk '{print $1}' | jq -R . | jq -s .)
  {
    job backup 'off-site backup' 36
    job check 'off-site check' 192
    job drill 'restore drill' 840
  } | jq -s \
    --arg at "$(now)" \
    --argjson disk "$(awk '{printf "{\"path\":\"%s\",\"free_bytes\":%s,\"total_bytes\":%s}", $1, $2, $3}' <<<"$disk_line")" \
    --argjson failed "$failed" \
    --slurpfile repo <(cat "$STATE_DIR/backup-jobs/repository.json" 2>/dev/null || true) \
    '{schema:"smotryashchiy.backup/1", generated_at:$at, jobs:., repositories:$repo, holds:[], disk:$disk, failed_units:$failed}' \
    >"$STATE_DIR/backup.json.tmp"
  chmod 644 "$STATE_DIR/backup.json.tmp"
  mv "$STATE_DIR/backup.json.tmp" "$STATE_DIR/backup.json"
}

run_backup() {
  local out="$STATE_DIR/backup-jobs/backup-output.json" statuses snapshot
  restic cat config >/dev/null 2>&1 || restic init >/dev/null || return
  set +e
  docker compose -f "$COMPOSE_FILE" exec -T "$SERVICE" /smotryashchiy admin backup --out - |
    restic backup --stdin --stdin-filename "$BUNDLE_NAME" --tag "$TAG" --host "$BACKUP_HOST" --json >"$out"
  statuses=("${PIPESTATUS[@]}")
  set -e
  snapshot=$(jq -r 'select(.message_type == "summary") | .snapshot_id // empty' "$out" 2>/dev/null | tail -1)
  rm -f -- "$out"
  if [[ ${statuses[0]} != 0 ]]; then
    # Restic stores whatever arrived, even an empty stream: drop that snapshot, or keeping only the
    # last two would push a good copy out and the next drill would restore the broken one.
    [[ -z $snapshot ]] || restic forget "$snapshot" --prune --quiet || true
    return "${statuses[0]}"
  fi
  [[ ${statuses[1]} == 0 && -n $snapshot ]] || return 1
  # Monitoring data is not critical (architect 2026-10-19): the latest copy and one before it.
  restic forget --tag "$TAG" --keep-last 2 --prune --quiet || return
  repository_facts
}

run_check() {
  restic check --read-data-subset=10% >/dev/null || return
  repository_facts
}

run_drill() {
  local container image volume rc=0
  container=$(docker compose -f "$COMPOSE_FILE" ps -q "$SERVICE") || return
  [[ -n $container ]] || return 1
  image=$(docker inspect --format '{{.Config.Image}}' "$container") || return
  volume="smotryashchiy-drill-$$"
  docker volume create "$volume" >/dev/null || return
  # `admin restore` verifies the manifest hash, `integrity_check` and the migration count.
  restic dump --tag "$TAG" latest "/$BUNDLE_NAME" |
    docker run --rm -i --volume "$volume:/data" --env SMOTRYASHCHIY_DB_PATH=/data/smotryashchiy.db \
      "$image" admin restore --from - >/dev/null || rc=$?
  docker volume rm --force "$volume" >/dev/null 2>&1 || true
  return "$rc"
}

started=$SECONDS
status=0
case $mode in
  backup) run_backup || status=$? ;;
  check) run_check || status=$? ;;
  drill) run_drill || status=$? ;;
  summary) ;;
esac
if [[ $mode != summary ]]; then
  if [[ $status == 0 ]]; then
    record "$mode" success $((SECONDS - started)) "$([[ $mode == drill ]] && echo offsite)"
  else
    record "$mode" failed $((SECONDS - started))
  fi
fi
write_summary
[[ $status == 0 ]] || fail "$mode failed (exit $status)"
