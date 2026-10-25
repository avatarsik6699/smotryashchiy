# Change history and compaction

`python3 scripts/change_history.py inspect` validates ordinary/compacted history and reports the
active file, covered range and next number. `python3 scripts/change_history.py next` refuses
planning until the active change is shipped. COMPACTED.md is not an active change. Numbering uses
`max(covered_through, active numbers, remaining archived numbers) + 1` (at least two digits).

## Reading compacted originals

```bash
python3 scripts/change_history.py read docs/changes/archive/01-project-skeleton.md
# A path present in several snapshots requires the original full source SHA:
python3 scripts/change_history.py read docs/changes/archive/01-project-skeleton.md --source <full source SHA>
```

`read` writes exact bytes to stdout and never touches the checkout; redirect binary data to a new
temporary path. The recorded full source SHA is immutable, independent of branch/tag movement.
This is local Git preservation, not an off-site backup. Missing source objects (including shallow
clones) stop the tool: fetch the recorded commit/full history from a trusted copy and rerun; never
substitute HEAD or reset numbering. After a normal ship, rerun `inspect`.

## Approved compaction

Only an explicitly approved compaction change may replace archives (AGENTS § Repo Memory Files).

1. List exact repository-relative files in a temporary sorted path file. Files deleted outside
   compaction must first be restored from their last commit so they enter verified history.
2. Run `python3 scripts/change_history.py snapshot <full-source-sha> <covered-through> <paths-file>`.
   The source must contain every original; each local file is compared byte-for-byte with its Git
   blob; symlinks/unsafe paths are rejected and archive coverage must be complete, including
   explicitly missing numbers. Only `docs/` sources and completed `.impeccable/critique/` artifacts
   are allowed; active changes and COMPACTED.md cannot be inputs.
3. Replace the single `<!-- compacted-metadata -->` fenced `json` block in
   `docs/changes/archive/COMPACTED.md` with the printed metadata (version 2: ordered `snapshots`
   with `covered_from`, `covered_through`, full `source_commit`, `source_paths`, `source_digest`,
   date and missing numbers; previous snapshots are retained unchanged).
4. Add compact prose: outcomes, decisions, human approvals, accepted risks and unresolved items.
5. Delete only the manifest files, rerun `inspect`, and prove every path with `read --source`.

The command never edits metadata or deletes files itself.

Tooling gate: `python3 -m unittest discover -s scripts/tests -p change_history_test.py` plus the
STACK Full Gate «History tooling» row. These are stdlib-only
checks; no app runtime, browser, API or deployment is involved.
