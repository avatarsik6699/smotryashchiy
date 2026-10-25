"""Read-only SDD history and verified Git snapshot support (Python standard library)."""

import argparse
import hashlib
import json
import re
import subprocess
import sys
from datetime import date
from pathlib import Path, PurePosixPath

NUMBERED = re.compile(r"([0-9]{2,})-[a-z0-9-]+\.md\Z")
CHECKPOINT = Path("docs/changes/archive/COMPACTED.md")
MARKER = "<!-- compacted-metadata -->"
LEGACY_FIELDS = {
    "covered_through",
    "source_commit",
    "date",
    "missing_numbers",
    "source_paths",
    "source_digest",
}
SNAPSHOT_FIELDS = LEGACY_FIELDS | {"covered_from"}
SNAPSHOT_ROOT_FIELDS = {"version", "snapshots"}


class HistoryError(ValueError):
    """History is incomplete or unsafe to interpret."""


def git(root: Path, *args: str) -> bytes:
    result = subprocess.run(["git", "-C", str(root), *args], capture_output=True, check=False)
    if result.returncode:
        raise HistoryError(
            "Git source unavailable. Fetch the recorded source commit/full history "
            "from a trusted repository; do not reset numbering or replace the SHA."
        )
    return result.stdout


def source_blob(root: Path, commit: str, path: str) -> bytes:
    if git(root, "cat-file", "-t", f"{commit}:{path}").strip() != b"blob":
        raise HistoryError(f"Source path is not a file: {path}")
    return git(root, "show", f"{commit}:{path}")


def checked_paths(value: object) -> list[str]:
    if not isinstance(value, list) or not value:
        raise HistoryError("source_paths must be a non-empty list")
    paths: list[str] = []
    for item in value:
        if not isinstance(item, str):
            raise HistoryError("Source paths must be strings")
        path = PurePosixPath(item)
        allowed = item.startswith("docs/") or item.startswith(".impeccable/critique/")
        if (
            not allowed
            or ".." in path.parts
            or str(path) != item
            or "\\" in item
            or any(ord(char) < 32 for char in item)
            or item == str(CHECKPOINT)
        ):
            raise HistoryError(f"Unsafe source path: {item!r}")
        paths.append(item)
    if paths != sorted(set(paths)):
        raise HistoryError("source_paths must be sorted and unique")
    return paths


def digest(root: Path, source: str, paths: list[str], checkout: bool = False) -> str:
    result = hashlib.sha256()
    for path in paths:
        blob = source_blob(root, source, path)
        if checkout:
            resolved_root = root.resolve()
            local = resolved_root / path
            current = resolved_root
            symlinked = False
            for part in PurePosixPath(path).parts:
                current /= part
                if current.is_symlink():
                    symlinked = True
                    break
            if symlinked or not local.is_file() or local.read_bytes() != blob:
                raise HistoryError(f"Checkout differs from snapshot: {path}")
        result.update(path.encode() + b"\0" + hashlib.sha256(blob).digest())
    return result.hexdigest()


def _archive_number(path: str) -> int | None:
    candidate = PurePosixPath(path)
    match = NUMBERED.fullmatch(candidate.name)
    if candidate.parent == CHECKPOINT.parent and match:
        return int(match[1])
    return None


