# CHANGE 21 — Current containers only, and server logs at their real level

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `21` |
| Slug | `containers-and-log-levels` |
| Title | Current containers only, and server logs at their real level |
| Status | `archived` |
| Branch | `feature/21-containers-and-log-levels` |

---

## Goal

The 2026-09-23 audit after the infraegev2 136/137 release found two problems on sre.infraege.ru.
First, the CONTAINERS block of `infraege-prod` showed `—` for the running `infraege-api-1`,
`infraege-nginx-1` and `infraege-web-1`, and still listed five removed `infraege-ops-*` containers and
one-off `db-migrate-run-*` containers. Each deploy gives a container a new series (its `image` label
changes). `latest=true` has no window, and `containerInfos` lets whichever series comes last
overwrite the values, so a stale series wins. The SPEC claimed that exited containers age out on
their own; they don't. Second, the server writes its per-request access line to stderr. The agent
maps stderr to `warn`, so every dashboard request shows up as a warning event from the server
container. Contract: `docs/SPEC.md` v1.21 (§4h, §5 CONTAINERS).

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     Group items by area (Backend / Frontend / Infra / Data, etc.).
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. Mark removed items as ~~BN~~ (removed).
     New items always take the next unused ID in their group, appended at the end. -->

### Frontend
- [x] `F1` `containerInfos` (§5): per container name and metric, use the series with the newest
      point, and show its `image`. Omit a container with no fresh sample in any series. Tests first,
      with the production shape: one container with stale/fresh/stale series in that order across
      three images shows the fresh values and the new image; a removed container and a one-off
      container with only stale points are not listed; sorting is unchanged. Browser check with
      Playwriter on a local server. — _Depends on:_ —

### Backend
- [x] `B1` Server log streams (§4h): the server installs a default `slog` handler that writes
      Debug/Info records to stdout and Warn/Error records to stderr. The access line becomes
      `slog.Info` and the recovered panic becomes `slog.Error`. Tests: the handler routes by level; a
      request writes its access line only to stdout; a panic is logged to stderr. The agent is
      unchanged. — _Depends on:_ —

### Infra
- [x] `I1` Turn off UFW packet logging on the monitoring VPS (`ufw logging off`): `[UFW BLOCK]` lines
      are ~400 `warn` events per hour of port-scan noise, and fail2ban reads sshd's log, not UFW's.
      Document it and the agent update procedure (checksum-verified binary from the GitHub Release,
      previous binary kept for rollback) in `docs/RUNBOOK.md`. The agent updates themselves need the
      v0.2.5 release and are a release-time Gate Check. — _Depends on:_ —

### Other
- [x] `T1` `docs/KNOWN_GOTCHAS.md`: `latest=true` has no window, so the UI must judge freshness; a
      Docker log stream is the only level signal the agent gets, so a well-behaved container writes
      info to stdout. — _Depends on:_ F1, B1

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate and opt-in Full Gate.
     Do not duplicate that list here. -->

---

## Files

### Create / modify
~~~
web/src/data/detail.ts, web/src/data/detail.test.ts                (F1)
internal/platform/logging/ (new: split handler + test)             (B1)
internal/platform/httpserver/middleware.go (+ test)                (B1)
cmd/smotryashchiy/main.go, cmd/smotryashchiy/server.go             (B1)
docs/RUNBOOK.md                                                    (I1)
docs/KNOWN_GOTCHAS.md                                              (T1)
docs/SPEC.md                                                       (v1.21, done by /plan)
~~~

### Do NOT touch
- The agent's stdout→`info` / stderr→`warn` mapping (SPEC §4h), and the `latest=true` API contract.
- Stored metric series; removed containers age out by the raw TTL as before.

---

## Contracts

See `docs/SPEC.md` §4h and §5 and the Files list above. Do not hand-copy the schema, endpoints,
types, or env vars into this file — the codebase and `SPEC.md` are the source of truth; this file
only tracks what to build and what's left.

---

## Gate Checks

> Fast Gate runs on every `/work`; Full Gate runs only on `/ship`. All gates are defined in
> [docs/STACK.md](../../STACK.md) — this section only records change-specific overrides.

After the release, update the `infraege-prod` agent (v0.2.3) and the `sre-monitoring` agent to
v0.2.5 per `docs/RUNBOOK.md` "Updating an agent", so the fleet runs one version. Then: the
`infraege-prod` CONTAINERS block on sre.infraege.ru lists exactly the running
containers, with values and current images; server request lines arrive as `info`; both agents
report `release` v0.2.5; no new `[UFW BLOCK]` events from `sre-monitoring`.

---

## Architect Review Notes

Use this section after manual product, UX, API, or workflow verification. This is the human-facing
channel for post-implementation fixes.

Add one unchecked checkbox per issue the agent must fix before the change can ship. Keep each item
independently fixable and describe observed behavior plus expected behavior. If the fix may change
SPEC/API/schema/security behavior, say so explicitly in the note.

The agent resolves these items through `/work [XX] review`. Leave an item unchecked while it is
still open. Check it off only after the fix is implemented and re-verified. If manual verification
found nothing, keep the default checked line below.

- [x] No architect review issues recorded

---

## Implementation Notes

- `latest=true` keeps its contract (no window); the UI now owns "current". The old unit test that
  expected an all-stale container listed with `—` encoded the bug and was rewritten: a stale metric
  next to fresh ones still shows `—`, an all-stale container is omitted.
- Local browser check (Playwriter, dev server fed through `agent push-file` with the production shape:
  three `infraege-api-1` series across images, a removed `infraege-ops-umami-1`, a one-off
  `db-migrate-run`): only running containers listed, current images and values shown, no console
  errors. The same run logged 58 access lines as `level=INFO` on stdout and nothing on stderr.
- `ufw logging off` applied on the monitoring VPS on 2026-09-23 (sshd jail unaffected). The
  infraege.ru host gets the same step in infraegev2's own change.

---

## Commit Message

```
fix(change-21): current containers only, server logs by level
```
