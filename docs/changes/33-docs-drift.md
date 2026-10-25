# CHANGE 33 — Documentation drift and history in SPEC

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `33` |
| Slug | `docs-drift` |
| Title | Documentation drift and history in SPEC |
| Status | `done` |
| Branch | `feature/33-docs-drift` |

---

## Goal

Second block of the architect's audit plan (2026-10-25): documents that contradict the code are
corrected, and SPEC history that no longer carries a contract is removed. Documentation only.

---

## Backlog

### Docs
- [x] `D1` SPEC: alerts are not built (entities, diagram), `admin` modes (no telemetry reset), static
  delivery (`.gitkeep` + `placeholder.html`), 16 guide lessons, five view tabs, image smoke runs on
  amd64 only, tracked-site wording instead of `infraege.ru`. — _Depends on:_ —
- [x] `D2` SPEC history: the transport spike, the Change 04 agent note, the «until then» compose note
  and the Change 19 timing anecdote are shortened; §7 Roadmap and §8 Open Questions say what is open;
  the struck-through §9 v2 backlog is removed (68 → 65 KB). — _Depends on:_ —
- [x] `D3` STACK: stack status, the stale «pending Stage-2 spike» transport row, project structure
  (analytics, platform packages, `deploy/backup`, `scripts/`), gopls row, «one page with view
  tabs». — _Depends on:_ —
- [x] `D4` RUNBOOK and DEPLOY:
  - `--document KIND=PATH` replaces the older `--business-file` (kept as an alias);
  - the agent update example uses the release being installed;
  - `SMOTRYASHCHIY_ANALYTICS_RETENTION_DAYS` added to the disk playbook;
  - an example agent unit;
  - the database contents listed in full. — _Depends on:_ —
- [x] `D5` Other files:
  - `.env.example`: proxies or TLS domain, `PUBLIC_URL` and `PUBLIC_ENDPOINT`;
  - the compose comment;
  - README status;
  - AGENTS no longer says frontend conventions are missing;
  - `docs/reference/` PRODUCT and DESIGN are labelled as predecessor documents;
  - playbooks name only the Claude Code wrappers this repository has. — _Depends on:_ —

---

## Gate Checks

Documentation only: links and paths checked by reading; Fast Gate rows are not affected.

### Gate evidence

| HEAD | Groups / focused checks | Result | Seconds |
|------|-------------------------|--------|---------|
| c447d17+wt | `gofmt -l`, `go vet` (unchanged code), referenced paths exist | PASS | 10 |

---

## Implementation Notes

Duplicated env and backup descriptions across SPEC, STACK, DEPLOY and RUNBOOK were left. SPEC states
the contract, the runbooks state the steps, and merging them would mix the two.

---

## Commit Message

```
docs(change-33): correct drift and cut history from SPEC
```