def _validate_snapshot(root: Path, data: object, start: int) -> dict:
    if not isinstance(data, dict) or set(data) != SNAPSHOT_FIELDS:
        raise HistoryError("Missing or unknown snapshot metadata fields")
    covered_from = data["covered_from"]
    through = data["covered_through"]
    if type(covered_from) is not int or covered_from != start or covered_from < 1:
        raise HistoryError("Snapshot ranges must be sequential and start at the next number")
    if type(through) is not int or through < covered_from:
        raise HistoryError("covered_through must be at least covered_from")
    source = data["source_commit"]
    if not isinstance(source, str) or not re.fullmatch(r"[0-9a-f]{40}", source):
        raise HistoryError("source_commit must be a full immutable SHA")
    if git(root, "cat-file", "-t", source).strip() != b"commit":
        raise HistoryError("source_commit is not a commit")
    try:
        date.fromisoformat(data["date"])
    except (TypeError, ValueError) as exc:
        raise HistoryError("Invalid snapshot date") from exc
    missing = data["missing_numbers"]
    if (
        not isinstance(missing, list)
        or any(type(n) is not int or not covered_from <= n <= through for n in missing)
        or missing != sorted(set(missing))
    ):
        raise HistoryError("Invalid missing_numbers")
    paths = checked_paths(data["source_paths"])
    archived = [_archive_number(path) for path in paths]
    for path, number in zip(paths, archived, strict=True):
        if path.startswith("docs/changes/") and number is None:
            raise HistoryError("Only numbered archived changes may enter the snapshot")
        if number is not None and not covered_from <= number <= through:
            raise HistoryError("Snapshot archive path is outside its covered range")
    archive_numbers = [number for number in archived if number is not None]
    expected = [n for n in range(covered_from, through + 1) if n not in missing]
    if sorted(archive_numbers) != expected:
        raise HistoryError("Archive coverage does not match covered range/missing_numbers")
    # Missing numbers cannot hide an archived source document in this range.
    source_archives = (
        git(root, "ls-tree", "--name-only", source, "docs/changes/archive/").decode().splitlines()
    )
    represented = {path for path, number in zip(paths, archived, strict=True) if number}
    for path in source_archives:
        number = _archive_number(path)
        if number is not None and covered_from <= number <= through and path not in represented:
            raise HistoryError(f"Unrepresented source archive: {path}")
    if data["source_digest"] != digest(root, source, paths):
        raise HistoryError("Snapshot digest mismatch")
    return data


def validate_metadata(root: Path, data: object) -> dict:
    """Validate legacy v1 metadata and normalize it to the v2 in-memory form."""
    if not isinstance(data, dict):
        raise HistoryError("Checkpoint metadata must be an object")
    if set(data) == LEGACY_FIELDS:
        # v1 implicitly covered 1..covered_through. Preserve every original proof value.
        legacy = dict(data)
        normalized = {"covered_from": 1, **legacy}
        return {"version": 2, "snapshots": [_validate_snapshot(root, normalized, 1)]}
    if (
        set(data) != SNAPSHOT_ROOT_FIELDS
        or type(data["version"]) is not int
        or data["version"] != 2
    ):
        raise HistoryError("Expected legacy checkpoint metadata or version 2 snapshots")
    snapshots = data["snapshots"]
    if not isinstance(snapshots, list) or not snapshots:
        raise HistoryError("Version 2 checkpoint must contain snapshots")
    validated: list[dict] = []
    seen_source_paths: set[tuple[str, str]] = set()
    next_number = 1
    for snapshot_data in snapshots:
        snapshot = _validate_snapshot(root, snapshot_data, next_number)
        source = snapshot["source_commit"]
        for path in snapshot["source_paths"]:
            key = source, path
            if key in seen_source_paths:
                raise HistoryError("Duplicate source/path proof across snapshots")
            seen_source_paths.add(key)
        validated.append(snapshot)
        next_number = snapshot["covered_through"] + 1
    return {"version": 2, "snapshots": validated}


def checkpoint(root: Path) -> dict | None:
    path = root / CHECKPOINT
    if not path.exists():
        # A deleted tracked checkpoint is not an empty initial project.
        tracked = git(root, "ls-files", "--", str(CHECKPOINT)).strip()
        committed = git(root, "log", "-1", "--format=%H", "--", str(CHECKPOINT)).strip()
        if tracked or committed:
            raise HistoryError("COMPACTED.md is missing; restore its metadata before planning")
        return None
    content = path.read_text()
    matches = re.findall(re.escape(MARKER) + r"\s*```json\s*\n(.*?)\n```", content, re.S)
    if len(matches) != 1 or content.count(MARKER) != 1:
        raise HistoryError("Expected exactly one compacted metadata block")
    try:
        data = json.loads(matches[0])
    except json.JSONDecodeError as exc:
        raise HistoryError("Invalid checkpoint JSON") from exc
    return validate_metadata(root, data)


