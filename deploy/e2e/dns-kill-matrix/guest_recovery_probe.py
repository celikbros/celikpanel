#!/usr/bin/env python3
"""Read-only, repeatable post-restart convergence probe for one S-1 DNS cell."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
from typing import Any, Callable


OUTPUT_SCHEMA = "celikpanel/dns-kill-recovery-probe/v1"
SCENARIO_SCHEMA = "celikpanel-dns-kill-matrix-trigger/v1"
IDENTITY_SCHEMA = "celikpanel-dns-kill-matrix-trigger-identity/v1"
STATE_SCHEMA = "celikpanel-dns-engine-state/v1"
CELL_RE = re.compile(r"[a-z0-9][a-z0-9_.:-]{0,191}")
IDENTITY_RE = re.compile(r"[0-9a-f]{32}")
QUALIFIER_RE = re.compile(r"dns-engine-switch/v1:sha256:[0-9a-f]{64}")

SCENARIO_KEYS = {
    "schema", "driver", "source_fixture", "mode", "source_engine",
    "target_engine", "source_epoch", "target_epoch", "source_revision",
    "topology", "pair_role", "local_ip", "local_ns", "peer_ip", "peer_ns",
    "zones",
}
SCENARIO_REQUIRED = {
    "schema", "driver", "source_fixture", "mode", "target_engine",
    "source_epoch", "target_epoch", "source_revision", "topology", "zones",
}
ZONE_KEYS = {
    "ordinal", "domain", "desired_generation", "delete", "zone_type",
    "records", "zone_qualifier",
}
RECORD_KEYS = {"name", "type", "content", "ttl", "prio", "disabled"}
IDENTITY_KEYS = {
    "schema", "cell_id", "driver", "source_fixture", "request_id", "owner_id",
    "manifest_qualifier",
}
STATE_KEYS = {
    "schema", "mode", "engine", "engine_epoch", "generation", "pair_role",
    "pair_local_ip", "pair_peer_ip", "primary_catalog_serial", "source_revision",
    "manifest_qualifier", "mutation_request_id", "mutation_owner_id",
}
LEDGER_KEYS = {"version", "active_request_id", "jobs"}
JOB_KEYS = {
    "request_id", "owner_id", "kind", "target", "package_name", "status",
    "phase", "attempt", "started_at", "updated_at", "lease_expires_at",
    "deadline_at", "finished_at", "error_code", "error_message", "worker_pid",
    "worker_started", "worker_command",
}
INSTALL_OWNERSHIP_KEYS = {
    "schema", "engine", "package_manager", "packages", "missing_before",
    "manifest_qualifier", "mutation_request_id", "mutation_owner_id",
}


class ProbeObservationError(RuntimeError):
    pass


def exact_keys(value: Any, allowed: set[str], required: set[str], label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ProbeObservationError(f"{label} is not an object")
    keys = set(value)
    unknown = sorted(keys - allowed)
    missing = sorted(required - keys)
    if unknown or missing:
        raise ProbeObservationError(
            f"{label} keys differ (unknown={unknown}, missing={missing})"
        )
    return value


def read_secure_json(
    path: Path, label: str, limit: int
) -> tuple[dict[str, Any], str, bytes]:
    if not path.is_absolute() or path != Path(os.path.normpath(path)):
        raise ProbeObservationError(f"{label} path is not clean and absolute")
    try:
        before = path.lstat()
    except OSError as exc:
        raise ProbeObservationError(f"{label} is unavailable: {exc}") from exc
    linux_metadata_invalid = os.name == "posix" and (
        stat.S_IMODE(before.st_mode) != 0o600 or before.st_uid != 0
    )
    if (
        stat.S_ISLNK(before.st_mode)
        or not stat.S_ISREG(before.st_mode)
        or linux_metadata_invalid
        or before.st_nlink != 1
        or before.st_size <= 0
        or before.st_size > limit
    ):
        raise ProbeObservationError(f"{label} metadata is not root-owned mode-0600 single-link regular")
    flags = os.O_RDONLY
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    descriptor = os.open(path, flags)
    try:
        opened = os.fstat(descriptor)
        if (opened.st_dev, opened.st_ino) != (before.st_dev, before.st_ino):
            raise ProbeObservationError(f"{label} changed while opening")
        raw = bytearray()
        while len(raw) <= limit:
            chunk = os.read(descriptor, min(65536, limit + 1 - len(raw)))
            if not chunk:
                break
            raw.extend(chunk)
        after = os.fstat(descriptor)
        if (after.st_dev, after.st_ino, after.st_size) != (
            before.st_dev, before.st_ino, before.st_size
        ):
            raise ProbeObservationError(f"{label} changed while reading")
    finally:
        os.close(descriptor)
    if len(raw) > limit:
        raise ProbeObservationError(f"{label} exceeds its size bound")
    try:
        text = bytes(raw).decode("utf-8")
        def reject_duplicates(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
            result: dict[str, Any] = {}
            for key, item in pairs:
                if key in result:
                    raise ProbeObservationError(f"{label} contains duplicate key {key!r}")
                result[key] = item
            return result
        value = json.loads(text, object_pairs_hook=reject_duplicates)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ProbeObservationError(f"{label} is not one strict JSON value: {exc}") from exc
    if not isinstance(value, dict):
        raise ProbeObservationError(f"{label} is not a JSON object")
    return value, hashlib.sha256(raw).hexdigest(), bytes(raw)


def validate_scenario(value: Any) -> dict[str, Any]:
    scenario = exact_keys(value, SCENARIO_KEYS, SCENARIO_REQUIRED, "scenario")
    if scenario.get("schema") != SCENARIO_SCHEMA:
        raise ProbeObservationError("scenario schema is invalid")
    if scenario.get("mode") not in {"switch", "adopt"}:
        raise ProbeObservationError("scenario mode is invalid")
    if scenario.get("target_engine") not in {"bind", "pdns"}:
        raise ProbeObservationError("scenario target engine is invalid")
    if scenario.get("source_fixture") not in {
        "uninitialized", "managed-pdns", "managed-bind",
        "external-pdns-adoption", "legacy-pdns-secondary",
    }:
        raise ProbeObservationError("scenario source fixture is invalid")
    for field in ("source_epoch", "target_epoch", "source_revision"):
        if isinstance(scenario.get(field), bool) or not isinstance(scenario.get(field), int):
            raise ProbeObservationError(f"scenario {field} is not an integer")
    zones = scenario.get("zones")
    if not isinstance(zones, list):
        raise ProbeObservationError("scenario zones is not an array")
    for index, raw_zone in enumerate(zones):
        zone = exact_keys(raw_zone, ZONE_KEYS, ZONE_KEYS, f"scenario zone {index}")
        if not isinstance(zone.get("records"), list):
            raise ProbeObservationError(f"scenario zone {index} records is not an array")
        for record_index, record in enumerate(zone["records"]):
            exact_keys(
                record, RECORD_KEYS, RECORD_KEYS,
                f"scenario zone {index} record {record_index}",
            )
    return scenario


def validate_identity(
    value: Any, raw: bytes, scenario: dict[str, Any], cell_id: str
) -> dict[str, Any]:
    receipt = exact_keys(value, IDENTITY_KEYS, IDENTITY_KEYS, "trigger identity receipt")
    if receipt.get("schema") != IDENTITY_SCHEMA or receipt.get("cell_id") != cell_id:
        raise ProbeObservationError("trigger identity receipt schema/cell differs")
    if receipt.get("driver") != scenario.get("driver") or receipt.get(
        "source_fixture"
    ) != scenario.get("source_fixture"):
        raise ProbeObservationError("trigger identity receipt differs from scenario route")
    if IDENTITY_RE.fullmatch(str(receipt.get("request_id", ""))) is None or \
       IDENTITY_RE.fullmatch(str(receipt.get("owner_id", ""))) is None or \
       QUALIFIER_RE.fullmatch(str(receipt.get("manifest_qualifier", ""))) is None:
        raise ProbeObservationError("trigger identity receipt has invalid mutation identity")
    expected_owner = hashlib.sha256(
        (
            "celikpanel/dns-kill-matrix-owner/v1\x00"
            + receipt["request_id"] + "\x00" + cell_id
        ).encode()
    ).hexdigest()[:32]
    if receipt["owner_id"] != expected_owner:
        raise ProbeObservationError("trigger identity receipt owner is not deterministic")
    ordered = {
        key: receipt[key]
        for key in (
            "schema", "cell_id", "driver", "source_fixture", "request_id", "owner_id",
            "manifest_qualifier",
        )
    }
    canonical = (json.dumps(ordered, separators=(",", ":")) + "\n").encode()
    if raw != canonical:
        raise ProbeObservationError("trigger identity receipt is not canonical compact JSON")
    return receipt


def validate_state(
    value: Any, scenario: dict[str, Any], receipt: dict[str, Any]
) -> dict[str, Any]:
    state = exact_keys(value, STATE_KEYS, {
        "schema", "mode", "engine", "engine_epoch", "source_revision",
        "manifest_qualifier", "mutation_request_id", "mutation_owner_id",
    }, "DNS engine state receipt")
    expected = {
        "schema": STATE_SCHEMA,
        "mode": scenario["mode"],
        "engine": scenario["target_engine"],
        "engine_epoch": scenario["target_epoch"],
        "source_revision": scenario["source_revision"],
        "manifest_qualifier": receipt["manifest_qualifier"],
        "mutation_request_id": receipt["request_id"],
        "mutation_owner_id": receipt["owner_id"],
    }
    differences = {
        key: {"want": wanted, "got": state.get(key)}
        for key, wanted in expected.items()
        if state.get(key) != wanted
    }
    if differences:
        raise ProbeObservationError(f"DNS engine state differs from target: {differences}")
    generation = str(state.get("generation", ""))
    if scenario["target_engine"] == "bind":
        if re.fullmatch(r"[0-9a-f]{64}", generation) is None:
            raise ProbeObservationError("BIND target state lacks a canonical generation")
    elif generation:
        raise ProbeObservationError("PowerDNS target state unexpectedly carries a BIND generation")
    topology = scenario.get("topology")
    pair_role = str(state.get("pair_role", ""))
    pair_local = str(state.get("pair_local_ip", ""))
    pair_peer = str(state.get("pair_peer_ip", ""))
    catalog_serial = state.get("primary_catalog_serial", 0)
    if isinstance(catalog_serial, bool) or not isinstance(catalog_serial, int):
        raise ProbeObservationError("DNS engine state catalog serial is not an integer")
    if topology == "standalone":
        if pair_role or pair_local or pair_peer or catalog_serial != 0:
            raise ProbeObservationError("standalone target state retains paired identity")
    elif topology == "paired":
        if (
            pair_role != scenario.get("pair_role", "")
            or pair_local != scenario.get("local_ip", "")
            or pair_peer != scenario.get("peer_ip", "")
        ):
            raise ProbeObservationError("paired target state differs from scenario identity")
        if pair_role == "primary" and catalog_serial <= 0:
            raise ProbeObservationError("paired-primary target state lacks a catalog serial")
        if pair_role == "secondary" and catalog_serial != 0:
            raise ProbeObservationError("paired-secondary target state carries a catalog serial")
        if pair_role not in {"primary", "secondary"}:
            raise ProbeObservationError("paired target state has an invalid role")
    else:
        raise ProbeObservationError("scenario topology is invalid")
    return state


def canonical_state_bytes(state: dict[str, Any]) -> bytes:
    required = ("schema", "mode", "engine", "engine_epoch")
    optional = (
        "generation", "pair_role", "pair_local_ip", "pair_peer_ip",
        "primary_catalog_serial",
    )
    tail = (
        "source_revision", "manifest_qualifier", "mutation_request_id",
        "mutation_owner_id",
    )
    ordered: dict[str, Any] = {key: state[key] for key in required}
    for key in optional:
        value = state.get(key)
        if value not in (None, "", 0):
            ordered[key] = value
    for key in tail:
        ordered[key] = state[key]
    return (json.dumps(ordered, separators=(",", ":")) + "\n").encode()


def optional_secure_json(
    path: Path, label: str, limit: int
) -> tuple[dict[str, Any] | None, str, bytes]:
    try:
        path.lstat()
    except FileNotFoundError:
        return None, "", b""
    except OSError as exc:
        raise ProbeObservationError(f"inspect {label}: {exc}") from exc
    return read_secure_json(path, label, limit)


def validate_ownership_state(value: Any, raw: bytes, engine: str, label: str) -> dict[str, Any]:
    receipt = exact_keys(
        value, STATE_KEYS,
        {"schema", "mode", "engine", "engine_epoch", "source_revision",
         "manifest_qualifier", "mutation_request_id", "mutation_owner_id"},
        label,
    )
    if receipt.get("schema") != STATE_SCHEMA or receipt.get("engine") != engine:
        raise ProbeObservationError(f"{label} schema/engine differs from its path")
    if raw != canonical_state_bytes(receipt):
        raise ProbeObservationError(f"{label} is not canonical JSON")
    return receipt


def validate_prior_source_receipt(
    receipt: dict[str, Any], scenario: dict[str, Any], label: str
) -> None:
    """Bind a retained source ownership receipt to the measured source tuple."""
    expected = {
        "engine_epoch": scenario["source_epoch"],
        "source_revision": scenario["source_revision"],
    }
    differences = {
        key: {"want": wanted, "got": receipt.get(key)}
        for key, wanted in expected.items()
        if receipt.get(key) != wanted
    }
    if differences:
        raise ProbeObservationError(
            f"{label} differs from measured source: {differences}"
        )
    if receipt.get("mode") not in {"switch", "adopt"}:
        raise ProbeObservationError(f"{label} mode is invalid")
    if QUALIFIER_RE.fullmatch(str(receipt.get("manifest_qualifier", ""))) is None or \
       IDENTITY_RE.fullmatch(str(receipt.get("mutation_request_id", ""))) is None or \
       IDENTITY_RE.fullmatch(str(receipt.get("mutation_owner_id", ""))) is None:
        raise ProbeObservationError(f"{label} mutation identity is invalid")
    generation = str(receipt.get("generation", ""))
    if receipt["engine"] == "bind":
        if re.fullmatch(r"[0-9a-f]{64}", generation) is None:
            raise ProbeObservationError(
                f"{label} lacks a canonical BIND generation"
            )
    elif generation:
        raise ProbeObservationError(
            f"{label} unexpectedly carries a BIND generation"
        )
    role = str(receipt.get("pair_role", ""))
    local_ip = str(receipt.get("pair_local_ip", ""))
    peer_ip = str(receipt.get("pair_peer_ip", ""))
    serial = receipt.get("primary_catalog_serial", 0)
    if isinstance(serial, bool) or not isinstance(serial, int):
        raise ProbeObservationError(f"{label} catalog serial is not an integer")
    if scenario.get("topology") == "standalone":
        if role or local_ip or peer_ip or serial != 0:
            raise ProbeObservationError(
                f"{label} retains paired identity for a standalone source"
            )
    elif scenario.get("topology") == "paired":
        if (
            role != scenario.get("pair_role", "")
            or local_ip != scenario.get("local_ip", "")
            or peer_ip != scenario.get("peer_ip", "")
        ):
            raise ProbeObservationError(
                f"{label} differs from measured source topology"
            )
        if role == "primary" and serial <= 0:
            raise ProbeObservationError(
                f"{label} paired-primary source lacks a catalog serial"
            )
        if role == "secondary" and serial != 0:
            raise ProbeObservationError(
                f"{label} paired-secondary source carries a catalog serial"
            )
        if role not in {"primary", "secondary"}:
            raise ProbeObservationError(f"{label} has an invalid pair role")
    else:
        raise ProbeObservationError("scenario topology is invalid")


def validate_install_ownership(value: Any, raw: bytes, engine: str, label: str) -> dict[str, Any]:
    receipt = exact_keys(
        value, INSTALL_OWNERSHIP_KEYS, INSTALL_OWNERSHIP_KEYS, label
    )
    if (
        receipt.get("schema") != "celikpanel-dns-engine-install-ownership/v1"
        or receipt.get("engine") != engine
        or receipt.get("package_manager") not in {"apt", "pacman", "dnf"}
        or QUALIFIER_RE.fullmatch(str(receipt.get("manifest_qualifier", ""))) is None
        or IDENTITY_RE.fullmatch(str(receipt.get("mutation_request_id", ""))) is None
        or IDENTITY_RE.fullmatch(str(receipt.get("mutation_owner_id", ""))) is None
    ):
        raise ProbeObservationError(f"{label} identity is invalid")
    packages, missing = receipt.get("packages"), receipt.get("missing_before")
    if (
        not isinstance(packages, list) or not isinstance(missing, list)
        or not packages or not missing or len(packages) > 32 or len(missing) > len(packages)
        or any(not isinstance(item, str) or not item for item in packages + missing)
        or packages != sorted(set(packages)) or missing != sorted(set(missing))
        or not set(missing).issubset(packages)
    ):
        raise ProbeObservationError(f"{label} package set is invalid")
    ordered = {key: receipt[key] for key in (
        "schema", "engine", "package_manager", "packages", "missing_before",
        "manifest_qualifier", "mutation_request_id", "mutation_owner_id",
    )}
    canonical = (json.dumps(ordered, separators=(",", ":")) + "\n").encode()
    if raw != canonical:
        raise ProbeObservationError(f"{label} is not canonical JSON")
    return receipt


def ownership_residue(
    state_dir: Path,
    scenario: dict[str, Any],
    active_state: dict[str, Any],
) -> tuple[dict[str, Any], list[str]]:
    target = scenario["target_engine"]
    source = scenario.get("source_engine", "")
    semantic: dict[str, Any] = {}
    errors: list[str] = []
    for engine in ("bind", "pdns"):
        ownership_path = state_dir / f"dns-engine-ownership-{engine}.json"
        label = f"{engine} engine ownership receipt"
        try:
            value, digest, raw = optional_secure_json(ownership_path, label, 1 << 20)
            current: dict[str, Any] = {"exists": value is not None}
            if value is not None:
                receipt = validate_ownership_state(value, raw, engine, label)
                current.update({
                    "sha256": digest,
                    "identity": {
                        key: receipt.get(key, "")
                        for key in (
                            "mode", "engine", "engine_epoch", "generation", "pair_role",
                            "pair_local_ip", "pair_peer_ip", "primary_catalog_serial",
                            "source_revision", "manifest_qualifier", "mutation_request_id",
                            "mutation_owner_id",
                        )
                    },
                })
                if engine == target and receipt != active_state:
                    errors.append(f"{label} differs from active target state")
                if engine == source and source != target:
                    try:
                        validate_prior_source_receipt(receipt, scenario, label)
                    except ProbeObservationError as exc:
                        errors.append(str(exc))
            if engine == target and value is None:
                errors.append(f"{label} is absent for the active target")
            elif engine == source and source and value is None:
                errors.append(f"{label} is absent for the prior managed source")
            elif engine not in {target, source} and value is not None:
                errors.append(f"{label} is unclaimed by source or target")
            semantic[f"engine_ownership_{engine}"] = current
        except ProbeObservationError as exc:
            errors.append(str(exc))
            semantic[f"engine_ownership_{engine}"] = {"error": str(exc)}

        install_path = state_dir / f"dns-engine-install-ownership-{engine}.json"
        install_label = f"{engine} engine install ownership receipt"
        try:
            value, digest, raw = optional_secure_json(install_path, install_label, 64 << 10)
            current = {"exists": value is not None}
            if value is not None:
                receipt = validate_install_ownership(value, raw, engine, install_label)
                current.update({"sha256": digest, "identity": receipt})
                errors.append(f"{install_label} remains after successful finalization")
            semantic[f"install_ownership_{engine}"] = current
        except ProbeObservationError as exc:
            errors.append(str(exc))
            semantic[f"install_ownership_{engine}"] = {"error": str(exc)}
    return semantic, errors


def zero_time(value: Any) -> bool:
    return value in {None, "", "0001-01-01T00:00:00Z"}


def validate_ledger(value: Any, scenario: dict[str, Any], receipt: dict[str, Any]) -> dict[str, Any]:
    ledger = exact_keys(value, LEDGER_KEYS, {"version", "jobs"}, "mutation ledger")
    if ledger.get("version") != 1 or ledger.get("active_request_id", "") != "":
        raise ProbeObservationError("mutation ledger is not version-1 idle")
    jobs = ledger.get("jobs")
    if not isinstance(jobs, dict):
        raise ProbeObservationError("mutation ledger jobs is not an object")
    job = exact_keys(
        jobs.get(receipt["request_id"]), JOB_KEYS,
        {"request_id", "owner_id", "kind", "target", "status", "phase", "attempt",
         "started_at", "updated_at", "deadline_at"},
        "measured mutation job",
    )
    expected_phase = (
        "commit/dns-engine-switch/v2/finalized/"
        + receipt["request_id"] + "/" + receipt["manifest_qualifier"]
    )
    expected = {
        "request_id": receipt["request_id"],
        "owner_id": receipt["owner_id"],
        "kind": "dns_engine_switch",
        "target": scenario["target_engine"],
        "package_name": receipt["manifest_qualifier"],
        "status": "succeeded",
        "phase": expected_phase,
    }
    differences = {
        key: {"want": wanted, "got": job.get(key, "")}
        for key, wanted in expected.items()
        if job.get(key, "") != wanted
    }
    if differences:
        raise ProbeObservationError(f"measured mutation job is not finalized: {differences}")
    parsed_times: dict[str, dt.datetime] = {}
    for field in ("started_at", "updated_at", "deadline_at", "finished_at"):
        raw_time = job.get(field)
        if not isinstance(raw_time, str) or not raw_time:
            raise ProbeObservationError(
                f"finalized mutation job {field} is absent"
            )
        try:
            parsed = dt.datetime.fromisoformat(raw_time.replace("Z", "+00:00"))
        except ValueError as exc:
            raise ProbeObservationError(
                f"finalized mutation job {field} is not RFC3339"
            ) from exc
        if parsed.year <= 1 or parsed.tzinfo is None:
            raise ProbeObservationError(
                f"finalized mutation job {field} is zero or timezone-naive"
            )
        parsed_times[field] = parsed
    if (
        isinstance(job.get("attempt"), bool)
        or not isinstance(job.get("attempt"), int)
        or job["attempt"] <= 0
        or not job.get("finished_at")
        or not zero_time(job.get("lease_expires_at"))
        or job.get("worker_pid", 0) != 0
        or str(job.get("worker_started", "")).strip()
        or str(job.get("worker_command", "")).strip()
        or str(job.get("error_code", "")).strip()
        or str(job.get("error_message", "")).strip()
        or parsed_times["updated_at"] < parsed_times["started_at"]
        or parsed_times["deadline_at"] < parsed_times["started_at"]
        or parsed_times["finished_at"] < parsed_times["started_at"]
        or parsed_times["updated_at"] != parsed_times["finished_at"]
    ):
        raise ProbeObservationError(
            "finalized mutation job retains invalid worker/lease/error/time state"
        )
    return job


def journal_observation(path: Path) -> tuple[bool, dict[str, Any] | None]:
    try:
        info = path.lstat()
    except FileNotFoundError:
        return False, None
    except OSError as exc:
        raise ProbeObservationError(f"inspect switch journal: {exc}") from exc
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise ProbeObservationError("switch journal path exists as a non-regular object")
    journal, _, _ = read_secure_json(path, "switch journal", 1 << 20)
    semantic = {
        key: journal.get(key)
        for key in (
            "schema", "phase", "mode", "mutation_request_id", "mutation_owner_id",
            "manifest_qualifier", "source_engine", "target_engine", "source_epoch",
            "target_epoch", "source_revision", "topology", "pair_role",
        )
    }
    return True, semantic


def systemd_states(runner: Callable[[str], str]) -> dict[str, str]:
    states = {unit: runner(unit) for unit in ("bind9.service", "named.service", "pdns.service")}
    return states


def validate_target_systemd(states: dict[str, str], target_engine: str) -> None:
    if target_engine == "bind":
        os_id = ""
        try:
            for line in Path("/etc/os-release").read_text(encoding="utf-8").splitlines():
                if line.startswith("ID="):
                    os_id = line[3:].strip().strip('"')
        except OSError:
            pass
        target_unit = "named.service" if os_id == "arch" else "bind9.service"
        if states[target_unit] != "active" or states["pdns.service"] == "active":
            raise ProbeObservationError(
                f"BIND target/source unit state is not converged: {states}"
            )
    elif states["pdns.service"] != "active" or any(
        states[unit] == "active" for unit in ("bind9.service", "named.service")
    ):
        raise ProbeObservationError(
            f"PowerDNS target/source unit state is not converged: {states}"
        )


def active_dns_engine(states: dict[str, str]) -> str:
    bind_active = any(
        states.get(unit) == "active" for unit in ("bind9.service", "named.service")
    )
    pdns_active = states.get("pdns.service") == "active"
    if bind_active == pdns_active:
        return ""
    return "bind" if bind_active else "pdns"


def inspect_unit(unit: str) -> str:
    result = subprocess.run(
        ["/usr/bin/systemctl", "is-active", unit],
        stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, check=False,
        timeout=5,
    )
    value = result.stdout.strip()
    return value if value else f"exit-{result.returncode}"


def probe(args: argparse.Namespace, unit_runner: Callable[[str], str] = inspect_unit) -> dict[str, Any]:
    errors: list[str] = []
    semantic: dict[str, Any] = {"cell_id": args.cell_id}
    scenario: dict[str, Any] | None = None
    receipt: dict[str, Any] | None = None
    active_state: dict[str, Any] | None = None
    observed_state: dict[str, Any] | None = None
    observed_state_bytes = b""
    unit_states: dict[str, str] | None = None
    try:
        raw, digest, _ = read_secure_json(args.scenario, "scenario", 65 << 20)
        scenario = validate_scenario(raw)
        semantic["scenario_sha256"] = digest
        semantic["scenario"] = {
            key: scenario.get(key)
            for key in (
                "driver", "source_fixture", "mode", "source_engine", "target_engine",
                "source_epoch", "target_epoch", "source_revision", "topology", "pair_role",
            )
        }
    except ProbeObservationError as exc:
        errors.append(str(exc))
    try:
        raw, digest, encoded = read_secure_json(
            args.identity_receipt, "trigger identity receipt", 4096
        )
        if scenario is None:
            raise ProbeObservationError("cannot bind trigger identity without a valid scenario")
        receipt = validate_identity(raw, encoded, scenario, args.cell_id)
        semantic["identity_sha256"] = digest
        semantic["identity"] = receipt
    except ProbeObservationError as exc:
        errors.append(str(exc))
    try:
        raw, digest, encoded = read_secure_json(
            args.state, "DNS engine state receipt", 1 << 20
        )
        observed_state = raw
        observed_state_bytes = encoded
        if scenario is None or receipt is None:
            raise ProbeObservationError("cannot bind engine state without scenario/identity")
        state = validate_state(raw, scenario, receipt)
        if encoded != canonical_state_bytes(state):
            raise ProbeObservationError("DNS engine state receipt is not canonical JSON")
        active_state = state
        semantic["state_sha256"] = digest
        semantic["state"] = {
            key: state.get(key)
            for key in (
                "mode", "engine", "engine_epoch", "generation", "pair_role",
                "source_revision", "manifest_qualifier", "mutation_request_id",
                "mutation_owner_id",
            )
        }
    except ProbeObservationError as exc:
        errors.append(str(exc))
    if scenario is not None and active_state is not None:
        residue, residue_errors = ownership_residue(
            args.state.parent, scenario, active_state
        )
        semantic["ownership_residue"] = residue
        errors.extend(residue_errors)
    else:
        errors.append("cannot validate DNS ownership residue without scenario/active state")
    try:
        raw, _, _ = read_secure_json(args.ledger, "mutation ledger", 1 << 20)
        if scenario is None or receipt is None:
            raise ProbeObservationError("cannot bind mutation ledger without scenario/identity")
        job = validate_ledger(raw, scenario, receipt)
        semantic["job"] = {
            key: job.get(key, "")
            for key in (
                "request_id", "owner_id", "kind", "target", "package_name", "status",
                "phase", "attempt", "error_code", "error_message",
            )
        }
        semantic["job"]["worker_present"] = bool(
            job.get("worker_pid", 0) or str(job.get("worker_started", "")).strip()
            or str(job.get("worker_command", "")).strip()
        )
        semantic["job"]["lease_present"] = not zero_time(job.get("lease_expires_at"))
    except ProbeObservationError as exc:
        errors.append(str(exc))
    try:
        exists, journal = journal_observation(args.journal)
        semantic["journal"] = {"exists": exists, "semantic": journal}
        if exists:
            errors.append("DNS engine switch journal remains after recovery")
    except ProbeObservationError as exc:
        errors.append(str(exc))
    try:
        if scenario is None:
            raise ProbeObservationError("cannot validate DNS units without a valid scenario")
        unit_states = systemd_states(unit_runner)
        semantic["units"] = unit_states
        validate_target_systemd(unit_states, scenario["target_engine"])
    except (ProbeObservationError, subprocess.SubprocessError, OSError) as exc:
        errors.append(str(exc))
    active_engine = active_dns_engine(unit_states) if unit_states is not None else ""
    if (
        active_engine
        and observed_state is not None
        and observed_state.get("engine") != active_engine
    ):
        active_engine = ""
    outcome = "indeterminate"
    if not errors:
        outcome = "target_converged"
    elif (
        scenario is not None
        and observed_state is not None
        and active_engine
        and active_engine == scenario.get("source_engine", "")
        and active_engine != scenario.get("target_engine", "")
    ):
        try:
            source_state = validate_ownership_state(
                observed_state,
                observed_state_bytes,
                active_engine,
                "rolled-back source state receipt",
            )
            validate_prior_source_receipt(
                source_state, scenario, "rolled-back source state receipt"
            )
            outcome = "rolled_back_source_active"
        except ProbeObservationError:
            pass
    semantic["active_dns_engine"] = active_engine
    semantic["recovery_outcome"] = outcome
    semantic["errors"] = sorted(errors)
    canonical = json.dumps(semantic, sort_keys=True, separators=(",", ":")).encode()
    fingerprint = hashlib.sha256(canonical).hexdigest()
    return {
        "schema": OUTPUT_SCHEMA,
        "converged": not errors,
        "recovery_outcome": outcome,
        "active_dns_engine": active_engine,
        "fingerprint": fingerprint,
        "detail": "converged" if not errors else "; ".join(sorted(errors))[:4096],
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--scenario", required=True, type=Path)
    parser.add_argument("--identity-receipt", required=True, type=Path)
    parser.add_argument("--ledger", required=True, type=Path)
    parser.add_argument("--state", required=True, type=Path)
    parser.add_argument("--journal", required=True, type=Path)
    args = parser.parse_args()
    if CELL_RE.fullmatch(args.cell_id) is None:
        parser.error("cell ID is not canonical")
    for name in ("scenario", "identity_receipt", "ledger", "state", "journal"):
        value = getattr(args, name)
        if not value.is_absolute() or value != Path(os.path.normpath(value)):
            parser.error(f"{name.replace('_', '-')} path must be clean and absolute")
    return args


def main() -> int:
    args = parse_args()
    try:
        result = probe(args)
    except BaseException as exc:  # Preserve one valid probe object even for an unexpected observation failure.
        semantic = f"unexpected:{type(exc).__name__}:{exc}"
        result = {
            "schema": OUTPUT_SCHEMA,
            "converged": False,
            "recovery_outcome": "indeterminate",
            "active_dns_engine": "",
            "fingerprint": hashlib.sha256(semantic.encode()).hexdigest(),
            "detail": semantic[:4096],
        }
    sys.stdout.write(json.dumps(result, sort_keys=True, separators=(",", ":")) + "\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
