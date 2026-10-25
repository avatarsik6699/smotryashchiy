"""Exercise the SDD contract using disposable real Git repositories."""

import copy
import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / "change_history.py"
spec = importlib.util.spec_from_file_location("change_history", SCRIPT)
assert spec and spec.loader
history = importlib.util.module_from_spec(spec)
spec.loader.exec_module(history)


class HistoryTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git("init", "-q")
        self.git("config", "user.name", "History test")
        self.git("config", "user.email", "history@example.invalid")
        self.git("commit", "--allow-empty", "-qm", "initial")
        self.archive = self.root / "docs/changes/archive"
        self.archive.mkdir(parents=True)

    def git(self, *args):
        return subprocess.check_output(["git", "-C", str(self.root), *args]).decode().strip()

    def commit(self):
        self.git("add", ".")
        self.git("commit", "-qm", "fixture")
        return self.git("rev-parse", "HEAD")

    def cli(self, *args):
        return subprocess.run(
            [sys.executable, str(SCRIPT), "--root", str(self.root), *args],
            capture_output=True,
            check=False,
        )

    def compact(self):
        for number in (1, 3):
            (self.archive / f"{number:02d}-example.md").write_text(f"history {number}\n")
        asset = self.root / "docs/artifacts/example.bin"
        asset.parent.mkdir()
        asset.write_bytes(b"\x00\xffexact\r\n")
        paths = sorted(
            str(p.relative_to(self.root)) for p in self.root.glob("docs/**/*") if p.is_file()
        )
        source = self.commit()
        data = history.snapshot(self.root, source, 3, paths)
        self.write_metadata(data)
        for path in paths:
            (self.root / path).unlink()
        self.commit()
        return data

    def append_snapshot(self, data, *, source_path="docs/artifacts/example.bin", payload=b"new"):
        self.write_metadata(data)
        archive = self.archive / "04-next.md"
        archive.write_text("history 4\n")
        asset = self.root / source_path
        asset.parent.mkdir(parents=True, exist_ok=True)
        asset.write_bytes(payload)
        source = self.commit()
        paths = sorted(str(path.relative_to(self.root)) for path in (archive, asset))
        return history.snapshot(self.root, source, 4, paths), source

    def write_metadata(self, data):
        (self.root / history.CHECKPOINT).write_text(
            f"# Compacted\n\n{history.MARKER}\n```json\n{json.dumps(data)}\n```\n"
        )

    def test_initial_number_and_ordinary_ship(self):
        self.assertEqual(self.cli("next").stdout, b"01\n")
        active = self.root / "docs/changes/01-first.md"
        active.write_text("active")
        self.assertEqual(history.inspect(self.root)["active"], ["docs/changes/01-first.md"])
        self.assertNotEqual(self.cli("next").returncode, 0)
        active.rename(self.archive / active.name)
        self.assertEqual(self.cli("next").stdout, b"02\n")

    def test_compact_numbering_and_normal_archive_afterward(self):
        data = self.compact()
        self.assertEqual(data["version"], 2)
        self.assertEqual(data["snapshots"][0]["missing_numbers"], [2])
        self.assertEqual(self.cli("next").stdout, b"04\n")
        active = self.root / "docs/changes/04-next.md"
        active.write_text("active")
        self.assertEqual(len(history.inspect(self.root)["active"]), 1)
        active.rename(self.archive / active.name)
        self.assertEqual(self.cli("next").stdout, b"05\n")
        result = self.cli("read", "docs/artifacts/example.bin")
        self.assertEqual(result.stdout, b"\x00\xffexact\r\n")
        self.assertFalse((self.root / "docs/artifacts/example.bin").exists())
        self.assertNotEqual(self.cli("read", "../../secret").returncode, 0)

    def test_corrupt_and_missing_metadata_fail_closed(self):
        original = self.compact()
        for key, value in (
            ("covered_through", True),
            ("source_commit", "main"),
            ("missing_numbers", [1, 2]),
            ("source_digest", "wrong"),
            ("date", "invalid"),
            ("source_paths", ["docs/../secret"]),
            ("covered_from", 0),
        ):
            with self.subTest(key=key):
                data = copy.deepcopy(original)
                data["snapshots"][0][key] = value
                self.write_metadata(data)
                self.assertNotEqual(self.cli("next").returncode, 0)
        data = copy.deepcopy(original)
        del data["snapshots"][0]["source_digest"]
        self.write_metadata(data)
        self.assertNotEqual(self.cli("next").returncode, 0)
        checkpoint = self.root / history.CHECKPOINT
        checkpoint.write_text("# Missing metadata\n")
        self.assertNotEqual(self.cli("next").returncode, 0)
        self.write_metadata(original)
        checkpoint.write_text(checkpoint.read_text() * 2)
        self.assertNotEqual(self.cli("next").returncode, 0)
        checkpoint.unlink()
        self.assertNotEqual(self.cli("next").returncode, 0)
        self.git("add", "-u")
        self.assertNotEqual(self.cli("next").returncode, 0)

    def test_legacy_metadata_is_read_and_preserved_when_appending(self):
        data = self.compact()
        original_proof = data["snapshots"][0]
        legacy = {key: value for key, value in original_proof.items() if key != "covered_from"}
        self.write_metadata(legacy)
        normalized = history.checkpoint(self.root)
        self.assertEqual(normalized["snapshots"][0]["covered_from"], 1)
        self.assertEqual(self.cli("next").stdout, b"04\n")
        for key, value in legacy.items():
            self.assertEqual(normalized["snapshots"][0][key], value)
        self.assertEqual(
            self.cli("read", "docs/artifacts/example.bin").stdout,
            b"\x00\xffexact\r\n",
        )
        appended, _ = self.append_snapshot(normalized)
        self.assertEqual([item["covered_from"] for item in appended["snapshots"]], [1, 4])
        self.assertEqual(appended["snapshots"][0]["source_digest"], legacy["source_digest"])

    def test_sequential_snapshots_resolve_repeated_path_by_source(self):
        first = self.compact()
        old_source = first["snapshots"][0]["source_commit"]
        second, new_source = self.append_snapshot(first, payload=b"new bytes\x00")
        self.write_metadata(second)
        self.assertEqual([s["covered_from"] for s in second["snapshots"]], [1, 4])
        self.assertEqual(
            self.cli("read", "docs/artifacts/example.bin").returncode,
            1,
        )
        self.assertEqual(
            self.cli("read", "docs/artifacts/example.bin", "--source", old_source).stdout,
            b"\x00\xffexact\r\n",
        )
        self.assertEqual(
            self.cli("read", "docs/artifacts/example.bin", "--source", new_source).stdout,
            b"new bytes\x00",
        )
        self.assertNotEqual(
            self.cli("read", "docs/artifacts/example.bin", "--source", old_source[:12]).returncode,
            0,
        )
        (self.archive / "04-next.md").unlink()
        (self.root / "docs/artifacts/example.bin").unlink()
        self.write_metadata(second)
        self.commit()
        self.assertEqual(history.inspect(self.root)["covered_through"], 4)

    def test_duplicate_source_path_proof_is_rejected(self):
        first = self.archive / "01-first.md"
        third = self.archive / "03-third.md"
        fourth = self.archive / "04-fourth.md"
        artifact = self.root / "docs/artifacts/repeated.bin"
        artifact.parent.mkdir(parents=True)
        for path in (first, third, fourth):
            path.write_text(path.name)
        artifact.write_bytes(b"shared")
        source = self.commit()
        initial_paths = sorted(
            str(path.relative_to(self.root)) for path in (first, third, artifact)
        )
        initial = history.snapshot(self.root, source, 3, initial_paths)
        self.write_metadata(initial)
        next_paths = sorted(str(path.relative_to(self.root)) for path in (fourth, artifact))
        with self.assertRaisesRegex(history.HistoryError, "Duplicate source/path"):
            history.snapshot(self.root, source, 4, next_paths)

    def test_missing_source_and_invalid_v2_ranges_fail_closed(self):
        data = self.compact()
        for mutate in (
            lambda value: value["snapshots"].append(copy.deepcopy(value["snapshots"][0])),
            lambda value: value["snapshots"][0].__setitem__("covered_from", 2),
            lambda value: value["snapshots"][0].__setitem__("source_commit", "f" * 40),
            lambda value: value["snapshots"][0].__setitem__(
                "source_paths", [".impeccable/other/file.md"]
            ),
        ):
            malformed = copy.deepcopy(data)
            mutate(malformed)
            self.write_metadata(malformed)
            self.assertNotEqual(self.cli("inspect").returncode, 0)

    def test_corrupt_old_proof_in_v2_checkpoint_fails_closed(self):
        first = self.compact()
        second, _ = self.append_snapshot(first)
        second["snapshots"][0]["source_digest"] = "0" * 64
        self.write_metadata(second)
        self.assertNotEqual(self.cli("inspect").returncode, 0)

    def test_missing_newer_source_in_shallow_clone_fails_closed(self):
        first = self.compact()
        first_source = first["snapshots"][0]["source_commit"]
        second, _ = self.append_snapshot(first)
        self.git("branch", "history-old", first_source)
        with tempfile.TemporaryDirectory() as temp:
            clone = Path(temp) / "shallow"
            subprocess.run(
                [
                    "git",
                    "clone",
                    "-q",
                    "--depth=1",
                    "--branch",
                    "history-old",
                    self.root.as_uri(),
                    str(clone),
                ],
                check=True,
            )
            checkpoint = clone / history.CHECKPOINT
            checkpoint.parent.mkdir(parents=True, exist_ok=True)
            checkpoint.write_text(
                f"# Compacted\n\n{history.MARKER}\n```json\n{json.dumps(second)}\n```\n"
            )
            with self.assertRaisesRegex(history.HistoryError, "Fetch the recorded"):
                history.inspect(clone)

    def test_snapshot_rejects_symlinked_parent_even_with_matching_external_bytes(self):
        archive = self.archive / "01-first.md"
        artifact = self.root / "docs/artifacts/example.bin"
        artifact.parent.mkdir()
        archive.write_text("one\n")
        artifact.write_bytes(b"same bytes\x00")
        source = self.commit()
        paths = sorted([str(archive.relative_to(self.root)), str(artifact.relative_to(self.root))])
        external = Path(self.temp.name) / "outside-artifacts"
        external.mkdir()
        (external / artifact.name).write_bytes(b"same bytes\x00")
        artifact.unlink()
        artifact.parent.rmdir()
        artifact.parent.symlink_to(external, target_is_directory=True)
        with self.assertRaisesRegex(history.HistoryError, "Checkout differs"):
            history.snapshot(self.root, source, 1, paths)

    def test_snapshot_accepts_only_narrow_impeccable_critique_paths(self):
        artifact = self.root / ".impeccable/critique/2026-09-13-review.md"
        artifact.parent.mkdir(parents=True)
        artifact.write_text("retained critique\n")
        archive = self.archive / "01-first.md"
        archive.write_text("one\n")
        source = self.commit()
        paths = sorted([str(artifact.relative_to(self.root)), str(archive.relative_to(self.root))])
        result = history.snapshot(self.root, source, 1, paths)
        self.assertEqual(result["snapshots"][0]["source_paths"], paths)
        with self.assertRaisesRegex(history.HistoryError, "Unsafe"):
            history.snapshot(self.root, source, 1, [".impeccable/other/file.md"])

    def test_shallow_source_unavailable(self):
        self.compact()
        with tempfile.TemporaryDirectory() as temp:
            clone = Path(temp) / "shallow"
            subprocess.run(
                ["git", "clone", "-q", "--depth=1", self.root.as_uri(), str(clone)], check=True
            )
            with self.assertRaisesRegex(history.HistoryError, "Fetch the recorded"):
                history.inspect(clone)

    def test_snapshot_requires_exact_bytes_and_complete_coverage(self):
        one = self.archive / "01-first.md"
        two = self.archive / "02-second.md"
        one.write_text("one")
        two.write_text("two")
        source = self.commit()
        paths = sorted(str(p.relative_to(self.root)) for p in (one, two))
        with self.assertRaisesRegex(history.HistoryError, "Unrepresented"):
            history.snapshot(self.root, source, 2, paths[:1])
        one.write_text("changed")
        with self.assertRaisesRegex(history.HistoryError, "differs"):
            history.snapshot(self.root, source, 2, paths)
        one.unlink()
        one.symlink_to(two)
        with self.assertRaisesRegex(history.HistoryError, "differs"):
            history.snapshot(self.root, source, 2, paths)

    def test_duplicate_numbers_and_active_changes_fail(self):
        self.compact()
        (self.archive / "03-collision.md").write_text("collision")
        with self.assertRaisesRegex(history.HistoryError, "compacted"):
            history.inspect(self.root)
        (self.archive / "03-collision.md").unlink()
        for number in (4, 5):
            (self.archive.parent / f"{number:02d}-active.md").write_text("active")
        with self.assertRaisesRegex(history.HistoryError, "Multiple active"):
            history.inspect(self.root)


if __name__ == "__main__":
    unittest.main()
