#!/usr/bin/env python3
"""Real SQLite file/WAL transactions, no VM/network/service or installed data."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sqlite3
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


probe = module("semantic_probe", "guest_probe.py")
evidence = module("semantic_evidence", "evidence.py")
SNAPSHOT = "20260914T120000Z-from-unknown-to-" + "a" * 40 + "-" + "b" * 32


@unittest.skipUnless(sys.platform == "linux" and getattr(os, "geteuid", lambda: -1)() == 0,
                     "native root no-follow SQLite file semantics")
class DatabaseSemanticsTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.db = self.root / "celikpanel.db"
        self.writer = sqlite3.connect(self.db, isolation_level=None)
        self.writer.execute("PRAGMA journal_mode=WAL")
        self.writer.execute("PRAGMA wal_autocheckpoint=0")
        self.writer.executescript("""
          CREATE TABLE schema_migrations(version INTEGER);
          INSERT INTO schema_migrations VALUES(1),(42);
          CREATE TABLE protected(id INTEGER PRIMARY KEY, value TEXT);
          INSERT INTO protected VALUES(1,'private-secret-do-not-output');
          CREATE TABLE sessions(id INTEGER PRIMARY KEY, value TEXT);
          INSERT INTO sessions VALUES(1,'session-secret-do-not-output');
          CREATE TABLE operations(id INTEGER PRIMARY KEY, value TEXT);
          INSERT INTO operations VALUES(1,'running');
        """)
        self.patch = patch.object(probe, "PANEL_DB", self.db)
        self.patch.start()

    def tearDown(self):
        self.patch.stop()
        self.writer.close()
        self.tmp.cleanup()

    def read(self):
        result = probe.observe_database()
        self.assertEqual(result.get("status"), "ok", result)
        return result

    def make_snapshot(self):
        directory = self.root / "snapshots" / SNAPSHOT
        directory.mkdir(parents=True)
        target = sqlite3.connect(directory / "celikpanel.db")
        self.writer.backup(target)
        target.close()
        for name, content in {"snapshot.version": b"6\n", "bin/agent": b"fixture-agent",
                              "bin/panel": b"fixture-panel", "web/index.html": b"fixture-web"}.items():
            path = directory / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        raw = b"".join((hashlib.sha256(path.read_bytes()).hexdigest() + "  ./" + path.relative_to(directory).as_posix() + "\n").encode()
                       for path in sorted(directory.rglob("*")) if path.is_file())
        (directory / "SHA256SUMS").write_bytes(raw)
        return directory, hashlib.sha256(raw).hexdigest()

    def snapshot_read(self, digest):
        with patch.object(probe, "SNAPSHOT_ROOT", self.root / "snapshots"):
            return probe.observe_snapshot_database(SNAPSHOT, digest)

    def test_committed_wal_is_read_without_database_or_wal_mutation(self):
        wal = Path(str(self.db) + "-wal")
        self.assertGreater(wal.stat().st_size, 0)
        before = {path: (hashlib.sha256(path.read_bytes()).hexdigest(), path.stat().st_mtime_ns, probe.protected_identity(path)) for path in (self.db, wal)}
        result = self.read()
        self.assertTrue(result["wal_included"])
        self.assertEqual(result["read_mode"], "sqlite-read-only-transaction")
        self.assertEqual(result["schema_version"], 42)
        self.assertEqual(result["semantic"]["excluded_tables"], [])
        self.assertEqual({row["name"]: row["rows"] for row in result["semantic"]["tables"]},
                         {"operations": 1, "protected": 1, "schema_migrations": 2, "sessions": 1})
        for path, old in before.items():
            self.assertEqual((hashlib.sha256(path.read_bytes()).hexdigest(), path.stat().st_mtime_ns, probe.protected_identity(path)), old)
        self.assertIn("wal", result["source_metadata_changed"])
        text = json.dumps(result)
        self.assertNotIn("private-secret", text)
        self.assertNotIn("session-secret", text)
        self.assertNotIn("CREATE TABLE", text)

    def test_one_consistent_snapshot_while_real_writer_commits(self):
        baseline = self.read()
        reading, committed = threading.Event(), threading.Event()
        errors = []
        def write():
            connection = sqlite3.connect(self.db, isolation_level=None)
            try:
                if not reading.wait(3):
                    raise RuntimeError("reader never fixed snapshot")
                connection.executescript("BEGIN; UPDATE protected SET value='new-secret'; UPDATE sessions SET value='new-session'; COMMIT;")
            except Exception as exc:
                errors.append(exc)
            finally:
                connection.close()
                committed.set()
        thread = threading.Thread(target=write)
        thread.start()
        original = probe._typed_row
        first = [True]
        def pause_after_snapshot(row, budget):
            if first[0]:
                first[0] = False
                reading.set()
                if not committed.wait(3):
                    raise RuntimeError("writer was blocked")
            return original(row, budget)
        with patch.object(probe, "_typed_row", side_effect=pause_after_snapshot):
            result = self.read()
        thread.join(3)
        self.assertFalse(thread.is_alive())
        self.assertEqual(errors, [])
        self.assertEqual(result["semantic"], baseline["semantic"])
        after = self.read()
        comparison = evidence.compare_database_semantics(result, after)
        self.assertEqual(comparison["result"], "DIFFERENT")
        self.assertEqual({row.get("name") for row in comparison["differences"]}, {"protected", "sessions"})

    def test_uncommitted_writer_changes_are_not_visible(self):
        baseline = self.read()
        self.writer.execute("BEGIN")
        self.writer.execute("UPDATE protected SET value='uncommitted'")
        try:
            self.assertEqual(self.read()["semantic"], baseline["semantic"])
        finally:
            self.writer.execute("ROLLBACK")

    def test_verified_snapshot_matches_all_tables_and_reports_session_operation_differences(self):
        _, digest = self.make_snapshot()
        before = self.snapshot_read(digest)
        self.assertEqual(before["status"], "ok", before)
        self.assertEqual(before["read_mode"], "verified-immutable-snapshot")
        self.assertEqual(before["snapshot"]["verified_files"], 5)
        self.assertEqual(evidence.compare_database_semantics(before, self.read())["result"], "EQUAL")
        self.writer.executescript("UPDATE sessions SET value='renewed'; UPDATE operations SET value='finished';")
        comparison = evidence.compare_database_semantics(before, self.read())
        self.assertEqual(comparison["result"], "DIFFERENT")
        self.assertEqual({row["name"] for row in comparison["differences"]}, {"sessions", "operations"})
        self.assertEqual(comparison["excluded_tables"], [])

    def test_exact_snapshot_comparison_rejects_missing_or_unrelated_manifest(self):
        _, digest = self.make_snapshot()
        snapshot, current = self.snapshot_read(digest), self.read()
        self.assertEqual(evidence.compare_verified_snapshot_database(snapshot, current, snapshot_name=SNAPSHOT, manifest_sha256=digest)["result"], "EQUAL")
        for old, name, expected in ((current, SNAPSHOT, digest), (snapshot, SNAPSHOT, "0" * 64), (snapshot, "../wrong", digest)):
            self.assertEqual(evidence.compare_verified_snapshot_database(old, current, snapshot_name=name, manifest_sha256=expected)["result"], "INCONCLUSIVE")

    def test_type_blob_and_implicit_rowid_changes_are_observed(self):
        self.writer.execute("CREATE TABLE values_table(value)")
        self.writer.execute("INSERT INTO values_table VALUES(?)", (1,))
        previous = self.read()
        for value in ("1", b"1", None, 1.5):
            self.writer.execute("UPDATE values_table SET value=?", (value,))
            current = self.read()
            self.assertEqual(evidence.compare_database_semantics(previous, current)["result"], "DIFFERENT")
            previous = current
        self.writer.execute("UPDATE values_table SET rowid=8")
        self.assertEqual(evidence.compare_database_semantics(previous, self.read())["result"], "DIFFERENT")

    def test_without_rowid_uses_sqlite_metadata_not_sql_literal(self):
        self.writer.executescript("CREATE TABLE nr(id INTEGER PRIMARY KEY, v TEXT) WITHOUT ROWID; INSERT INTO nr VALUES(1,'v'); CREATE TABLE misleading(v TEXT DEFAULT 'WITHOUT ROWID'); INSERT INTO misleading DEFAULT VALUES;")
        tables = {row["name"]: row for row in self.read()["semantic"]["tables"]}
        self.assertFalse(tables["nr"]["rowid_included"])
        self.assertTrue(tables["misleading"]["rowid_included"])

    def test_schema_and_table_add_remove_are_explicit(self):
        before = self.read()
        self.writer.execute("DROP TABLE sessions")
        self.writer.execute("CREATE TABLE extra(value TEXT)")
        value = evidence.compare_database_semantics(before, self.read())
        self.assertIn({"scope": "database", "field": "schema_sha256"}, value["differences"])
        self.assertIn({"scope": "table", "name": "sessions", "change": "removed"}, value["differences"])
        self.assertIn({"scope": "table", "name": "extra", "change": "added"}, value["differences"])

    def test_source_symlink_hardlink_fifo_and_unsafe_owner_refused(self):
        for kind in ("symlink", "hardlink", "fifo", "owner", "writable"):
            path = self.root / kind
            if kind == "symlink": path.symlink_to(self.db)
            elif kind == "hardlink": os.link(self.db, path)
            elif kind == "fifo": os.mkfifo(path)
            else:
                path.write_bytes(self.db.read_bytes())
                if kind == "owner": os.chown(path, 65534, 65534)
                else: path.chmod(0o666)
            try:
                with patch.object(probe, "PANEL_DB", path):
                    self.assertEqual(probe.observe_database()["status"], "unknown", kind)
            finally:
                path.unlink()

    def test_parent_symlink_and_group_writable_directory_refused(self):
        alias = self.root / "alias"
        alias.symlink_to(self.root, target_is_directory=True)
        with patch.object(probe, "PANEL_DB", alias / "celikpanel.db"):
            self.assertEqual(probe.observe_database()["status"], "unknown")
        self.root.chmod(0o770)
        try:
            self.assertEqual(probe.observe_database()["status"], "unknown")
        finally:
            self.root.chmod(0o700)

    def test_missing_and_unsafe_wal_sidecar_never_opens_sqlite(self):
        shm = Path(str(self.db) + "-shm")
        saved = self.root / "saved-shm"
        shm.rename(saved)
        try:
            for kind in ("missing", "symlink", "fifo"):
                if kind == "symlink": shm.symlink_to(saved)
                if kind == "fifo": os.mkfifo(shm)
                with patch.object(probe.sqlite3, "connect") as opened:
                    self.assertEqual(probe.observe_database()["status"], "unknown")
                opened.assert_not_called()
                if kind != "missing": shm.unlink()
        finally:
            saved.rename(shm)

    def test_mismatched_sidecar_permissions_refused_before_sqlite_can_normalize(self):
        wal = Path(str(self.db) + "-wal")
        original = wal.stat().st_mode & 0o777
        wal.chmod(0o600 if original != 0o600 else 0o640)
        before = probe.metadata(wal.stat())
        try:
            with patch.object(probe.sqlite3, "connect") as opened:
                self.assertEqual(probe.observe_database()["status"], "unknown")
            opened.assert_not_called()
            self.assertEqual(probe.metadata(wal.stat()), before)
        finally:
            wal.chmod(original)

    def test_permission_change_during_read_is_unknown(self):
        original = probe._database_semantics
        def changed(connection, deadline):
            value = original(connection, deadline)
            self.db.chmod(0o666)
            return value
        try:
            with patch.object(probe, "_database_semantics", side_effect=changed):
                self.assertEqual(probe.observe_database()["status"], "unknown")
        finally:
            self.db.chmod(0o600)

    def test_busy_exclusive_database_and_bounds_are_unknown(self):
        path = self.root / "locked.db"
        locked = sqlite3.connect(path, isolation_level=None)
        locked.execute("CREATE TABLE v(n)")
        locked.execute("BEGIN EXCLUSIVE")
        try:
            with patch.object(probe, "PANEL_DB", path):
                self.assertEqual(probe.observe_database()["status"], "unknown")
        finally:
            locked.execute("ROLLBACK")
            locked.close()
        for field, value in (("DB_MAX_ROWS", 1), ("DB_MAX_VALUE_BYTES", 4), ("DB_TIMEOUT", 0)):
            with patch.object(probe, field, value):
                result = probe.observe_database()
                self.assertEqual(result["status"], "unknown", field)
                self.assertNotIn("secret", json.dumps(result))

    def test_snapshot_unexpected_file_checksum_sidecar_and_identity_refused(self):
        directory, digest = self.make_snapshot()
        self.assertEqual(self.snapshot_read("0" * 64)["status"], "unknown")
        for relative in ("extra", "celikpanel.db-wal"):
            extra = directory / relative
            extra.write_bytes(b"unexpected")
            self.assertEqual(self.snapshot_read(digest)["status"], "unknown")
            extra.unlink()
        (directory / "bin/agent").write_bytes(b"changed")
        self.assertEqual(self.snapshot_read(digest)["status"], "unknown")
        with patch.object(probe, "SNAPSHOT_ROOT", directory.parent):
            self.assertEqual(probe.observe_snapshot_database("../" + SNAPSHOT, digest)["status"], "unknown")

    def test_snapshot_parent_symlink_and_special_entries_refused(self):
        directory, digest = self.make_snapshot()
        link = directory / "link"
        link.symlink_to(directory / "celikpanel.db")
        self.assertEqual(self.snapshot_read(digest)["status"], "unknown")
        link.unlink()
        fifo = directory / "fifo"
        os.mkfifo(fifo)
        self.assertEqual(self.snapshot_read(digest)["status"], "unknown")
        fifo.unlink()
        alias = self.root / "alias"
        alias.symlink_to(directory.parent)
        with patch.object(probe, "SNAPSHOT_ROOT", alias):
            self.assertEqual(probe.observe_snapshot_database(SNAPSHOT, digest)["status"], "unknown")

    def test_fifo_and_database_swap_refuse_without_blocking(self):
        fifo = self.root / "fifo"
        os.mkfifo(fifo)
        with self.assertRaises(probe.ProbeError):
            probe.bounded_file(fifo, 100)
        real_open = os.open
        def swapped(path, flags, *args, **kwargs):
            if Path(path) == self.db:
                self.assertTrue(flags & os.O_NONBLOCK)
                return real_open(fifo, flags, *args, **kwargs)
            return real_open(path, flags, *args, **kwargs)
        with patch.object(probe.os, "open", side_effect=swapped):
            self.assertEqual(probe.observe_database()["status"], "unknown")

    def test_snapshot_walk_deadline_precedes_payload_reads(self):
        directory, digest = self.make_snapshot()
        with patch.object(probe.time, "monotonic", side_effect=[0, 31]):
            result = self.snapshot_read(digest)
        self.assertEqual(result["status"], "unknown")
        self.assertIn("inventory exceeded", result["reason"])

    def test_comparison_incomplete_tampered_or_excluded_data_stays_unknown(self):
        good = self.read()
        for bad in ({"status": "unknown"}, {}, dict(good, integrity_check=["bad"])):
            self.assertEqual(evidence.compare_database_semantics(good, bad)["result"], "INCONCLUSIVE")
        for field, value in (("excluded_tables", ["sessions"]), ("sha256", "0" * 64), ("row_count", 99)):
            bad = json.loads(json.dumps(good))
            bad["semantic"][field] = value
            self.assertEqual(evidence.compare_database_semantics(good, bad)["result"], "INCONCLUSIVE")


if __name__ == "__main__":
    unittest.main()
