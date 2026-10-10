#!/usr/bin/env python3
"""set3: the Panel database's migration ledger and schema, read-only, inside a marked disposable QEMU guest.

One mode, ``read``: opens ``/var/lib/celikpanel/celikpanel.db`` read-only in one read transaction and reports

  - the ledger (``schema_migrations``: version, filename, sha256) as its highest version, its row count and the
    SHA-256 that ``populated_database.MIGRATIONS`` pins for a released ledger (the same canonical JSON);
  - the schema (``sqlite_schema``: type, name, tbl_name, sql, ordered by type and name) as the SHA-256 that
    ``populated_database.SCHEMAS`` pins, and the same without SQLite's own statistics tables;
  - the table names, whether ``request_identities`` exists with its columns and row count, and SQLite's own
    ``integrity_check`` and ``foreign_key_check``.

It prints no row of any table but the ledger's three public columns. Nothing is written; no service is touched.

set3: Panel veritabanının geçiş defteri ve şeması, işaretli geçici QEMU konuğunda salt-okunur okunur.
"""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import sqlite3
import sys
import time

HERE = Path(__file__).resolve().parent
PANEL_DB = Path("/var/lib/celikpanel/celikpanel.db")
SCHEMA = "celikpanel/set3-schema-ledger/v1"
GUARDED_TABLE = "request_identities"


def _load_probe():
    spec = importlib.util.spec_from_file_location("set3_ledger_probe", HERE / "guest_probe.py")
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


def _sha(raw: bytes) -> str:
    return hashlib.sha256(raw).hexdigest()


def ledger_digest(rows: list) -> str:
    """populated_database._schema: the canonical JSON of the ledger rows."""
    return _sha(json.dumps(rows, sort_keys=True, ensure_ascii=True, separators=(",", ":"), allow_nan=False).encode())


def schema_digest(objects: list) -> str:
    """populated_database._schema: the JSON of (type, name, tbl_name, sql) ordered by type and name."""
    return _sha(json.dumps([list(item) for item in objects], ensure_ascii=True, separators=(",", ":")).encode())


def describe(connection) -> dict:
    connection.execute("BEGIN")
    try:
        rows = [{"version": version, "filename": name, "sha256": digest} for version, name, digest in connection.execute(
            "SELECT version,filename,sha256 FROM schema_migrations ORDER BY version")]
        objects = connection.execute("SELECT type,name,tbl_name,sql FROM sqlite_schema ORDER BY type,name").fetchall()
        tables = sorted(name for kind, name, _, _ in objects if kind == "table")
        guarded = {"exists": GUARDED_TABLE in tables}
        if guarded["exists"]:
            guarded["columns"] = [row[1] for row in connection.execute(f"PRAGMA table_info({GUARDED_TABLE})")]
            guarded["rows"] = connection.execute(f"SELECT COUNT(*) FROM {GUARDED_TABLE}").fetchone()[0]
        integrity = [row[0] for row in connection.execute("PRAGMA integrity_check").fetchall()][:5]
        foreign = connection.execute("PRAGMA foreign_key_check").fetchone()
    finally:
        connection.execute("ROLLBACK")
    without_statistics = [item for item in objects if not str(item[1]).startswith("sqlite_stat")]
    return {"schema_version": max((row["version"] for row in rows), default=None), "ledger_rows": len(rows),
            "ledger_contiguous": [row["version"] for row in rows] == list(range(1, len(rows) + 1)),
            "ledger_sha256": ledger_digest(rows), "ledger_last": rows[-3:],
            "schema_sha256": schema_digest(objects), "schema_sha256_without_statistics": schema_digest(without_statistics),
            "schema_objects": len(objects), "statistics_tables": sorted(str(item[1]) for item in objects
                                                                        if str(item[1]).startswith("sqlite_stat")),
            "tables": tables, "table_count": len(tables), GUARDED_TABLE: guarded,
            "integrity_check": integrity, "foreign_key_check_clean": foreign is None}


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("mode", choices=("read",))
    for name in ("lab-nonce", "vm-uuid", "cell-id", "node"):
        parser.add_argument("--" + name, required=True)
    args = parser.parse_args(argv)
    _load_probe().guard_guest(args)
    connection = sqlite3.connect("file:" + str(PANEL_DB) + "?mode=ro", uri=True, timeout=20, isolation_level=None)
    try:
        connection.execute("PRAGMA query_only=ON")
        value = describe(connection)
    finally:
        connection.close()
    value.update(schema=SCHEMA, mode=args.mode, database=str(PANEL_DB),
                 at=time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()))
    print(json.dumps(value, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:  # noqa: BLE001 - reported as data; the driver decides
        print(json.dumps({"refused": type(exc).__name__, "reason": str(exc)[:300]}), file=sys.stderr)
        raise SystemExit(2)
