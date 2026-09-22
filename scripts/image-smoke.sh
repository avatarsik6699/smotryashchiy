#!/usr/bin/env bash
# Builds the image and proves it works as shipped (docs/SPEC.md §4f): runs as non-root with a read-only
# root filesystem, becomes healthy, serves the UI, keeps its data across a restart, takes and restores a
# backup, boots in production mode and refuses to boot with an incomplete production configuration.
# Usage: scripts/image-smoke.sh [image-tag]   (env: SMOKE_PORT, default 18140; needs docker, curl)
set -Eeuo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
image="${1:-smotryashchiy:smoke}"
port="${SMOKE_PORT:-18140}"
run_id="smoke-$$"
work=$(mktemp -d)
containers=("$run_id-a" "$run_id-prod" "$run_id-bad")
volumes=("$run_id-data" "$run_id-prod-data" "$run_id-bad-data")

cleanup() {
  for c in "${containers[@]}"; do docker rm -f "$c" >/dev/null 2>&1 || true; done
  for v in "${volumes[@]}"; do docker volume rm -f "$v" >/dev/null 2>&1 || true; done
  rm -rf -- "$work"
}
trap cleanup EXIT

step() { printf '\n== %s\n' "$*"; }
fail() { echo "image-smoke: FAIL: $*" >&2; exit 1; }

wait_healthy() { # container [seconds]
  local c=$1 limit=${2:-60} i status
  for ((i = 0; i < limit; i++)); do
    status=$(docker inspect -f '{{.State.Health.Status}}' "$c" 2>/dev/null || echo missing)
    [ "$status" = healthy ] && return 0
    [ "$(docker inspect -f '{{.State.Running}}' "$c" 2>/dev/null || echo false)" = true ] || fail "$c stopped: $(docker logs --tail 5 "$c" 2>&1 | tr '\n' ' ')"
    sleep 1
  done
  fail "$c did not become healthy within ${limit}s (last status: $status)"
}

step "build $image"
docker build --quiet --build-arg RELEASE="$(git -C "$repo_dir" rev-parse HEAD)" -t "$image" "$repo_dir" >/dev/null

step "run as non-root with a read-only root filesystem"
docker run -d --name "$run_id-a" --read-only -p "127.0.0.1:$port:8080" -v "$run_id-data:/data" "$image" >/dev/null
wait_healthy "$run_id-a"
[ "$(docker inspect -f '{{.Config.User}}' "$run_id-a")" = "65532:65532" ] || fail "image user is not 65532:65532"
[ "$(docker inspect -f '{{.HostConfig.ReadonlyRootfs}}' "$run_id-a")" = true ] || fail "rootfs is not read-only"
[ "$(docker top "$run_id-a" -eo user,pid | awk 'NR==2 {print $1}')" = 65532 ] || fail "the server process is not running as uid 65532"
echo "ok: uid 65532, read-only rootfs, healthy"

step "serves the UI with security headers"
headers=$(curl -fsS -D - -o "$work/index.html" "http://127.0.0.1:$port/")
grep -q '<div id="root">' "$work/index.html" || fail "GET / did not return the SPA shell"
grep -qi '^content-security-policy:' <<<"$headers" || fail "no Content-Security-Policy header"
echo "ok: SPA shell and CSP"

step "log in, create data"
password=$(docker logs "$run_id-a" 2>&1 | sed -n 's/.*(shown once): //p' | head -1)
[ -n "$password" ] || fail "no development password in the logs"
login() {
  curl -fsS -o /dev/null -c "$work/jar" -H 'Content-Type: application/json' -d "{\"password\":\"$password\"}" "http://127.0.0.1:$port/api/auth/login"
}
login
curl -fsS -b "$work/jar" -H 'Content-Type: application/json' -d '{"name":"smoke-host"}' "http://127.0.0.1:$port/api/hosts" | grep -q '"name":"smoke-host"' || fail "host was not created"
curl -fsS -b "$work/jar" -H 'Content-Type: application/json' -d '{"name":"smoke-tcp","kind":"tcp","target":"127.0.0.1:8080"}' "http://127.0.0.1:$port/api/uptime" | grep -q '"name":"smoke-tcp"' || fail "uptime target was not created"
echo "ok: host and uptime target created"

