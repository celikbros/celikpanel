#!/usr/bin/env python3
"""Classify collected native update/automatic-restore facts without host access.

This checks consistency and completeness, not the authenticity of evidence. The
controller must collect refs from the real released installer, update worker,
rollback body and guest probes. A mock or a manually written ownership receipt
cannot become a released baseline by putting its name in this JSON document.

The input schema is ``celikpanel/release-recovery-evidence/v1``. Required sections
are documented in ``example_record`` in test_evidence.py. ``None`` represents an
unknown observation. Every section needs nonempty evidence refs. Exact protected
sentinels are fixture data digests, never a whole live SQLite database digest.
Expected-regression classification is separate: reproduction never changes a
recovery FAIL into PASS. No commands, services or guest files are touched here.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from pathlib import Path
from typing import Any


SCHEMA = "celikpanel/release-recovery-evidence/v1"
RESULT_SCHEMA = "celikpanel/release-recovery-result/v1"
MAX_BYTES = 4 * 1024 * 1024
HASH = re.compile(r"[0-9a-f]{64}\Z")
COMMIT = re.compile(r"[0-9a-f]{40}\Z")
OPERATION = re.compile(r"[0-9a-f]{32}\Z")
ARTIFACTS = {"agent", "panel", "web"}
SECTIONS = {"baseline", "candidate", "update", "fault", "recovery", "after", "workloads"}
RUNTIME_FILES_V1 = {
    "bin/recovery", "bin/panel-checker", "bin/agent-checker", "bin/schema17-bridge",
    "update.sh", "rollback.sh", "deploy/release-transaction-guard.sh",
    "deploy/release-unit-transition.sh", "deploy/release-recovery-foundation.sh",
    "deploy/panel-tls-snapshot.sh", "deploy/release-recovery-observation.sh",
    "deploy/recovery/runtime-entry.sh",
}


class EvidenceError(ValueError):
    """The evidence document is not unambiguous JSON."""


def _pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise EvidenceError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def decode(raw: bytes) -> dict[str, Any]:
    if len(raw) > MAX_BYTES:
        raise EvidenceError("evidence document exceeds size bound")
    try:
        value = json.loads(raw, object_pairs_hook=_pairs,
                           parse_constant=lambda _: (_ for _ in ()).throw(EvidenceError("nonfinite number")))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise EvidenceError(f"invalid JSON: {exc}") from exc
    if not isinstance(value, dict):
        raise EvidenceError("evidence must be a JSON object")
    return value


def compare_database_semantics(snapshot: Any, current: Any) -> dict[str, Any]:
    """Compare all observed schema/tables; never silently exempt volatile tables.

    Inputs come from the guarded probe. Snapshot manifest/operation provenance
    must still be supplied by its native controller; JSON is not authentication.
    """
    result = {"status": "unknown", "result": "INCONCLUSIVE", "differences": []}
    def checked(observation):
        if not isinstance(observation, dict) or observation.get("status") != "ok" or observation.get("integrity_check") != ["ok"]:
            raise EvidenceError("consistent database observation missing")
        value = observation.get("semantic")
        fields = {"schema", "schema_sha256", "user_version", "application_id", "tables", "excluded_tables", "row_count", "sha256"}
        if not isinstance(value, dict) or set(value) != fields or value.get("schema") != "celikpanel/sqlite-semantic-observation/v1" or value.get("excluded_tables") != []:
            raise EvidenceError("complete semantic schema missing")
        for field in ("schema_sha256", "sha256"):
            if not isinstance(value[field], str) or not HASH.fullmatch(value[field]):
                raise EvidenceError("invalid semantic digest")
        for field in ("user_version", "application_id", "row_count"):
            if type(value[field]) is not int:
                raise EvidenceError("invalid semantic count")
        tables = value.get("tables")
        if not isinstance(tables, list) or len(tables) > 256:
            raise EvidenceError("table inventory unavailable")
        names, count = [], 0
        for table in tables:
            if not isinstance(table, dict) or set(table) != {"name", "rows", "columns", "rowid_included", "sha256"}:
                raise EvidenceError("table evidence incomplete")
            if not isinstance(table["name"], str) or not table["name"] or len(table["name"].encode()) > 256:
                raise EvidenceError("invalid table name")
            if type(table["rows"]) is not int or table["rows"] < 0 or type(table["columns"]) is not int or not 1 <= table["columns"] <= 256 or type(table["rowid_included"]) is not bool:
                raise EvidenceError("invalid table counts")
            if not isinstance(table["sha256"], str) or not HASH.fullmatch(table["sha256"]):
                raise EvidenceError("invalid table digest")
            names.append(table["name"])
            count += table["rows"]
        if names != sorted(set(names)) or count != value["row_count"] or not 0 <= count <= 100000:
            raise EvidenceError("table inventory is ambiguous")
        digest_input = {key: item for key, item in value.items() if key != "sha256"}
        import hashlib
        expected = hashlib.sha256(json.dumps(digest_input, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
        if value["sha256"] != expected:
            raise EvidenceError("semantic envelope digest differs")
        return value
    try:
        old, new = checked(snapshot), checked(current)
        differences = []
        for field in ("schema_sha256", "user_version", "application_id"):
            if old[field] != new[field]:
                differences.append({"scope": "database", "field": field})
        before = {table["name"]: table for table in old["tables"]}
        after = {table["name"]: table for table in new["tables"]}
        for name in sorted(before.keys() | after.keys()):
            if name not in before:
                differences.append({"scope": "table", "name": name, "change": "added"})
            elif name not in after:
                differences.append({"scope": "table", "name": name, "change": "removed"})
            elif before[name] != after[name]:
                differences.append({"scope": "table", "name": name, "change": "changed",
                                    "fields": sorted(key for key in before[name] if before[name][key] != after[name][key])})
        result.update(status="ok", result="DIFFERENT" if differences else "EQUAL", differences=differences,
                      snapshot_semantic_sha256=old["sha256"], current_semantic_sha256=new["sha256"], excluded_tables=[])
    except (EvidenceError, UnicodeError) as exc:
        result["reason"] = str(exc)
    return result


def compare_verified_snapshot_database(snapshot: Any, current: Any, *, snapshot_name: str,
                                       manifest_sha256: str) -> dict[str, Any]:
    """Require the native full-snapshot observation to match the pinned evidence.

    The controller must bind these expected fields to its exact operation before
    collection; this function neither guesses latest nor authenticates JSON.
    """
    proof = snapshot.get("snapshot") if isinstance(snapshot, dict) else None
    expected_name = re.compile(r"[0-9]{8}T[0-9]{6}Z-from-[A-Za-z0-9._-]+-to-[0-9a-f]{40}-[0-9a-f]{32}\Z")
    if (not isinstance(snapshot_name, str) or not expected_name.fullmatch(snapshot_name)
            or not isinstance(manifest_sha256, str) or not HASH.fullmatch(manifest_sha256)
            or not isinstance(proof, dict) or set(proof) != {"name", "manifest_sha256", "database_sha256", "verified_files"}
            or snapshot.get("read_mode") != "verified-immutable-snapshot"
            or proof.get("name") != snapshot_name or proof.get("manifest_sha256") != manifest_sha256
            or not isinstance(proof.get("database_sha256"), str) or not HASH.fullmatch(proof["database_sha256"])
            or type(proof.get("verified_files")) is not int or not 5 <= proof["verified_files"] <= 20000):
        return {"status": "unknown", "result": "INCONCLUSIVE", "differences": [],
                "reason": "exact verified snapshot database observation missing"}
    result = compare_database_semantics(snapshot, current)
    result["snapshot"] = dict(proof)
    return result


class _Assessment:
    def __init__(self) -> None:
        self.failures: list[str] = []
        self.missing: list[str] = []

    def invalid(self, path: str) -> None:
        if path not in self.missing:
            self.missing.append(path)

    def fail(self, path: str) -> None:
        if path not in self.failures:
            self.failures.append(path)

    def obj(self, value: Any, path: str, allowed: set[str]) -> dict[str, Any]:
        if not isinstance(value, dict):
            self.invalid(path)
            return {}
        if set(value) - allowed:
            self.invalid(path + ".unknown_fields")
        return value

    def text(self, value: Any, path: str, pattern: re.Pattern[str] | None = None) -> str | None:
        if not isinstance(value, str) or not value or len(value) > 512 or (pattern and not pattern.fullmatch(value)):
            self.invalid(path)
            return None
        return value

    def expect(self, value: Any, expected: Any, path: str) -> None:
        if value is None or type(value) is not type(expected) or value in ("unknown", "UNKNOWN"):
            self.invalid(path)
        elif value != expected:
            self.fail(path)

    def refs(self, value: Any, path: str) -> None:
        if not isinstance(value, list) or not value:
            self.invalid(path)
            return
        for index, ref in enumerate(value):
            self.text(ref, f"{path}[{index}]")

    def hashes(self, value: Any, path: str, keys: set[str] | None = None) -> dict[str, str]:
        if not isinstance(value, dict) or not value:
            self.invalid(path)
            return {}
        if keys is not None and set(value) != keys:
            self.invalid(path + ".artifact_names")
        result = {}
        for key, digest in value.items():
            name = self.text(key, path + ".name")
            checked = self.text(digest, path + "." + str(key), HASH)
            if name and checked:
                result[name] = checked
        return result

    def compare(self, actual: dict[str, str], expected: dict[str, str], path: str) -> None:
        if not actual or not expected:
            self.invalid(path)
            return
        if set(actual) != set(expected):
            self.invalid(path + ".names")
        for name in actual.keys() & expected.keys():
            if actual[name] != expected[name]:
                self.fail(path + "." + name)

    def same(self, value: Any, expected: str | None, path: str) -> None:
        checked = self.text(value, path)
        if checked is not None and expected is not None and checked != expected:
            self.fail(path)

    def outcome(self, value: Any, path: str) -> None:
        if value == "FAIL":
            self.fail(path)
        elif value != "PASS":
            self.invalid(path)


def _runtime_proof(a: _Assessment, recovery: dict[str, Any]) -> None:
    path = "recovery.runtime_proof"
    value = a.obj(recovery.get("runtime_proof"), path, {
        "schema", "protocol", "snapshot_format", "manifest_sha256", "selected_manifest_sha256",
        "executed_manifest_sha256", "files", "inventory_verified", "refs",
    })
    if value.get("schema") != "celikpanel/recovery-runtime-proof/v1":
        a.invalid(path + ".schema")
    for field, expected in (("protocol", 1), ("snapshot_format", 6)):
        if type(value.get(field)) is not int or value[field] != expected:
            a.invalid(path + "." + field)  # unsupported format is unknown evidence
    a.refs(value.get("refs"), path + ".refs")
    a.expect(value.get("inventory_verified"), True, path + ".inventory_verified")
    manifest = a.text(value.get("manifest_sha256"), path + ".manifest_sha256", HASH)
    for field in ("selected_manifest_sha256", "executed_manifest_sha256"):
        observed = a.text(value.get(field), path + "." + field, HASH)
        if observed and manifest and observed != manifest:
            a.fail(path + "." + field)
    files = a.hashes(value.get("files"), path + ".files", RUNTIME_FILES_V1)
    if set(files) == RUNTIME_FILES_V1 and manifest:
        raw = "format=celikpanel-recovery-runtime-v1\nprotocol=1\nsnapshot=6\n"
        raw += "".join(files[name] + "  " + name + "\n" for name in sorted(files))
        if hashlib.sha256(raw.encode()).hexdigest() != manifest:
            a.fail(path + ".manifest_contents")
    if files.get("rollback.sh") and recovery.get("rollback_script_sha256") != files["rollback.sh"]:
        a.fail(path + ".executed_rollback_script")


def classify(record: Any, *, expected_regression: str | None = None) -> dict[str, Any]:
    """Return PASS/FAIL/INCONCLUSIVE; known failures dominate unknown observations."""
    a = _Assessment()
    root = a.obj(record, "record", SECTIONS | {"schema", "run_id"})
    if root.get("schema") != SCHEMA:
        a.invalid("schema")
    run_id = a.text(root.get("run_id"), "run_id")
    sections: dict[str, dict[str, Any]] = {}
    allowed = {
        "baseline": {"origin", "release", "commit", "manifest_sha256", "artifacts", "protected_sentinels", "refs"},
        "candidate": {"release", "commit", "manifest_sha256", "artifacts", "refs"},
        "update": {"operation_id", "snapshot_id", "entrypoint", "started_operation_ids", "snapshot_complete", "refs"},
        "fault": {"operation_id", "kind", "boundary", "observed", "installed_artifacts", "refs"},
        "recovery": {"operation_id", "snapshot_id", "automatic", "entrypoint", "rollback_script_sha256", "restore_started", "restore_completed", "exit_code", "terminal_outcome", "failure_code", "runtime_proof", "refs"},
        "after": {"artifacts", "running_artifacts", "protected_sentinels", "database", "services", "transaction_markers", "https", "refs"},
        "workloads": {"required", "observations", "refs"},
    }
    for section in sorted(SECTIONS):
        sections[section] = a.obj(root.get(section), section, allowed[section])
        a.refs(sections[section].get("refs"), section + ".refs")
    baseline, candidate = sections["baseline"], sections["candidate"]
    for name, release in (("baseline", baseline), ("candidate", candidate)):
        a.text(release.get("release"), name + ".release")
        a.text(release.get("commit"), name + ".commit", COMMIT)
        a.text(release.get("manifest_sha256"), name + ".manifest_sha256", HASH)
    if baseline.get("origin") != "released-installation":
        a.invalid("baseline.origin")
    old = a.hashes(baseline.get("artifacts"), "baseline.artifacts", ARTIFACTS)
    new = a.hashes(candidate.get("artifacts"), "candidate.artifacts", ARTIFACTS)
    if old and new and old == new:
        a.invalid("candidate.artifacts.no_transition")
    sentinels = a.hashes(baseline.get("protected_sentinels"), "baseline.protected_sentinels")

    update = sections["update"]
    operation = a.text(update.get("operation_id"), "update.operation_id", OPERATION)
    snapshot = a.text(update.get("snapshot_id"), "update.snapshot_id")
    if not isinstance(update.get("entrypoint"), str) or update["entrypoint"] not in {"installed-agent-self-update-worker", "released-updater"}:
        a.invalid("update.entrypoint")
    starts = update.get("started_operation_ids")
    if not isinstance(starts, list) or not starts:
        a.invalid("update.started_operation_ids")
    else:
        for index, item in enumerate(starts):
            a.text(item, f"update.started_operation_ids[{index}]", OPERATION)
        if operation and starts != [operation]:
            a.fail("update.duplicate_or_unrelated_operation")
    a.expect(update.get("snapshot_complete"), True, "update.snapshot_complete")

    fault = sections["fault"]
    a.same(fault.get("operation_id"), operation, "fault.operation_id")
    if not isinstance(fault.get("kind"), str) or fault["kind"] not in {"sigkill", "returned-error", "reboot", "disk-write-failure", "port-conflict"}:
        a.invalid("fault.kind")
    if fault.get("observed") is not True:
        a.invalid("fault.observed")
    if fault.get("boundary") != "candidate-installed":
        a.invalid("fault.boundary")
    injected = a.hashes(fault.get("installed_artifacts"), "fault.installed_artifacts", ARTIFACTS)
    a.compare(injected, new, "fault.candidate_identity")

    recovery = sections["recovery"]
    a.same(recovery.get("operation_id"), operation, "recovery.operation_id")
    a.same(recovery.get("snapshot_id"), snapshot, "recovery.snapshot_id")
    a.expect(recovery.get("automatic"), True, "recovery.automatic")
    if recovery.get("entrypoint") == "independent-runtime":
        _runtime_proof(a, recovery)
    else:
        a.expect(recovery.get("entrypoint"), "retained-release-rollback", "recovery.entrypoint")
        if "runtime_proof" in recovery:
            a.invalid("recovery.runtime_proof.inconsistent_entrypoint")
    a.text(recovery.get("rollback_script_sha256"), "recovery.rollback_script_sha256", HASH)
    a.expect(recovery.get("restore_started"), True, "recovery.restore_started")
    a.expect(recovery.get("restore_completed"), True, "recovery.restore_completed")
    a.expect(recovery.get("exit_code"), 0, "recovery.exit_code")
    a.expect(recovery.get("terminal_outcome"), "rolled-back", "recovery.terminal_outcome")
    failure_code = recovery.get("failure_code")
    if failure_code is not None:
        if a.text(failure_code, "recovery.failure_code"):
            a.fail("recovery.reported_failure")

    after = sections["after"]
    a.compare(a.hashes(after.get("artifacts"), "after.artifacts", ARTIFACTS), old, "after.old_artifacts")
    a.compare(a.hashes(after.get("running_artifacts"), "after.running_artifacts", {"agent", "panel"}),
              {k: v for k, v in old.items() if k != "web"}, "after.running_old_artifacts")
    a.compare(a.hashes(after.get("protected_sentinels"), "after.protected_sentinels"), sentinels, "after.protected_sentinels")
    services = a.obj(after.get("services"), "after.services", {"agent", "panel"})
    for service in ("agent", "panel"):
        a.expect(services.get(service), "active", "after.services." + service)
    a.expect(after.get("transaction_markers"), "clear", "after.transaction_markers")
    a.outcome(after.get("https"), "after.https")
    database = a.obj(after.get("database"), "after.database", {"integrity", "semantic_checks"})
    a.expect(database.get("integrity"), "ok", "after.database.integrity")
    checks = database.get("semantic_checks")
    if not isinstance(checks, list) or not checks:
        a.invalid("after.database.semantic_checks")
    else:
        seen = set()
        for index, value in enumerate(checks):
            path = f"after.database.semantic_checks[{index}]"
            check = a.obj(value, path, {"name", "result", "refs"})
            name = a.text(check.get("name"), path + ".name")
            if name in seen:
                a.invalid(path + ".duplicate")
            seen.add(name)
            a.outcome(check.get("result"), path + ".result")
            a.refs(check.get("refs"), path + ".refs")

    workloads = sections["workloads"]
    required = workloads.get("required")
    if not isinstance(required, list) or not required or any(not isinstance(x, str) or not x for x in required):
        a.invalid("workloads.required")
        required = []
    elif len(set(required)) != len(required):
        a.invalid("workloads.required.duplicate")
    observations = workloads.get("observations")
    observed = set()
    if not isinstance(observations, list):
        a.invalid("workloads.observations")
        observations = []
    for index, value in enumerate(observations):
        path = f"workloads.observations[{index}]"
        check = a.obj(value, path, {"name", "before", "after", "refs"})
        name = a.text(check.get("name"), path + ".name")
        if name in observed:
            a.invalid(path + ".duplicate")
        observed.add(name)
        a.outcome(check.get("before"), path + ".before")
        a.outcome(check.get("after"), path + ".after")
        a.refs(check.get("refs"), path + ".refs")
    for name in required:
        if name not in observed:
            a.invalid("workloads.missing." + name)
    result: dict[str, Any] = {
        "schema": RESULT_SCHEMA, "run_id": run_id,
        "status": "FAIL" if a.failures else "INCONCLUSIVE" if a.missing else "PASS",
        "failures": sorted(a.failures), "missing_evidence": sorted(a.missing),
    }
    if expected_regression is not None:
        critical = ("record", "schema", "run_id", "baseline", "candidate", "update", "fault",
                    "recovery.operation_id", "recovery.snapshot_id", "recovery.automatic",
                    "recovery.entrypoint", "recovery.rollback_script_sha256", "recovery.refs",
                    "recovery.failure_code", "recovery.exit_code", "recovery.terminal_outcome")
        missing_failure_proof = any(item.startswith(critical) for item in a.missing)
        identity_failed = any(item.startswith(("update", "fault", "recovery.operation_id", "recovery.snapshot_id", "recovery.automatic", "recovery.entrypoint")) for item in a.failures)
        failure_observed = (type(recovery.get("exit_code")) is int and recovery["exit_code"] != 0
                            and recovery.get("terminal_outcome") == "recovery-required")
        if missing_failure_proof or identity_failed or result["status"] == "INCONCLUSIVE":
            reproduction = "INCONCLUSIVE"
        elif failure_code == expected_regression and failure_observed:
            reproduction = "REPRODUCED"
        elif failure_code is None and result["status"] != "PASS":
            reproduction = "INCONCLUSIVE"
        else:
            reproduction = "NOT_REPRODUCED"
        result["regression"] = {"expected_failure_code": expected_regression, "status": reproduction}
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("record", type=Path)
    parser.add_argument("--expected-regression")
    args = parser.parse_args()
    try:
        with args.record.open("rb") as stream:
            record = decode(stream.read(MAX_BYTES + 1))
        result = classify(record, expected_regression=args.expected_regression)
    except (OSError, EvidenceError) as exc:
        result = {"schema": RESULT_SCHEMA, "run_id": None, "status": "INCONCLUSIVE",
                  "failures": [], "missing_evidence": [str(exc)]}
    print(json.dumps(result, indent=2, sort_keys=True))
    return {"PASS": 0, "FAIL": 1, "INCONCLUSIVE": 2}[result["status"]]


if __name__ == "__main__":
    sys.exit(main())