def inspect(root: Path) -> dict:
    metadata = checkpoint(root)
    covered = metadata["snapshots"][-1]["covered_through"] if metadata else 0
    active: list[str] = []
    numbers: set[int] = set()
    for folder in (root / "docs/changes", root / "docs/changes/archive"):
        for path in sorted(folder.glob("*.md")):
            match = NUMBERED.fullmatch(path.name)
            if not match:
                if path != root / CHECKPOINT:
                    raise HistoryError(f"Unexpected change filename: {path}")
                continue
            number = int(match[1])
            if number < 1 or number in numbers or number <= covered:
                raise HistoryError(f"Duplicate or already compacted number: {number}")
            numbers.add(number)
            if folder.name != "archive":
                active.append(path.relative_to(root).as_posix())
    if len(active) > 1:
        raise HistoryError("Multiple active changes; resolve them before planning")
    return {"next": max(numbers | {covered}) + 1, "active": active, "covered_through": covered}


def snapshot(root: Path, source: str, through: int, paths: list[str]) -> dict:
    if not re.fullmatch(r"[0-9a-f]{40}", source):
        raise HistoryError("Snapshot source must be a full SHA")
    previous = checkpoint(root)
    existing = previous["snapshots"] if previous else []
    start = existing[-1]["covered_through"] + 1 if existing else 1
    checked_paths(paths)
    data = {
        "covered_from": start,
        "covered_through": through,
        "source_commit": source,
        "date": date.today().isoformat(),
        "missing_numbers": [
            n
            for n in range(start, through + 1)
            if n not in {_archive_number(path) for path in paths}
        ],
        "source_paths": paths,
        "source_digest": digest(root, source, paths, checkout=True),
    }
    _validate_snapshot(root, data, start)
    result = {"version": 2, "snapshots": [*existing, data]}
    return validate_metadata(root, result)


def read(root: Path, path: str, source: str | None = None) -> bytes:
    if source is not None and not re.fullmatch(r"[0-9a-f]{40}", source):
        raise HistoryError("Selected source must be a full SHA")
    metadata = checkpoint(root)
    if not metadata:
        raise HistoryError("Path is not represented by the checkpoint")
    matches = [
        snapshot_data
        for snapshot_data in metadata["snapshots"]
        if path in snapshot_data["source_paths"]
        and (source is None or snapshot_data["source_commit"] == source)
    ]
    if not matches:
        raise HistoryError("Path is not represented by the selected checkpoint source")
    if len(matches) > 1:
        raise HistoryError("Path appears in multiple snapshots; specify --source SHA")
    return source_blob(root, matches[0]["source_commit"], path)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path.cwd())
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("inspect")
    sub.add_parser("next")
    read_command = sub.add_parser("read")
    read_command.add_argument("path")
    read_command.add_argument("--source", help="select the full source commit SHA")
    create = sub.add_parser("snapshot")
    create.add_argument("source")
    create.add_argument("through", type=int)
    create.add_argument("paths_file", type=Path)
    args = parser.parse_args()
    root = args.root.resolve()
    try:
        if args.command == "snapshot":
            result = snapshot(
                root, args.source, args.through, args.paths_file.read_text().splitlines()
            )
        elif args.command == "read":
            sys.stdout.buffer.write(read(root, args.path, args.source))
            return 0
        else:
            result = inspect(root)
            if args.command == "next":
                if result["active"]:
                    raise HistoryError("Ship the active change before planning another")
                print(f"{result['next']:02d}")
                return 0
        print(json.dumps(result, indent=2, ensure_ascii=False))
        return 0
    except (HistoryError, OSError) as exc:
        print(f"History error: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