step "data survives a container restart"
docker restart "$run_id-a" >/dev/null
wait_healthy "$run_id-a"
login
hosts=$(curl -fsS -b "$work/jar" "http://127.0.0.1:$port/api/hosts")
grep -q '"name":"smoke-host"' <<<"$hosts" || fail "the host is gone after the restart"
echo "ok: same password works and the host is still there"

step "online backup streamed to the host, then restore streamed into the same volume"
# Streaming avoids every file-permission trap between a 0600 bundle and the container's uid 65532.
(umask 077; docker exec "$run_id-a" /smotryashchiy admin backup --out - > "$work/backup.tar.gz") || fail "backup failed"
[ "$(stat -c %a "$work/backup.tar.gz")" = 600 ] || fail "the bundle on the host is not mode 600"
[ -s "$work/backup.tar.gz" ] || fail "the bundle is empty"
docker stop "$run_id-a" >/dev/null
docker run --rm -i -v "$run_id-data:/data" "$image" admin restore --from - --force < "$work/backup.tar.gz" | grep -q 'restored' || fail "restore failed"
docker start "$run_id-a" >/dev/null
wait_healthy "$run_id-a"
login
curl -fsS -b "$work/jar" "http://127.0.0.1:$port/api/hosts" | grep -q '"name":"smoke-host"' || fail "the host is gone after the restore"
echo "ok: backup taken while running, restored, data intact"

step "production mode boots with a complete configuration"
# Production refuses to start without an admin password: create the container, set the password (stdin
# only) in the same volume, then start it.
docker create --name "$run_id-prod" --read-only -v "$run_id-prod-data:/data" \
  -e SMOTRYASHCHIY_PRODUCTION=true -e SMOTRYASHCHIY_PUBLIC_URL=https://monitor.example.com \
  -e SMOTRYASHCHIY_PUBLIC_ENDPOINT=monitor.example.com:51820 -e SMOTRYASHCHIY_TRUSTED_PROXY_CIDRS=172.16.0.0/12 \
  "$image" >/dev/null
printf '%s\n' "smoke-production-password-0123456789" | docker run --rm -i -v "$run_id-prod-data:/data" \
  -e SMOTRYASHCHIY_PRODUCTION=true -e SMOTRYASHCHIY_PUBLIC_URL=https://monitor.example.com \
  -e SMOTRYASHCHIY_PUBLIC_ENDPOINT=monitor.example.com:51820 -e SMOTRYASHCHIY_TRUSTED_PROXY_CIDRS=172.16.0.0/12 \
  "$image" admin set-password | grep -q 'initialized' || fail "could not set the production admin password"
docker start "$run_id-prod" >/dev/null
wait_healthy "$run_id-prod"
echo "ok: production mode healthy"

step "production mode refuses an incomplete configuration"
docker run -d --name "$run_id-bad" -v "$run_id-bad-data:/data" -e SMOTRYASHCHIY_PRODUCTION=true "$image" >/dev/null
for _ in $(seq 1 20); do
  [ "$(docker inspect -f '{{.State.Running}}' "$run_id-bad")" = false ] && break
  sleep 0.5
done
[ "$(docker inspect -f '{{.State.Running}}' "$run_id-bad")" = false ] || fail "the server started with an incomplete production configuration"
docker logs "$run_id-bad" 2>&1 | grep -q 'SMOTRYASHCHIY_' || fail "the failure did not name the missing setting"
echo "ok: refused, and the log names the setting"

echo
echo "image-smoke: PASS ($image)"
