#!/usr/bin/env python3
"""Run one S-1 DNS journal SIGKILL cell on an already-provisioned host.

This controller deliberately does not provision VMs or construct driver fixtures.
Commands are JSON argv arrays and are never evaluated by a shell.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import ipaddress
import json
import os
import select
import secrets
import signal
import socket
import sqlite3
import stat
import struct
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, BinaryIO, Mapping, Sequence


MATRIX_SCHEMA = "celikpanel/dns-kill-matrix/v1"
MARKER_SCHEMA = "celikpanel-dns-kill-matrix-boundary/v1"
ROLLBACK_PRECURSOR_SCHEMA = (
    "celikpanel-dns-kill-matrix-rollback-precursor/v1"
)
ROLLBACK_PRECURSOR_ACTION = "returned-injected-error"
JOURNAL_SCHEMA = "celikpanel-dns-engine-switch-journal/v1"
PROOF_SCHEMA = "celikpanel/dns-kill-proof/v1"
RESULT_SCHEMA = "celikpanel/dns-kill-result/v1"
RECOVERY_PROBE_SCHEMA = "celikpanel/dns-kill-recovery-probe/v1"
SOURCE_PROOF_SCHEMA = "celikpanel/dns-kill-source-proof/v1"
SOURCE_PREINSTALL_SCHEMA = "celikpanel/dns-kill-source-preinstall/v1"
SOURCE_PREINSTALL_PROOF_PATH = (
    "/var/lib/celikpanel-dns-kill-matrix/source-preinstall-pdns.json"
)
SOURCE_ADOPTION_SCHEMA = "celikpanel/dns-kill-source-adoption/v2"
SOURCE_ADOPTION_PROOF_PATH = (
    "/var/lib/celikpanel-dns-kill-matrix/source-adoption-pdns.json"
)
EXTERNAL_PDNS_PREIMAGE_SCHEMA = (
    "celikpanel/dns-kill-external-pdns-adoption-preimage/v1"
)
EXTERNAL_PDNS_PREIMAGE_PATH = (
    "/var/lib/celikpanel-dns-kill-matrix/source-external-pdns-preimage.json"
)
SOURCE_NORMALIZATION_IDENTITY_SCHEMA = (
    "celikpanel-dns-kill-matrix-pdns-normalization-identity/v1"
)
SOURCE_NORMALIZATION_IDENTITY_PATH = (
    "/var/lib/celikpanel-dns-kill-matrix/source-normalization-pdns-identity.json"
)
SCENARIO_SCHEMA = "celikpanel-dns-kill-matrix-trigger/v1"
TRIGGER_IDENTITY_RECEIPT_SCHEMA = (
    "celikpanel-dns-kill-matrix-trigger-identity/v1"
)
TRIGGER_OWNER_NAMESPACE = "celikpanel/dns-kill-matrix-owner/v1"

SELECTOR_PREFIX = "CELIKPANEL_DNS_KILL_MATRIX_"
EXTERNAL_LOCK_FD_ENV = "CELIKPANEL_MUTATION_LOCK_FD"
SELECTOR_NAMES = (
    "CELIKPANEL_DNS_KILL_MATRIX_CELL_ID",
    "CELIKPANEL_DNS_KILL_MATRIX_DRIVER",
    "CELIKPANEL_DNS_KILL_MATRIX_POINT",
    "CELIKPANEL_DNS_KILL_MATRIX_PHASE",
    "CELIKPANEL_DNS_KILL_MATRIX_REQUEST_ID",
    "CELIKPANEL_DNS_KILL_MATRIX_NONCE",
    "CELIKPANEL_DNS_KILL_MATRIX_MARKER",
    "CELIKPANEL_DNS_KILL_MATRIX_READY_FD",
)

PHASES = (
    "intent",
    "target-staged",
    "source-stopped",
    "target-started",
    "target-verified",
    "committed",
    "rolling-back",
    "rolled-back",
)

MAX_JSON_BYTES = 96 << 20
MAX_COMMAND_OUTPUT = 1 << 20
DNS_TYPES = {"A": 1, "NS": 2, "SOA": 6, "AAAA": 28}
PRODUCTION_AGENT_GROUP = "celikpanel"
PRODUCTION_AGENT_UMASK = 0o027
DEFAULT_COMMAND_PATH = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
PRODUCTION_DKIM_DIR = "/etc/celikpanel/dkim"
PRODUCTION_RUNTIMES_DIR = "/opt/celikpanel/runtimes"

BOUNDARY_JOURNAL_IDENTITY_FIELDS = (
    "mode",
    "mutation_owner_id",
    "manifest_qualifier",
    "source_engine",
    "target_engine",
    "source_epoch",
    "target_epoch",
    "source_revision",
    "topology",
    "pair_role",
)
OPTIONAL_BOUNDARY_JOURNAL_IDENTITY_FIELDS = {"source_engine", "pair_role"}

ROLLBACK_PRECURSOR_PHASES = {
    "bind": "target-staged",
    "pdns-switch": "target-staged",
    "pdns-adopt": "intent",
    "pdns-secondary-reconfigure": "target-staged",
}


class ControllerError(RuntimeError):
    """A fail-closed controller or post-kill assertion failure."""


class BoundaryUnverified(ControllerError):
    """The requested boundary kill was not proven."""


class TriggerExitedBeforeBoundary(BoundaryUnverified):
    """The socket trigger completed before the requested hook fired."""

    def __init__(self, raw_returncode: int):
        self.raw_returncode = raw_returncode
        self.exit_code = 128 - raw_returncode if raw_returncode < 0 else raw_returncode
        super().__init__(
            "scenario trigger exited before boundary notification "
            f"with exit code {self.exit_code} (raw {self.raw_returncode})"
        )


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")


def _json_object_without_duplicates(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ControllerError(f"JSON contains duplicate key {key!r}")
        result[key] = value
    return result


def decode_json(raw: bytes, label: str) -> Any:
    try:
        return json.loads(raw, object_pairs_hook=_json_object_without_duplicates)
    except (UnicodeDecodeError, json.JSONDecodeError, ControllerError) as exc:
        raise ControllerError(f"decode {label}: {exc}") from exc


def require_clean_absolute(path: str, label: str) -> str:
    if not path or not os.path.isabs(path) or os.path.normpath(path) != path:
        raise ControllerError(f"{label} must be a clean absolute path")
    return path


def require_real_directory(path: str, label: str) -> os.stat_result:
    require_clean_absolute(path, label)
    try:
        status = os.lstat(path)
    except OSError as exc:
        raise ControllerError(f"inspect {label}: {exc}") from exc
    if stat.S_ISLNK(status.st_mode) or not stat.S_ISDIR(status.st_mode):
        raise ControllerError(f"{label} must be a real directory")
    return status


def require_new_output_path(path: str, label: str) -> None:
    require_clean_absolute(path, label)
    require_real_directory(os.path.dirname(path), f"{label} parent")
    try:
        os.lstat(path)
    except FileNotFoundError:
        return
    except OSError as exc:
        raise ControllerError(f"inspect {label}: {exc}") from exc
    raise ControllerError(f"{label} already exists: {path}")


def secure_read_bytes(
    path: str,
    label: str,
    *,
    maximum: int = MAX_JSON_BYTES,
    required_mode: int | None = None,
    required_uid: int | None = None,
) -> tuple[bytes, os.stat_result]:
    require_clean_absolute(path, label)
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    try:
        before = os.lstat(path)
        fd = os.open(path, flags)
    except OSError as exc:
        raise ControllerError(f"open {label}: {exc}") from exc
    try:
        opened = os.fstat(fd)
        if stat.S_ISLNK(before.st_mode) or not stat.S_ISREG(opened.st_mode):
            raise ControllerError(f"{label} is not a regular non-symlink file")
        if (before.st_dev, before.st_ino) != (opened.st_dev, opened.st_ino):
            raise ControllerError(f"{label} changed while opening")
        if opened.st_nlink != 1:
            raise ControllerError(f"{label} must have exactly one link")
        if required_mode is not None and stat.S_IMODE(opened.st_mode) != required_mode:
            raise ControllerError(
                f"{label} mode is {stat.S_IMODE(opened.st_mode):04o}, want {required_mode:04o}"
            )
        if required_uid is not None and opened.st_uid != required_uid:
            raise ControllerError(f"{label} owner is {opened.st_uid}, want {required_uid}")
        chunks: list[bytes] = []
        total = 0
        while True:
            chunk = os.read(fd, min(65536, maximum + 1 - total))
            if not chunk:
                break
            chunks.append(chunk)
            total += len(chunk)
            if total > maximum:
                raise ControllerError(f"{label} exceeds {maximum} bytes")
        after = os.fstat(fd)
        if (
            (after.st_dev, after.st_ino, after.st_size)
            != (opened.st_dev, opened.st_ino, opened.st_size)
            or total != opened.st_size
        ):
            raise ControllerError(f"{label} changed while reading")
        return b"".join(chunks), opened
    finally:
        os.close(fd)


def secure_regular_metadata(
    path: str,
    label: str,
    *,
    required_mode: int,
    required_uid: int,
    required_gid: int,
    required_size: int,
    require_empty: bool = False,
) -> os.stat_result:
    """Inspect a live regular file without treating its contents as immutable."""
    require_clean_absolute(path, label)
    flags = (
        os.O_RDONLY
        | getattr(os, "O_CLOEXEC", 0)
        | getattr(os, "O_NOFOLLOW", 0)
        | getattr(os, "O_NONBLOCK", 0)
    )
    try:
        before = os.lstat(path)
        if stat.S_ISLNK(before.st_mode) or not stat.S_ISREG(before.st_mode):
            raise ControllerError(f"{label} is not a regular non-symlink file")
        fd = os.open(path, flags)
    except ControllerError:
        raise
    except OSError as exc:
        raise ControllerError(f"open {label}: {exc}") from exc
    try:
        opened = os.fstat(fd)

        def identity(status: os.stat_result) -> tuple[int, ...]:
            return (
                status.st_dev,
                status.st_ino,
                status.st_mode,
                status.st_nlink,
                status.st_uid,
                status.st_gid,
                status.st_size,
            )

        if not stat.S_ISREG(opened.st_mode):
            raise ControllerError(f"{label} is not a regular non-symlink file")
        if identity(before) != identity(opened):
            raise ControllerError(f"{label} changed while opening")
        if opened.st_nlink != 1:
            raise ControllerError(f"{label} must have exactly one link")
        if stat.S_IMODE(opened.st_mode) != required_mode:
            raise ControllerError(
                f"{label} mode is {stat.S_IMODE(opened.st_mode):04o}, "
                f"want {required_mode:04o}"
            )
        if opened.st_uid != required_uid or opened.st_gid != required_gid:
            raise ControllerError(
                f"{label} owner is {opened.st_uid}:{opened.st_gid}, "
                f"want {required_uid}:{required_gid}"
            )
        if opened.st_size != required_size:
            raise ControllerError(
                f"{label} size is {opened.st_size}, want {required_size}"
            )
        if require_empty and os.read(fd, 1) != b"":
            raise ControllerError(f"{label} must be empty")
        try:
            after_path = os.lstat(path)
        except OSError as exc:
            raise ControllerError(f"reinspect {label}: {exc}") from exc
        final = os.fstat(fd)
        if identity(after_path) != identity(opened) or identity(final) != identity(opened):
            raise ControllerError(f"{label} changed while inspecting")
        return final
    finally:
        os.close(fd)


def secure_read_json(
    path: str,
    label: str,
    *,
    maximum: int = MAX_JSON_BYTES,
    required_mode: int | None = None,
    required_uid: int | None = None,
) -> tuple[Any, os.stat_result]:
    raw, status = secure_read_bytes(
        path,
        label,
        maximum=maximum,
        required_mode=required_mode,
        required_uid=required_uid,
    )
    return decode_json(raw, label), status


def atomic_write_new_json(path: str, value: Any, mode: int = 0o600) -> None:
    require_new_output_path(path, "JSON output")
    parent = os.path.dirname(path)
    encoded = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode("utf-8")
    fd, staged = tempfile.mkstemp(prefix=f".{os.path.basename(path)}.tmp-", dir=parent)
    published = False
    try:
        os.fchmod(fd, mode)
        with os.fdopen(fd, "wb", closefd=True) as stream:
            stream.write(encoded)
            stream.flush()
            os.fsync(stream.fileno())
        fd = -1
        os.link(staged, path)
        published = True
        os.unlink(staged)
        directory_fd = os.open(parent, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
    finally:
        if fd >= 0:
            os.close(fd)
        if not published:
            try:
                os.unlink(staged)
            except FileNotFoundError:
                pass


def sha256_file(path: str) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as stream:
        while chunk := stream.read(1 << 20):
            digest.update(chunk)
    return digest.hexdigest()


class Transcript:
    def __init__(self, path: str):
        require_new_output_path(path, "transcript")
        self.path = path
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_APPEND, 0o600)
        self._stream: BinaryIO = os.fdopen(fd, "ab", buffering=0)

    def fileno(self) -> int:
        return self._stream.fileno()

    def event(self, event: str, **fields: Any) -> None:
        record = {"at": utc_now(), "event": event, **fields}
        self._stream.write(("CONTROLLER " + json.dumps(record, sort_keys=True) + "\n").encode())

    def command_output(self, label: str, output: bytes, truncated: bool) -> None:
        self.event("command-output", label=label, bytes=len(output), truncated=truncated)
        if output:
            self._stream.write(output)
            if not output.endswith(b"\n"):
                self._stream.write(b"\n")

    def close(self) -> None:
        if not self._stream.closed:
            self._stream.flush()
            os.fsync(self._stream.fileno())
            self._stream.close()


def require_string(value: Any, label: str) -> str:
    if not isinstance(value, str) or not value:
        raise ControllerError(f"{label} must be a non-empty string")
    return value


def valid_lower_hex(value: str, minimum: int, maximum: int) -> bool:
    if len(value) < minimum or len(value) > maximum or len(value) % 2:
        return False
    return all(character in "0123456789abcdef" for character in value)


def deterministic_trigger_owner(cell_id: str, request_id: str) -> str:
    if not cell_id or not valid_lower_hex(request_id, 32, 32):
        raise ControllerError("cannot derive a trigger owner from invalid identity")
    digest = hashlib.sha256(
        (
            TRIGGER_OWNER_NAMESPACE
            + "\x00"
            + request_id
            + "\x00"
            + cell_id
        ).encode("utf-8")
    ).digest()
    return digest[:16].hex()


def validate_trigger_identity_receipt(
    path: str, cell: "CellSpec", request_id: str
) -> dict[str, Any]:
    receipt, status = secure_read_json(
        path,
        "scenario trigger identity receipt",
        maximum=1 << 20,
        required_mode=0o600,
        required_uid=os.geteuid(),
    )
    if not isinstance(receipt, dict):
        raise ControllerError("scenario trigger identity receipt root is not an object")
    keys = {
        "schema",
        "cell_id",
        "driver",
        "source_fixture",
        "request_id",
        "owner_id",
        "manifest_qualifier",
    }
    if set(receipt) != keys:
        raise ControllerError(
            "scenario trigger identity receipt fields differ from the exact contract"
        )
    expected = {
        "schema": TRIGGER_IDENTITY_RECEIPT_SCHEMA,
        "cell_id": cell.cell_id,
        "driver": cell.driver,
        "request_id": request_id,
        "owner_id": deterministic_trigger_owner(cell.cell_id, request_id),
    }
    for key, value in expected.items():
        if receipt.get(key) != value:
            raise ControllerError(
                f"scenario trigger identity receipt {key}={receipt.get(key)!r}, want {value!r}"
            )
    source_fixture = receipt.get("source_fixture")
    qualifier = receipt.get("manifest_qualifier")
    prefix = "dns-engine-switch/v1:sha256:"
    if not isinstance(source_fixture, str) or not source_fixture:
        raise ControllerError("scenario trigger identity receipt has no source fixture")
    if (
        not isinstance(qualifier, str)
        or not qualifier.startswith(prefix)
        or not valid_lower_hex(qualifier[len(prefix) :], 64, 64)
    ):
        raise ControllerError(
            "scenario trigger identity receipt has an invalid manifest qualifier"
        )
    return {
        **receipt,
        "path": path,
        "device": status.st_dev,
        "inode": status.st_ino,
        "mode": f"{stat.S_IMODE(status.st_mode):04o}",
        "sha256": sha256_file(path),
    }


def validate_socket_boundary_identity(
    source_proof: Mapping[str, Any],
    identity_receipt: Mapping[str, Any],
) -> dict[str, Any]:
    """Bind the stopped journal identity to the preflight and trigger receipt."""

    if identity_receipt.get("source_fixture") != source_proof.get("source_fixture"):
        raise BoundaryUnverified(
            "scenario trigger identity receipt source fixture differs from source proof"
        )
    if identity_receipt.get("path") != source_proof.get("identity_receipt_path"):
        raise BoundaryUnverified(
            "scenario trigger identity receipt path differs from source proof"
        )
    scenario_identity = source_proof.get("scenario_identity")
    scenario_fields = {
        "mode",
        "source_engine",
        "target_engine",
        "source_epoch",
        "target_epoch",
        "source_revision",
        "topology",
        "pair_role",
    }
    if not isinstance(scenario_identity, dict) or set(scenario_identity) != scenario_fields:
        raise BoundaryUnverified(
            "validated source proof lacks an exact scenario journal identity"
        )
    return {
        "mode": scenario_identity["mode"],
        "mutation_owner_id": identity_receipt["owner_id"],
        "manifest_qualifier": identity_receipt["manifest_qualifier"],
        "source_engine": scenario_identity["source_engine"],
        "target_engine": scenario_identity["target_engine"],
        "source_epoch": scenario_identity["source_epoch"],
        "target_epoch": scenario_identity["target_epoch"],
        "source_revision": scenario_identity["source_revision"],
        "topology": scenario_identity["topology"],
        "pair_role": scenario_identity["pair_role"],
    }


@dataclass(frozen=True)
class CellSpec:
    cell_id: str
    driver: str
    role: str
    peer_reachability: str
    phase: str
    edge: str
    point: str
    source_fixture_policy: str = "driver-specific"

    @classmethod
    def from_manifest(cls, manifest: Mapping[str, Any], cell_id: str) -> "CellSpec":
        if manifest.get("schema") != MATRIX_SCHEMA:
            raise ControllerError("matrix manifest schema is unsupported")
        cells = manifest.get("cells")
        if not isinstance(cells, list):
            raise ControllerError("matrix manifest cells are absent")
        matches = [cell for cell in cells if isinstance(cell, dict) and cell.get("id") == cell_id]
        if len(matches) != 1:
            raise ControllerError(f"matrix cell {cell_id!r} is absent or duplicated")
        cell = matches[0]
        if cell.get("status") != "runnable":
            raise ControllerError(f"matrix cell {cell_id!r} is not runnable")
        boundary = cell.get("boundary")
        selector = cell.get("fault_selector")
        if not isinstance(boundary, dict) or not isinstance(selector, dict):
            raise ControllerError("matrix cell boundary or selector is absent")
        phase = require_string(boundary.get("phase"), "cell boundary phase")
        edge = require_string(boundary.get("edge"), "cell boundary edge")
        point = require_string(selector.get("point"), "cell selector point")
        if selector.get("phase") != phase:
            raise ControllerError("cell selector phase differs from its boundary")
        expected_point = "pre_intent" if edge == "window" else edge.replace("-", "_")
        if point != expected_point:
            raise ControllerError("cell selector point differs from its boundary edge")
        if phase == "pre-intent":
            if edge != "window" or point != "pre_intent":
                raise ControllerError("pre-intent cell has an invalid selector")
        elif phase not in PHASES or edge not in ("before-write", "after-write"):
            raise ControllerError("cell names an unsupported journal boundary")
        driver = require_string(cell.get("driver"), "cell driver")
        role = require_string(cell.get("role"), "cell role")
        peer = require_string(cell.get("peer_reachability"), "cell peer reachability")
        placement = cell.get("placement")
        if not isinstance(placement, dict):
            raise ControllerError("matrix cell placement is absent")
        source_fixture_policy = require_string(
            placement.get("source_fixture_policy"),
            "cell source fixture policy",
        )
        if source_fixture_policy not in {
            "driver-specific",
            "managed-pdns-required",
            "uninitialized-permitted-noncritical",
        }:
            raise ControllerError("cell source fixture policy is unsupported")
        if role not in ("standalone", "paired-primary", "paired-secondary"):
            raise ControllerError("cell role is unsupported")
        if peer not in ("reachable", "unreachable"):
            raise ControllerError("cell peer reachability is unsupported")
        return cls(
            cell_id,
            driver,
            role,
            peer,
            phase,
            edge,
            point,
            source_fixture_policy,
        )


def load_cell(manifest_path: str, cell_id: str) -> CellSpec:
    manifest, _ = secure_read_json(manifest_path, "matrix manifest")
    if not isinstance(manifest, dict):
        raise ControllerError("matrix manifest root must be an object")
    return CellSpec.from_manifest(manifest, cell_id)


def parse_command_json(raw: str, label: str) -> tuple[str, ...]:
    try:
        value = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise ControllerError(f"decode {label}: {exc}") from exc
    if (
        not isinstance(value, list)
        or not value
        or any(not isinstance(item, str) or not item or "\x00" in item for item in value)
    ):
        raise ControllerError(f"{label} must be a non-empty JSON array of non-empty strings")
    return tuple(value)


def socket_trigger_retry_contract(
    trigger: Sequence[str], recovery: Sequence[str]
) -> dict[str, str]:
    expected_flags = ("--scenario", "--identity-receipt", "--timeout")
    if len(trigger) != 8 or len(recovery) != 8:
        raise ControllerError(
            "socket trigger and recovery commands must contain exactly three flag/value pairs"
        )
    if trigger[1] != "rpc-switch" or recovery[1] != "rpc-retry":
        raise ControllerError(
            "socket recovery must pair rpc-switch with rpc-retry"
        )
    if trigger[0] != recovery[0] or tuple(trigger[2:]) != tuple(recovery[2:]):
        raise ControllerError(
            "socket recovery must reuse the exact trigger executable, scenario, identity receipt, and timeout"
        )
    if tuple(trigger[2::2]) != expected_flags:
        raise ControllerError(
            "socket trigger arguments must be --scenario, --identity-receipt, --timeout in exact order"
        )
    scenario_path = require_clean_absolute(trigger[3], "scenario trigger manifest")
    receipt_path = require_clean_absolute(
        trigger[5], "scenario trigger identity receipt"
    )
    require_real_directory(
        os.path.dirname(receipt_path), "scenario trigger identity receipt parent"
    )
    if not trigger[7].strip():
        raise ControllerError("scenario trigger timeout is empty")
    return {
        "scenario_path": scenario_path,
        "identity_receipt_path": receipt_path,
        "operation_timeout": trigger[7],
    }


SOURCE_PROOF_KEYS = {
    "schema",
    "cell_id",
    "source_fixture",
    "scenario_sha256",
    "identity_receipt_path",
    "identity_receipt_preexisting",
    "engine",
    "engine_epoch",
    "source_revision",
    "serving_before_tagged_agent",
    "engine_state_receipt_path",
    "engine_state_receipt_sha256",
    "engine_state_identity",
    "authoritative_preflight",
    "uninitialized_global_port53",
    "receipt_origin",
    "source_setup_scenario_sha256",
    "source_setup_identity_receipt_sha256",
    "source_preinstall_proof_path",
    "source_preinstall_proof_sha256",
    "source_adoption_proof_path",
    "source_adoption_proof_sha256",
    "external_pdns_preimage_path",
    "external_pdns_preimage_sha256",
    "source_normalization_identity_receipt_path",
    "source_normalization_identity_receipt_sha256",
}
SOURCE_PREINSTALL_KEYS = {
    "schema",
    "cell_id",
    "scope",
    "package_install_origin",
    "source_packages",
    "measured_target_packages",
    "install_guard",
    "mask_removed_before_external_source_start",
    "source_unit_before_external_configuration",
    "dns_state_absent",
    "dns_journal_absent",
    "dns_ownership_receipts_absent",
    "global_udp_tcp_53_bindable",
    "production_pdns_adoption_pending",
}
SOURCE_ADOPTION_KEYS = {
    "schema",
    "cell_id",
    "scope",
    "construction_origin",
    "production_adoption_driver",
    "source_setup_scenario_sha256",
    "source_setup_identity_receipt_sha256",
    "source_packages",
    "measured_target_packages",
    "main_config",
    "managed_config",
    "cluster_config",
    "database",
    "source_unit_after_adoption",
    "production_receipts",
    "external_artifacts_unchanged_by_adoption",
}
EXTERNAL_PDNS_PREIMAGE_KEYS = {
    "schema",
    "cell_id",
    "scope",
    "source_fixture",
    "construction_origin",
    "production_adoption_driver",
    "production_adoption_pending",
    "scenario_sha256",
    "source_preinstall_proof_path",
    "source_preinstall_proof_sha256",
    "source_packages",
    "main_config",
    "managed_config",
    "cluster_config",
    "database",
    "source_unit_before_tagged_agent",
    "authoritative_preflight",
    "production_receipts_absent",
}
SCENARIO_KEYS = {
    "schema",
    "driver",
    "source_fixture",
    "mode",
    "source_engine",
    "target_engine",
    "source_epoch",
    "target_epoch",
    "source_revision",
    "topology",
    "pair_role",
    "local_ip",
    "local_ns",
    "peer_ip",
    "peer_ns",
    "zones",
}
SCENARIO_REQUIRED_KEYS = {
    "schema",
    "driver",
    "source_fixture",
    "mode",
    "target_engine",
    "source_epoch",
    "target_epoch",
    "source_revision",
    "topology",
    "zones",
}
SOURCE_FIXTURE_DRIVERS = {
    "bind": {"uninitialized", "managed-pdns"},
    "pdns-switch": {"uninitialized", "managed-bind"},
    "pdns-adopt": {"external-pdns-adoption"},
    "pdns-secondary-reconfigure": {"legacy-pdns-secondary"},
}
SOURCE_FIXTURE_ENGINES = {
    "uninitialized": "",
    "managed-pdns": "pdns",
    "managed-bind": "bind",
    "external-pdns-adoption": "pdns",
    "legacy-pdns-secondary": "pdns",
}
MANAGED_SOURCE_FIXTURES = {"managed-pdns", "managed-bind"}
DNS_STATE_KEYS = {
    "schema",
    "mode",
    "engine",
    "engine_epoch",
    "generation",
    "pair_role",
    "pair_local_ip",
    "pair_peer_ip",
    "primary_catalog_serial",
    "source_revision",
    "manifest_qualifier",
    "mutation_request_id",
    "mutation_owner_id",
}
DNS_STATE_REQUIRED_KEYS = {
    "schema",
    "mode",
    "engine",
    "engine_epoch",
    "source_revision",
    "manifest_qualifier",
    "mutation_request_id",
    "mutation_owner_id",
}


def valid_sha256(value: Any) -> bool:
    return isinstance(value, str) and valid_lower_hex(value, 64, 64)


def _canonical_identity_number(value: str, label: str) -> int:
    if not value or not value.isascii() or not value.isdigit():
        raise ControllerError(f"{label} is not canonical decimal")
    parsed = int(value, 10)
    if parsed <= 0 or parsed > (1 << 31) - 1 or str(parsed) != value:
        raise ControllerError(f"{label} is outside the safe identity range")
    return parsed


def _parse_pdns_getent_records(passwd_raw: bytes, group_raw: bytes) -> tuple[int, int]:
    try:
        passwd_text = passwd_raw.decode("ascii")
        group_text = group_raw.decode("ascii")
    except UnicodeDecodeError as exc:
        raise ControllerError("pdns account lookup returned non-ASCII data") from exc
    if (
        not passwd_text.endswith("\n")
        or passwd_text.count("\n") != 1
        or not group_text.endswith("\n")
        or group_text.count("\n") != 1
    ):
        raise ControllerError("pdns account lookup returned non-canonical records")
    passwd_fields = passwd_text[:-1].split(":")
    group_fields = group_text[:-1].split(":")
    if (
        len(passwd_fields) != 7
        or passwd_fields[0] != "pdns"
        or passwd_fields[1] != "x"
        or not passwd_fields[5].startswith("/")
        or not passwd_fields[6].startswith("/")
    ):
        raise ControllerError("pdns passwd record is not canonical")
    if (
        len(group_fields) != 4
        or group_fields[0] != "pdns"
        or group_fields[1] != "x"
        or group_fields[3] != ""
    ):
        raise ControllerError("pdns group record is not canonical")
    uid = _canonical_identity_number(passwd_fields[2], "pdns uid")
    passwd_gid = _canonical_identity_number(passwd_fields[3], "pdns passwd gid")
    group_gid = _canonical_identity_number(group_fields[2], "pdns group gid")
    if passwd_gid != group_gid:
        raise ControllerError("pdns passwd and group identities differ")
    return uid, group_gid


def resolve_exact_pdns_owner_identity() -> tuple[int, int]:
    path = "/usr/bin/getent"
    try:
        status = os.lstat(path)
    except OSError as exc:
        raise ControllerError(f"inspect exact getent executable: {exc}") from exc
    if (
        stat.S_ISLNK(status.st_mode)
        or not stat.S_ISREG(status.st_mode)
        or status.st_uid != 0
        or status.st_gid != 0
        or stat.S_IMODE(status.st_mode) & 0o022
    ):
        raise ControllerError("exact getent executable has unsafe identity")

    def lookup(database: str) -> bytes:
        try:
            result = subprocess.run(
                [path, database, "pdns"],
                check=False,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                timeout=5,
                env={"PATH": DEFAULT_COMMAND_PATH, "LC_ALL": "C"},
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            raise ControllerError(f"resolve pdns {database} identity: {exc}") from exc
        if result.returncode != 0 or result.stderr:
            raise ControllerError(f"resolve pdns {database} identity failed exactly")
        return result.stdout

    first = _parse_pdns_getent_records(lookup("passwd"), lookup("group"))
    second = _parse_pdns_getent_records(lookup("passwd"), lookup("group"))
    if second != first:
        raise ControllerError("pdns file-owner identity changed during exact lookup")
    return second


def validate_source_preinstall_document(
    value: Any, raw: bytes, cell: CellSpec
) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != SOURCE_PREINSTALL_KEYS:
        raise ControllerError("source preinstall proof fields differ from the exact contract")
    canonical = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode("utf-8")
    if raw != canonical:
        raise ControllerError("source preinstall proof is not canonical sorted JSON")
    if (
        cell.driver == "bind"
        and cell.role == "standalone"
        and cell.phase in {"source-stopped", "target-started"}
    ):
        scope = "managed-pdns-source-preparation-for-bind-only"
        measured_target_packages = [{"name": "bind9", "status": "absent"}]
    elif cell.driver == "pdns-adopt" and cell.role == "standalone":
        scope = "external-pdns-source-preparation-for-measured-adoption-only"
        measured_target_packages = [
            {
                "name": "pdns-backend-sqlite3",
                "status": "preexisting-required-by-adoption",
            },
            {
                "name": "pdns-server",
                "status": "preexisting-required-by-adoption",
            },
        ]
    else:
        raise ControllerError(
            "source preinstall proof escaped its exact BIND or PowerDNS adoption scope"
        )
    expected = {
        "schema": SOURCE_PREINSTALL_SCHEMA,
        "cell_id": cell.cell_id,
        "scope": scope,
        "package_install_origin": "harness-source-preinstall",
        "measured_target_packages": measured_target_packages,
        "install_guard": {
            "unit": "pdns.service",
            "persistent_mask_target": "/dev/null",
            "package_hooks_could_not_start": True,
        },
        "mask_removed_before_external_source_start": True,
        "dns_state_absent": True,
        "dns_journal_absent": True,
        "dns_ownership_receipts_absent": True,
        "global_udp_tcp_53_bindable": True,
        "production_pdns_adoption_pending": True,
    }
    if any(value.get(key) != wanted for key, wanted in expected.items()):
        raise ControllerError("source preinstall proof identity or safety claims differ")
    packages = value.get("source_packages")
    if not isinstance(packages, list) or len(packages) != 2:
        raise ControllerError("source preinstall proof package set is invalid")
    package_names: list[str] = []
    for package in packages:
        if not isinstance(package, dict) or set(package) != {"name", "status", "version"}:
            raise ControllerError("source preinstall package fields differ")
        version = package.get("version")
        if (
            package.get("status") != "install ok installed"
            or not isinstance(version, str)
            or not version
            or len(version) > 256
            or version != version.strip()
            or any(ord(character) < 0x21 or ord(character) > 0x7E for character in version)
        ):
            raise ControllerError("source preinstall package evidence is invalid")
        package_names.append(package.get("name"))
    if package_names != ["pdns-backend-sqlite3", "pdns-server"]:
        raise ControllerError("source preinstall proof names the wrong source packages")
    unit = value.get("source_unit_before_external_configuration")
    if (
        not isinstance(unit, dict)
        or set(unit) != {"name", "load_state", "active_state", "unit_file_state"}
        or unit.get("name") != "pdns.service"
        or unit.get("load_state") != "loaded"
        or unit.get("active_state") != "inactive"
        or unit.get("unit_file_state") not in {"disabled", "enabled"}
    ):
        raise ControllerError("source preinstall unit terminal state is invalid")
    return value


def validate_source_setup_provenance(
    proof: Mapping[str, Any], source_fixture: str
) -> dict[str, str]:
    origin = proof.get("receipt_origin")
    scenario_hash = proof.get("source_setup_scenario_sha256")
    identity_hash = proof.get("source_setup_identity_receipt_sha256")
    if source_fixture == "managed-pdns":
        if origin != "production-pdns-adopt-normalized":
            raise ControllerError(
                "managed PowerDNS source proof has the wrong receipt origin"
            )
        if not valid_sha256(scenario_hash) or not valid_sha256(identity_hash):
            raise ControllerError(
                "managed PowerDNS source proof lacks exact setup receipt hashes"
            )
    elif source_fixture == "external-pdns-adoption":
        if (
            origin != "harness-external-pdns-preimage"
            or scenario_hash != "absent"
            or identity_hash != "absent"
        ):
            raise ControllerError(
                "external PowerDNS source has invalid harness preimage provenance"
            )
    elif source_fixture == "uninitialized":
        if (
            origin != "absent-by-proof"
            or scenario_hash != "absent"
            or identity_hash != "absent"
        ):
            raise ControllerError(
                "uninitialized source proof has invalid absent setup provenance"
            )
    else:
        raise ControllerError(
            f"source fixture {source_fixture!r} has no exact source-proof provenance contract"
        )
    return {
        "receipt_origin": origin,
        "source_setup_scenario_sha256": scenario_hash,
        "source_setup_identity_receipt_sha256": identity_hash,
    }


def validate_source_preinstall_provenance(
    proof: Mapping[str, Any], source_fixture: str, cell: CellSpec
) -> dict[str, Any]:
    claimed_path = proof.get("source_preinstall_proof_path")
    claimed_hash = proof.get("source_preinstall_proof_sha256")
    if source_fixture == "uninitialized":
        if claimed_path != "absent" or claimed_hash != "absent":
            raise ControllerError(
                "uninitialized source proof has invalid absent preinstall provenance"
            )
        require_absent_path(
            SOURCE_PREINSTALL_PROOF_PATH, "uninitialized source preinstall proof"
        )
        return {
            "path": "absent",
            "sha256": "absent",
            "exists": False,
        }
    if source_fixture not in {"managed-pdns", "external-pdns-adoption"}:
        raise ControllerError(
            f"source fixture {source_fixture!r} has no preinstall-proof contract"
        )
    if claimed_path != SOURCE_PREINSTALL_PROOF_PATH or not valid_sha256(claimed_hash):
        raise ControllerError(
            "PowerDNS source proof lacks its exact preinstall path or hash"
        )
    value, raw, observed_hash, status = secure_json_with_digest(
        SOURCE_PREINSTALL_PROOF_PATH,
        "PowerDNS source preinstall proof",
        maximum=1 << 20,
    )
    if observed_hash != claimed_hash:
        raise ControllerError("source preinstall proof hash differs from source proof")
    validate_source_preinstall_document(value, raw, cell)
    return {
        "path": SOURCE_PREINSTALL_PROOF_PATH,
        "sha256": observed_hash,
        "device": status.st_dev,
        "inode": status.st_ino,
        "mode": f"{stat.S_IMODE(status.st_mode):04o}",
        "schema": value["schema"],
        "cell_id": value["cell_id"],
        "scope": value["scope"],
        "package_install_origin": value["package_install_origin"],
        "source_packages": value["source_packages"],
        "measured_target_packages": value["measured_target_packages"],
        "install_guard": value["install_guard"],
        "mask_removed_before_external_source_start": value[
            "mask_removed_before_external_source_start"
        ],
        "source_unit_before_external_configuration": value[
            "source_unit_before_external_configuration"
        ],
    }


def validate_source_adoption_document(
    value: Any, raw: bytes, cell: CellSpec
) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != SOURCE_ADOPTION_KEYS:
        raise ControllerError("source adoption proof fields differ from the exact contract")
    canonical = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode("utf-8")
    if raw != canonical:
        raise ControllerError("source adoption proof is not canonical sorted JSON")
    if (
        cell.driver != "bind"
        or cell.role != "standalone"
        or cell.phase not in {"source-stopped", "target-started"}
    ):
        raise ControllerError("source adoption proof escaped critical standalone BIND scope")
    expected = {
        "schema": SOURCE_ADOPTION_SCHEMA,
        "cell_id": cell.cell_id,
        "scope": "external-pdns-source-for-production-adoption-before-bind",
        "construction_origin": "harness-external-pdns",
        "production_adoption_driver": "pdns-adopt",
        "measured_target_packages": [{"name": "bind9", "status": "absent"}],
        "external_artifacts_unchanged_by_adoption": True,
    }
    if any(value.get(key) != wanted for key, wanted in expected.items()):
        raise ControllerError("source adoption proof identity or safety claims differ")
    for key in (
        "source_setup_scenario_sha256",
        "source_setup_identity_receipt_sha256",
    ):
        if not valid_sha256(value.get(key)):
            raise ControllerError("source adoption proof lacks exact setup hashes")
    packages = value.get("source_packages")
    if not isinstance(packages, list) or len(packages) != 2:
        raise ControllerError("source adoption proof package set is invalid")
    package_names: list[str] = []
    for package in packages:
        if not isinstance(package, dict) or set(package) != {"name", "status", "version"}:
            raise ControllerError("source adoption package fields differ")
        version = package.get("version")
        if (
            package.get("status") != "install ok installed"
            or not isinstance(version, str)
            or not version
            or len(version) > 256
            or version != version.strip()
            or any(ord(character) < 0x21 or ord(character) > 0x7E for character in version)
        ):
            raise ControllerError("source adoption package evidence is invalid")
        package_names.append(package.get("name"))
    if package_names != ["pdns-backend-sqlite3", "pdns-server"]:
        raise ControllerError("source adoption proof names the wrong source packages")

    main = value.get("main_config")
    if (
        not isinstance(main, dict)
        or set(main) != {"path", "sha256", "owner", "mode"}
        or main.get("path") != "/etc/powerdns/pdns.conf"
        or not valid_sha256(main.get("sha256"))
        or main.get("owner") not in {"root:pdns", "root:root"}
        or main.get("mode") != "0640"
    ):
        raise ControllerError("source adoption main-config evidence is invalid")
    managed = value.get("managed_config")
    if (
        not isinstance(managed, dict)
        or set(managed) != {"path", "sha256", "owner", "mode"}
        or managed.get("path") != "/etc/powerdns/pdns.d/celikpanel.conf"
        or not valid_sha256(managed.get("sha256"))
        or managed.get("owner") != "root:root"
        or managed.get("mode") != "0644"
    ):
        raise ControllerError("source adoption managed-config evidence is invalid")
    cluster = value.get("cluster_config")
    if cluster != {
        "path": "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
        "status": "absent",
    }:
        raise ControllerError("source adoption cluster-config evidence is invalid")
    database = value.get("database")
    database_keys = {
        "path",
        "sha256",
        "owner",
        "mode",
        "schema_path",
        "schema_sha256",
        "quick_check",
        "sidecars",
    }
    if (
        not isinstance(database, dict)
        or set(database) != database_keys
        or database.get("path") != "/var/lib/powerdns/pdns.sqlite3"
        or not valid_sha256(database.get("sha256"))
        or database.get("owner") != "pdns:pdns"
        or database.get("mode") != "0640"
        or database.get("schema_path")
        != "/usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql"
        or not valid_sha256(database.get("schema_sha256"))
        or database.get("quick_check") != "ok"
    ):
        raise ControllerError("source adoption database evidence is invalid")
    sidecars = database.get("sidecars")
    if not isinstance(sidecars, dict) or set(sidecars) != {
        "rollback_journal",
        "shared_memory",
        "write_ahead_log",
    }:
        raise ControllerError("source adoption database sidecar fields differ")
    journal = sidecars.get("rollback_journal")
    if journal != {
        "path": "/var/lib/powerdns/pdns.sqlite3-journal",
        "status": "absent",
    }:
        raise ControllerError("source adoption rollback-journal evidence is invalid")
    wal = sidecars.get("write_ahead_log")
    shm = sidecars.get("shared_memory")
    metadata_keys = {
        "path",
        "file_type",
        "owner",
        "mode",
        "link_count",
        "device",
        "inode",
        "size",
        "content_policy",
    }
    if (
        not isinstance(wal, dict)
        or set(wal) != metadata_keys
        or wal.get("path") != "/var/lib/powerdns/pdns.sqlite3-wal"
        or wal.get("file_type") != "regular"
        or wal.get("owner") != "pdns:pdns"
        or wal.get("mode") != "0640"
        or type(wal.get("link_count")) is not int
        or wal.get("link_count") != 1
        or not isinstance(wal.get("device"), int)
        or isinstance(wal.get("device"), bool)
        or wal.get("device") <= 0
        or not isinstance(wal.get("inode"), int)
        or isinstance(wal.get("inode"), bool)
        or wal.get("inode") <= 0
        or type(wal.get("size")) is not int
        or wal.get("size") != 0
        or wal.get("content_policy") != "empty"
    ):
        raise ControllerError("source adoption write-ahead-log evidence is invalid")
    if (
        not isinstance(shm, dict)
        or set(shm) != metadata_keys
        or shm.get("path") != "/var/lib/powerdns/pdns.sqlite3-shm"
        or shm.get("file_type") != "regular"
        or shm.get("owner") != "pdns:pdns"
        or shm.get("mode") != "0640"
        or type(shm.get("link_count")) is not int
        or shm.get("link_count") != 1
        or not isinstance(shm.get("device"), int)
        or isinstance(shm.get("device"), bool)
        or shm.get("device") <= 0
        or not isinstance(shm.get("inode"), int)
        or isinstance(shm.get("inode"), bool)
        or shm.get("inode") <= 0
        or type(shm.get("size")) is not int
        or shm.get("size") != 32768
        or shm.get("content_policy") != "volatile-unhashed"
    ):
        raise ControllerError("source adoption shared-memory evidence is invalid")
    if (
        wal["device"] != shm["device"]
        or wal["inode"] == shm["inode"]
    ):
        raise ControllerError("source adoption sidecar identities conflict")
    unit = value.get("source_unit_after_adoption")
    if unit != {
        "name": "pdns.service",
        "load_state": "loaded",
        "active_state": "active",
        "sub_state": "running",
        "unit_file_state": "enabled",
    }:
        raise ControllerError("source adoption unit evidence is invalid")
    receipts = value.get("production_receipts")
    receipt_keys = {
        "state_sha256",
        "active_ownership_sha256",
        "source_install_ownership_absent",
        "measured_target_ownership_absent",
        "measured_target_install_ownership_absent",
        "switch_journal_absent",
    }
    if (
        not isinstance(receipts, dict)
        or set(receipts) != receipt_keys
        or not valid_sha256(receipts.get("state_sha256"))
        or receipts.get("active_ownership_sha256") != receipts.get("state_sha256")
        or any(
            receipts.get(key) is not True
            for key in (
                "source_install_ownership_absent",
                "measured_target_ownership_absent",
                "measured_target_install_ownership_absent",
                "switch_journal_absent",
            )
        )
    ):
        raise ControllerError("source adoption production-receipt evidence is invalid")
    return value


def validate_source_adoption_provenance(
    proof: Mapping[str, Any], source_fixture: str, cell: CellSpec
) -> dict[str, Any]:
    claimed_path = proof.get("source_adoption_proof_path")
    claimed_hash = proof.get("source_adoption_proof_sha256")
    if source_fixture in {"uninitialized", "external-pdns-adoption"}:
        if claimed_path != "absent" or claimed_hash != "absent":
            raise ControllerError(
                f"{source_fixture} source proof has invalid absent adoption provenance"
            )
        require_absent_path(
            SOURCE_ADOPTION_PROOF_PATH, f"{source_fixture} source adoption proof"
        )
        return {"path": "absent", "sha256": "absent", "exists": False}
    if source_fixture != "managed-pdns":
        raise ControllerError(
            f"source fixture {source_fixture!r} has no adoption-proof contract"
        )
    if claimed_path != SOURCE_ADOPTION_PROOF_PATH or not valid_sha256(claimed_hash):
        raise ControllerError(
            "managed PowerDNS source proof lacks its exact adoption path or hash"
        )
    value, raw, observed_hash, status = secure_json_with_digest(
        SOURCE_ADOPTION_PROOF_PATH,
        "managed PowerDNS source adoption proof",
        maximum=1 << 20,
    )
    if observed_hash != claimed_hash:
        raise ControllerError("source adoption proof hash differs from source proof")
    validate_source_adoption_document(value, raw, cell)
    if (
        value["source_setup_scenario_sha256"]
        != proof.get("source_setup_scenario_sha256")
        or value["source_setup_identity_receipt_sha256"]
        != proof.get("source_setup_identity_receipt_sha256")
    ):
        raise ControllerError("source adoption proof differs from setup provenance")

    root_uid = os.geteuid()
    if root_uid != 0:
        raise ControllerError("source adoption proof requires root controller identity")
    # Adoption is an immutable historical checkpoint. The immediately-following
    # production ConfigurePowerDNSSQLite and SyncDNSZoneV3 calls legitimately
    # rewrite the configuration/database and their SQLite sidecars, so those
    # mutable live artifacts are proved by normalization provenance below.
    schema = value["database"]
    schema_raw, schema_status = secure_read_bytes(
        schema["schema_path"],
        "source adoption package schema",
        maximum=1 << 20,
        required_uid=root_uid,
    )
    if schema_status.st_gid != 0:
        raise ControllerError(
            f"source adoption package schema group is {schema_status.st_gid}, want 0"
        )
    schema_digest = hashlib.sha256(schema_raw).hexdigest()
    if schema_digest != schema["schema_sha256"]:
        raise ControllerError("source adoption package schema hash changed")
    artifact_evidence = {
        "adoption_checkpoint": {
            "main_config": value["main_config"],
            "managed_config": value["managed_config"],
            "database": value["database"],
        },
        "schema": {
        "path": schema["schema_path"],
        "sha256": schema_digest,
        "device": schema_status.st_dev,
        "inode": schema_status.st_ino,
        "mode": f"{stat.S_IMODE(schema_status.st_mode):04o}",
        "uid": schema_status.st_uid,
        "gid": schema_status.st_gid,
        },
    }
    require_absent_path(
        value["cluster_config"]["path"], "source adoption cluster config"
    )
    return {
        "path": SOURCE_ADOPTION_PROOF_PATH,
        "sha256": observed_hash,
        "device": status.st_dev,
        "inode": status.st_ino,
        "mode": f"{stat.S_IMODE(status.st_mode):04o}",
        "schema": value["schema"],
        "cell_id": value["cell_id"],
        "scope": value["scope"],
        "construction_origin": value["construction_origin"],
        "production_adoption_driver": value["production_adoption_driver"],
        "source_packages": value["source_packages"],
        "measured_target_packages": value["measured_target_packages"],
        "artifacts": artifact_evidence,
        "source_unit_after_adoption": value["source_unit_after_adoption"],
        "production_receipts": value["production_receipts"],
        "external_artifacts_unchanged_by_adoption": value[
            "external_artifacts_unchanged_by_adoption"
        ],
    }


def _external_pdns_zone_snapshot_sha256(
    scenario: Mapping[str, Any],
) -> str:
    zones = scenario.get("zones")
    if not isinstance(zones, list) or not zones:
        raise ControllerError("external PowerDNS preimage scenario has no zones")
    encoded = json.dumps(
        zones, separators=(",", ":"), sort_keys=True
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def _external_pdns_receipt_paths(state_dir: str) -> dict[str, dict[str, str]]:
    require_clean_absolute(state_dir, "external PowerDNS preimage state directory")
    return {
        "dns_engine_state": {
            "path": os.path.join(state_dir, "dns-engine-state.json"),
            "status": "absent",
        },
        "dns_engine_switch_journal": {
            "path": os.path.join(state_dir, "dns-engine-switch-journal.json"),
            "status": "absent",
        },
        "bind_engine_ownership": {
            "path": os.path.join(state_dir, "dns-engine-ownership-bind.json"),
            "status": "absent",
        },
        "bind_install_ownership": {
            "path": os.path.join(
                state_dir, "dns-engine-install-ownership-bind.json"
            ),
            "status": "absent",
        },
        "pdns_engine_ownership": {
            "path": os.path.join(state_dir, "dns-engine-ownership-pdns.json"),
            "status": "absent",
        },
        "pdns_install_ownership": {
            "path": os.path.join(
                state_dir, "dns-engine-install-ownership-pdns.json"
            ),
            "status": "absent",
        },
    }


def validate_external_pdns_preimage_document(
    value: Any,
    raw: bytes,
    cell: CellSpec,
    scenario: Mapping[str, Any],
    scenario_sha256: str,
    preinstall: Mapping[str, Any],
    state_dir: str,
    dns_address: str,
    dns_port: int,
    dns_name: str,
    dns_type: str,
) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != EXTERNAL_PDNS_PREIMAGE_KEYS:
        raise ControllerError(
            "external PowerDNS preimage fields differ from the exact contract"
        )
    canonical = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode(
        "utf-8"
    )
    if raw != canonical:
        raise ControllerError(
            "external PowerDNS preimage is not canonical sorted JSON"
        )
    if cell.driver != "pdns-adopt" or cell.role != "standalone":
        raise ControllerError(
            "external PowerDNS preimage escaped standalone adoption scope"
        )
    expected = {
        "schema": EXTERNAL_PDNS_PREIMAGE_SCHEMA,
        "cell_id": cell.cell_id,
        "scope": "external-pdns-measured-adoption-preimage",
        "source_fixture": "external-pdns-adoption",
        "construction_origin": "harness-external-pdns",
        "production_adoption_driver": "pdns-adopt",
        "production_adoption_pending": True,
        "scenario_sha256": scenario_sha256,
        "source_preinstall_proof_path": SOURCE_PREINSTALL_PROOF_PATH,
        "source_preinstall_proof_sha256": preinstall.get("sha256"),
        "source_packages": preinstall.get("source_packages"),
        "cluster_config": {
            "path": "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
            "status": "absent",
        },
        "source_unit_before_tagged_agent": {
            "name": "pdns.service",
            "load_state": "loaded",
            "active_state": "active",
            "sub_state": "running",
            "unit_file_state": "enabled",
        },
        "authoritative_preflight": {
            "claimed": True,
            "address": dns_address,
            "port": dns_port,
            "name": dns_name.rstrip(".").lower(),
            "type": dns_type,
            "udp": True,
            "tcp": True,
        },
        "production_receipts_absent": _external_pdns_receipt_paths(state_dir),
    }
    if any(value.get(key) != wanted for key, wanted in expected.items()):
        raise ControllerError(
            "external PowerDNS preimage identity or safety claims differ"
        )
    for label, path, owners, mode in (
        (
            "main",
            "/etc/powerdns/pdns.conf",
            {"root:pdns", "root:root"},
            "0640",
        ),
        (
            "managed",
            "/etc/powerdns/pdns.d/celikpanel.conf",
            {"root:root"},
            "0644",
        ),
    ):
        artifact = value.get(f"{label}_config")
        if (
            not isinstance(artifact, dict)
            or set(artifact) != {"path", "sha256", "owner", "mode"}
            or artifact.get("path") != path
            or not valid_sha256(artifact.get("sha256"))
            or artifact.get("owner") not in owners
            or artifact.get("mode") != mode
        ):
            raise ControllerError(
                f"external PowerDNS preimage {label} config is invalid"
            )
    database = value.get("database")
    database_keys = {
        "path",
        "sha256",
        "owner",
        "mode",
        "schema_path",
        "schema_sha256",
        "quick_check",
        "journal_mode",
        "zone_snapshot_sha256",
        "domain_count",
        "record_count",
        "auxiliary_authority_count",
        "sidecars",
    }
    zones = scenario.get("zones")
    expected_domains = sum(
        1 for zone in zones if isinstance(zone, dict) and not zone.get("delete")
    )
    expected_records = sum(
        len(zone.get("records", []))
        for zone in zones
        if isinstance(zone, dict) and not zone.get("delete")
    )
    if (
        not isinstance(database, dict)
        or set(database) != database_keys
        or database.get("path") != "/var/lib/powerdns/pdns.sqlite3"
        or not valid_sha256(database.get("sha256"))
        or database.get("owner") != "pdns:pdns"
        or database.get("mode") != "0640"
        or database.get("schema_path")
        != "/usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql"
        or not valid_sha256(database.get("schema_sha256"))
        or database.get("quick_check") != "ok"
        or database.get("journal_mode") != "wal"
        or database.get("zone_snapshot_sha256")
        != _external_pdns_zone_snapshot_sha256(scenario)
        or database.get("domain_count") != expected_domains
        or database.get("record_count") != expected_records
        or database.get("auxiliary_authority_count") != 0
    ):
        raise ControllerError(
            "external PowerDNS preimage database evidence is invalid"
        )
    sidecars = database.get("sidecars")
    if not isinstance(sidecars, dict) or set(sidecars) != {
        "rollback_journal",
        "write_ahead_log",
        "shared_memory",
    }:
        raise ControllerError(
            "external PowerDNS preimage SQLite sidecar fields differ"
        )
    if sidecars.get("rollback_journal") != {
        "path": "/var/lib/powerdns/pdns.sqlite3-journal",
        "status": "absent",
    }:
        raise ControllerError(
            "external PowerDNS preimage rollback-journal evidence is invalid"
        )
    metadata_keys = {
        "path",
        "file_type",
        "owner",
        "mode",
        "link_count",
        "device",
        "inode",
        "size",
        "content_policy",
    }
    wal = sidecars.get("write_ahead_log")
    shm = sidecars.get("shared_memory")
    if (
        not isinstance(wal, dict)
        or set(wal) != metadata_keys
        or wal.get("path") != "/var/lib/powerdns/pdns.sqlite3-wal"
        or wal.get("file_type") != "regular"
        or wal.get("owner") != "pdns:pdns"
        or wal.get("mode") != "0640"
        or not isinstance(wal.get("link_count"), int)
        or isinstance(wal.get("link_count"), bool)
        or wal.get("link_count") != 1
        or not isinstance(wal.get("device"), int)
        or isinstance(wal.get("device"), bool)
        or wal.get("device") <= 0
        or not isinstance(wal.get("inode"), int)
        or isinstance(wal.get("inode"), bool)
        or wal.get("inode") <= 0
        or not isinstance(wal.get("size"), int)
        or isinstance(wal.get("size"), bool)
        or wal.get("size") != 0
        or wal.get("content_policy") != "empty"
    ):
        raise ControllerError(
            "external PowerDNS preimage write-ahead-log evidence is invalid"
        )
    if (
        not isinstance(shm, dict)
        or set(shm) != metadata_keys
        or shm.get("path") != "/var/lib/powerdns/pdns.sqlite3-shm"
        or shm.get("file_type") != "regular"
        or shm.get("owner") != "pdns:pdns"
        or shm.get("mode") != "0640"
        or not isinstance(shm.get("link_count"), int)
        or isinstance(shm.get("link_count"), bool)
        or shm.get("link_count") != 1
        or not isinstance(shm.get("device"), int)
        or isinstance(shm.get("device"), bool)
        or shm.get("device") <= 0
        or not isinstance(shm.get("inode"), int)
        or isinstance(shm.get("inode"), bool)
        or shm.get("inode") <= 0
        or not isinstance(shm.get("size"), int)
        or isinstance(shm.get("size"), bool)
        or shm.get("size") != 32768
        or shm.get("content_policy") != "volatile-unhashed"
    ):
        raise ControllerError(
            "external PowerDNS preimage shared-memory evidence is invalid"
        )
    if wal["device"] != shm["device"] or wal["inode"] == shm["inode"]:
        raise ControllerError(
            "external PowerDNS preimage sidecar identities conflict"
        )
    return value


def validate_external_pdns_preimage_provenance(
    proof: Mapping[str, Any],
    source_fixture: str,
    cell: CellSpec,
    scenario: Mapping[str, Any],
    scenario_sha256: str,
    preinstall: Mapping[str, Any],
    state_dir: str,
    dns_address: str,
    dns_port: int,
    dns_name: str,
    dns_type: str,
) -> dict[str, Any]:
    claimed_path = proof.get("external_pdns_preimage_path")
    claimed_hash = proof.get("external_pdns_preimage_sha256")
    if source_fixture != "external-pdns-adoption":
        if claimed_path != "absent" or claimed_hash != "absent":
            raise ControllerError(
                f"{source_fixture} source proof unexpectedly claims an external PowerDNS preimage"
            )
        require_absent_path(
            EXTERNAL_PDNS_PREIMAGE_PATH,
            f"{source_fixture} external PowerDNS preimage",
        )
        return {"path": "absent", "sha256": "absent", "exists": False}
    if (
        claimed_path != EXTERNAL_PDNS_PREIMAGE_PATH
        or not valid_sha256(claimed_hash)
    ):
        raise ControllerError(
            "external PowerDNS source lacks its exact preimage path or hash"
        )
    value, raw, observed_hash, status = secure_json_with_digest(
        EXTERNAL_PDNS_PREIMAGE_PATH,
        "external PowerDNS adoption preimage",
        maximum=1 << 20,
    )
    if observed_hash != claimed_hash:
        raise ControllerError(
            "external PowerDNS preimage hash differs from source proof"
        )
    validate_external_pdns_preimage_document(
        value,
        raw,
        cell,
        scenario,
        scenario_sha256,
        preinstall,
        state_dir,
        dns_address,
        dns_port,
        dns_name,
        dns_type,
    )

    root_uid = os.geteuid()
    if root_uid != 0:
        raise ControllerError(
            "external PowerDNS preimage requires root controller identity"
        )
    pdns_uid, pdns_gid = resolve_exact_pdns_owner_identity()

    def validate_live_artifact(
        artifact: Mapping[str, Any],
        label: str,
        maximum: int,
        required_mode: int,
        required_uid: int,
        allowed_gids: set[int],
    ) -> tuple[bytes, os.stat_result, dict[str, Any]]:
        artifact_raw, artifact_status = secure_read_bytes(
            artifact["path"],
            label,
            maximum=maximum,
            required_mode=required_mode,
            required_uid=required_uid,
        )
        digest = hashlib.sha256(artifact_raw).hexdigest()
        if artifact_status.st_gid not in allowed_gids:
            raise ControllerError(f"{label} group differs from the preimage contract")
        owner = (
            "root:pdns"
            if required_uid == root_uid and artifact_status.st_gid == pdns_gid
            else "root:root"
            if required_uid == root_uid and artifact_status.st_gid == 0
            else "pdns:pdns"
            if required_uid == pdns_uid and artifact_status.st_gid == pdns_gid
            else ""
        )
        if (
            digest != artifact.get("sha256")
            or artifact.get("owner") != owner
            or artifact.get("mode") != f"{required_mode:04o}"
        ):
            raise ControllerError(f"{label} differs from the sealed preimage")
        return artifact_raw, artifact_status, {
            "path": artifact["path"],
            "sha256": digest,
            "device": artifact_status.st_dev,
            "inode": artifact_status.st_ino,
            "mode": f"{stat.S_IMODE(artifact_status.st_mode):04o}",
            "uid": artifact_status.st_uid,
            "gid": artifact_status.st_gid,
        }

    main_raw, _, main_evidence = validate_live_artifact(
        value["main_config"],
        "external PowerDNS main configuration",
        1 << 20,
        0o640,
        root_uid,
        {0, pdns_gid},
    )
    managed_raw, _, managed_evidence = validate_live_artifact(
        value["managed_config"],
        "external PowerDNS managed configuration",
        1 << 20,
        0o644,
        root_uid,
        {0},
    )
    active_main = [
        line.strip()
        for line in main_raw.decode("utf-8").splitlines()
        if line.strip() and not line.lstrip().startswith("#")
    ]
    if active_main.count("include-dir=/etc/powerdns/pdns.d") != 1:
        raise ControllerError(
            "external PowerDNS main configuration lacks its exact include"
        )
    managed_lines = managed_raw.decode("utf-8").splitlines()
    fixed_managed = {
        "# Managed by CelikPanel; do not edit by hand.",
        "launch=gsqlite3",
        "gsqlite3-dnssec=yes",
        "gsqlite3-database=/var/lib/powerdns/pdns.sqlite3",
        f"local-address={dns_address}",
        "zone-cache-refresh-interval=0",
        "webserver=no",
        "api=no",
    }
    if set(managed_lines) != fixed_managed:
        raise ControllerError(
            "external PowerDNS managed configuration differs from the exact fixture"
        )
    require_absent_path(
        value["cluster_config"]["path"],
        "external PowerDNS cluster configuration",
    )
    database = value["database"]
    schema_raw, _, schema_evidence = validate_live_artifact(
        {
            "path": database["schema_path"],
            "sha256": database["schema_sha256"],
            "owner": "root:root",
            "mode": "0644",
        },
        "external PowerDNS package schema",
        1 << 20,
        0o644,
        root_uid,
        {0},
    )
    if not schema_raw:
        raise ControllerError("external PowerDNS package schema is empty")
    database_raw, database_status, database_evidence = validate_live_artifact(
        database,
        "external PowerDNS database",
        65 << 20,
        0o640,
        pdns_uid,
        {pdns_gid},
    )
    sidecars = database["sidecars"]
    require_absent_path(
        sidecars["rollback_journal"]["path"],
        "external PowerDNS rollback journal",
    )

    def validate_live_sidecar(
        evidence: Mapping[str, Any],
        label: str,
        required_size: int,
        require_empty: bool,
    ) -> dict[str, Any]:
        sidecar_status = secure_regular_metadata(
            evidence["path"],
            label,
            required_mode=0o640,
            required_uid=pdns_uid,
            required_gid=pdns_gid,
            required_size=required_size,
            require_empty=require_empty,
        )
        observed = {
            "path": evidence["path"],
            "file_type": "regular",
            "owner": "pdns:pdns",
            "mode": "0640",
            "link_count": sidecar_status.st_nlink,
            "device": sidecar_status.st_dev,
            "inode": sidecar_status.st_ino,
            "size": sidecar_status.st_size,
            "content_policy": (
                "empty" if require_empty else "volatile-unhashed"
            ),
        }
        if observed != evidence:
            raise ControllerError(f"{label} differs from the sealed preimage")
        return observed

    wal_evidence = validate_live_sidecar(
        sidecars["write_ahead_log"],
        "external PowerDNS write-ahead log",
        0,
        True,
    )
    shm_evidence = validate_live_sidecar(
        sidecars["shared_memory"],
        "external PowerDNS shared memory",
        32768,
        False,
    )
    if (
        wal_evidence["device"] != database_status.st_dev
        or shm_evidence["device"] != database_status.st_dev
        or len(
            {
                database_status.st_ino,
                wal_evidence["inode"],
                shm_evidence["inode"],
            }
        )
        != 3
    ):
        raise ControllerError(
            "external PowerDNS database and sidecar identities conflict"
        )
    for receipt in value["production_receipts_absent"].values():
        require_absent_path(
            receipt["path"], "external PowerDNS production receipt"
        )

    expected_domains: list[tuple[Any, ...]] = []
    expected_records: list[tuple[Any, ...]] = []
    for zone in scenario["zones"]:
        if zone.get("delete"):
            continue
        expected_domains.append((zone["domain"], zone["zone_type"]))
        for record in zone["records"]:
            expected_records.append(
                (
                    zone["domain"],
                    record["name"],
                    record["type"],
                    record["content"],
                    record["ttl"],
                    record["prio"],
                    int(record["disabled"]),
                )
            )
    expected_domains.sort()
    expected_records.sort()
    try:
        connection = sqlite3.connect(
            "file:/var/lib/powerdns/pdns.sqlite3?mode=ro",
            uri=True,
            timeout=5.0,
        )
        try:
            connection.execute("PRAGMA query_only=ON")
            query_only = connection.execute("PRAGMA query_only").fetchone()
            quick_check = connection.execute("PRAGMA quick_check").fetchall()
            journal_mode = connection.execute("PRAGMA journal_mode").fetchone()
            domains = connection.execute(
                "SELECT name, type FROM domains ORDER BY name, type"
            ).fetchall()
            records = connection.execute(
                "SELECT d.name, r.name, r.type, r.content, r.ttl, r.prio, r.disabled "
                "FROM records AS r JOIN domains AS d ON d.id = r.domain_id "
                "ORDER BY d.name, r.name, r.type, r.content, r.ttl, r.prio, r.disabled"
            ).fetchall()
            auxiliary_count = connection.execute(
                "SELECT "
                "(SELECT COUNT(*) FROM supermasters) + "
                "(SELECT COUNT(*) FROM comments) + "
                "(SELECT COUNT(*) FROM domainmetadata) + "
                "(SELECT COUNT(*) FROM cryptokeys) + "
                "(SELECT COUNT(*) FROM tsigkeys) + "
                "(SELECT COUNT(*) FROM records WHERE domain_id IS NULL OR "
                "domain_id NOT IN (SELECT id FROM domains))"
            ).fetchone()[0]
        finally:
            connection.close()
    except (OSError, sqlite3.Error, IndexError, TypeError) as exc:
        raise ControllerError(
            f"inspect external PowerDNS preimage database: {exc}"
        ) from exc
    if (
        query_only != (1,)
        or quick_check != [("ok",)]
        or journal_mode != ("wal",)
        or domains != expected_domains
        or records != expected_records
        or auxiliary_count != 0
    ):
        raise ControllerError(
            "external PowerDNS database differs from the exact adoption preimage"
        )
    require_absent_path(
        sidecars["rollback_journal"]["path"],
        "external PowerDNS rollback journal after query",
    )
    database_after, database_after_status = secure_read_bytes(
        database["path"],
        "external PowerDNS database after query",
        maximum=65 << 20,
        required_mode=0o640,
        required_uid=pdns_uid,
    )
    if (
        database_after != database_raw
        or (
            database_after_status.st_dev,
            database_after_status.st_ino,
            database_after_status.st_mode,
            database_after_status.st_uid,
            database_after_status.st_gid,
        )
        != (
            database_status.st_dev,
            database_status.st_ino,
            database_status.st_mode,
            database_status.st_uid,
            database_status.st_gid,
        )
    ):
        raise ControllerError(
            "external PowerDNS database changed while proving the preimage"
        )
    if validate_live_sidecar(
        sidecars["write_ahead_log"],
        "external PowerDNS write-ahead log after query",
        0,
        True,
    ) != wal_evidence or validate_live_sidecar(
        sidecars["shared_memory"],
        "external PowerDNS shared memory after query",
        32768,
        False,
    ) != shm_evidence:
        raise ControllerError(
            "external PowerDNS sidecar identity changed while proving the preimage"
        )
    return {
        "path": EXTERNAL_PDNS_PREIMAGE_PATH,
        "sha256": observed_hash,
        "device": status.st_dev,
        "inode": status.st_ino,
        "mode": f"{stat.S_IMODE(status.st_mode):04o}",
        "schema": value["schema"],
        "cell_id": value["cell_id"],
        "scope": value["scope"],
        "source_preinstall_proof_sha256": value[
            "source_preinstall_proof_sha256"
        ],
        "configuration": {
            "main": main_evidence,
            "managed": managed_evidence,
        },
        "schema_file": schema_evidence,
        "database": {
            **database_evidence,
            "quick_check": "ok",
            "journal_mode": "wal",
            "domain_count": len(domains),
            "record_count": len(records),
            "auxiliary_authority_count": auxiliary_count,
            "sidecars": {
                "rollback_journal": sidecars["rollback_journal"],
                "write_ahead_log": wal_evidence,
                "shared_memory": shm_evidence,
            },
        },
        "source_unit_before_tagged_agent": value[
            "source_unit_before_tagged_agent"
        ],
        "authoritative_preflight": value["authoritative_preflight"],
        "production_receipts_absent": value["production_receipts_absent"],
    }


def _normalization_request_id(base_request_id: str, purpose: str) -> str:
    return hashlib.sha256(
        (
            "celikpanel/dns-kill-matrix-pdns-normalization-request/v1"
            + "\x00"
            + base_request_id
            + "\x00"
            + purpose
        ).encode("utf-8")
    ).digest()[:16].hex()


def _canonical_pdns_v3_qualifier(
    engine_epoch: int, zone: Mapping[str, Any]
) -> str:
    def frame(value: str) -> bytes:
        encoded = value.encode("utf-8")
        return struct.pack(">I", len(encoded)) + encoded

    domain_value = zone.get("domain")
    zone_type_value = zone.get("zone_type")
    generation = zone.get("desired_generation")
    delete = zone.get("delete")
    records_value = zone.get("records")
    if (
        not isinstance(domain_value, str)
        or not isinstance(zone_type_value, str)
        or not isinstance(generation, int)
        or isinstance(generation, bool)
        or generation < 0
        or type(delete) is not bool
        or not isinstance(records_value, list)
    ):
        raise ControllerError("normalization scenario zone cannot be canonicalized")
    domain = domain_value.strip().lower().removesuffix(".")
    zone_type = zone_type_value.strip().upper()
    records: list[tuple[str, str, str, int, int, bool]] = []
    for record in records_value:
        if not isinstance(record, dict):
            raise ControllerError("normalization scenario record is not an object")
        name_value = record.get("name")
        type_value = record.get("type")
        content = record.get("content")
        ttl = record.get("ttl")
        priority = record.get("prio")
        disabled = record.get("disabled")
        if (
            not isinstance(name_value, str)
            or not isinstance(type_value, str)
            or not isinstance(content, str)
            or not isinstance(ttl, int)
            or isinstance(ttl, bool)
            or not isinstance(priority, int)
            or isinstance(priority, bool)
            or type(disabled) is not bool
            or ttl < 0
            or ttl > (1 << 31) - 1
            or priority < 0
            or priority > (1 << 16) - 1
        ):
            raise ControllerError("normalization scenario record is invalid")
        name = name_value.strip().lower().removesuffix(".")
        record_type = type_value.strip().upper()
        records.append((name, record_type, content, ttl, priority, disabled))
    if delete:
        if records:
            raise ControllerError("normalization deletion contains hidden records")
        records = []
    elif not records:
        raise ControllerError("normalization source zone has an empty snapshot")
    records.sort()
    digest = hashlib.sha256()
    for value in (
        "celikpanel/service-mutation-payload",
        "dns-zone-sync/v3",
        "dns_zone_sync",
        "pdns",
        "Agent.SyncDNSZoneV3",
    ):
        digest.update(frame(value))
    digest.update(struct.pack(">Q", engine_epoch))
    digest.update(struct.pack(">Q", generation))
    digest.update(frame(domain))
    digest.update(frame("delete" if delete else "sync"))
    digest.update(frame(zone_type))
    digest.update(struct.pack(">I", len(records)))
    for name, record_type, content, ttl, priority, disabled in records:
        digest.update(frame(name))
        digest.update(frame(record_type))
        digest.update(frame(content))
        digest.update(struct.pack(">I", ttl))
        digest.update(struct.pack(">H", priority))
        digest.update(b"\x01" if disabled else b"\x00")
    return "dns-zone-sync/v3:sha256:" + digest.hexdigest()


def validate_source_normalization_provenance(
    proof: Mapping[str, Any],
    source_fixture: str,
    cell: CellSpec,
    scenario: Mapping[str, Any],
    state_dir: str,
    dns_address: str,
) -> dict[str, Any]:
    claimed_path = proof.get("source_normalization_identity_receipt_path")
    claimed_hash = proof.get("source_normalization_identity_receipt_sha256")
    if source_fixture in {"uninitialized", "external-pdns-adoption"}:
        if claimed_path != "absent" or claimed_hash != "absent":
            raise ControllerError(
                f"{source_fixture} source proof has invalid absent normalization provenance"
            )
        require_absent_path(
            SOURCE_NORMALIZATION_IDENTITY_PATH,
            f"{source_fixture} source normalization identity receipt",
        )
        return {"path": "absent", "sha256": "absent", "exists": False}
    if source_fixture != "managed-pdns":
        raise ControllerError(
            f"source fixture {source_fixture!r} has no normalization contract"
        )
    if (
        claimed_path != SOURCE_NORMALIZATION_IDENTITY_PATH
        or not valid_sha256(claimed_hash)
    ):
        raise ControllerError(
            "managed PowerDNS source lacks its exact normalization receipt path or hash"
        )
    value, raw, observed_hash, status = secure_json_with_digest(
        SOURCE_NORMALIZATION_IDENTITY_PATH,
        "managed PowerDNS source normalization identity receipt",
        maximum=1 << 20,
    )
    if observed_hash != claimed_hash:
        raise ControllerError(
            "source normalization identity receipt hash differs from source proof"
        )
    envelope_keys = {
        "schema",
        "cell_id",
        "driver",
        "source_fixture",
        "base_request_id",
        "source_engine",
        "source_epoch",
        "configure",
        "zone_syncs",
    }
    mutation_keys = {
        "method",
        "request_id",
        "owner_id",
        "kind",
        "target",
        "package_name",
        "terminal_phase",
    }
    zone_keys = mutation_keys | {
        "engine",
        "engine_epoch",
        "desired_generation",
        "domain",
        "delete",
        "zone_type",
        "qualifier",
    }
    if not isinstance(value, dict) or set(value) != envelope_keys:
        raise ControllerError("source normalization identity fields differ")
    base_request_id = hashlib.sha256(
        (cell.cell_id + "\x00source-pdns-normalize").encode("utf-8")
    ).digest()[:16].hex()
    expected_envelope = {
        "schema": SOURCE_NORMALIZATION_IDENTITY_SCHEMA,
        "cell_id": cell.cell_id,
        "driver": "bind",
        "source_fixture": "managed-pdns",
        "base_request_id": base_request_id,
        "source_engine": "pdns",
        "source_epoch": scenario.get("source_epoch"),
    }
    if any(value.get(key) != expected for key, expected in expected_envelope.items()):
        raise ControllerError("source normalization identity envelope differs")
    configure = value.get("configure")
    if not isinstance(configure, dict) or set(configure) != mutation_keys:
        raise ControllerError("source normalization configure fields differ")
    configure_request_id = _normalization_request_id(base_request_id, "configure")
    expected_configure = {
        "method": "Agent.ConfigurePowerDNSSQLite",
        "request_id": configure_request_id,
        "owner_id": deterministic_trigger_owner(cell.cell_id, configure_request_id),
        "kind": "pdns_configure",
        "target": "pdns",
        "package_name": "",
        "terminal_phase": "completed",
    }
    if configure != expected_configure:
        raise ControllerError("source normalization configure identity differs")
    scenario_zones = scenario.get("zones")
    zone_syncs = value.get("zone_syncs")
    if (
        not isinstance(scenario_zones, list)
        or not scenario_zones
        or not isinstance(zone_syncs, list)
        or len(zone_syncs) != len(scenario_zones)
    ):
        raise ControllerError("source normalization zone set differs")
    expected_zone_syncs: list[dict[str, Any]] = []
    expected_rows: list[tuple[Any, ...]] = []
    for index, (zone, operation) in enumerate(zip(scenario_zones, zone_syncs)):
        if not isinstance(zone, dict) or not isinstance(operation, dict) or set(operation) != zone_keys:
            raise ControllerError("source normalization zone identity fields differ")
        domain = zone.get("domain")
        qualifier = _canonical_pdns_v3_qualifier(scenario["source_epoch"], zone)
        claimed_zone_qualifier = zone.get("zone_qualifier")
        if (
            not isinstance(domain, str)
            or claimed_zone_qualifier not in {"", qualifier}
        ):
            raise ControllerError("source normalization scenario zone identity is invalid")
        request_id = _normalization_request_id(
            base_request_id, f"zone-sync/{index}/{domain}"
        )
        terminal_phase = (
            "commit/dns-zone-sync/v3/published/"
            + request_id
            + "/"
            + domain
            + "/"
            + qualifier
        )
        expected_operation = {
            "method": "Agent.SyncDNSZoneV3",
            "request_id": request_id,
            "owner_id": deterministic_trigger_owner(cell.cell_id, request_id),
            "kind": "dns_zone_sync",
            "target": domain,
            "package_name": qualifier,
            "terminal_phase": terminal_phase,
            "engine": "pdns",
            "engine_epoch": scenario["source_epoch"],
            "desired_generation": zone.get("desired_generation"),
            "domain": domain,
            "delete": zone.get("delete"),
            "zone_type": zone.get("zone_type"),
            "qualifier": qualifier,
        }
        if operation != expected_operation:
            raise ControllerError("source normalization zone identity differs")
        expected_zone_syncs.append(expected_operation)
        expected_rows.append(
            (
                domain,
                "pdns",
                scenario["source_epoch"],
                request_id,
                expected_operation["owner_id"],
                qualifier,
                zone.get("desired_generation"),
                "delete" if zone.get("delete") else "sync",
                zone.get("zone_type"),
                "dns-zone-sync/v3",
            )
        )
    canonical_value = {
        "schema": value["schema"],
        "cell_id": value["cell_id"],
        "driver": value["driver"],
        "source_fixture": value["source_fixture"],
        "base_request_id": value["base_request_id"],
        "source_engine": value["source_engine"],
        "source_epoch": value["source_epoch"],
        "configure": expected_configure,
        "zone_syncs": expected_zone_syncs,
    }
    canonical = (json.dumps(canonical_value, separators=(",", ":")) + "\n").encode(
        "utf-8"
    )
    if raw != canonical:
        raise ControllerError(
            "source normalization identity receipt is not canonical Go JSON"
        )

    ledger_path = os.path.join(state_dir, "service-mutations.json")
    ledger, ledger_status = secure_read_json(
        ledger_path,
        "source normalization service-mutation ledger",
        maximum=1 << 20,
        required_mode=0o600,
        required_uid=0,
    )
    if (
        not isinstance(ledger, dict)
        or ledger.get("version") != 1
        or ledger.get("active_request_id", "") != ""
        or not isinstance(ledger.get("jobs"), dict)
    ):
        raise ControllerError("source normalization mutation ledger is invalid")
    jobs = ledger["jobs"]
    for operation in [expected_configure, *expected_zone_syncs]:
        job = jobs.get(operation["request_id"])
        if not isinstance(job, dict):
            raise ControllerError("source normalization terminal job is absent")
        expected_job = {
            "request_id": operation["request_id"],
            "owner_id": operation["owner_id"],
            "kind": operation["kind"],
            "target": operation["target"],
            "package_name": operation["package_name"],
            "status": "succeeded",
            "phase": operation["terminal_phase"],
        }
        if any(job.get(key, "") != expected for key, expected in expected_job.items()):
            raise ControllerError("source normalization terminal job identity differs")
        if (
            not isinstance(job.get("attempt"), int)
            or isinstance(job.get("attempt"), bool)
            or job["attempt"] <= 0
            or job.get("worker_pid", 0) != 0
            or job.get("worker_started", "") != ""
            or job.get("worker_command", "") != ""
            or not job.get("finished_at")
            or job.get("error_code", "") != ""
            or job.get("error_message", "") != ""
        ):
            raise ControllerError("source normalization terminal job envelope differs")

    root_uid = os.geteuid()
    if root_uid != 0:
        raise ControllerError("source normalization proof requires root controller identity")
    pdns_uid, pdns_gid = resolve_exact_pdns_owner_identity()
    config_evidence: dict[str, Any] = {}
    for label, path, mode, uid, gids in (
        ("main", "/etc/powerdns/pdns.conf", 0o640, root_uid, {0, pdns_gid}),
        ("managed", "/etc/powerdns/pdns.d/celikpanel.conf", 0o644, root_uid, {0}),
    ):
        config_raw, config_status = secure_read_bytes(
            path,
            f"normalized PowerDNS {label} configuration",
            maximum=1 << 20,
            required_mode=mode,
            required_uid=uid,
        )
        if config_status.st_gid not in gids:
            raise ControllerError(
                f"normalized PowerDNS {label} configuration group differs"
            )
        config_evidence[label] = {
            "path": path,
            "sha256": hashlib.sha256(config_raw).hexdigest(),
            "device": config_status.st_dev,
            "inode": config_status.st_ino,
            "mode": f"{stat.S_IMODE(config_status.st_mode):04o}",
            "uid": config_status.st_uid,
            "gid": config_status.st_gid,
        }
        if label == "main":
            active_lines = [
                line.strip()
                for line in config_raw.decode("utf-8").splitlines()
                if line.strip() and not line.lstrip().startswith("#")
            ]
            if active_lines.count("include-dir=/etc/powerdns/pdns.d") != 1:
                raise ControllerError(
                    "normalized PowerDNS main configuration lacks its exact include"
                )
        else:
            lines = config_raw.decode("utf-8").splitlines()
            fixed = {
                "# Managed by CelikPanel; do not edit by hand.",
                "launch=gsqlite3",
                "gsqlite3-dnssec=yes",
                "gsqlite3-database=/var/lib/powerdns/pdns.sqlite3",
                "zone-cache-refresh-interval=0",
                "webserver=no",
                "api=no",
            }
            if not fixed.issubset(set(lines)):
                raise ControllerError(
                    "normalized PowerDNS managed configuration differs"
                )
            address_lines = [line for line in lines if line.startswith("local-address=")]
            if len(address_lines) != 1:
                raise ControllerError(
                    "normalized PowerDNS managed configuration has ambiguous listeners"
                )
            addresses = address_lines[0].removeprefix("local-address=").split(",")
            if dns_address not in addresses or len(set(addresses)) != len(addresses):
                raise ControllerError(
                    "normalized PowerDNS managed listeners differ from the source"
                )
            try:
                if any(
                    str(ipaddress.ip_address(address)) != address
                    or ipaddress.ip_address(address).is_unspecified
                    or ipaddress.ip_address(address).is_loopback
                    or ipaddress.ip_address(address).is_link_local
                    or ipaddress.ip_address(address).is_multicast
                    for address in addresses
                ):
                    raise ControllerError(
                        "normalized PowerDNS managed listener is not canonical global unicast"
                    )
            except ValueError as exc:
                raise ControllerError(
                    "normalized PowerDNS managed listener is invalid"
                ) from exc
    require_absent_path(
        "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
        "normalized PowerDNS cluster configuration",
    )

    database_path = "/var/lib/powerdns/pdns.sqlite3"
    try:
        before = os.lstat(database_path)
    except OSError as exc:
        raise ControllerError(f"inspect normalized PowerDNS database: {exc}") from exc
    if (
        stat.S_ISLNK(before.st_mode)
        or not stat.S_ISREG(before.st_mode)
        or stat.S_IMODE(before.st_mode) != 0o640
        or before.st_uid != pdns_uid
        or before.st_gid != pdns_gid
        or before.st_nlink != 1
        or before.st_size <= 0
    ):
        raise ControllerError("normalized PowerDNS database envelope differs")
    try:
        connection = sqlite3.connect(
            f"file:{database_path}?mode=ro", uri=True, timeout=5.0
        )
        try:
            quick_check = connection.execute("PRAGMA quick_check").fetchall()
            legacy_count = connection.execute(
                "SELECT COUNT(*) FROM celikpanel_dns_zone_sync_receipts"
            ).fetchone()[0]
            manifest_count = connection.execute(
                "SELECT COUNT(*) FROM celikpanel_dns_engine_manifest_receipt"
            ).fetchone()[0]
            rows = connection.execute(
                "SELECT domain, engine, engine_epoch, request_id, owner_id, qualifier, "
                "desired_generation, action, zone_type, schema "
                "FROM celikpanel_dns_zone_sync_v3_receipts ORDER BY domain"
            ).fetchall()
            domain_count = connection.execute("SELECT COUNT(*) FROM domains").fetchone()[0]
            auxiliary_count = connection.execute(
                "SELECT "
                "(SELECT COUNT(*) FROM supermasters) + "
                "(SELECT COUNT(*) FROM comments) + "
                "(SELECT COUNT(*) FROM domainmetadata) + "
                "(SELECT COUNT(*) FROM cryptokeys) + "
                "(SELECT COUNT(*) FROM tsigkeys) + "
                "(SELECT COUNT(*) FROM records WHERE domain_id IS NULL OR "
                "domain_id NOT IN (SELECT id FROM domains))"
            ).fetchone()[0]
        finally:
            connection.close()
    except (OSError, sqlite3.Error, IndexError, TypeError) as exc:
        raise ControllerError(
            f"inspect normalized PowerDNS private schema: {exc}"
        ) from exc
    try:
        after = os.lstat(database_path)
    except OSError as exc:
        raise ControllerError(f"reinspect normalized PowerDNS database: {exc}") from exc
    if (before.st_dev, before.st_ino, before.st_mode, before.st_uid, before.st_gid) != (
        after.st_dev,
        after.st_ino,
        after.st_mode,
        after.st_uid,
        after.st_gid,
    ):
        raise ControllerError("normalized PowerDNS database changed identity while reading")
    expected_domain_count = sum(1 for zone in scenario_zones if not zone.get("delete"))
    if (
        quick_check != [("ok",)]
        or legacy_count != 0
        or manifest_count != 0
        or rows != sorted(expected_rows)
        or domain_count != expected_domain_count
        or auxiliary_count != 0
    ):
        raise ControllerError(
            "normalized PowerDNS database does not contain only the exact source snapshot"
        )
    return {
        "path": SOURCE_NORMALIZATION_IDENTITY_PATH,
        "sha256": observed_hash,
        "device": status.st_dev,
        "inode": status.st_ino,
        "mode": f"{stat.S_IMODE(status.st_mode):04o}",
        "schema": value["schema"],
        "base_request_id": base_request_id,
        "configure": expected_configure,
        "zone_syncs": expected_zone_syncs,
        "mutation_ledger": {
            "path": ledger_path,
            "device": ledger_status.st_dev,
            "inode": ledger_status.st_ino,
        },
        "configuration": config_evidence,
        "database": {
            "path": database_path,
            "device": after.st_dev,
            "inode": after.st_ino,
            "mode": f"{stat.S_IMODE(after.st_mode):04o}",
            "uid": after.st_uid,
            "gid": after.st_gid,
            "quick_check": "ok",
            "legacy_receipt_count": legacy_count,
            "v3_receipt_count": len(rows),
            "manifest_receipt_count": manifest_count,
            "domain_count": domain_count,
            "auxiliary_authority_count": auxiliary_count,
        },
    }


def canonical_dns_state_bytes(state: Mapping[str, Any]) -> bytes:
    ordered: dict[str, Any] = {
        key: state[key] for key in ("schema", "mode", "engine", "engine_epoch")
    }
    for key in (
        "generation",
        "pair_role",
        "pair_local_ip",
        "pair_peer_ip",
        "primary_catalog_serial",
    ):
        value = state.get(key)
        if value not in (None, "", 0):
            ordered[key] = value
    for key in (
        "source_revision",
        "manifest_qualifier",
        "mutation_request_id",
        "mutation_owner_id",
    ):
        ordered[key] = state[key]
    return (json.dumps(ordered, separators=(",", ":")) + "\n").encode("utf-8")


def validate_managed_source_state(
    state: Any,
    raw: bytes,
    scenario: Mapping[str, Any],
    expected_engine: str,
) -> dict[str, Any]:
    if not isinstance(state, dict):
        raise ControllerError("managed source state receipt is not an object")
    unknown = sorted(set(state) - DNS_STATE_KEYS)
    missing = sorted(DNS_STATE_REQUIRED_KEYS - set(state))
    if unknown or missing:
        raise ControllerError(
            f"managed source state fields differ (unknown={unknown}, missing={missing})"
        )
    if raw != canonical_dns_state_bytes(state):
        raise ControllerError("managed source state receipt is not canonical production JSON")
    expected = {
        "schema": "celikpanel-dns-engine-state/v1",
        "engine": expected_engine,
        "engine_epoch": scenario["source_epoch"],
        "source_revision": scenario["source_revision"],
    }
    if any(state.get(key) != value for key, value in expected.items()):
        raise ControllerError("managed source state receipt has the wrong identity")
    if state.get("mode") not in {"switch", "adopt"}:
        raise ControllerError("managed source state receipt has an invalid mode")
    qualifier = state.get("manifest_qualifier")
    prefix = "dns-engine-switch/v1:sha256:"
    if (
        not isinstance(qualifier, str)
        or not qualifier.startswith(prefix)
        or not valid_sha256(qualifier[len(prefix) :])
        or not valid_lower_hex(str(state.get("mutation_request_id", "")), 32, 32)
        or not valid_lower_hex(str(state.get("mutation_owner_id", "")), 32, 32)
    ):
        raise ControllerError("managed source state receipt has invalid mutation identity")
    generation = state.get("generation", "")
    if expected_engine == "bind":
        if not valid_sha256(generation):
            raise ControllerError("managed BIND source state lacks a canonical generation")
    elif generation:
        raise ControllerError("managed PowerDNS source state carries a BIND generation")
    role = state.get("pair_role", "")
    local_ip = state.get("pair_local_ip", "")
    peer_ip = state.get("pair_peer_ip", "")
    serial = state.get("primary_catalog_serial", 0)
    if isinstance(serial, bool) or not isinstance(serial, int) or serial < 0:
        raise ControllerError("managed source state catalog serial is invalid")
    if scenario.get("topology") == "standalone":
        if role or local_ip or peer_ip or serial != 0:
            raise ControllerError("managed standalone source retains paired identity")
    elif scenario.get("topology") == "paired":
        if (
            role != scenario.get("pair_role", "")
            or local_ip != scenario.get("local_ip", "")
            or peer_ip != scenario.get("peer_ip", "")
        ):
            raise ControllerError("managed source topology differs from the scenario")
        if role == "primary" and serial <= 0:
            raise ControllerError("managed paired-primary source lacks a catalog serial")
        if role == "secondary" and serial != 0:
            raise ControllerError("managed paired-secondary source carries a catalog serial")
        if role not in {"primary", "secondary"}:
            raise ControllerError("managed paired source role is invalid")
    else:
        raise ControllerError("managed source scenario topology is invalid")
    return state


def require_absent_path(path: str, label: str) -> None:
    require_clean_absolute(path, label)
    try:
        os.lstat(path)
    except FileNotFoundError:
        return
    except OSError as exc:
        raise ControllerError(f"inspect {label}: {exc}") from exc
    raise ControllerError(f"{label} must be absent before the tagged agent starts")


def secure_json_with_digest(
    path: str,
    label: str,
    *,
    maximum: int,
    required_mode: int = 0o600,
) -> tuple[Any, bytes, str, os.stat_result]:
    raw, status = secure_read_bytes(
        path,
        label,
        maximum=maximum,
        required_mode=required_mode,
        required_uid=os.geteuid(),
    )
    return decode_json(raw, label), raw, hashlib.sha256(raw).hexdigest(), status


def validate_source_scenario(
    path: str, cell: CellSpec
) -> tuple[dict[str, Any], dict[str, Any]]:
    value, _raw, digest, status = secure_json_with_digest(
        path, "source scenario", maximum=65 << 20
    )
    if not isinstance(value, dict):
        raise ControllerError("source scenario root is not an object")
    unknown = sorted(set(value) - SCENARIO_KEYS)
    missing = sorted(SCENARIO_REQUIRED_KEYS - set(value))
    if unknown or missing:
        raise ControllerError(
            f"source scenario fields differ (unknown={unknown}, missing={missing})"
        )
    if value.get("schema") != SCENARIO_SCHEMA or value.get("driver") != cell.driver:
        raise ControllerError("source scenario schema or driver differs from the cell")
    source_fixture = value.get("source_fixture")
    allowed = SOURCE_FIXTURE_DRIVERS.get(cell.driver)
    if allowed is None or source_fixture not in allowed:
        raise ControllerError("source scenario fixture is invalid for the cell driver")
    if cell.source_fixture_policy == "managed-pdns-required" and source_fixture != "managed-pdns":
        raise ControllerError("matrix placement requires a managed PowerDNS source")
    if (
        cell.source_fixture_policy == "uninitialized-permitted-noncritical"
        and source_fixture != "uninitialized"
    ):
        raise ControllerError("matrix placement requires its explicit uninitialized source")
    for field in ("source_epoch", "target_epoch", "source_revision"):
        item = value.get(field)
        if isinstance(item, bool) or not isinstance(item, int) or item < 0:
            raise ControllerError(f"source scenario {field} is invalid")
    if not isinstance(value.get("zones"), list):
        raise ControllerError("source scenario zones are not an array")
    expected_source_engine = (
        SOURCE_FIXTURE_ENGINES[source_fixture]
        if source_fixture in MANAGED_SOURCE_FIXTURES
        else ""
    )
    if value.get("source_engine", "") != expected_source_engine:
        raise ControllerError("source scenario engine differs from its fixture")
    if source_fixture == "uninitialized" and (
        value["source_epoch"] != 0 or value["source_revision"] != 0
    ):
        raise ControllerError("uninitialized scenario is not an exact empty 0/0 source")
    if source_fixture in MANAGED_SOURCE_FIXTURES and value["source_epoch"] < 1:
        raise ControllerError("managed source scenario has no positive source epoch")
    expected_topology = "standalone" if cell.role == "standalone" else "paired"
    if value.get("topology") != expected_topology:
        raise ControllerError("source scenario topology differs from the matrix role")
    if cell.role == "standalone":
        if any(value.get(field, "") for field in ("pair_role", "peer_ip", "peer_ns")):
            raise ControllerError("standalone source scenario contains peer identity")
    else:
        expected_role = "primary" if cell.role == "paired-primary" else "secondary"
        if value.get("pair_role") != expected_role:
            raise ControllerError("source scenario pair role differs from the matrix role")
        try:
            ipaddress.ip_address(require_string(value.get("peer_ip"), "scenario peer IP"))
        except ValueError as exc:
            raise ControllerError("source scenario peer IP is invalid") from exc
    return value, {
        "path": path,
        "sha256": digest,
        "device": status.st_dev,
        "inode": status.st_ino,
        "mode": f"{stat.S_IMODE(status.st_mode):04o}",
    }


def scenario_boundary_journal_identity(
    scenario: Mapping[str, Any], cell: CellSpec
) -> dict[str, Any]:
    pair_role = ""
    if cell.role != "standalone" and cell.driver != "pdns-adopt":
        pair_role = scenario.get("pair_role", "")
    return {
        "mode": scenario["mode"],
        "source_engine": scenario.get("source_engine", ""),
        "target_engine": scenario["target_engine"],
        "source_epoch": scenario["source_epoch"],
        "target_epoch": scenario["target_epoch"],
        "source_revision": scenario["source_revision"],
        "topology": scenario["topology"],
        "pair_role": pair_role,
    }


def validate_socket_source_proof(
    path: str,
    cell: CellSpec,
    scenario_path: str,
    identity_receipt_path: str,
    state_dir: str,
    journal_path: str,
    dns_address: str,
    dns_port: int,
    dns_name: str,
    dns_type: str,
) -> dict[str, Any]:
    scenario, scenario_evidence = validate_source_scenario(scenario_path, cell)
    proof, raw, proof_digest, status = secure_json_with_digest(
        path, "source proof", maximum=1 << 20
    )
    if not isinstance(proof, dict) or set(proof) != SOURCE_PROOF_KEYS:
        raise ControllerError("source proof fields differ from the exact contract")
    canonical = (json.dumps(proof, indent=2, sort_keys=True) + "\n").encode("utf-8")
    if raw != canonical:
        raise ControllerError("source proof is not canonical sorted JSON")
    source_fixture = scenario["source_fixture"]
    expected = {
        "schema": SOURCE_PROOF_SCHEMA,
        "cell_id": cell.cell_id,
        "source_fixture": source_fixture,
        "scenario_sha256": scenario_evidence["sha256"],
        "identity_receipt_path": identity_receipt_path,
        "identity_receipt_preexisting": False,
        "engine": SOURCE_FIXTURE_ENGINES[source_fixture],
        "source_revision": scenario["source_revision"],
        "serving_before_tagged_agent": source_fixture != "uninitialized",
    }
    for key, wanted in expected.items():
        if proof.get(key) != wanted:
            raise ControllerError(
                f"source proof {key}={proof.get(key)!r}, want {wanted!r}"
            )
    setup_provenance = validate_source_setup_provenance(proof, source_fixture)
    preinstall_provenance = validate_source_preinstall_provenance(
        proof, source_fixture, cell
    )
    adoption_provenance = validate_source_adoption_provenance(
        proof, source_fixture, cell
    )
    external_pdns_preimage = validate_external_pdns_preimage_provenance(
        proof,
        source_fixture,
        cell,
        scenario,
        scenario_evidence["sha256"],
        preinstall_provenance,
        state_dir,
        dns_address,
        dns_port,
        dns_name,
        dns_type,
    )
    normalization_provenance = validate_source_normalization_provenance(
        proof, source_fixture, cell, scenario, state_dir, dns_address
    )
    expected_epoch = (
        scenario["source_epoch"] if source_fixture in MANAGED_SOURCE_FIXTURES else 0
    )
    if proof.get("engine_epoch") != expected_epoch or isinstance(
        proof.get("engine_epoch"), bool
    ):
        raise ControllerError("source proof engine epoch differs from the source fixture")
    require_absent_path(identity_receipt_path, "measured trigger identity receipt")

    state_path = os.path.join(state_dir, "dns-engine-state.json")
    state_evidence: dict[str, Any]
    ownership_paths = {
        (kind, engine): os.path.join(
            state_dir,
            (
                f"dns-engine-ownership-{engine}.json"
                if kind == "ownership"
                else f"dns-engine-install-ownership-{engine}.json"
            ),
        )
        for kind in ("ownership", "install")
        for engine in ("bind", "pdns")
    }
    ownership_evidence: dict[str, Any] = {}
    if source_fixture in MANAGED_SOURCE_FIXTURES:
        if proof.get("engine_state_receipt_path") != state_path or not valid_sha256(
            proof.get("engine_state_receipt_sha256")
        ):
            raise ControllerError("managed source proof lacks its canonical state receipt")
        state, state_raw, state_digest, state_status = secure_json_with_digest(
            state_path, "managed source engine-state receipt", maximum=1 << 20
        )
        if not isinstance(state, dict) or proof.get("engine_state_identity") != state:
            raise ControllerError("managed source state identity differs from its receipt")
        if state_digest != proof["engine_state_receipt_sha256"]:
            raise ControllerError("managed source state receipt hash differs from proof")
        validate_managed_source_state(
            state, state_raw, scenario, expected["engine"]
        )
        state_evidence = {
            "path": state_path,
            "sha256": state_digest,
            "device": state_status.st_dev,
            "inode": state_status.st_ino,
            "identity": state,
        }
        managed_engine = expected["engine"]
        managed_ownership_path = ownership_paths[("ownership", managed_engine)]
        ownership, ownership_raw, ownership_digest, ownership_status = (
            secure_json_with_digest(
                managed_ownership_path,
                f"managed {managed_engine} source ownership receipt",
                maximum=1 << 20,
            )
        )
        if ownership != state:
            raise ControllerError(
                f"managed {managed_engine} ownership receipt differs from active state"
            )
        if ownership_raw != state_raw or ownership_digest != state_digest:
            raise ControllerError(
                f"managed {managed_engine} ownership receipt bytes differ from active state"
            )
        ownership_evidence[f"ownership_{managed_engine}"] = {
            "path": managed_ownership_path,
            "sha256": ownership_digest,
            "device": ownership_status.st_dev,
            "inode": ownership_status.st_ino,
            "identity": ownership,
        }
        if source_fixture == "managed-pdns":
            adoption_receipts = adoption_provenance["production_receipts"]
            if (
                adoption_receipts["state_sha256"] != state_digest
                or adoption_receipts["active_ownership_sha256"]
                != ownership_digest
            ):
                raise ControllerError(
                    "source adoption receipt hashes differ from production state"
                )
        for (kind, engine), candidate in ownership_paths.items():
            if kind == "ownership" and engine == managed_engine:
                continue
            require_absent_path(
                candidate,
                f"managed source preimage {kind} receipt for {engine}",
            )
            ownership_evidence[f"{kind}_{engine}"] = {
                "path": candidate,
                "exists": False,
            }
        require_absent_path(journal_path, "managed source switch journal")
    else:
        if (
            proof.get("engine_state_receipt_path") != ""
            or proof.get("engine_state_receipt_sha256") != ""
            or proof.get("engine_state_identity") is not None
        ):
            raise ControllerError("unmanaged source proof unexpectedly claims an engine-state receipt")
        state_evidence = {"path": "", "sha256": "", "identity": None}
        if source_fixture in {"uninitialized", "external-pdns-adoption"}:
            require_absent_path(
                state_path, f"{source_fixture} source engine-state receipt"
            )
            require_absent_path(
                journal_path, f"{source_fixture} source switch journal"
            )
            for (kind, engine), candidate in ownership_paths.items():
                require_absent_path(
                    candidate,
                    f"{source_fixture} source {kind} receipt for {engine}",
                )
                ownership_evidence[f"{kind}_{engine}"] = {
                    "path": candidate,
                    "exists": False,
                }

    authoritative = proof.get("authoritative_preflight")
    authoritative_keys = {"claimed", "address", "port", "name", "type", "udp", "tcp"}
    if not isinstance(authoritative, dict) or set(authoritative) != authoritative_keys:
        raise ControllerError("source proof authoritative preflight fields are invalid")
    serving = expected["serving_before_tagged_agent"]
    authoritative_expected = {
        "claimed": serving,
        "address": dns_address,
        "port": dns_port,
        "type": dns_type,
        "udp": serving,
        "tcp": serving,
    }
    if any(authoritative.get(key) != value for key, value in authoritative_expected.items()):
        raise ControllerError("source proof authoritative preflight differs from the cell")
    proof_name = authoritative.get("name")
    if (
        not isinstance(proof_name, str)
        or proof_name.rstrip(".").lower() != dns_name.rstrip(".").lower()
    ):
        raise ControllerError("source proof authoritative name differs from the cell")
    port53 = proof.get("uninitialized_global_port53")
    port53_keys = {"udp_bindable", "tcp_bindable", "authoritative_answer_observed"}
    if not isinstance(port53, dict) or set(port53) != port53_keys:
        raise ControllerError("source proof global port-53 observation is invalid")
    port53_expected = (
        {
            "udp_bindable": True,
            "tcp_bindable": True,
            "authoritative_answer_observed": False,
        }
        if source_fixture == "uninitialized"
        else {
            "udp_bindable": False,
            "tcp_bindable": False,
            "authoritative_answer_observed": False,
        }
    )
    if port53 != port53_expected:
        raise ControllerError("source proof global port-53 observation differs from fixture")
    return {
        "path": path,
        "sha256": proof_digest,
        "device": status.st_dev,
        "inode": status.st_ino,
        "mode": f"{stat.S_IMODE(status.st_mode):04o}",
        "schema": proof["schema"],
        "cell_id": proof["cell_id"],
        "source_fixture": source_fixture,
        "engine": proof["engine"],
        "engine_epoch": proof["engine_epoch"],
        "source_revision": proof["source_revision"],
        "serving_before_tagged_agent": serving,
        "scenario": scenario_evidence,
        "scenario_identity": scenario_boundary_journal_identity(scenario, cell),
        "identity_receipt_path": identity_receipt_path,
        "identity_receipt_preexisting": False,
        "engine_state_receipt": state_evidence,
        "source_ownership_receipts": ownership_evidence,
        "source_preinstall_proof": preinstall_provenance,
        "source_adoption_proof": adoption_provenance,
        "external_pdns_preimage": external_pdns_preimage,
        "source_normalization": normalization_provenance,
        "authoritative_preflight": authoritative,
        "uninitialized_global_port53": port53,
        "topology": scenario["topology"],
        "peer_ip": scenario.get("peer_ip", ""),
        **setup_provenance,
    }


def minimal_command_environment(base: Mapping[str, str]) -> dict[str, str]:
    unexpected = sorted(key for key in base if key.startswith("CELIKPANEL_"))
    if unexpected:
        raise ControllerError(
            "caller environment contains unexpected CELIKPANEL variables: "
            + ", ".join(unexpected)
        )
    result = {"PATH": base.get("PATH", DEFAULT_COMMAND_PATH)}
    for key, value in base.items():
        if key in {"LANG", "LANGUAGE"} or key.startswith("LC_"):
            if "\x00" in value:
                raise ControllerError(f"caller locale variable {key} contains NUL")
            result[key] = value
    return result


def production_command_environment(
    base: Mapping[str, str],
    state_dir: str,
    mutation_lock: str,
    agent_socket: str,
    agent_token_file: str,
) -> dict[str, str]:
    result = minimal_command_environment(base)
    result.update(
        {
            "CELIKPANEL_AGENT_STATE_DIR": state_dir,
            "CELIKPANEL_MUTATION_LOCK": mutation_lock,
            "CELIKPANEL_AGENT_SOCKET": agent_socket,
            "CELIKPANEL_AGENT_TOKEN_FILE": agent_token_file,
            "CELIKPANEL_DKIM_DIR": PRODUCTION_DKIM_DIR,
            "CELIKPANEL_RUNTIMES_DIR": PRODUCTION_RUNTIMES_DIR,
        }
    )
    return result


def tagged_agent_environment(
    base: Mapping[str, str],
    cell: CellSpec,
    request_id: str,
    nonce: str,
    marker_path: str,
    ready_fd: int,
    state_dir: str,
    mutation_lock: str,
    agent_socket: str,
    agent_token_file: str,
) -> dict[str, str]:
    result = production_command_environment(
        base, state_dir, mutation_lock, agent_socket, agent_token_file
    )
    result.update(
        {
            SELECTOR_NAMES[0]: cell.cell_id,
            SELECTOR_NAMES[1]: cell.driver,
            SELECTOR_NAMES[2]: cell.point,
            SELECTOR_NAMES[3]: cell.phase,
            SELECTOR_NAMES[4]: request_id,
            SELECTOR_NAMES[5]: nonce,
            SELECTOR_NAMES[6]: marker_path,
            SELECTOR_NAMES[7]: str(ready_fd),
        }
    )
    present = {key for key in result if key.startswith(SELECTOR_PREFIX)}
    if present != set(SELECTOR_NAMES):
        raise ControllerError("tagged agent environment does not contain exactly eight selectors")
    return result


def ordinary_environment(
    base: Mapping[str, str],
    state_dir: str,
    mutation_lock: str,
    agent_socket: str,
    agent_token_file: str,
    *,
    cell: CellSpec | None = None,
    request_id: str = "",
    nonce: str = "",
    proof_path: str = "",
) -> dict[str, str]:
    result = production_command_environment(
        base, state_dir, mutation_lock, agent_socket, agent_token_file
    )
    if cell is not None:
        result.update(
            {
                "CELIKPANEL_S1_CELL_ID": cell.cell_id,
                "CELIKPANEL_S1_DRIVER": cell.driver,
                "CELIKPANEL_S1_REQUEST_ID": request_id,
                "CELIKPANEL_S1_NONCE": nonce,
                "CELIKPANEL_S1_KILL_PROOF": proof_path,
            }
        )
    if any(key.startswith(SELECTOR_PREFIX) for key in result):
        raise ControllerError("untagged command environment retained a fault selector")
    return result


@dataclass(frozen=True)
class ProcStat:
    pid: int
    state: str
    start_ticks: str


def production_group_id() -> int:
    try:
        import grp

        return grp.getgrnam(PRODUCTION_AGENT_GROUP).gr_gid
    except (ImportError, KeyError) as exc:
        raise ControllerError(
            f"production agent group {PRODUCTION_AGENT_GROUP!r} is unavailable"
        ) from exc


def validate_controller_identity() -> dict[str, Any]:
    expected_gid = production_group_id()
    uid = os.geteuid()
    gid = os.getegid()
    if uid != 0 or gid != expected_gid:
        raise ControllerError(
            "controller must run with effective UID 0 and primary GID "
            f"{PRODUCTION_AGENT_GROUP} ({expected_gid}); got {uid}:{gid}"
        )
    return {
        "effective_uid": uid,
        "effective_gid": gid,
        "effective_group": PRODUCTION_AGENT_GROUP,
        "expected_umask": f"{PRODUCTION_AGENT_UMASK:04o}",
    }


def validate_production_runtime_paths(
    agent_token_file: str, expected_gid: int
) -> dict[str, Any]:
    try:
        token = os.lstat(agent_token_file)
    except OSError as exc:
        raise ControllerError(f"inspect production agent token: {exc}") from exc
    if (
        stat.S_ISLNK(token.st_mode)
        or not stat.S_ISREG(token.st_mode)
        or token.st_nlink != 1
        or token.st_uid != 0
        or token.st_gid != expected_gid
        or stat.S_IMODE(token.st_mode) != 0o640
        or token.st_size <= 0
        or token.st_size > 4096
    ):
        raise ControllerError(
            "production agent token is not root:celikpanel mode-0640 bounded single-link data"
        )
    dkim = require_real_directory(PRODUCTION_DKIM_DIR, "production DKIM directory")
    runtimes = require_real_directory(
        PRODUCTION_RUNTIMES_DIR, "production runtimes directory"
    )
    return {
        "agent_token": {
            "path": agent_token_file,
            "uid": token.st_uid,
            "gid": token.st_gid,
            "mode": f"{stat.S_IMODE(token.st_mode):04o}",
            "size": token.st_size,
            "device": token.st_dev,
            "inode": token.st_ino,
        },
        "dkim_directory": {
            "path": PRODUCTION_DKIM_DIR,
            "device": dkim.st_dev,
            "inode": dkim.st_ino,
        },
        "runtimes_directory": {
            "path": PRODUCTION_RUNTIMES_DIR,
            "device": runtimes.st_dev,
            "inode": runtimes.st_ino,
        },
    }


def read_proc_identity(pid: int) -> dict[str, Any]:
    try:
        raw = Path(f"/proc/{pid}/status").read_text(encoding="ascii")
    except OSError as exc:
        raise ControllerError(f"read /proc identity for PID {pid}: {exc}") from exc
    fields: dict[str, str] = {}
    for line in raw.splitlines():
        if ":" in line:
            key, value = line.split(":", 1)
            fields[key] = value.strip()
    try:
        uids = tuple(int(item) for item in fields["Uid"].split())
        gids = tuple(int(item) for item in fields["Gid"].split())
    except (KeyError, ValueError) as exc:
        raise ControllerError(f"/proc identity for PID {pid} is malformed") from exc
    umask = fields.get("Umask", "")
    if len(uids) != 4 or len(gids) != 4 or not umask:
        raise ControllerError(f"/proc identity for PID {pid} is incomplete")
    return {"pid": pid, "uids": list(uids), "gids": list(gids), "umask": umask}


def validate_agent_process_identity(pid: int, expected_gid: int) -> dict[str, Any]:
    identity = read_proc_identity(pid)
    if any(uid != 0 for uid in identity["uids"]):
        raise ControllerError(f"agent PID {pid} does not retain root UID identity")
    if any(gid != expected_gid for gid in identity["gids"]):
        raise ControllerError(
            f"agent PID {pid} primary GID differs from {PRODUCTION_AGENT_GROUP}"
        )
    if identity["umask"] != f"{PRODUCTION_AGENT_UMASK:04o}":
        raise ControllerError(
            f"agent PID {pid} umask is {identity['umask']}, want {PRODUCTION_AGENT_UMASK:04o}"
        )
    return identity


def parse_proc_stat(raw: str) -> ProcStat:
    opening = raw.find("(")
    closing = raw.rfind(")")
    if opening <= 0 or closing <= opening or closing + 2 > len(raw):
        raise ControllerError("/proc stat has an invalid process name")
    try:
        pid = int(raw[:opening].strip())
    except ValueError as exc:
        raise ControllerError("/proc stat has an invalid PID") from exc
    fields = raw[closing + 2 :].split()
    if len(fields) < 20 or len(fields[0]) != 1:
        raise ControllerError("/proc stat is truncated")
    start_ticks = fields[19]
    if not start_ticks.isdecimal() or int(start_ticks) <= 0:
        raise ControllerError("/proc stat has invalid process start ticks")
    return ProcStat(pid=pid, state=fields[0], start_ticks=start_ticks)


def read_proc_stat(pid: int, proc_root: str = "/proc") -> ProcStat:
    if pid <= 1:
        raise ControllerError(f"refuse unsafe PID {pid}")
    path = os.path.join(proc_root, str(pid), "stat")
    try:
        with open(path, "r", encoding="utf-8") as stream:
            value = parse_proc_stat(stream.read())
    except OSError as exc:
        raise ControllerError(f"read {path}: {exc}") from exc
    if value.pid != pid:
        raise ControllerError(f"{path} describes PID {value.pid}, want {pid}")
    return value


def wait_for_proc(pid: int, timeout: float) -> ProcStat:
    deadline = time.monotonic() + timeout
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            return read_proc_stat(pid)
        except ControllerError as exc:
            last_error = exc
            time.sleep(0.01)
    raise BoundaryUnverified(f"process identity did not appear: {last_error}")


def wait_for_stopped_process(
    process: subprocess.Popen[bytes], expected_start_ticks: str, timeout: float
) -> ProcStat:
    deadline = time.monotonic() + timeout
    last: ProcStat | None = None
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise BoundaryUnverified(
                f"tagged agent exited before SIGKILL with return code {process.returncode}"
            )
        last = read_proc_stat(process.pid)
        if last.start_ticks != expected_start_ticks:
            raise BoundaryUnverified("tagged agent PID was reused before signal delivery")
        if last.state in ("T", "t"):
            return last
        time.sleep(0.01)
    state = last.state if last else "unknown"
    raise BoundaryUnverified(f"tagged agent did not enter stopped state; last state {state!r}")


@dataclass(frozen=True)
class CommandResult:
    argv: tuple[str, ...]
    returncode: int
    output: bytes
    truncated: bool
    duration_seconds: float

    def report(self) -> dict[str, Any]:
        return {
            "argv": list(self.argv),
            "returncode": self.returncode,
            "duration_seconds": round(self.duration_seconds, 6),
            "output_sha256": hashlib.sha256(self.output).hexdigest(),
            "output_bytes": len(self.output),
            "output_truncated": self.truncated,
        }


def run_bounded_command(
    argv: Sequence[str],
    label: str,
    timeout: float,
    env: Mapping[str, str],
    cwd: str,
    transcript: Transcript,
    *,
    pass_fds: Sequence[int] = (),
) -> CommandResult:
    inherited = tuple(pass_fds)
    transcript.event(
        "command-start",
        label=label,
        argv=list(argv),
        timeout_seconds=timeout,
        inherited_fds=list(inherited),
    )
    started = time.monotonic()
    with tempfile.TemporaryFile() as output:
        process = subprocess.Popen(
            list(argv),
            stdin=subprocess.DEVNULL,
            stdout=output,
            stderr=subprocess.STDOUT,
            env=dict(env),
            cwd=cwd,
            start_new_session=True,
            pass_fds=inherited,
            close_fds=True,
            umask=PRODUCTION_AGENT_UMASK,
        )
        try:
            returncode = process.wait(timeout=timeout)
        except subprocess.TimeoutExpired as exc:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait(timeout=5)
            raise ControllerError(f"{label} exceeded {timeout} seconds") from exc
        output.seek(0)
        captured = output.read(MAX_COMMAND_OUTPUT + 1)
    truncated = len(captured) > MAX_COMMAND_OUTPUT
    captured = captured[:MAX_COMMAND_OUTPUT]
    result = CommandResult(
        tuple(argv), returncode, captured, truncated, time.monotonic() - started
    )
    transcript.command_output(label, captured, truncated)
    transcript.event("command-finish", label=label, **result.report())
    return result


def require_command_success(result: CommandResult, label: str) -> None:
    if result.returncode != 0:
        raise ControllerError(f"{label} exited {result.returncode}")
    if result.truncated:
        raise ControllerError(f"{label} output exceeded {MAX_COMMAND_OUTPUT} bytes")


def start_async_command(
    argv: Sequence[str], env: Mapping[str, str], cwd: str, transcript: Transcript, label: str
) -> subprocess.Popen[bytes]:
    transcript.event("command-start", label=label, argv=list(argv), asynchronous=True)
    return subprocess.Popen(
        list(argv),
        stdin=subprocess.DEVNULL,
        stdout=transcript.fileno(),
        stderr=subprocess.STDOUT,
        env=dict(env),
        cwd=cwd,
        start_new_session=True,
        umask=PRODUCTION_AGENT_UMASK,
    )


def finish_async_command(
    process: subprocess.Popen[bytes] | None,
    label: str,
    timeout: float,
    transcript: Transcript,
) -> int | None:
    if process is None:
        return None
    try:
        returncode = process.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        returncode = process.wait(timeout=5)
        transcript.event("command-timeout-killed", label=label, returncode=returncode)
        return returncode
    finally:
        if process.returncode is not None:
            transcript.event("command-finish", label=label, returncode=process.returncode)
    return returncode


def wait_for_unix_socket(
    path: str,
    timeout: float,
    *,
    process: subprocess.Popen[bytes] | None = None,
    early_ready_fd: int | None = None,
    previous_identity: tuple[int, int] | None = None,
) -> tuple[int, int]:
    deadline = time.monotonic() + timeout
    last_error = "socket absent"
    while time.monotonic() < deadline:
        if process is not None and process.poll() is not None:
            raise BoundaryUnverified(
                f"tagged agent exited before socket readiness: {process.returncode}"
            )
        if early_ready_fd is not None:
            readable, _, _ = select.select([early_ready_fd], [], [], 0)
            if readable:
                raise BoundaryUnverified("kill boundary fired before agent socket readiness")
        try:
            status = os.lstat(path)
            if stat.S_ISLNK(status.st_mode) or not stat.S_ISSOCK(status.st_mode):
                raise ControllerError("agent socket path is not an exact non-symlink socket")
            identity = (status.st_dev, status.st_ino)
            if previous_identity is not None and identity == previous_identity:
                last_error = "socket inode was not replaced"
            else:
                connection = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                try:
                    connection.settimeout(min(0.25, max(0.01, deadline - time.monotonic())))
                    connection.connect(path)
                finally:
                    connection.close()
                return identity
        except (FileNotFoundError, ConnectionError, TimeoutError, OSError, ControllerError) as exc:
            last_error = str(exc)
        time.sleep(0.025)
    raise ControllerError(f"agent socket did not become ready: {last_error}")


def assert_unix_socket_ready(path: str, timeout: float) -> None:
    wait_for_unix_socket(path, timeout)


def wait_for_tcp(address: str, port: int, timeout: float) -> None:
    deadline = time.monotonic() + timeout
    last_error = "not attempted"
    while time.monotonic() < deadline:
        try:
            with socket.create_connection((address, port), timeout=min(0.25, timeout)):
                return
        except OSError as exc:
            last_error = str(exc)
            time.sleep(0.025)
    raise ControllerError(f"TCP endpoint {address}:{port} did not become ready: {last_error}")


def read_ready_nonce(
    fd: int,
    expected_nonce: str,
    timeout: float,
    agent: subprocess.Popen[bytes],
    *,
    trigger: subprocess.Popen[bytes] | None = None,
) -> bytes:
    deadline = time.monotonic() + timeout
    payload = bytearray()
    while time.monotonic() < deadline:
        if agent.poll() is not None:
            raise BoundaryUnverified(
                f"tagged agent exited before boundary notification: {agent.returncode}"
            )
        readable, _, _ = select.select([fd], [], [], min(0.1, deadline - time.monotonic()))
        if not readable:
            if trigger is not None:
                trigger_returncode = trigger.poll()
                if trigger_returncode is not None:
                    raise TriggerExitedBeforeBoundary(trigger_returncode)
            continue
        chunk = os.read(fd, 256)
        if not chunk:
            expected = (expected_nonce + "\n").encode("ascii")
            if bytes(payload) != expected:
                raise BoundaryUnverified(
                    f"ready pipe payload {bytes(payload)!r} differs from expected nonce"
                )
            return bytes(payload)
        payload.extend(chunk)
        if len(payload) > 130:
            raise BoundaryUnverified("ready pipe payload is oversized")
    if trigger is not None:
        trigger_returncode = trigger.poll()
        if trigger_returncode is not None:
            raise TriggerExitedBeforeBoundary(trigger_returncode)
    raise BoundaryUnverified(f"boundary hook did not fire within {timeout} seconds")


def rollback_precursor_phase(cell: CellSpec) -> str | None:
    if cell.phase not in ("rolling-back", "rolled-back"):
        return None
    return ROLLBACK_PRECURSOR_PHASES.get(cell.driver)


def expected_journal_phase(cell: CellSpec) -> str | None:
    if cell.edge == "window" or (cell.edge == "before-write" and cell.phase == "intent"):
        return None
    if cell.edge == "after-write":
        return cell.phase
    if cell.phase == "rolling-back":
        predecessor = rollback_precursor_phase(cell)
        if predecessor is None:
            raise ControllerError(
                "rolling-back:before-write lacks a deterministic rollback precursor"
            )
        return predecessor
    if cell.phase == "rolled-back":
        return "rolling-back"
    if cell.driver == "pdns-adopt" and cell.phase == "target-verified":
        return "intent"
    predecessors = {
        "target-staged": "intent",
        "source-stopped": "target-staged",
        "target-started": "source-stopped",
        "target-verified": "target-started",
        "committed": "target-verified",
    }
    try:
        return predecessors[cell.phase]
    except KeyError as exc:
        raise ControllerError(f"no journal predecessor rule for {cell.phase}") from exc


def validate_observed_journal(
    cell: CellSpec,
    observed: Mapping[str, Any],
    request_id: str,
    expected_identity: Mapping[str, Any],
    *,
    expected_phase: str | None = None,
) -> None:
    if observed.get("schema") != JOURNAL_SCHEMA:
        raise BoundaryUnverified("marker observed an unexpected journal schema")
    wanted_phase = cell.phase if expected_phase is None else expected_phase
    if observed.get("phase") != wanted_phase:
        raise BoundaryUnverified("marker observed another in-memory phase")
    if observed.get("mutation_request_id") != request_id:
        raise BoundaryUnverified("marker observed another mutation request")
    owner = observed.get("mutation_owner_id")
    if not isinstance(owner, str) or not valid_lower_hex(owner, 32, 32):
        raise BoundaryUnverified("marker mutation owner is not exact lowercase hex")
    if cell.driver == "bind":
        expected_target, expected_mode = "bind", "switch"
    elif cell.driver == "pdns-adopt":
        expected_target, expected_mode = "pdns", "adopt"
    elif cell.driver in ("pdns-switch", "pdns-secondary-reconfigure"):
        expected_target, expected_mode = "pdns", "switch"
    elif cell.driver == "signed-update-finalize":
        expected_target, expected_mode = "bind", "switch"
    else:
        raise BoundaryUnverified("marker driver is unsupported")
    if observed.get("target_engine") != expected_target or observed.get("mode") != expected_mode:
        raise BoundaryUnverified("marker target/mode does not prove the selected driver")
    expected_topology = "standalone" if cell.role == "standalone" else "paired"
    if observed.get("topology") != expected_topology:
        raise BoundaryUnverified("marker topology does not prove the selected role")
    expected_role = ""
    if cell.driver != "pdns-adopt" and cell.role != "standalone":
        expected_role = cell.role.removeprefix("paired-")
    if observed.get("pair_role", "") != expected_role:
        raise BoundaryUnverified("marker pair role does not prove the selected cell")
    if set(expected_identity) != set(BOUNDARY_JOURNAL_IDENTITY_FIELDS):
        raise ControllerError("expected boundary journal identity is incomplete")
    for key in BOUNDARY_JOURNAL_IDENTITY_FIELDS:
        default: Any = "" if key in OPTIONAL_BOUNDARY_JOURNAL_IDENTITY_FIELDS else None
        actual = observed.get(key, default)
        wanted = expected_identity[key]
        if type(actual) is not type(wanted) or actual != wanted:
            raise BoundaryUnverified(
                f"marker observed journal {key}={actual!r}, want {wanted!r}"
            )


def validate_rollback_precursor(
    marker: Mapping[str, Any],
    cell: CellSpec,
    request_id: str,
    journal_path: str,
    expected_journal_identity: Mapping[str, Any],
) -> None:
    precursor_phase = rollback_precursor_phase(cell)
    if precursor_phase is None:
        if "rollback_precursor" in marker:
            raise BoundaryUnverified(
                "boundary marker has an unexpected rollback precursor"
            )
        return

    if "rollback_precursor" not in marker:
        raise BoundaryUnverified(
            "rollback boundary marker lacks its one exact precursor"
        )
    precursor = marker["rollback_precursor"]
    if not isinstance(precursor, dict):
        raise BoundaryUnverified("rollback precursor is not one object")
    expected_fields = {
        "schema",
        "driver",
        "observed_driver",
        "point",
        "phase",
        "request_id",
        "action",
        "observed_journal",
    }
    if set(precursor) != expected_fields:
        raise BoundaryUnverified("rollback precursor fields are not exact")
    expected = {
        "schema": ROLLBACK_PRECURSOR_SCHEMA,
        "driver": cell.driver,
        "observed_driver": cell.driver,
        "point": "after_write",
        "phase": precursor_phase,
        "request_id": request_id,
        "action": ROLLBACK_PRECURSOR_ACTION,
    }
    for key, value in expected.items():
        if precursor.get(key) != value:
            raise BoundaryUnverified(
                f"rollback precursor {key}={precursor.get(key)!r}, want {value!r}"
            )
    observed = precursor.get("observed_journal")
    if not isinstance(observed, dict):
        raise BoundaryUnverified("rollback precursor lacks observed_journal")
    if set(expected_journal_identity) != set(BOUNDARY_JOURNAL_IDENTITY_FIELDS):
        raise ControllerError("expected rollback precursor identity is incomplete")
    expected_observed_fields = {
        "path",
        "schema",
        "phase",
        "mode",
        "mutation_request_id",
        "mutation_owner_id",
        "manifest_qualifier",
        "target_engine",
        "source_epoch",
        "target_epoch",
        "source_revision",
        "topology",
    }
    for optional in OPTIONAL_BOUNDARY_JOURNAL_IDENTITY_FIELDS:
        if expected_journal_identity[optional] != "":
            expected_observed_fields.add(optional)
    if set(observed) != expected_observed_fields:
        raise BoundaryUnverified(
            "rollback precursor observed journal fields are not exact"
        )
    if observed.get("path") != journal_path:
        raise BoundaryUnverified("rollback precursor names another journal path")
    validate_observed_journal(
        cell,
        observed,
        request_id,
        expected_journal_identity,
        expected_phase=precursor_phase,
    )


def validate_marker(
    path: str,
    cell: CellSpec,
    request_id: str,
    nonce: str,
    ready_fd: int,
    agent_pid: int,
    start_ticks: str,
    journal_path: str,
    expected_journal_identity: Mapping[str, Any],
) -> dict[str, Any]:
    marker, _ = secure_read_json(
        path,
        "boundary marker",
        maximum=1 << 20,
        required_mode=0o600,
        required_uid=os.geteuid(),
    )
    if not isinstance(marker, dict):
        raise BoundaryUnverified("boundary marker root is not an object")
    expected = {
        "schema": MARKER_SCHEMA,
        "cell_id": cell.cell_id,
        "driver": cell.driver,
        "observed_driver": cell.driver,
        "point": cell.point,
        "phase": cell.phase,
        "request_id": request_id,
        "nonce": nonce,
        "marker": path,
        "ready_fd": ready_fd,
        "pid": agent_pid,
        "process_start_ticks": start_ticks,
    }
    for key, value in expected.items():
        if marker.get(key) != value:
            raise BoundaryUnverified(
                f"boundary marker {key}={marker.get(key)!r}, want {value!r}"
            )
    recorded_at = marker.get("recorded_at")
    if not isinstance(recorded_at, str):
        raise BoundaryUnverified("boundary marker lacks recorded_at")
    try:
        dt.datetime.fromisoformat(recorded_at.replace("Z", "+00:00"))
    except ValueError as exc:
        raise BoundaryUnverified("boundary marker recorded_at is invalid") from exc
    observed = marker.get("observed_journal")
    if not isinstance(observed, dict):
        raise BoundaryUnverified("boundary marker lacks observed_journal")
    expected_path = "" if cell.point == "pre_intent" else journal_path
    if observed.get("path", "") != expected_path:
        raise BoundaryUnverified("boundary marker names another journal path")
    validate_observed_journal(
        cell, observed, request_id, expected_journal_identity
    )
    validate_rollback_precursor(
        marker,
        cell,
        request_id,
        journal_path,
        expected_journal_identity,
    )
    return marker


def validate_journal_disk_state(
    journal_path: str,
    expected_phase: str | None,
    request_id: str,
    expected_identity: Mapping[str, Any],
) -> dict[str, Any]:
    if expected_phase is None:
        try:
            os.lstat(journal_path)
        except FileNotFoundError:
            return {"exists": False, "expected_phase": None}
        except OSError as exc:
            raise BoundaryUnverified(f"inspect absent journal expectation: {exc}") from exc
        raise BoundaryUnverified("journal exists at a boundary that requires absence")
    journal, status = secure_read_json(journal_path, "DNS switch journal")
    if not isinstance(journal, dict):
        raise BoundaryUnverified("DNS switch journal root is not an object")
    if journal.get("schema") != JOURNAL_SCHEMA:
        raise BoundaryUnverified("DNS switch journal schema differs")
    if journal.get("phase") != expected_phase:
        raise BoundaryUnverified(
            f"DNS switch journal phase {journal.get('phase')!r}, want {expected_phase!r}"
        )
    if journal.get("mutation_request_id") != request_id:
        raise BoundaryUnverified("DNS switch journal belongs to another mutation")
    if set(expected_identity) != set(BOUNDARY_JOURNAL_IDENTITY_FIELDS):
        raise ControllerError("expected disk journal identity is incomplete")
    for key in BOUNDARY_JOURNAL_IDENTITY_FIELDS:
        default: Any = "" if key in OPTIONAL_BOUNDARY_JOURNAL_IDENTITY_FIELDS else None
        actual = journal.get(key, default)
        wanted = expected_identity[key]
        if type(actual) is not type(wanted) or actual != wanted:
            raise BoundaryUnverified(
                f"DNS switch journal {key}={actual!r}, want {wanted!r}"
            )
    return {
        "exists": True,
        "expected_phase": expected_phase,
        "observed_phase": journal.get("phase"),
        "request_id": journal.get("mutation_request_id"),
        "device": status.st_dev,
        "inode": status.st_ino,
        "mode": f"{stat.S_IMODE(status.st_mode):04o}",
        "sha256": sha256_file(journal_path),
    }


def encode_dns_name(name: str) -> bytes:
    if name == ".":
        return b"\x00"
    canonical = name.rstrip(".")
    if not canonical:
        raise ControllerError("DNS name is empty")
    result = bytearray()
    for label in canonical.split("."):
        try:
            encoded = label.encode("idna")
        except UnicodeError as exc:
            raise ControllerError(f"DNS label {label!r} is invalid") from exc
        if not 1 <= len(encoded) <= 63:
            raise ControllerError(f"DNS label {label!r} has an invalid length")
        result.append(len(encoded))
        result.extend(encoded)
    result.append(0)
    if len(result) > 255:
        raise ControllerError("DNS name exceeds 255 wire bytes")
    return bytes(result)


def build_dns_query(name: str, qtype: str, transaction_id: int) -> bytes:
    if qtype not in DNS_TYPES:
        raise ControllerError(f"unsupported DNS query type {qtype}")
    header = struct.pack("!HHHHHH", transaction_id, 0, 1, 0, 0, 0)
    return header + encode_dns_name(name) + struct.pack("!HH", DNS_TYPES[qtype], 1)


def validate_dns_response(raw: bytes, transaction_id: int, transport: str) -> dict[str, Any]:
    if len(raw) < 12:
        raise ControllerError(f"{transport} DNS response is truncated")
    response_id, flags, questions, answers, authority, additional = struct.unpack(
        "!HHHHHH", raw[:12]
    )
    if response_id != transaction_id:
        raise ControllerError(f"{transport} DNS response has another transaction ID")
    if flags & 0x8000 == 0 or flags & 0x0400 == 0:
        raise ControllerError(f"{transport} DNS response is not authoritative")
    if flags & 0x0200:
        raise ControllerError(f"{transport} DNS response is truncated")
    if flags & 0x000F:
        raise ControllerError(f"{transport} DNS response RCODE is {flags & 0x000F}")
    if questions != 1 or answers < 1:
        raise ControllerError(
            f"{transport} DNS response has questions={questions}, answers={answers}"
        )
    return {
        "transport": transport,
        "bytes": len(raw),
        "flags": flags,
        "questions": questions,
        "answers": answers,
        "authority": authority,
        "additional": additional,
    }


def _recv_exact(connection: socket.socket, count: int) -> bytes:
    result = bytearray()
    while len(result) < count:
        chunk = connection.recv(count - len(result))
        if not chunk:
            raise ControllerError("TCP DNS response ended early")
        result.extend(chunk)
    return bytes(result)


def query_authoritative_dns(
    address: str, port: int, name: str, qtype: str, timeout: float
) -> dict[str, Any]:
    parsed = ipaddress.ip_address(address)
    family = socket.AF_INET if parsed.version == 4 else socket.AF_INET6
    destination: tuple[Any, ...]
    destination = (address, port) if family == socket.AF_INET else (address, port, 0, 0)
    transaction_id = secrets.randbelow(65535) + 1
    query = build_dns_query(name, qtype, transaction_id)
    with socket.socket(family, socket.SOCK_DGRAM) as udp:
        udp.settimeout(timeout)
        udp.connect(destination)
        udp.sendall(query)
        udp_raw = udp.recv(65535)
    udp_result = validate_dns_response(udp_raw, transaction_id, "udp")
    with socket.socket(family, socket.SOCK_STREAM) as tcp:
        tcp.settimeout(timeout)
        tcp.connect(destination)
        tcp.sendall(struct.pack("!H", len(query)) + query)
        length = struct.unpack("!H", _recv_exact(tcp, 2))[0]
        if length == 0:
            raise ControllerError("TCP DNS response has zero length")
        tcp_raw = _recv_exact(tcp, length)
    tcp_result = validate_dns_response(tcp_raw, transaction_id, "tcp")
    return {"udp": udp_result, "tcp": tcp_result}


def read_ssh_banner(address: str, timeout: float) -> dict[str, Any]:
    parsed = ipaddress.ip_address(address)
    family = socket.AF_INET if parsed.version == 4 else socket.AF_INET6
    destination: tuple[Any, ...] = (
        (address, 22) if family == socket.AF_INET else (address, 22, 0, 0)
    )
    started = time.monotonic()
    with socket.socket(family, socket.SOCK_STREAM) as connection:
        connection.settimeout(timeout)
        connection.connect(destination)
        banner = bytearray()
        while len(banner) <= 512 and not banner.endswith(b"\n"):
            chunk = connection.recv(1)
            if not chunk:
                break
            banner.extend(chunk)
    if len(banner) > 512 or not (
        bytes(banner).startswith(b"SSH-2.0-")
        or bytes(banner).startswith(b"SSH-1.99-")
    ):
        raise ControllerError("peer port 22 did not return a bounded SSH banner")
    return {
        "address": address,
        "port": 22,
        "banner": bytes(banner).decode("ascii", errors="replace").rstrip("\r\n"),
        "elapsed_seconds": time.monotonic() - started,
    }


def probe_udp_tcp_bindability(address: str, port: int = 53) -> dict[str, Any]:
    parsed = ipaddress.ip_address(address)
    family = socket.AF_INET if parsed.version == 4 else socket.AF_INET6
    tcp_destination: tuple[Any, ...] = (
        (address, port) if family == socket.AF_INET else (address, port, 0, 0)
    )
    with socket.socket(family, socket.SOCK_STREAM) as tcp, socket.socket(
        family, socket.SOCK_DGRAM
    ) as udp:
        tcp.bind(tcp_destination)
        actual_port = tcp.getsockname()[1]
        tcp.listen(1)
        udp_destination: tuple[Any, ...] = (
            (address, actual_port)
            if family == socket.AF_INET
            else (address, actual_port, 0, 0)
        )
        udp.bind(udp_destination)
        return {
            "address": address,
            "port": actual_port,
            "udp_bindable": True,
            "tcp_bindable": True,
            "authoritative_answer_observed": False,
        }


def observe_peer_after_kill(
    address: str,
    expected_reachability: str,
    connect_timeout: float,
    stability_seconds: float,
    stability_interval: float,
) -> tuple[dict[str, Any], str | None]:
    if expected_reachability == "reachable":
        try:
            return {
                "expected": "reachable",
                "samples": [{"ordinal": 1, "reachable": True, **read_ssh_banner(address, connect_timeout)}],
            }, None
        except (ControllerError, OSError) as exc:
            return {
                "expected": "reachable",
                "samples": [{"ordinal": 1, "reachable": False, "error": str(exc)}],
            }, f"peer reachability after kill was not proven: {exc}"
    deadline = time.monotonic() + stability_seconds
    samples: list[dict[str, Any]] = []
    ordinal = 0
    observed_reachable = False
    while True:
        ordinal += 1
        sample: dict[str, Any] = {"ordinal": ordinal, "at": utc_now()}
        try:
            sample.update({"reachable": True, **read_ssh_banner(address, connect_timeout)})
            observed_reachable = True
        except (ControllerError, OSError) as exc:
            sample.update({"reachable": False, "error": str(exc)})
        samples.append(sample)
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            break
        time.sleep(min(stability_interval, remaining))
    report = {
        "expected": "unreachable",
        "duration_seconds": stability_seconds,
        "interval_seconds": stability_interval,
        "samples": samples,
    }
    if observed_reachable:
        return report, "peer remained SSH-reachable during the unreachable stability window"
    return report, None


def observe_peer_once(
    address: str, expected_reachability: str, timeout: float
) -> tuple[dict[str, Any], str | None]:
    try:
        banner = read_ssh_banner(address, timeout)
    except (ControllerError, OSError) as exc:
        report = {
            "expected": expected_reachability,
            "reachable": False,
            "error": str(exc),
        }
        if expected_reachability == "unreachable":
            return report, None
        return report, f"peer expected reachable but SSH proof failed: {exc}"
    report = {"expected": expected_reachability, "reachable": True, **banner}
    if expected_reachability == "unreachable":
        return report, "peer expected unreachable but returned an SSH banner"
    return report, None


def inspect_dns_unit_states(
    timeout: float, environment: Mapping[str, str]
) -> dict[str, str]:
    states: dict[str, str] = {}
    for unit in ("bind9.service", "named.service", "pdns.service"):
        try:
            completed = subprocess.run(
                ["/usr/bin/systemctl", "show", "--property=ActiveState", "--value", unit],
                stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                env=dict(environment),
                check=False,
                timeout=timeout,
            )
        except (OSError, subprocess.SubprocessError) as exc:
            raise ControllerError(f"inspect pre-kill DNS unit {unit}: {exc}") from exc
        value = completed.stdout.decode("utf-8", errors="replace").strip()
        if completed.returncode != 0 or value not in {"active", "inactive", "failed"}:
            raise ControllerError(
                f"pre-kill DNS unit {unit} has untrusted state {value!r} "
                f"(exit {completed.returncode})"
            )
        states[unit] = value
    return states


def inspect_restarted_agent_process(
    timeout: float, environment: Mapping[str, str], expected_gid: int
) -> dict[str, Any]:
    try:
        completed = subprocess.run(
            [
                "/usr/bin/systemctl",
                "show",
                "--property=MainPID",
                "--value",
                "celikpanel-agent.service",
            ],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=dict(environment),
            check=False,
            timeout=timeout,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        raise ControllerError(f"inspect restarted agent MainPID: {exc}") from exc
    raw_pid = completed.stdout.decode("ascii", errors="replace").strip()
    if completed.returncode != 0 or not raw_pid.isdecimal() or int(raw_pid) <= 0:
        raise ControllerError(
            f"restarted agent MainPID is invalid: {raw_pid!r} (exit {completed.returncode})"
        )
    return validate_agent_process_identity(int(raw_pid), expected_gid)


def validate_source_unit_states(
    source_fixture: str, states: Mapping[str, str]
) -> None:
    bind_active = states.get("bind9.service") == "active" or states.get(
        "named.service"
    ) == "active"
    pdns_active = states.get("pdns.service") == "active"
    expected_engine = SOURCE_FIXTURE_ENGINES[source_fixture]
    if expected_engine == "" and (bind_active or pdns_active):
        raise ControllerError("uninitialized source has an active DNS engine unit")
    if expected_engine == "bind" and (not bind_active or pdns_active):
        raise ControllerError("managed BIND source unit state is not exclusive")
    if expected_engine == "pdns" and (not pdns_active or bind_active):
        raise ControllerError("PowerDNS source unit state is not exclusive")


def decode_recovery_probe(result: CommandResult, ordinal: int) -> dict[str, Any]:
    report = result.report()
    report["ordinal"] = ordinal
    if result.returncode != 0 or result.truncated:
        report["valid"] = False
        report["error"] = "probe command failed or exceeded output limit"
        return report
    try:
        value = decode_json(result.output, f"recovery probe {ordinal}")
    except ControllerError as exc:
        report["valid"] = False
        report["error"] = str(exc)
        return report
    expected_keys = {
        "schema",
        "converged",
        "recovery_outcome",
        "active_dns_engine",
        "fingerprint",
        "detail",
    }
    if (
        not isinstance(value, dict)
        or set(value) != expected_keys
        or value.get("schema") != RECOVERY_PROBE_SCHEMA
    ):
        report["valid"] = False
        report["error"] = "probe schema is invalid"
        return report
    fingerprint = value.get("fingerprint")
    converged = value.get("converged")
    if not valid_sha256(fingerprint):
        report["valid"] = False
        report["error"] = "probe fingerprint is invalid"
        return report
    if not isinstance(converged, bool):
        report["valid"] = False
        report["error"] = "probe converged field is invalid"
        return report
    outcome = value.get("recovery_outcome")
    active_engine = value.get("active_dns_engine")
    detail = value.get("detail")
    if outcome not in {
        "target_converged",
        "rolled_back_source_active",
        "indeterminate",
    } or active_engine not in {"", "bind", "pdns"}:
        report["valid"] = False
        report["error"] = "probe recovery outcome is invalid"
        return report
    if not isinstance(detail, str) or len(detail) > 4096:
        report["valid"] = False
        report["error"] = "probe detail is invalid"
        return report
    if converged != (outcome == "target_converged"):
        report["valid"] = False
        report["error"] = "probe convergence disagrees with its recovery outcome"
        return report
    if outcome == "rolled_back_source_active" and not active_engine:
        report["valid"] = False
        report["error"] = "rolled-back probe has no active source engine"
        return report
    report.update(
        {
            "valid": True,
            "converged": converged,
            "fingerprint": fingerprint,
            "detail": detail,
            "recovery_outcome": outcome,
            "active_dns_engine": active_engine,
        }
    )
    return report


def assess_recovery_probes(first: Mapping[str, Any], second: Mapping[str, Any]) -> list[str]:
    errors: list[str] = []
    if not first.get("valid"):
        errors.append("first recovery probe is invalid")
    elif not first.get("converged"):
        errors.append("first recovery probe did not converge")
    if not second.get("valid"):
        errors.append("second recovery probe is invalid")
    elif not second.get("converged"):
        errors.append("second recovery probe did not converge")
    if first.get("valid") and second.get("valid") and first.get("fingerprint") != second.get(
        "fingerprint"
    ):
        errors.append("recovery fingerprint changed on the second probe")
    return errors


def summarize_recovery_outcome(
    first: Mapping[str, Any],
    second: Mapping[str, Any],
    dns_serving: bool,
) -> dict[str, Any]:
    valid = bool(first.get("valid") and second.get("valid"))
    same_fingerprint = valid and first.get("fingerprint") == second.get("fingerprint")
    changed = valid and not same_fingerprint
    target_converged = bool(
        same_fingerprint
        and first.get("recovery_outcome") == "target_converged"
        and second.get("recovery_outcome") == "target_converged"
    )
    same_rolled_source = bool(
        same_fingerprint
        and first.get("recovery_outcome") == "rolled_back_source_active"
        and second.get("recovery_outcome") == "rolled_back_source_active"
        and first.get("active_dns_engine") == second.get("active_dns_engine")
        and first.get("active_dns_engine") in {"bind", "pdns"}
    )
    rolled_back_source_serving = bool(same_rolled_source and dns_serving)
    repeated_nonconvergence = bool(
        same_fingerprint and not target_converged and not rolled_back_source_serving
    )
    if not valid:
        classification = "unverified"
    elif changed:
        classification = "changed/race"
    elif target_converged:
        classification = "target_converged"
    elif rolled_back_source_serving:
        classification = "rolled_back_source_serving"
    else:
        classification = "repeated_nonconvergence"
    return {
        "classification": classification,
        "target_converged": target_converged,
        "rolled_back_source_serving": rolled_back_source_serving,
        "repeated_nonconvergence": repeated_nonconvergence,
        "changed_or_racy": changed,
        "probe_pair_valid": valid,
        "same_fingerprint": same_fingerprint,
        "dns_serving_after_restart": dns_serving,
    }


@dataclass(frozen=True)
class Settings:
    cell: CellSpec
    request_id: str
    nonce: str
    tagged_agent_command: tuple[str, ...]
    trigger_mode: str
    trigger_command: tuple[str, ...] | None
    recovery_command: tuple[str, ...]
    source_proof_path: str | None
    agent_restart_command: tuple[str, ...]
    panel_restart_command: tuple[str, ...]
    recovery_probe_command: tuple[str, ...]
    peer_partition_command: tuple[str, ...] | None
    command_cwd: str
    state_dir: str
    mutation_lock: str
    agent_socket: str
    agent_token_file: str
    journal_path: str
    marker_path: str
    proof_path: str
    result_path: str
    transcript_path: str
    dns_address: str
    dns_port: int
    dns_name: str
    dns_type: str
    panel_address: str
    panel_port: int
    startup_timeout: float
    boundary_timeout: float
    stop_timeout: float
    kill_timeout: float
    command_timeout: float
    recovery_timeout: float
    endpoint_timeout: float
    dns_timeout: float
    stability_seconds: float
    stability_interval: float


def inspect_command_executable(
    argv: Sequence[str], label: str, *, require_regular_path: bool = False
) -> dict[str, Any]:
    executable = argv[0]
    require_clean_absolute(executable, f"{label} executable")
    try:
        link_status = os.lstat(executable)
        status = os.stat(executable)
    except OSError as exc:
        raise ControllerError(f"inspect {label} executable: {exc}") from exc
    if require_regular_path and stat.S_ISLNK(link_status.st_mode):
        raise ControllerError(f"{label} executable must not be a symlink")
    if not stat.S_ISREG(status.st_mode) or not os.access(executable, os.X_OK):
        raise ControllerError(f"{label} executable is not an executable regular file")
    return {
        "argv": list(argv),
        "path": executable,
        "resolved_path": os.path.realpath(executable),
        "device": status.st_dev,
        "inode": status.st_ino,
        "mode": f"{stat.S_IMODE(status.st_mode):04o}",
        "uid": status.st_uid,
        "sha256": sha256_file(executable),
    }


def describe_initial_journal(path: str) -> dict[str, Any]:
    try:
        os.lstat(path)
    except FileNotFoundError:
        return {"exists": False}
    except OSError as exc:
        raise ControllerError(f"inspect initial DNS switch journal: {exc}") from exc
    value, status = secure_read_json(path, "initial DNS switch journal")
    if not isinstance(value, dict):
        raise ControllerError("initial DNS switch journal root is not an object")
    return {
        "exists": True,
        "schema": value.get("schema"),
        "phase": value.get("phase"),
        "request_id": value.get("mutation_request_id"),
        "device": status.st_dev,
        "inode": status.st_ino,
        "sha256": sha256_file(path),
    }


def validate_signed_startup_preconditions(
    state_dir: str,
    journal_path: str,
    request_id: str,
    cell: CellSpec,
) -> dict[str, Any]:
    journal, journal_status = secure_read_json(
        journal_path,
        "signed-update startup DNS journal",
        required_mode=0o600,
        required_uid=0,
    )
    if not isinstance(journal, dict):
        raise ControllerError("signed-update startup DNS journal is not an object")
    expected_topology = "standalone" if cell.role == "standalone" else "paired"
    expected_role = "" if cell.role == "standalone" else "primary"
    journal_expected = {
        "schema": JOURNAL_SCHEMA,
        "phase": "rolling-back",
        "mode": "switch",
        "mutation_request_id": request_id,
        "target_engine": "bind",
        "topology": expected_topology,
    }
    for key, value in journal_expected.items():
        if journal.get(key) != value:
            raise ControllerError(
                f"signed-update startup journal {key}={journal.get(key)!r}, "
                f"want {value!r}"
            )
    if journal.get("pair_role", "") != expected_role:
        raise ControllerError(
            "signed-update startup journal does not prove the selected role"
        )
    peer_ip = ""
    if expected_topology == "paired":
        peer_ip = require_string(
            journal.get("peer_ip"), "signed-update startup journal peer IP"
        )
        try:
            ipaddress.ip_address(peer_ip)
        except ValueError as exc:
            raise ControllerError(
                "signed-update startup journal peer IP is invalid"
            ) from exc
    owner_id = journal.get("mutation_owner_id")
    qualifier = journal.get("manifest_qualifier")
    if not isinstance(owner_id, str) or not valid_lower_hex(owner_id, 32, 32):
        raise ControllerError("signed-update startup journal owner is invalid")
    if not isinstance(qualifier, str) or not qualifier:
        raise ControllerError("signed-update startup journal qualifier is invalid")
    for field in ("source_epoch", "target_epoch", "source_revision"):
        value = journal.get(field)
        if isinstance(value, bool) or not isinstance(value, int) or value < 0:
            raise ControllerError(
                f"signed-update startup journal {field} is invalid"
            )
    source_engine = journal.get("source_engine", "")
    if not isinstance(source_engine, str):
        raise ControllerError("signed-update startup journal source engine is invalid")

    ledger_path = os.path.join(state_dir, "service-mutations.json")
    ledger, ledger_status = secure_read_json(
        ledger_path,
        "signed-update startup mutation ledger",
        required_mode=0o600,
        required_uid=0,
    )
    if (
        not isinstance(ledger, dict)
        or ledger.get("version") != 1
        or ledger.get("active_request_id", "") != ""
        or not isinstance(ledger.get("jobs"), dict)
    ):
        raise ControllerError(
            "signed-update startup mutation ledger is not exact and idle"
        )
    job = ledger["jobs"].get(request_id)
    if not isinstance(job, dict):
        raise ControllerError(
            "signed-update startup ledger lacks the selected request"
        )
    job_expected = {
        "request_id": request_id,
        "owner_id": owner_id,
        "kind": "dns_engine_switch",
        "target": "bind",
        "package_name": qualifier,
        "status": "failed",
        "phase": "failed",
    }
    for key, value in job_expected.items():
        if job.get(key) != value:
            raise ControllerError(
                f"signed-update startup ledger job {key}={job.get(key)!r}, "
                f"want {value!r}"
            )
    parsed_times: dict[str, dt.datetime] = {}
    for field in ("started_at", "updated_at", "deadline_at", "finished_at"):
        raw_time = job.get(field)
        if not isinstance(raw_time, str) or not raw_time:
            raise ControllerError(
                f"signed-update startup ledger job {field} is absent"
            )
        try:
            parsed = dt.datetime.fromisoformat(raw_time.replace("Z", "+00:00"))
        except ValueError as exc:
            raise ControllerError(
                f"signed-update startup ledger job {field} is invalid"
            ) from exc
        if parsed.year <= 1 or parsed.tzinfo is None:
            raise ControllerError(
                f"signed-update startup ledger job {field} is zero or naive"
            )
        parsed_times[field] = parsed
    attempt = job.get("attempt")
    worker_pid = job.get("worker_pid", 0)
    if (
        not isinstance(attempt, int)
        or isinstance(attempt, bool)
        or attempt <= 0
        or not isinstance(worker_pid, int)
        or isinstance(worker_pid, bool)
        or worker_pid != 0
        or job.get("worker_started", "") != ""
        or job.get("worker_command", "") != ""
        or job.get("lease_expires_at") != "0001-01-01T00:00:00Z"
        or parsed_times["updated_at"] < parsed_times["started_at"]
        or parsed_times["deadline_at"] < parsed_times["started_at"]
        or parsed_times["finished_at"] < parsed_times["started_at"]
        or parsed_times["updated_at"] != parsed_times["finished_at"]
    ):
        raise ControllerError(
            "signed-update startup ledger job is not terminal and worker-idle"
        )
    return {
        "journal": {
            "path": journal_path,
            "phase": journal["phase"],
            "request_id": request_id,
            "owner_id": owner_id,
            "peer_ip": peer_ip,
            "device": journal_status.st_dev,
            "inode": journal_status.st_ino,
            "sha256": sha256_file(journal_path),
        },
        "ledger": {
            "path": ledger_path,
            "version": ledger["version"],
            "active_request_id": "",
            "job_status": job["status"],
            "job_phase": job["phase"],
            "worker_pid": worker_pid,
            "device": ledger_status.st_dev,
            "inode": ledger_status.st_ino,
            "sha256": sha256_file(ledger_path),
        },
        "marker_identity": {
            "mode": journal["mode"],
            "mutation_owner_id": owner_id,
            "manifest_qualifier": qualifier,
            "source_engine": source_engine,
            "target_engine": journal["target_engine"],
            "source_epoch": journal["source_epoch"],
            "target_epoch": journal["target_epoch"],
            "source_revision": journal["source_revision"],
            "topology": journal["topology"],
            "pair_role": journal.get("pair_role", ""),
        },
    }


def acquire_external_mutation_lock(path: str) -> tuple[int, dict[str, Any]]:
    import fcntl

    flags = (
        os.O_RDWR
        | getattr(os, "O_CLOEXEC", 0)
        | getattr(os, "O_NOFOLLOW", 0)
    )
    try:
        before = os.lstat(path)
        fd = os.open(path, flags)
    except OSError as exc:
        raise ControllerError(f"open external mutation lock: {exc}") from exc
    try:
        opened = os.fstat(fd)
        if (
            stat.S_ISLNK(before.st_mode)
            or not stat.S_ISREG(opened.st_mode)
            or (before.st_dev, before.st_ino) != (opened.st_dev, opened.st_ino)
            or opened.st_uid != 0
            or stat.S_IMODE(opened.st_mode) != 0o600
            or opened.st_nlink != 1
            or opened.st_size != 0
        ):
            raise ControllerError(
                "external mutation lock is not the real root-owned 0600 "
                "empty single-link path"
            )
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise ControllerError("external mutation lock is already held") from exc
        os.set_inheritable(fd, True)
        return fd, {
            "path": path,
            "device": opened.st_dev,
            "inode": opened.st_ino,
            "uid": opened.st_uid,
            "gid": opened.st_gid,
            "mode": f"{stat.S_IMODE(opened.st_mode):04o}",
            "size": opened.st_size,
            "links": opened.st_nlink,
            "fd": fd,
            "exclusive_flock": True,
        }
    except BaseException:
        os.close(fd)
        raise


def validate_settings(settings: Settings) -> dict[str, Any]:
    if not valid_lower_hex(settings.request_id, 32, 32):
        raise ControllerError(
            "request ID must be exactly 32 lowercase hexadecimal characters"
        )
    if not valid_lower_hex(settings.nonce, 32, 128):
        raise ControllerError("nonce must be 32 to 128 lowercase hexadecimal characters")
    require_real_directory(settings.command_cwd, "command working directory")
    require_real_directory(settings.state_dir, "agent state directory")
    require_clean_absolute(settings.mutation_lock, "mutation lock")
    require_real_directory(os.path.dirname(settings.mutation_lock), "mutation lock parent")
    require_clean_absolute(settings.agent_socket, "agent socket")
    require_real_directory(os.path.dirname(settings.agent_socket), "agent socket parent")
    require_clean_absolute(settings.agent_token_file, "agent token file")
    require_real_directory(
        os.path.dirname(settings.agent_token_file), "agent token file parent"
    )
    require_clean_absolute(settings.journal_path, "DNS switch journal")
    require_real_directory(os.path.dirname(settings.journal_path), "journal parent")
    for path, label in (
        (settings.marker_path, "boundary marker"),
        (settings.proof_path, "kill proof"),
        (settings.result_path, "cell result"),
        (settings.transcript_path, "raw transcript"),
    ):
        require_new_output_path(path, label)
    try:
        os.lstat(settings.agent_socket)
    except FileNotFoundError:
        pass
    except OSError as exc:
        raise ControllerError(f"inspect initial agent socket: {exc}") from exc
    else:
        raise ControllerError(f"initial agent socket must be absent: {settings.agent_socket}")
    for address, label in (
        (settings.dns_address, "DNS address"),
        (settings.panel_address, "panel address"),
    ):
        try:
            ipaddress.ip_address(address)
        except ValueError as exc:
            raise ControllerError(f"{label} must be a numeric IP address") from exc
    if settings.dns_type not in DNS_TYPES:
        raise ControllerError(f"DNS type must be one of {', '.join(DNS_TYPES)}")
    encode_dns_name(settings.dns_name)
    if not 1 <= settings.dns_port <= 65535 or not 1 <= settings.panel_port <= 65535:
        raise ControllerError("DNS and panel ports must be in 1..65535")
    timeouts = {
        "startup timeout": settings.startup_timeout,
        "boundary timeout": settings.boundary_timeout,
        "stop timeout": settings.stop_timeout,
        "kill timeout": settings.kill_timeout,
        "command timeout": settings.command_timeout,
        "recovery timeout": settings.recovery_timeout,
        "endpoint timeout": settings.endpoint_timeout,
        "DNS timeout": settings.dns_timeout,
        "stability duration": settings.stability_seconds,
        "stability interval": settings.stability_interval,
    }
    for label, value in timeouts.items():
        if (
            not isinstance(value, (int, float))
            or isinstance(value, bool)
            or value <= 0
        ):
            raise ControllerError(f"{label} must be positive")
    if settings.stability_interval > settings.stability_seconds:
        raise ControllerError(
            "stability interval must not exceed the stability duration"
        )
    needs_partition = (
        settings.cell.role != "standalone"
        and settings.cell.peer_reachability == "unreachable"
    )
    if needs_partition != (settings.peer_partition_command is not None):
        if needs_partition:
            raise ControllerError(
                "paired unreachable cell requires a peer-partition command"
            )
        raise ControllerError(
            "peer-partition command is valid only for paired unreachable cells"
        )
    expected_journal_phase(settings.cell)
    if settings.trigger_mode == "startup":
        if (
            settings.cell.driver != "signed-update-finalize"
            or settings.cell.phase != "rolled-back"
            or settings.trigger_command is not None
            or settings.source_proof_path is not None
            or len(settings.recovery_command) != 2
            or settings.recovery_command[1]
            != "--prepare-bind-generation-root-under-external-lock"
            or len(settings.tagged_agent_command) != 2
            or settings.tagged_agent_command[1]
            != "--prepare-bind-generation-root-under-external-lock"
        ):
            raise ControllerError(
                "startup trigger mode is only the exact signed-update-finalize "
                "rolled-back one-shot"
            )
    elif settings.trigger_mode == "socket":
        if (
            settings.cell.driver == "signed-update-finalize"
            or settings.trigger_command is None
            or settings.source_proof_path is None
        ):
            raise ControllerError(
                "socket trigger mode requires a non-signed-update scenario command and source proof"
            )
        require_clean_absolute(settings.source_proof_path, "source proof")
        socket_trigger_retry_contract(
            settings.trigger_command, settings.recovery_command
        )
    else:
        raise ControllerError("trigger mode must be socket or startup")
    commands: dict[str, Any] = {
        "tagged_agent": inspect_command_executable(
            settings.tagged_agent_command,
            "tagged agent",
            require_regular_path=True,
        ),
        "agent_restart": inspect_command_executable(
            settings.agent_restart_command, "agent restart"
        ),
        "panel_restart": inspect_command_executable(
            settings.panel_restart_command, "panel restart"
        ),
        "recovery_probe": inspect_command_executable(
            settings.recovery_probe_command, "recovery probe"
        ),
        "recovery": inspect_command_executable(
            settings.recovery_command,
            "recovery",
            require_regular_path=settings.trigger_mode == "startup",
        ),
    }
    if settings.trigger_command is not None:
        commands["scenario_trigger"] = inspect_command_executable(
            settings.trigger_command, "scenario trigger"
        )
        commands["socket_identity_contract"] = socket_trigger_retry_contract(
            settings.trigger_command, settings.recovery_command
        )
    if settings.peer_partition_command is not None:
        commands["peer_partition"] = inspect_command_executable(
            settings.peer_partition_command, "peer partition"
        )
    return commands


def start_tagged_agent(
    settings: Settings,
    write_fd: int,
    external_lock_fd: int | None,
    environment: Mapping[str, str],
    transcript: Transcript,
) -> subprocess.Popen[bytes]:
    pass_fds = (
        (write_fd,)
        if external_lock_fd is None
        else (write_fd, external_lock_fd)
    )
    transcript.event(
        "command-start",
        label="tagged-agent",
        argv=list(settings.tagged_agent_command),
        inherited_ready_fd=write_fd,
        inherited_external_lock_fd=external_lock_fd,
        selector_names=list(SELECTOR_NAMES),
        asynchronous=True,
    )
    return subprocess.Popen(
        list(settings.tagged_agent_command),
        stdin=subprocess.DEVNULL,
        stdout=transcript.fileno(),
        stderr=subprocess.STDOUT,
        env=dict(environment),
        cwd=settings.command_cwd,
        pass_fds=pass_fds,
        close_fds=True,
        start_new_session=True,
        umask=PRODUCTION_AGENT_UMASK,
    )


def normalize_wait_exit(returncode: int) -> int:
    return 128 - returncode if returncode < 0 else returncode


def kill_exact_stopped_child(
    process: subprocess.Popen[bytes], expected_start_ticks: str, timeout: float
) -> dict[str, Any]:
    current = read_proc_stat(process.pid)
    if current.start_ticks != expected_start_ticks:
        raise BoundaryUnverified("tagged agent PID changed immediately before SIGKILL")
    if current.state not in ("T", "t"):
        raise BoundaryUnverified(
            f"tagged agent was not stopped immediately before SIGKILL: {current.state!r}"
        )
    delivered_at = utc_now()
    try:
        os.kill(process.pid, signal.SIGKILL)
    except OSError as exc:
        raise BoundaryUnverified(f"deliver SIGKILL to tagged agent: {exc}") from exc
    try:
        raw_returncode = process.wait(timeout=timeout)
    except subprocess.TimeoutExpired as exc:
        raise BoundaryUnverified("tagged agent did not reap after SIGKILL") from exc
    exit_code = normalize_wait_exit(raw_returncode)
    if exit_code != 137:
        raise BoundaryUnverified(
            f"tagged agent exit was {exit_code} (raw {raw_returncode}), want 137"
        )
    if os.path.exists(os.path.join("/proc", str(process.pid))):
        raise BoundaryUnverified("tagged agent /proc entry remains after reap")
    return {
        "pid": process.pid,
        "process_start_ticks": expected_start_ticks,
        "state_before_signal": current.state,
        "signal": "SIGKILL",
        "signal_number": signal.SIGKILL,
        "delivered_at": delivered_at,
        "raw_returncode": raw_returncode,
        "exit_code": exit_code,
        "proc_entry_absent_after_reap": True,
    }


def cleanup_child(
    process: subprocess.Popen[bytes] | None,
    expected_start_ticks: str | None,
    transcript: Transcript,
    label: str,
) -> None:
    if process is None or process.poll() is not None:
        return
    try:
        current = read_proc_stat(process.pid)
        if (
            expected_start_ticks is not None
            and current.start_ticks != expected_start_ticks
        ):
            transcript.event(
                "cleanup-refused",
                label=label,
                pid=process.pid,
                reason="PID identity changed",
            )
            return
        if current.state in ("T", "t"):
            os.kill(process.pid, signal.SIGCONT)
        os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=5)
        transcript.event("cleanup-killed", label=label, pid=process.pid)
    except (ControllerError, OSError, subprocess.TimeoutExpired) as exc:
        transcript.event("cleanup-error", label=label, pid=process.pid, error=str(exc))


def handle_trigger_exit_before_boundary(
    trigger: subprocess.Popen[bytes],
    tagged: subprocess.Popen[bytes],
    tagged_start_ticks: str,
    error: TriggerExitedBeforeBoundary,
    result: dict[str, Any],
    transcript: Transcript,
) -> None:
    """Persist early trigger completion evidence and stop the still-tagged agent."""
    observed_returncode = finish_async_command(
        trigger, "scenario-trigger", 0.0, transcript
    )
    if observed_returncode is None:
        raise ControllerError(
            "scenario trigger disappeared while recording its pre-boundary exit"
        )
    if observed_returncode != error.raw_returncode:
        raise ControllerError(
            "scenario trigger return code changed while recording its pre-boundary exit"
        )
    report = {
        "detected_at": utc_now(),
        "reason": str(error),
        "raw_returncode": observed_returncode,
        "exit_code": normalize_wait_exit(observed_returncode),
        "tagged_agent_cleanup_requested": True,
        "tagged_agent_pid": tagged.pid,
        "tagged_agent_process_start_ticks": tagged_start_ticks,
    }
    result["scenario_trigger_returncode"] = observed_returncode
    result["trigger_exit_before_boundary"] = report
    transcript.event("scenario-trigger-exited-before-boundary", **report)
    cleanup_child(tagged, tagged_start_ticks, transcript, "tagged-agent")


def assert_unix_socket_stable(
    path: str, expected_identity: tuple[int, int], timeout: float
) -> dict[str, Any]:
    identity = wait_for_unix_socket(path, timeout)
    if identity != expected_identity:
        raise ControllerError("agent socket inode changed during the stability window")
    return {"device": identity[0], "inode": identity[1]}


def run_recovery_probe(
    settings: Settings,
    environment: Mapping[str, str],
    transcript: Transcript,
    ordinal: int,
) -> dict[str, Any]:
    try:
        command = run_bounded_command(
            settings.recovery_probe_command,
            f"recovery-probe-{ordinal}",
            settings.recovery_timeout,
            environment,
            settings.command_cwd,
            transcript,
        )
    except ControllerError as exc:
        transcript.event("recovery-probe-error", ordinal=ordinal, error=str(exc))
        return {
            "ordinal": ordinal,
            "valid": False,
            "converged": False,
            "error": str(exc),
        }
    return decode_recovery_probe(command, ordinal)


def checked_command(
    settings: Settings,
    argv: Sequence[str],
    label: str,
    timeout: float,
    environment: Mapping[str, str],
    transcript: Transcript,
    *,
    pass_fds: Sequence[int] = (),
) -> tuple[dict[str, Any], str | None]:
    try:
        command = run_bounded_command(
            argv,
            label,
            timeout,
            environment,
            settings.command_cwd,
            transcript,
            pass_fds=pass_fds,
        )
        require_command_success(command, label)
        return command.report(), None
    except (ControllerError, OSError) as exc:
        transcript.event("command-assertion-failed", label=label, error=str(exc))
        return {"argv": list(argv), "error": str(exc)}, f"{label}: {exc}"


def endpoint_check(
    label: str, operation: Any, transcript: Transcript
) -> tuple[dict[str, Any], str | None]:
    try:
        detail = operation()
        report = {"ok": True, "detail": detail}
        transcript.event("endpoint-check", label=label, **report)
        return report, None
    except (ControllerError, OSError) as exc:
        report = {"ok": False, "error": str(exc)}
        transcript.event("endpoint-check", label=label, **report)
        return report, f"{label}: {exc}"


def run_stability_window(
    settings: Settings,
    agent_identity: tuple[int, int],
    transcript: Transcript,
    peer_ip: str,
) -> tuple[dict[str, Any], list[str], list[str]]:
    deadline = time.monotonic() + settings.stability_seconds
    samples: list[dict[str, Any]] = []
    failures: list[str] = []
    peer_verification_failures: list[str] = []
    ordinal = 0
    while True:
        ordinal += 1
        sample: dict[str, Any] = {"ordinal": ordinal, "at": utc_now()}
        agent, error = endpoint_check(
            "agent-stability",
            lambda: assert_unix_socket_stable(
                settings.agent_socket,
                agent_identity,
                settings.endpoint_timeout,
            ),
            transcript,
        )
        sample["agent"] = agent
        if error:
            failures.append(f"stability sample {ordinal}: {error}")
        panel, error = endpoint_check(
            "panel-stability",
            lambda: (
                wait_for_tcp(
                    settings.panel_address,
                    settings.panel_port,
                    settings.endpoint_timeout,
                )
                or {
                    "address": settings.panel_address,
                    "port": settings.panel_port,
                }
            ),
            transcript,
        )
        sample["panel"] = panel
        if error:
            failures.append(f"stability sample {ordinal}: {error}")
        dns, error = endpoint_check(
            "dns-stability",
            lambda: query_authoritative_dns(
                settings.dns_address,
                settings.dns_port,
                settings.dns_name,
                settings.dns_type,
                settings.dns_timeout,
            ),
            transcript,
        )
        sample["dns"] = dns
        if error:
            failures.append(f"stability sample {ordinal}: {error}")
        if settings.cell.role != "standalone":
            peer, peer_error = observe_peer_once(
                peer_ip,
                settings.cell.peer_reachability,
                settings.endpoint_timeout,
            )
            sample["peer"] = peer
            transcript.event(
                "peer-stability-sample",
                ordinal=ordinal,
                observation=peer,
                error=peer_error,
            )
            if peer_error:
                peer_verification_failures.append(
                    f"stability sample {ordinal}: {peer_error}"
                )
        samples.append(sample)
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            break
        time.sleep(min(settings.stability_interval, remaining))
    return {
        "duration_seconds": settings.stability_seconds,
        "interval_seconds": settings.stability_interval,
        "samples": samples,
    }, failures, peer_verification_failures


def classify_cell_status(
    safety_failures: Sequence[str], verification_failures: Sequence[str]
) -> tuple[str, str]:
    """Keep D-021 safety failures separate from unverified dimensions."""
    safety_status = "failed" if safety_failures else "passed"
    status = "unverified" if verification_failures else safety_status
    return safety_status, status


def run_cell(settings: Settings) -> int:
    controller_identity = validate_controller_identity()
    clean_base_environment = minimal_command_environment(os.environ)
    command_evidence = validate_settings(settings)
    production_paths = validate_production_runtime_paths(
        settings.agent_token_file, controller_identity["effective_gid"]
    )
    transcript = Transcript(settings.transcript_path)
    result: dict[str, Any] = {
        "schema": RESULT_SCHEMA,
        "cell_id": settings.cell.cell_id,
        "driver": settings.cell.driver,
        "role": settings.cell.role,
        "peer_reachability": settings.cell.peer_reachability,
        "boundary": {
            "phase": settings.cell.phase,
            "edge": settings.cell.edge,
            "point": settings.cell.point,
        },
        "request_id": settings.request_id,
        "nonce": settings.nonce,
        "trigger_mode": settings.trigger_mode,
        "started_at": utc_now(),
        "status": "unverified",
        "safety_status": "unverified",
        "kill_proven": False,
        "failures": [],
        "commands": command_evidence,
        "controller_identity": controller_identity,
        "production_paths": production_paths,
        "timeouts_seconds": {
            "startup": settings.startup_timeout,
            "boundary": settings.boundary_timeout,
            "stop": settings.stop_timeout,
            "kill": settings.kill_timeout,
            "command": settings.command_timeout,
            "recovery": settings.recovery_timeout,
            "endpoint": settings.endpoint_timeout,
            "dns": settings.dns_timeout,
            "stability": settings.stability_seconds,
            "stability_interval": settings.stability_interval,
        },
    }
    tagged: subprocess.Popen[bytes] | None = None
    trigger: subprocess.Popen[bytes] | None = None
    tagged_start_ticks: str | None = None
    read_fd = -1
    write_fd = -1
    ready_fd_number = -1
    external_lock_fd = -1
    kill_proven = False
    safety_failures: list[str] = []
    verification_failures: list[str] = []
    diagnostic_failures: list[str] = []
    peer_ip = ""
    source_proof: dict[str, Any] | None = None
    socket_contract: dict[str, str] | None = None
    identity_receipt: dict[str, Any] | None = None
    boundary_identity: dict[str, Any] | None = None
    try:
        if settings.trigger_mode == "startup":
            external_lock_fd, external_lock = acquire_external_mutation_lock(
                settings.mutation_lock
            )
            result["external_mutation_lock"] = external_lock
            transcript.event("external-lock-acquired", **external_lock)
            initial_state = validate_signed_startup_preconditions(
                settings.state_dir,
                settings.journal_path,
                settings.request_id,
                settings.cell,
            )
            result["startup_preconditions"] = initial_state
            peer_ip = initial_state["journal"]["peer_ip"]
            boundary_identity = initial_state["marker_identity"]
        else:
            if settings.trigger_command is None or settings.source_proof_path is None:
                raise ControllerError("socket source proof disappeared after validation")
            socket_contract = socket_trigger_retry_contract(
                settings.trigger_command, settings.recovery_command
            )
            source_proof = validate_socket_source_proof(
                settings.source_proof_path,
                settings.cell,
                socket_contract["scenario_path"],
                socket_contract["identity_receipt_path"],
                settings.state_dir,
                settings.journal_path,
                settings.dns_address,
                settings.dns_port,
                settings.dns_name,
                settings.dns_type,
            )
            result["source_proof"] = source_proof
            peer_ip = source_proof["peer_ip"]
            transcript.event(
                "source-proof-proven",
                path=source_proof["path"],
                sha256=source_proof["sha256"],
                scenario_sha256=source_proof["scenario"]["sha256"],
                source_fixture=source_proof["source_fixture"],
                engine=source_proof["engine"],
                engine_epoch=source_proof["engine_epoch"],
                source_revision=source_proof["source_revision"],
                serving_before_tagged_agent=source_proof[
                    "serving_before_tagged_agent"
                ],
                engine_state_receipt_sha256=source_proof[
                    "engine_state_receipt"
                ]["sha256"],
            )
            if source_proof["serving_before_tagged_agent"]:
                try:
                    source_dns = query_authoritative_dns(
                        settings.dns_address,
                        settings.dns_port,
                        settings.dns_name,
                        settings.dns_type,
                        settings.dns_timeout,
                    )
                except (ControllerError, OSError) as exc:
                    raise ControllerError(
                        f"pre-kill source DNS is not independently authoritative: {exc}"
                    ) from exc
                result["source_dns_before_tagged_agent"] = {
                    "queried": True,
                    "result": source_dns,
                }
                transcript.event(
                    "source-dns-authoritative-before-tagged-agent",
                    address=settings.dns_address,
                    port=settings.dns_port,
                    name=settings.dns_name,
                    dns_type=settings.dns_type,
                    result=source_dns,
                )
            else:
                result["source_dns_before_tagged_agent"] = {
                    "queried": False,
                    "reason": "exact uninitialized source; avoid querying an unrelated resolver",
                }
                transcript.event(
                    "source-dns-preflight-not-applicable",
                    source_fixture=source_proof["source_fixture"],
                    engine_state_receipt_absent=True,
                    switch_journal_absent=True,
                )
            source_unit_states = inspect_dns_unit_states(
                settings.endpoint_timeout, clean_base_environment
            )
            validate_source_unit_states(
                source_proof["source_fixture"], source_unit_states
            )
            result["source_dns_units_before_tagged_agent"] = source_unit_states
            transcript.event(
                "source-dns-units-proven-before-tagged-agent",
                source_fixture=source_proof["source_fixture"],
                states=source_unit_states,
            )
            initial_state = describe_initial_journal(settings.journal_path)
            result["initial_journal"] = initial_state
        if settings.cell.role == "standalone":
            if peer_ip:
                raise ControllerError("standalone cell unexpectedly resolved a peer IP")
            result["peer_before_tagged_agent"] = {
                "applicable": False,
                "reason": "standalone cell",
            }
        else:
            if not peer_ip:
                raise ControllerError("paired cell has no scenario-bound peer IP")
            try:
                peer_before = read_ssh_banner(peer_ip, settings.endpoint_timeout)
            except (ControllerError, OSError) as exc:
                raise ControllerError(
                    f"peer was not SSH-reachable before tagged launch: {exc}"
                ) from exc
            result["peer_before_tagged_agent"] = {
                "applicable": True,
                "expected": "reachable",
                "observation": peer_before,
            }
            transcript.event(
                "peer-reachable-before-tagged-agent",
                cell_id=settings.cell.cell_id,
                observation=peer_before,
            )
        if (
            settings.trigger_mode == "socket"
            and result["source_proof"]["source_fixture"] == "uninitialized"
        ):
            try:
                live_port53 = probe_udp_tcp_bindability(settings.dns_address, 53)
            except OSError as exc:
                raise ControllerError(
                    f"uninitialized source global port 53 is not live-bindable: {exc}"
                ) from exc
            expected_port53 = {
                **result["source_proof"]["uninitialized_global_port53"],
                "address": settings.dns_address,
                "port": 53,
            }
            if live_port53 != expected_port53:
                raise ControllerError(
                    "live uninitialized global port-53 proof differs from durable source proof"
                )
            result["uninitialized_global_port53_before_tagged_agent"] = live_port53
            transcript.event(
                "uninitialized-global-port53-proven-immediately-before-tagged-agent",
                observation=live_port53,
            )
        else:
            result["uninitialized_global_port53_before_tagged_agent"] = {
                "applicable": False,
                "reason": "source DNS engine is intentionally serving or startup recovery mode",
            }
        transcript.event(
            "cell-start",
            cell_id=settings.cell.cell_id,
            request_id=settings.request_id,
            nonce=settings.nonce,
            trigger_mode=settings.trigger_mode,
            initial_state=initial_state,
        )
        if hasattr(os, "pipe2"):
            read_fd, write_fd = os.pipe2(os.O_CLOEXEC)
        else:
            read_fd, write_fd = os.pipe()
            os.set_inheritable(read_fd, False)
        os.set_inheritable(write_fd, True)
        ready_fd_number = write_fd
        tagged_environment = tagged_agent_environment(
            clean_base_environment,
            settings.cell,
            settings.request_id,
            settings.nonce,
            settings.marker_path,
            ready_fd_number,
            settings.state_dir,
            settings.mutation_lock,
            settings.agent_socket,
            settings.agent_token_file,
        )
        result["command_environment"] = {
            "production": {
                key: value
                for key, value in tagged_environment.items()
                if not key.startswith(SELECTOR_PREFIX)
            },
            "fault_selector_names": list(SELECTOR_NAMES),
            "inherited_caller_names": sorted(clean_base_environment),
        }
        inherited_lock_fd: int | None = None
        if external_lock_fd >= 0:
            inherited_lock_fd = external_lock_fd
            tagged_environment[EXTERNAL_LOCK_FD_ENV] = str(external_lock_fd)
        tagged = start_tagged_agent(
            settings,
            ready_fd_number,
            inherited_lock_fd,
            tagged_environment,
            transcript,
        )
        os.close(write_fd)
        write_fd = -1
        launched = wait_for_proc(tagged.pid, settings.startup_timeout)
        tagged_start_ticks = launched.start_ticks
        tagged_process_identity = validate_agent_process_identity(
            tagged.pid, controller_identity["effective_gid"]
        )
        result["tagged_agent"] = {
            "pid": tagged.pid,
            "process_start_ticks": tagged_start_ticks,
            "process_identity": tagged_process_identity,
        }
        transcript.event(
            "tagged-agent-identity",
            pid=tagged.pid,
            process_start_ticks=tagged_start_ticks,
            process_identity=tagged_process_identity,
        )
        old_socket_identity: tuple[int, int] | None = None
        ordinary = ordinary_environment(
            clean_base_environment,
            settings.state_dir,
            settings.mutation_lock,
            settings.agent_socket,
            settings.agent_token_file,
            cell=settings.cell,
            request_id=settings.request_id,
            nonce=settings.nonce,
            proof_path=settings.proof_path,
        )
        if settings.trigger_mode == "socket":
            old_socket_identity = wait_for_unix_socket(
                settings.agent_socket,
                settings.startup_timeout,
                process=tagged,
                early_ready_fd=read_fd,
            )
            readable, _, _ = select.select([read_fd], [], [], 0)
            if readable:
                raise BoundaryUnverified(
                    "kill boundary fired before the post-socket trigger handoff"
                )
            result["tagged_agent"]["socket_identity"] = {
                "device": old_socket_identity[0],
                "inode": old_socket_identity[1],
            }
            if settings.trigger_command is None:
                raise ControllerError("socket trigger command disappeared after validation")
            trigger = start_async_command(
                settings.trigger_command,
                ordinary,
                settings.command_cwd,
                transcript,
                "scenario-trigger",
            )
        else:
            result["tagged_agent"]["socket_identity"] = None
            transcript.event(
                "startup-trigger-armed",
                pid=tagged.pid,
                process_start_ticks=tagged_start_ticks,
                precondition="signed-update rolled-back one-shot",
            )
        try:
            ready_payload = read_ready_nonce(
                read_fd,
                settings.nonce,
                settings.boundary_timeout,
                tagged,
                trigger=trigger,
            )
        except TriggerExitedBeforeBoundary as exc:
            if trigger is None or tagged_start_ticks is None:
                raise ControllerError(
                    "pre-boundary trigger exit lacked child identity"
                ) from exc
            handle_trigger_exit_before_boundary(
                trigger,
                tagged,
                tagged_start_ticks,
                exc,
                result,
                transcript,
            )
            trigger = None
            raise
        result["ready_pipe"] = {
            "fd": ready_fd_number,
            "payload_sha256": hashlib.sha256(ready_payload).hexdigest(),
            "bytes": len(ready_payload),
        }
        os.close(read_fd)
        read_fd = -1
        if settings.trigger_mode == "socket":
            if socket_contract is None or source_proof is None:
                raise ControllerError(
                    "socket trigger identity prerequisites disappeared before the boundary"
                )
            try:
                identity_receipt = validate_trigger_identity_receipt(
                    socket_contract["identity_receipt_path"],
                    settings.cell,
                    settings.request_id,
                )
            except ControllerError as exc:
                raise BoundaryUnverified(
                    f"prove scenario trigger identity before SIGKILL: {exc}"
                ) from exc
            boundary_identity = validate_socket_boundary_identity(
                source_proof, identity_receipt
            )
            result["trigger_identity_receipt"] = identity_receipt
            transcript.event(
                "trigger-identity-receipt-proven-before-kill",
                path=identity_receipt["path"],
                request_id=identity_receipt["request_id"],
                owner_id=identity_receipt["owner_id"],
                manifest_qualifier=identity_receipt["manifest_qualifier"],
                sha256=identity_receipt["sha256"],
            )
        if boundary_identity is None:
            raise ControllerError("boundary journal identity was not established")
        marker = validate_marker(
            settings.marker_path,
            settings.cell,
            settings.request_id,
            settings.nonce,
            ready_fd_number,
            tagged.pid,
            tagged_start_ticks,
            settings.journal_path,
            boundary_identity,
        )
        stopped = wait_for_stopped_process(
            tagged, tagged_start_ticks, settings.stop_timeout
        )
        disk_phase = expected_journal_phase(settings.cell)
        journal_disk = validate_journal_disk_state(
            settings.journal_path,
            disk_phase,
            settings.request_id,
            boundary_identity,
        )
        marker_sha256 = sha256_file(settings.marker_path)
        result["boundary_marker"] = marker
        result["journal_at_boundary"] = journal_disk
        transcript.event(
            "boundary-proven",
            pid=tagged.pid,
            state=stopped.state,
            marker_sha256=marker_sha256,
            journal=journal_disk,
        )
        kill = kill_exact_stopped_child(
            tagged, tagged_start_ticks, settings.kill_timeout
        )
        killed_start_ticks = tagged_start_ticks
        tagged_start_ticks = None
        result["kill"] = kill
        proof = {
            "schema": PROOF_SCHEMA,
            "cell_id": settings.cell.cell_id,
            "driver": settings.cell.driver,
            "role": settings.cell.role,
            "peer_reachability": settings.cell.peer_reachability,
            "phase": settings.cell.phase,
            "point": settings.cell.point,
            "request_id": settings.request_id,
            "nonce": settings.nonce,
            "trigger_mode": settings.trigger_mode,
            "kill_proven": True,
            "pid": tagged.pid,
            "process_start_ticks": killed_start_ticks,
            "exit_code": 137,
            "raw_returncode": kill["raw_returncode"],
            "state_before_signal": stopped.state,
            "marker_path": settings.marker_path,
            "marker_sha256": marker_sha256,
            "journal_at_boundary": journal_disk,
            "boundary_journal_identity": boundary_identity,
            "identity_evidence": (
                {
                    "kind": "scenario-trigger-receipt",
                    "path": identity_receipt["path"],
                    "sha256": identity_receipt["sha256"],
                    "device": identity_receipt["device"],
                    "inode": identity_receipt["inode"],
                }
                if identity_receipt is not None
                else {
                    "kind": "signed-update-startup-journal",
                    "path": settings.journal_path,
                    "sha256": initial_state["journal"]["sha256"],
                }
            ),
            "proved_at": utc_now(),
        }
        atomic_write_new_json(settings.proof_path, proof)
        persisted_proof, _ = secure_read_json(
            settings.proof_path,
            "persisted kill proof",
            maximum=1 << 20,
            required_mode=0o600,
            required_uid=os.geteuid(),
        )
        if persisted_proof != proof:
            raise BoundaryUnverified(
                "persisted kill proof differs from the value published"
            )
        kill_proven = True
        result["kill_proven"] = True
        result["proof"] = {
            "path": settings.proof_path,
            "sha256": sha256_file(settings.proof_path),
        }
        transcript.event(
            "kill-proven",
            proof_path=settings.proof_path,
            pid=tagged.pid,
            exit_code=137,
        )
        trigger_return = finish_async_command(
            trigger, "scenario-trigger", settings.command_timeout, transcript
        )
        result["scenario_trigger_returncode"] = trigger_return
        trigger = None
        if settings.trigger_mode == "socket":
            if trigger_return != 75:
                diagnostic_failures.append(
                    f"scenario trigger exited {trigger_return}, want exact uncertain status 75 after agent kill"
                )
            if identity_receipt is None:
                raise ControllerError(
                    "pre-kill trigger identity proof disappeared after SIGKILL"
                )
        if settings.peer_partition_command is not None:
            peer_report, error = checked_command(
                settings,
                settings.peer_partition_command,
                "peer-unreachable-rendezvous-after-kill",
                settings.command_timeout,
                ordinary,
                transcript,
            )
            result["peer_partition"] = peer_report
            if error:
                verification_failures.append(error)
        if settings.cell.role != "standalone":
            peer_after, peer_error = observe_peer_after_kill(
                peer_ip,
                settings.cell.peer_reachability,
                settings.endpoint_timeout,
                settings.stability_seconds,
                settings.stability_interval,
            )
            result["peer_after_kill"] = peer_after
            transcript.event(
                "peer-reachability-after-kill",
                cell_id=settings.cell.cell_id,
                expected=settings.cell.peer_reachability,
                observation=peer_after,
                error=peer_error,
            )
            if peer_error:
                verification_failures.append(peer_error)
        else:
            result["peer_after_kill"] = {
                "applicable": False,
                "reason": "standalone cell",
            }
        recovery_attempts: list[dict[str, Any]] = []
        recovery_probes: list[dict[str, Any]] = []
        recovery: dict[str, Any]
        if settings.trigger_mode == "startup":
            if external_lock_fd < 3:
                raise ControllerError(
                    "startup recovery lost its inherited external mutation lock"
                )
            recovery_environment = dict(ordinary)
            recovery_environment[EXTERNAL_LOCK_FD_ENV] = str(external_lock_fd)
            for ordinal in (1, 2):
                recovery_report, error = checked_command(
                    settings,
                    settings.recovery_command,
                    f"startup-recovery-attempt-{ordinal}",
                    settings.recovery_timeout,
                    recovery_environment,
                    transcript,
                    pass_fds=(external_lock_fd,),
                )
                attempt = {
                    "ordinal": ordinal,
                    "command": recovery_report,
                    "error": error,
                }
                recovery_attempts.append(attempt)
                if error:
                    diagnostic_failures.append(error)
                recovery_probes.append(
                    run_recovery_probe(settings, ordinary, transcript, ordinal)
                )
                if settings.cell.role != "standalone":
                    peer_observation, peer_error = observe_peer_once(
                        peer_ip,
                        settings.cell.peer_reachability,
                        settings.endpoint_timeout,
                    )
                    attempt["peer_after_probe"] = peer_observation
                    if peer_error:
                        verification_failures.append(
                            f"recovery attempt {ordinal}: {peer_error}"
                        )
            recovery = {
                "mode": "signed-update-startup-one-shot",
                "timing": "before ordinary service restart",
                "inherited_external_lock_fd": external_lock_fd,
                "attempts": recovery_attempts,
            }
            released_fd = external_lock_fd
            os.close(external_lock_fd)
            external_lock_fd = -1
            result["external_mutation_lock"]["released_before_restart"] = True
            result["external_mutation_lock"]["released_at"] = utc_now()
            transcript.event(
                "external-lock-released-after-recovery",
                fd=released_fd,
                before_restart=True,
            )
        else:
            recovery = {
                "mode": "rpc-retry",
                "timing": "after replacement agent socket, before panel restart and final liveness",
                "attempts": recovery_attempts,
            }

        restarts: dict[str, Any] = {}
        agent_restart, error = checked_command(
            settings,
            settings.agent_restart_command,
            "agent-restart",
            settings.command_timeout,
            ordinary,
            transcript,
        )
        restarts["agent-restart"] = agent_restart
        if error:
            diagnostic_failures.append(error)
        agent_identity: tuple[int, int] | None = None
        agent_ready, error = endpoint_check(
            "agent-ready-for-recovery",
            lambda: wait_for_unix_socket(
                settings.agent_socket,
                settings.endpoint_timeout,
                previous_identity=old_socket_identity,
            ),
            transcript,
        )
        if error:
            diagnostic_failures.append(error)
        else:
            identity_detail = agent_ready["detail"]
            if (
                not isinstance(identity_detail, tuple)
                or len(identity_detail) != 2
                or any(not isinstance(item, int) for item in identity_detail)
            ):
                raise ControllerError(
                    "agent recovery readiness returned an invalid socket identity"
                )
            agent_identity = identity_detail
            agent_ready["detail"] = {
                "device": agent_identity[0],
                "inode": agent_identity[1],
            }
        recovery["agent_ready"] = agent_ready

        if settings.trigger_mode == "socket":
            if agent_identity is None:
                diagnostic_failures.append(
                    "post-restart RPC retries were not run because the replacement agent socket was not proven"
                )
            elif identity_receipt is None:
                diagnostic_failures.append(
                    "post-restart RPC retries were not run because exact mutation identity was not proven"
                )
            else:
                recovery["identity"] = {
                    "cell_id": identity_receipt["cell_id"],
                    "driver": identity_receipt["driver"],
                    "request_id": identity_receipt["request_id"],
                    "owner_id": identity_receipt["owner_id"],
                    "manifest_qualifier": identity_receipt["manifest_qualifier"],
                }
                for ordinal in (1, 2):
                    retry_report, error = checked_command(
                        settings,
                        settings.recovery_command,
                        f"post-restart-rpc-retry-{ordinal}",
                        settings.recovery_timeout,
                        ordinary,
                        transcript,
                    )
                    attempt = {
                        "ordinal": ordinal,
                        "command": retry_report,
                        "error": error,
                    }
                    recovery_attempts.append(attempt)
                    if error:
                        diagnostic_failures.append(error)
                    try:
                        receipt_after = validate_trigger_identity_receipt(
                            identity_receipt["path"],
                            settings.cell,
                            settings.request_id,
                        )
                        unchanged_fields = ("device", "inode", "sha256", "owner_id")
                        if any(
                            receipt_after[field] != identity_receipt[field]
                            for field in unchanged_fields
                        ):
                            raise ControllerError(
                                "trigger identity receipt changed during RPC retry"
                            )
                        attempt["identity_receipt_after"] = receipt_after
                    except ControllerError as exc:
                        attempt["identity_receipt_after"] = {"error": str(exc)}
                        diagnostic_failures.append(
                            f"post-retry attempt {ordinal} identity receipt: {exc}"
                        )
                    recovery_probes.append(
                        run_recovery_probe(settings, ordinary, transcript, ordinal)
                    )
                    if settings.cell.role != "standalone":
                        peer_observation, peer_error = observe_peer_once(
                            peer_ip,
                            settings.cell.peer_reachability,
                            settings.endpoint_timeout,
                        )
                        attempt["peer_after_probe"] = peer_observation
                        if peer_error:
                            verification_failures.append(
                                f"recovery attempt {ordinal}: {peer_error}"
                            )

        if len(recovery_probes) != 2:
            while len(recovery_probes) < 2:
                ordinal = len(recovery_probes) + 1
                attempt = {
                    "ordinal": ordinal,
                    "command": {"not_run": True},
                    "error": "recovery prerequisite was not proven",
                }
                recovery_attempts.append(attempt)
                recovery_probes.append(
                    run_recovery_probe(settings, ordinary, transcript, ordinal)
                )
                if settings.cell.role != "standalone":
                    peer_observation, peer_error = observe_peer_once(
                        peer_ip,
                        settings.cell.peer_reachability,
                        settings.endpoint_timeout,
                    )
                    attempt["peer_after_probe"] = peer_observation
                    if peer_error:
                        verification_failures.append(
                            f"recovery attempt {ordinal}: {peer_error}"
                        )
        result["recovery_probes"] = recovery_probes
        diagnostic_failures.extend(
            assess_recovery_probes(recovery_probes[0], recovery_probes[1])
        )

        result["recovery"] = recovery
        panel_restart, error = checked_command(
            settings,
            settings.panel_restart_command,
            "panel-restart",
            settings.command_timeout,
            ordinary,
            transcript,
        )
        restarts["panel-restart"] = panel_restart
        if error:
            diagnostic_failures.append(error)
        result["restarts"] = restarts
        agent_check, error = endpoint_check(
            "agent-after-restart",
            lambda: (
                assert_unix_socket_stable(
                    settings.agent_socket,
                    agent_identity,
                    settings.endpoint_timeout,
                )
                if agent_identity is not None
                else wait_for_unix_socket(
                    settings.agent_socket,
                    settings.endpoint_timeout,
                    previous_identity=old_socket_identity,
                )
            ),
            transcript,
        )
        if error:
            safety_failures.append(error)
        else:
            identity_detail = agent_check["detail"]
            if agent_identity is None:
                if (
                    not isinstance(identity_detail, tuple)
                    or len(identity_detail) != 2
                    or any(not isinstance(item, int) for item in identity_detail)
                ):
                    raise ControllerError(
                        "agent liveness returned an invalid socket identity"
                    )
                agent_identity = identity_detail
                agent_check["detail"] = {
                    "device": agent_identity[0],
                    "inode": agent_identity[1],
                }
            try:
                restarted_process = inspect_restarted_agent_process(
                    settings.endpoint_timeout,
                    ordinary,
                    controller_identity["effective_gid"],
                )
                result["restarted_agent_process"] = restarted_process
                transcript.event(
                    "restarted-agent-process-identity",
                    process_identity=restarted_process,
                )
            except ControllerError as exc:
                result["restarted_agent_process"] = {"error": str(exc)}
                verification_failures.append(
                    f"restarted agent process identity: {exc}"
                )
        panel_check, error = endpoint_check(
            "panel-after-restart",
            lambda: (
                wait_for_tcp(
                    settings.panel_address,
                    settings.panel_port,
                    settings.endpoint_timeout,
                )
                or {
                    "address": settings.panel_address,
                    "port": settings.panel_port,
                }
            ),
            transcript,
        )
        if error:
            safety_failures.append(error)
        dns_check, error = endpoint_check(
            "dns-after-restart",
            lambda: query_authoritative_dns(
                settings.dns_address,
                settings.dns_port,
                settings.dns_name,
                settings.dns_type,
                settings.dns_timeout,
            ),
            transcript,
        )
        if error:
            safety_failures.append(error)
        result["post_restart"] = {
            "agent": agent_check,
            "panel": panel_check,
            "dns": dns_check,
        }
        if agent_identity is not None:
            stability, stability_failures, peer_stability_failures = (
                run_stability_window(
                    settings, agent_identity, transcript, peer_ip
                )
            )
            result["stability"] = stability
            safety_failures.extend(stability_failures)
            verification_failures.extend(peer_stability_failures)
        else:
            result["stability"] = {
                "samples": [],
                "error": "agent socket never became ready after restart",
            }
            safety_failures.append(
                "stability window could not establish a stable agent identity"
            )
        result["recovery_outcome"] = summarize_recovery_outcome(
            recovery_probes[0],
            recovery_probes[1],
            bool(dns_check.get("ok")),
        )
        stability_samples = result["stability"].get("samples", [])
        result["safety_assertions"] = {
            "kill_proven": kill_proven,
            "dns_engine_serving": bool(dns_check.get("ok"))
            and all(sample.get("dns", {}).get("ok") for sample in stability_samples),
            "panel_started": bool(panel_check.get("ok"))
            and all(sample.get("panel", {}).get("ok") for sample in stability_samples),
            "agent_stayed_running": bool(agent_check.get("ok"))
            and all(sample.get("agent", {}).get("ok") for sample in stability_samples),
        }
        result["safety_status"], result["status"] = classify_cell_status(
            safety_failures, verification_failures
        )
    except (
        BoundaryUnverified,
        ControllerError,
        OSError,
        subprocess.SubprocessError,
    ) as exc:
        transcript.event(
            "cell-error",
            error_type=type(exc).__name__,
            error=str(exc),
            kill_proven=kill_proven,
        )
        verification_failures.append(str(exc))
        result["status"] = "unverified"
        result["safety_status"] = "unverified"
    finally:
        if read_fd >= 0:
            os.close(read_fd)
        if write_fd >= 0:
            os.close(write_fd)
        cleanup_child(tagged, tagged_start_ticks, transcript, "tagged-agent")
        if external_lock_fd >= 0:
            os.close(external_lock_fd)
            transcript.event(
                "external-lock-cleanup-released", fd=external_lock_fd
            )
            external_lock_fd = -1
        if trigger is not None:
            finish_async_command(
                trigger, "scenario-trigger", settings.command_timeout, transcript
            )
        result["kill_proven"] = kill_proven
        result["failures"] = safety_failures
        result["safety_failures"] = safety_failures
        result["verification_failures"] = verification_failures
        result["diagnostic_failures"] = diagnostic_failures
        result["finished_at"] = utc_now()
        transcript.event(
            "cell-finish",
            status=result["status"],
            safety_status=result["safety_status"],
            kill_proven=kill_proven,
            safety_failures=safety_failures,
            verification_failures=verification_failures,
            diagnostic_failures=diagnostic_failures,
        )
        transcript.close()
    result["transcript"] = {
        "path": settings.transcript_path,
        "sha256": sha256_file(settings.transcript_path),
    }
    atomic_write_new_json(settings.result_path, result)
    return {"passed": 0, "failed": 1, "unverified": 2}[result["status"]]


def build_argument_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--request-id", required=True)
    parser.add_argument("--nonce", required=True)
    parser.add_argument(
        "--tagged-agent-command", required=True, help="JSON argv array"
    )
    parser.add_argument(
        "--trigger-mode",
        required=True,
        choices=("socket", "startup"),
        help="socket RPC/command trigger, or signed-update startup one-shot",
    )
    parser.add_argument(
        "--trigger-command",
        help="JSON argv array; required only with --trigger-mode socket",
    )
    parser.add_argument(
        "--recovery-command",
        required=True,
        help="JSON argv array; rpc-retry for socket mode or untagged signed-update one-shot",
    )
    parser.add_argument(
        "--source-proof",
        help="root-owned canonical source-proof JSON; required only for socket mode",
    )
    parser.add_argument(
        "--agent-restart-command", required=True, help="JSON argv array"
    )
    parser.add_argument(
        "--panel-restart-command", required=True, help="JSON argv array"
    )
    parser.add_argument(
        "--recovery-probe-command", required=True, help="JSON argv array"
    )
    parser.add_argument(
        "--peer-partition-command",
        help=(
            "guest-side link-down rendezvous/check JSON argv array; "
            "paired/unreachable only; the host QMP action is external"
        ),
    )
    parser.add_argument("--command-cwd", required=True)
    parser.add_argument("--state-dir", required=True)
    parser.add_argument("--mutation-lock", required=True)
    parser.add_argument("--agent-socket", required=True)
    parser.add_argument("--agent-token-file", required=True)
    parser.add_argument("--journal", required=True)
    parser.add_argument("--marker", required=True)
    parser.add_argument("--proof", required=True)
    parser.add_argument("--result", required=True)
    parser.add_argument("--transcript", required=True)
    parser.add_argument("--dns-address", required=True)
    parser.add_argument("--dns-port", required=True, type=int)
    parser.add_argument("--dns-name", required=True)
    parser.add_argument("--dns-type", default="SOA", choices=tuple(DNS_TYPES))
    parser.add_argument("--panel-address", required=True)
    parser.add_argument("--panel-port", required=True, type=int)
    parser.add_argument("--startup-timeout", required=True, type=float)
    parser.add_argument("--boundary-timeout", required=True, type=float)
    parser.add_argument("--stop-timeout", required=True, type=float)
    parser.add_argument("--kill-timeout", required=True, type=float)
    parser.add_argument("--command-timeout", required=True, type=float)
    parser.add_argument("--recovery-timeout", required=True, type=float)
    parser.add_argument("--endpoint-timeout", required=True, type=float)
    parser.add_argument("--dns-timeout", required=True, type=float)
    parser.add_argument("--stability-seconds", required=True, type=float)
    parser.add_argument("--stability-interval", required=True, type=float)
    return parser


def settings_from_args(args: argparse.Namespace) -> Settings:
    manifest = require_clean_absolute(args.manifest, "matrix manifest")
    cell = load_cell(manifest, args.cell_id)
    peer = (
        parse_command_json(args.peer_partition_command, "peer partition command")
        if args.peer_partition_command is not None
        else None
    )
    trigger = (
        parse_command_json(args.trigger_command, "scenario trigger command")
        if args.trigger_command is not None
        else None
    )
    return Settings(
        cell=cell,
        request_id=args.request_id,
        nonce=args.nonce,
        tagged_agent_command=parse_command_json(
            args.tagged_agent_command, "tagged agent command"
        ),
        trigger_mode=args.trigger_mode,
        trigger_command=trigger,
        recovery_command=parse_command_json(
            args.recovery_command, "recovery command"
        ),
        source_proof_path=args.source_proof,
        agent_restart_command=parse_command_json(
            args.agent_restart_command, "agent restart command"
        ),
        panel_restart_command=parse_command_json(
            args.panel_restart_command, "panel restart command"
        ),
        recovery_probe_command=parse_command_json(
            args.recovery_probe_command, "recovery probe command"
        ),
        peer_partition_command=peer,
        command_cwd=args.command_cwd,
        state_dir=args.state_dir,
        mutation_lock=args.mutation_lock,
        agent_socket=args.agent_socket,
        agent_token_file=args.agent_token_file,
        journal_path=args.journal,
        marker_path=args.marker,
        proof_path=args.proof,
        result_path=args.result,
        transcript_path=args.transcript,
        dns_address=args.dns_address,
        dns_port=args.dns_port,
        dns_name=args.dns_name,
        dns_type=args.dns_type,
        panel_address=args.panel_address,
        panel_port=args.panel_port,
        startup_timeout=args.startup_timeout,
        boundary_timeout=args.boundary_timeout,
        stop_timeout=args.stop_timeout,
        kill_timeout=args.kill_timeout,
        command_timeout=args.command_timeout,
        recovery_timeout=args.recovery_timeout,
        endpoint_timeout=args.endpoint_timeout,
        dns_timeout=args.dns_timeout,
        stability_seconds=args.stability_seconds,
        stability_interval=args.stability_interval,
    )


def main(argv: Sequence[str] | None = None) -> int:
    if sys.platform != "linux":
        print("run_cell.py is a Linux-only controller", file=sys.stderr)
        return 64
    try:
        validate_controller_identity()
    except ControllerError as exc:
        print(f"run_cell.py identity preflight: {exc}", file=sys.stderr)
        return 64
    args = build_argument_parser().parse_args(argv)
    try:
        settings = settings_from_args(args)
        exit_code = run_cell(settings)
    except (ControllerError, OSError, subprocess.SubprocessError) as exc:
        print(
            json.dumps(
                {
                    "schema": RESULT_SCHEMA,
                    "cell_id": getattr(args, "cell_id", ""),
                    "status": "controller-error",
                    "error": str(exc),
                },
                sort_keys=True,
            ),
            file=sys.stderr,
        )
        return 64
    print(
        json.dumps(
            {
                "cell_id": settings.cell.cell_id,
                "exit_code": exit_code,
                "result": settings.result_path,
                "proof": settings.proof_path if exit_code != 2 else None,
                "transcript": settings.transcript_path,
            },
            sort_keys=True,
        )
    )
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
