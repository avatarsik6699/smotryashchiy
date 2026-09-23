# CHANGE 23 — Routine SSH pre-auth rejections do not count as errors

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `23` |
| Slug | `routine-ssh-errors` |
| Title | Routine SSH pre-auth rejections do not count as errors |
| Status | `archived` |
| Branch | `feature/23-routine-ssh-errors` |

---

## Goal

After the v0.2.6 release the summary line on sre.infraege.ru read "Watch: errors in the last hour:
6". Almost every one of those errors was sshd on the monitoring VPS logging `error: maximum
authentication attempts exceeded for root from … [preauth]`: bots guessing the root password.
Password login stays by architect decision, so these lines never stop. `MaxAuthTries` and fail2ban
already handle them, and the fail2ban check reports the bans. As long as they count, the summary
cannot say "All good", which defeats its purpose. Architect decision on 2026-09-23: they do not
count as errors. They stay visible in EVENTS, and the guide says how many were set aside.
Contract: `docs/SPEC.md` v1.23 §5.

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
- [x] `F1` Tests first. `isRoutineSshRejection(event)` is true for an error or critical event
      whose unit is `ssh`, `ssh.service`, `sshd` or `sshd.service` and whose message ends in
      `[preauth]`:
      - `errorsLastHour` counts only the other errors;
      - `routineSshRejectionsLastHour` counts these;
      - a post-auth sshd error and a `[preauth]` text from another unit still count.

      — _Depends on:_ —
- [x] `F2` The summary and the error assessment use the new count. The events lesson's live values
      show the rejections set aside and point to the fail2ban check, and the lesson text explains
      them. — _Depends on:_ F1

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate and opt-in Full Gate.
     Do not duplicate that list here. -->

---

## Files

### Create / modify
~~~
web/src/data/model.ts (+ test), web/src/data/health.ts (+ test)   (F1, F2)
web/src/guide/live.ts, web/src/guide/lessons.ts (+ guide test)     (F2)
docs/SPEC.md (v1.23, done by /plan)
~~~

### Do NOT touch
- The agent and server (event levels stay as sshd reports them); EVENTS still lists every line.

---

## Contracts

See `docs/SPEC.md` §5 and the Files list above. Do not hand-copy the schema, endpoints, types, or
env vars into this file — the codebase and `SPEC.md` are the source of truth; this file only
tracks what to build and what's left.

---

## Gate Checks

> Fast Gate runs on every `/work`; Full Gate runs only on `/ship`. All gates are defined in
> [docs/STACK.md](../../STACK.md) — this section only records change-specific overrides.

After release: the server runs v0.2.7; agents are unchanged. On production, the summary no longer
counts sshd `[preauth]` lines, and the events lesson shows how many were set aside.

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

- `state.errors` still holds every error and critical event; only the count excludes routine
  rejections, so the events lesson can show both numbers. The summary and assessment follow
  automatically through `errorsLastHour`.
- Matching needs all three: an error level, an sshd unit label, and a message ending in
  `[preauth]`. An sshd error without it, or `[preauth]` text from another unit, still counts.

---

## Commit Message

```
fix(change-23): do not count routine SSH pre-auth rejections as errors
```
