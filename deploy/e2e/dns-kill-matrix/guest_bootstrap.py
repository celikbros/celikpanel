#!/usr/bin/env python3
"""Install matrix artifacts and prepare honest DNS source state in a fixture VM."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
from typing import Any, Iterable

import fixture


SOURCE_PROOF_SCHEMA = "celikpanel/dns-kill-source-proof/v1"
SCENARIO_SCHEMA = "celikpanel-dns-kill-matrix-trigger/v1"
CELL_RE = re.compile(r"[a-z0-9][a-z0-9_.-]{0,239}")
SHA256_RE = re.compile(r"[0-9a-f]{64}")
EARLY_UNINITIALIZED_PHASES = frozenset({"pre-intent", "intent", "target-staged"})
CRITICAL_MANAGED_PDNS_PHASES = frozenset({"source-stopped", "target-started", "rolled-back"})
# A fresh BIND install (prior state: no DNS engine, D-026) writes every forward
# phase. Standalone target-verified is a post-start cut whose manifest policy is
# driver-specific, so the proved empty source is its honest fixture there.
# source-stopped, target-started and rolled-back stay managed-pdns-required by
# matrix design (generate_manifest.py host_placement), not by product limit.
FRESH_BIND_STANDALONE_PHASES = EARLY_UNINITIALIZED_PHASES | frozenset({"target-verified"})
# Early PowerDNS -> BIND cuts, before the target ever started. The manifest
# places the standalone Debian cells at these phases as driver-specific, so an
# empty source is honest there and a genuine managed PowerDNS source is equally
# admissible: on certified Debian it makes the producer write the V2 frozen
# source journal from intent on (prepareBINDIndependentInverseJournal). The
# preparation path is the unchanged managed-pdns one. Earlier exclusion was
# only "not implemented"; Arch placements and paired roles stay refused.
EARLY_MANAGED_PDNS_BIND_PHASES = frozenset({"intent", "target-staged"})
PDNS_SWITCH_PHASES = frozenset(
    {
        "pre-intent", "intent", "target-staged", "source-stopped",
        "target-started", "target-verified", "committed",
        "rolling-back", "rolled-back",
    }
)
# A fresh standalone PowerDNS install uses the V1 journal writer in
# switchToPDNSOnCertifiedProfile: source-stopped runs a no-op stop of the
# inactive target and still writes its phase; rollback phases follow the
# tagged target-staged precursor. Every matrix phase is therefore real.
FRESH_PDNS_STANDALONE_PHASES = PDNS_SWITCH_PHASES
# Fresh paired SECONDARY installs (register rows 3 and 5) against a panel-free
# native primary peer prepared by native_primary_peer.py. BIND: the early cuts
# plus target-verified, as for the standalone fresh install; committed and
# rolling-back have no pass definition yet, and the managed-pdns-required
# phases need a managed PowerDNS secondary source (row 8). PowerDNS: the
# paired-secondary V1 path is switchToPDNSOnCertifiedProfile, the same writer
# as the fresh standalone install, and writes every phase
# (cmd/agent/dns_engine_pdns_switch.go:1838-2081; zones are retrieved before
# target-started at 2018-2023); rollback phases use the tagged target-staged
# precursor. Only peer-reachable cells are admitted: the unreachable twins need
# their own pass definition derived from the paired Reconcile behaviour.
FRESH_BIND_SECONDARY_PHASES = EARLY_UNINITIALIZED_PHASES | frozenset({"target-verified"})
FRESH_PDNS_SECONDARY_PHASES = PDNS_SWITCH_PHASES
# Fresh paired PowerDNS PRIMARY (register row 6, journal V3): the phases whose
# V3 journal write carries the kill hook (run_cell.py
# FRESH_PRIMARY_V3_BOUNDARIES; source-stopped selects target-enable-intent).
# Peer-reachable only; the native BIND secondary is native_pdns_bind_peer.py.
FRESH_PDNS_PRIMARY_PHASES = frozenset(
    {"intent", "target-staged", "source-stopped", "target-started",
     "target-verified", "committed"}
)
# (phase, edge) -> variant, mirrored from run_cell.py FRESH_PRIMARY_V3_BOUNDARIES.
FRESH_PDNS_PRIMARY_POST_START_PHASES = frozenset(
    {"target-started", "target-verified", "committed"}
)
PAIRED_SECONDARY_DRIVERS = frozenset({"bind", "pdns-switch"})
PEER_ENGINES = ("bind", "pdns")
# Catalog producer the native primary peer serves (native_primary_peer.py
# --catalog-format). A PowerDNS peer defaults to its native PRODUCER catalog,
# which is what a real PowerDNS primary, a CelikPanel one included, publishes.
PEER_CATALOG_FORMATS = ("bind", "pdns-native")
DEFAULT_PEER_CATALOG_FORMAT = {"bind": "bind", "pdns": "pdns-native"}
# The Agent logs the accepted peer catalog format once per operation; the
# controller judges that line against the format the host prepared.
PEER_CATALOG_BIND_FLAG = "--peer-catalog-format-bind"
PEER_CATALOG_PDNS_FLAG = "--peer-catalog-format-pdns-native"
PEER_CATALOG_FLAGS = {"bind": PEER_CATALOG_BIND_FLAG, "pdns-native": PEER_CATALOG_PDNS_FLAG}
# A PowerDNS consumer writes {"consumer": {"unique": "<label>."}} on the
# consumed member row; the label is the member's node label in the peer's
# catalog: SHA-224 hex (BIND format) or 32-character base32hex (PowerDNS).
PEER_CATALOG_MEMBER_LABEL_PREFIX = "--peer-catalog-member-label="
MEMBER_LABEL_RE = re.compile(r"[0-9a-f]{56}|[0-9a-v]{32}")
SECONDARY_LOCAL_NS = "ns2.s1-kill.test"
SECONDARY_PEER_NS = "ns1.s1-kill.test"
PRIMARY_MEMBER_SOA_SERIAL = 2026083101
# Rows 12 and 14: provenance-only fixtures on existing Debian standalone BIND
# cells (no manifest change). The takeover is byte-identical to a fresh install
# on the wire; the reinstall is mode "reinstall" with an equal BIND epoch.
UNMANAGED_BIND_STOPPED = "unmanaged-bind-stopped"
MANAGED_BIND_ABSENT = "managed-bind-absent"
PROVENANCE_BIND_CELLS = {
    UNMANAGED_BIND_STOPPED: "bind__target-staged__after-write__standalone__peer-reachable",
    MANAGED_BIND_ABSENT: "bind__target-staged__after-write__standalone__peer-reachable",
}
PDNS_ADOPT_PHASES = frozenset(
    {
        "pre-intent",
        "intent",
        "target-verified",
        "committed",
        "rolling-back",
        "rolled-back",
    }
)
INDEPENDENT_PDNS_HANDOFF_CELL = (
    "pdns-adopt__rolling-back__after-write__standalone__peer-reachable"
)
INDEPENDENT_BIND_HANDOFF_CELL = (
    "bind__rolling-back__after-write__standalone__peer-reachable"
)
INDEPENDENT_HANDOFF_CELLS = frozenset({
    INDEPENDENT_PDNS_HANDOFF_CELL, INDEPENDENT_BIND_HANDOFF_CELL,
})
INDEPENDENT_PDNS_HANDOFF_FLAG = "--stop-after-kill-for-independent-recovery"
LATER_BIND_ROLLBACK_FLAG = "--bind-rollback-after-target-started"
# Agent decides, owner executes: the controller restarts the ordinary Agent,
# leaves it running and runs the owner command the Agent names. The admitted
# cells, their source fixture and manifest policy must equal run_cell.py's
# OWNER_INVERSE_ADMISSIONS (checked by the tests).
OWNER_INVERSE_FLAG = "--owner-inverse-after-restart"
OWNER_INVERSE_CRITICAL_PHASES = frozenset({"source-stopped", "target-started"})
# cell ID -> (source fixture, manifest source policy, target-started precursor)
OWNER_INVERSE_ADMISSIONS = {
    "bind__intent__after-write__standalone__peer-reachable":
        ("managed-pdns", "driver-specific", False),
    "bind__target-staged__after-write__standalone__peer-reachable":
        ("managed-pdns", "driver-specific", False),
    "bind__target-staged__before-write__standalone__peer-unreachable":
        ("managed-pdns", "driver-specific", False),
    "bind__source-stopped__after-write__standalone__peer-reachable":
        ("managed-pdns", "managed-pdns-required", False),
    "bind__source-stopped__before-write__standalone__peer-reachable":
        ("managed-pdns", "managed-pdns-required", False),
    "bind__target-started__after-write__standalone__peer-reachable":
        ("managed-pdns", "managed-pdns-required", False),
    "bind__target-started__before-write__standalone__peer-reachable":
        ("managed-pdns", "managed-pdns-required", False),
    "bind__rolled-back__before-write__standalone__peer-reachable":
        ("managed-pdns", "managed-pdns-required", False),
    "bind__rolled-back__after-write__standalone__peer-reachable":
        ("managed-pdns", "managed-pdns-required", False),
    "bind__rolling-back__after-write__standalone__peer-reachable":
        ("owner-bind", "driver-specific", True),
}
OWNER_INVERSE_CELLS = frozenset(OWNER_INVERSE_ADMISSIONS)
# The PowerDNS -> BIND switch cells (managed PowerDNS source, V2 journal).
V2_SWITCH_OWNER_CELLS = frozenset(
    cell_id for cell_id, admission in OWNER_INVERSE_ADMISSIONS.items()
    if admission[0] == "managed-pdns"
)
# Row 13 with a running Agent: the Agent rolls back by itself at restart and
# the same request converges forward on rpc-retry; no owner command applies.
# Since 3cc2de22 the restarted Agent also finishes a V1 adoption journal that
# is already at rolled-back (re-proof, no effect, verdict, journal retired),
# so that cut belongs here and no longer to the owner-inverse flow.
STARTUP_ROLLBACK_FLAG = "--expect-agent-startup-rollback"
PDNS_ADOPTION_ROLLED_BACK_CELL = (
    "pdns-adopt__rolled-back__after-write__standalone__peer-reachable"
)
STARTUP_ROLLBACK_CELLS = frozenset({
    "pdns-adopt__intent__after-write__standalone__peer-reachable",
    "pdns-adopt__rolling-back__after-write__standalone__peer-reachable",
    PDNS_ADOPTION_ROLLED_BACK_CELL,
})
# After a passed V2 owner rollback (and an optional reboot), request the same
# switch again as a NEW request; it must complete forward.
RETRY_SWITCH_FLAG = "--retry-switch-after-rollback"
# Reboot during recovery. The controller exits REBOOT_REQUESTED_EXIT after it
# persisted a checkpoint; run-prepared reboots exactly this guest through
# fixture.reboot_guest and runs the prepared argv again with RESUME_FLAG.
REBOOT_BEFORE_OWNER_FLAG = "--reboot-before-owner-command"
REBOOT_AFTER_RECOVERY_FLAG = "--reboot-after-recovery"
# Diagnostic only: reboot after recovery even when the pre-reboot verdict is
# not a complete pass; the post-reboot state is recorded with judged: false.
REBOOT_EVEN_IF_FAILED_FLAG = "--reboot-even-if-failed"
# Stop and disable the Panel and Agent units before the after-recovery reboot;
# the DNS daemon must then serve alone and the second window judges only DNS.
DISABLE_MANAGEMENT_FLAG = "--disable-management-before-reboot"
RESUME_FLAG = "--resume-after-reboot"
# Fresh paired PowerDNS primary: owner edit between the kill and the Agent
# restart (run-prepared --owner-edit {config,sql}), and the Agent-released
# owner recovery of a pre-start config edit.
OWNER_EDIT_FLAGS = {"config": "--owner-edit-config", "sql": "--owner-edit-sql"}
OWNER_RELEASE_FLAG = "--owner-release-recovery"
# Stopped-BIND takeover prepared with --owner-directives.
OWNER_DIRECTIVES_FLAG = "--expect-owner-directives"
OWNER_DIRECTIVES_STAGE_NAME = "owner-bind-directives"
GATE_CLOSED_EXIT = 4
FRESH_PRIMARY_EVIDENCE_DIRECTORY = "fresh-primary-peer"
REBOOT_REQUESTED_EXIT = 3
# Canonical order of the flags the host passes to the guest program.
PREPARED_FLAG_ORDER = (
    INDEPENDENT_PDNS_HANDOFF_FLAG,
    LATER_BIND_ROLLBACK_FLAG,
    OWNER_INVERSE_FLAG,
    STARTUP_ROLLBACK_FLAG,
    OWNER_EDIT_FLAGS["config"],
    OWNER_EDIT_FLAGS["sql"],
    OWNER_RELEASE_FLAG,
    OWNER_DIRECTIVES_FLAG,
    PEER_CATALOG_BIND_FLAG,
    PEER_CATALOG_PDNS_FLAG,
    RETRY_SWITCH_FLAG,
    REBOOT_BEFORE_OWNER_FLAG,
    REBOOT_AFTER_RECOVERY_FLAG,
    REBOOT_EVEN_IF_FAILED_FLAG,
    DISABLE_MANAGEMENT_FLAG,
    RESUME_FLAG,
)
RECOVERY_KIT_NAME = "recovery-kit.tar.gz"
OWNER_RECOVERY_LAUNCHER = "/usr/libexec/celikpanel/recovery"
BIND_SOURCE_INVERSE_MARKER = "celikpanel-bind-source-inverse/v1"
BIND_ADOPTION_INVERSE_COMMAND = "check-bind-adoption-inverse-v1"
BIND_ADOPTION_INVERSE_MARKER = "celikpanel-bind-adoption-inverse/v1"
SOURCE_FIXTURE_POLICIES = frozenset(
    {
        "driver-specific",
        "uninitialized-permitted-noncritical",
        "managed-pdns-required",
    }
)
NODE_FOR_PLACEMENT = {"arch": "arch", "debian-13": "debian13"}
STAGE_PREFIX = "/var/tmp/celikpanel-dns-kill-bootstrap-"
ZONE_NAME = "s1-kill.test"
QUERY_NAME = "www.s1-kill.test"


RUN_PREPARED_CODE = r"""import json
import os
import re
import stat
import sys

path = "/var/lib/celikpanel-dns-kill-matrix/controller-argv.json"
fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
try:
    info = os.fstat(fd)
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0
            or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1):
        raise SystemExit("prepared controller argv has unsafe ownership")
    with os.fdopen(fd, "r", encoding="utf-8") as handle:
        fd = -1
        argv = json.load(handle)
finally:
    if fd >= 0:
        os.close(fd)
if (not isinstance(argv, list) or len(argv) < 4
        or any(not isinstance(value, str) or chr(0) in value for value in argv)
        or argv[0] != "/opt/celikpanel/libexec/dns-kill-run-cell.py"
        or argv.count("--cell-id") != 1):
    raise SystemExit("prepared controller argv has unexpected shape")
index = argv.index("--cell-id")
if index + 1 >= len(argv) or argv[index + 1] != sys.argv[1]:
    raise SystemExit("prepared controller argv belongs to another cell")
cell = sys.argv[1]
flags = sys.argv[2:]
handoff = "--stop-after-kill-for-independent-recovery"
later = "--bind-rollback-after-target-started"
owner = "--owner-inverse-after-restart"
startup = "--expect-agent-startup-rollback"
owner_config = "--owner-edit-config"
owner_sql = "--owner-edit-sql"
owner_release = "--owner-release-recovery"
directives = "--expect-owner-directives"
peer_bind = "--peer-catalog-format-bind"
peer_pdns = "--peer-catalog-format-pdns-native"
retry = "--retry-switch-after-rollback"
reboot_before = "--reboot-before-owner-command"
reboot_after = "--reboot-after-recovery"
even_if_failed = "--reboot-even-if-failed"
disable = "--disable-management-before-reboot"
resume = "--resume-after-reboot"
order = [handoff, later, owner, startup, owner_config, owner_sql, owner_release, directives,
         peer_bind, peer_pdns, retry, reboot_before, reboot_after, even_if_failed, disable,
         resume]
label_prefix = "--peer-catalog-member-label="
labels = [flag for flag in flags if flag.startswith(label_prefix)]
plain = [flag for flag in flags if not flag.startswith(label_prefix)]
if (len(sys.argv) < 2 or len(set(plain)) != len(plain)
        or any(flag not in order for flag in plain)
        or plain != [flag for flag in order if flag in plain]):
    raise SystemExit("unexpected prepared controller invocation")
chosen = set(plain)
if labels:
    value = labels[0][len(label_prefix):]
    if (len(labels) != 1 or not re.fullmatch("[0-9a-f]{56}|[0-9a-v]{32}", value)
            or not cell.startswith("pdns-switch__") or "__paired-secondary__" not in cell
            or not chosen & {"--peer-catalog-format-bind", "--peer-catalog-format-pdns-native"}):
        raise SystemExit("the peer catalog member label applies only to a PowerDNS paired secondary")
if (any(flag in argv for flag in order) or "--reboot-dir" in argv
        or any(item.startswith(label_prefix) for item in argv)):
    raise SystemExit("prepared controller argv already carries a mode flag")
bind_handoff = "bind__rolling-back__after-write__standalone__peer-reachable"
handoff_cells = {
    "pdns-adopt__rolling-back__after-write__standalone__peer-reachable",
    bind_handoff,
}
owner_cells = set(@OWNER_CELLS@)
startup_cells = set(@STARTUP_CELLS@)
retry_cells = set(@RETRY_CELLS@)
if owner in chosen:
    if (cell not in owner_cells or handoff in chosen or startup in chosen
            or (later in chosen) != (cell == bind_handoff)):
        raise SystemExit("owner inverse after restart is not valid for prepared cell")
if handoff in chosen and cell not in handoff_cells:
    raise SystemExit("independent recovery handoff is not valid for prepared cell")
if later in chosen and (cell != bind_handoff or not chosen & {handoff, owner}):
    raise SystemExit("later BIND rollback requires the exact handoff cell")
if startup in chosen and (cell not in startup_cells or handoff in chosen):
    raise SystemExit("agent startup rollback is not valid for prepared cell")
if chosen & {peer_bind, peer_pdns} and (
        "__paired-secondary__" not in cell or {peer_bind, peer_pdns} <= chosen):
    raise SystemExit("one peer catalog format applies only to a paired-secondary cell")
if retry in chosen and (owner not in chosen or cell not in retry_cells):
    raise SystemExit("retry after rollback requires the owner inverse flow of a V2 switch cell")
if reboot_before in chosen and owner not in chosen:
    raise SystemExit("reboot before the owner command requires the owner inverse flow")
if chosen & {reboot_before, reboot_after} and handoff in chosen:
    raise SystemExit("reboot steps exclude the independent handoff")
if even_if_failed in chosen and reboot_after not in chosen:
    raise SystemExit("rebooting after a failed flow requires the after-recovery reboot")
if disable in chosen and (reboot_after not in chosen or owner in chosen):
    raise SystemExit("disabling management requires the after-recovery reboot of the rpc-retry flow")
if resume in chosen and not chosen & {reboot_before, reboot_after}:
    raise SystemExit("resume requires the reboot flag of the suspended run")
fresh_primary = (cell.startswith("pdns-switch__")
                 and cell.endswith("__paired-primary__peer-reachable"))
if chosen & {owner_config, owner_sql} and (
        not fresh_primary or {owner_config, owner_sql} <= chosen
        or chosen & {handoff, later, owner, startup, retry, reboot_before, reboot_after}):
    raise SystemExit("one owner edit applies only to a fresh paired PowerDNS primary cell without reboot")
if owner_release in chosen and owner_config not in chosen:
    raise SystemExit("the owner release recovery needs the owner configuration edit")
if directives in chosen and cell != "bind__target-staged__after-write__standalone__peer-reachable":
    raise SystemExit("owner directives apply only to the stopped-BIND takeover cell")
if chosen & {handoff, owner, startup, reboot_before, reboot_after, owner_config, owner_sql,
             directives}:
    if argv.count("--trigger-mode") != 1:
        raise SystemExit("prepared controller mode requires one trigger mode")
    trigger_index = argv.index("--trigger-mode")
    if argv[trigger_index + 1:trigger_index + 2] != ["socket"]:
        raise SystemExit("prepared controller mode requires a socket trigger")
argv.extend(flags)
if chosen & {reboot_before, reboot_after}:
    if argv.count("--result") != 1:
        raise SystemExit("prepared controller argv has no single result path")
    result = argv[argv.index("--result") + 1:argv.index("--result") + 2]
    if not result or not os.path.isabs(result[0]):
        raise SystemExit("prepared controller result path is not absolute")
    argv.extend(["--reboot-dir", os.path.dirname(result[0])])
os.execv(argv[0], argv)
""".replace(
    "@OWNER_CELLS@", repr(sorted(OWNER_INVERSE_CELLS))
).replace(
    "@STARTUP_CELLS@", repr(sorted(STARTUP_ROLLBACK_CELLS))
).replace(
    "@RETRY_CELLS@", repr(sorted(V2_SWITCH_OWNER_CELLS))
)

class BootstrapError(RuntimeError):
    pass


def read_json(path: Path, label: str) -> Any:
    path = path.resolve(strict=True)
    info = path.lstat()
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise BootstrapError(f"{label} must be a regular non-symlink file")
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise BootstrapError(f"read {label}: {exc}") from exc


def regular_file(path: Path, label: str, *, executable: bool = False) -> Path:
    try:
        clean = path.resolve(strict=True)
        info = path.lstat()
    except OSError as exc:
        raise BootstrapError(f"inspect {label}: {exc}") from exc
    if clean != path.absolute() or stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise BootstrapError(f"{label} must be a clean regular non-symlink file")
    if info.st_nlink != 1 or info.st_size <= 0:
        raise BootstrapError(f"{label} must be a nonempty single-link file")
    if executable and info.st_mode & 0o111 == 0:
        raise BootstrapError(f"{label} is not executable")
    return clean


def web_members(root: Path) -> list[tuple[Path, str]]:
    clean = root.resolve(strict=True)
    if clean != root.absolute() or not clean.is_dir() or root.is_symlink():
        raise BootstrapError("web directory must be a clean real directory")
    result: list[tuple[Path, str]] = []
    for current, directories, files in os.walk(clean, followlinks=False):
        directories.sort()
        files.sort()
        current_path = Path(current)
        relative_dir = current_path.relative_to(clean)
        for name in directories:
            path = current_path / name
            info = path.lstat()
            if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
                raise BootstrapError(f"web tree contains a non-directory or symlink: {path}")
            relative = (relative_dir / name).as_posix()
            if "\n" in relative or "\r" in relative:
                raise BootstrapError("web tree contains a newline in a path")
            result.append((path, relative + "/"))
        for name in files:
            path = current_path / name
            info = path.lstat()
            if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
                raise BootstrapError(f"web tree contains a special file or symlink: {path}")
            relative = (relative_dir / name).as_posix()
            if "\n" in relative or "\r" in relative:
                raise BootstrapError("web tree contains a newline in a path")
            result.append((path, relative))
    if not any(relative == "index.html" for _, relative in result):
        raise BootstrapError("web tree does not contain index.html")
    return sorted(result, key=lambda item: item[1])


def write_deterministic_web_tar(root: Path, target: Path) -> None:
    members = web_members(root)
    with tarfile.open(target, "w", format=tarfile.PAX_FORMAT) as archive:
        for path, relative in members:
            info = path.lstat()
            member = tarfile.TarInfo(relative)
            member.uid = 0
            member.gid = 0
            member.uname = "root"
            member.gname = "root"
            member.mtime = 0
            if relative.endswith("/"):
                member.type = tarfile.DIRTYPE
                member.mode = 0o755
                archive.addfile(member)
                continue
            member.type = tarfile.REGTYPE
            member.mode = 0o644
            member.size = info.st_size
            with path.open("rb") as handle:
                archive.addfile(member, handle)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        while chunk := handle.read(1 << 20):
            digest.update(chunk)
    return digest.hexdigest()


def load_manifest_cell(manifest_path: Path, cell_id: str) -> dict[str, Any]:
    if CELL_RE.fullmatch(cell_id) is None:
        raise BootstrapError("cell ID is not canonical")
    manifest = read_json(manifest_path, "matrix manifest")
    if not isinstance(manifest, dict) or manifest.get("schema") != "celikpanel/dns-kill-matrix/v1":
        raise BootstrapError("matrix manifest schema is unsupported")
    matches = [cell for cell in manifest.get("cells", []) if cell.get("id") == cell_id]
    if len(matches) != 1:
        raise BootstrapError("matrix cell ID is missing or duplicated")
    cell = matches[0]
    if cell.get("status") != "runnable" or cell.get("applicability") != "verified":
        raise BootstrapError("guest state may only be prepared for a verified runnable cell")
    return cell


def early_managed_pdns_bind_cell(
    cell: dict[str, Any], phase: Any, source_policy: Any
) -> bool:
    """Exact standalone intent/target-staged BIND cell for a managed source."""

    boundary = cell.get("boundary", {})
    edge = boundary.get("edge")
    peer = cell.get("peer_reachability")
    return (
        source_policy == "driver-specific"
        and phase in EARLY_MANAGED_PDNS_BIND_PHASES
        and cell.get("driver") == "bind"
        and cell.get("role") == "standalone"
        and edge in {"before-write", "after-write"}
        and peer in {"reachable", "unreachable"}
        and cell.get("id") == f"bind__{phase}__{edge}__standalone__peer-{peer}"
        and cell.get("fault_selector") == {
            "phase": phase, "point": edge.replace("-", "_"),
        }
    )


def fresh_pdns_primary_cell(cell: dict[str, Any]) -> bool:
    """An exact admitted fresh paired PowerDNS primary cell (journal V3)."""

    boundary = cell.get("boundary", {})
    phase, edge = boundary.get("phase"), boundary.get("edge")
    return (
        cell.get("driver") == "pdns-switch"
        and cell.get("role") == "paired-primary"
        and cell.get("peer_reachability") == "reachable"
        and phase in FRESH_PDNS_PRIMARY_PHASES
        and edge in ("before-write", "after-write")
        and cell.get("id") == f"pdns-switch__{phase}__{edge}__paired-primary__peer-reachable"
        and cell.get("fault_selector") == {"phase": phase, "point": edge.replace("-", "_")}
        and cell.get("placement", {}).get("source_fixture_policy") == "driver-specific"
        and cell.get("placement", {}).get("kill_host") == "debian-13"
    )


def fresh_pdns_primary_variant(cell: dict[str, Any]) -> str:
    """pre-journal, pre-start or post-start, as run_cell.py judges the cut."""

    if not fresh_pdns_primary_cell(cell):
        raise BootstrapError("not an admitted fresh paired PowerDNS primary cell")
    boundary = cell["boundary"]
    if boundary["phase"] == "intent" and boundary["edge"] == "before-write":
        return "pre-journal"
    if boundary["phase"] in FRESH_PDNS_PRIMARY_POST_START_PHASES:
        return "post-start"
    return "pre-start"


def paired_secondary_cell(cell: dict[str, Any]) -> bool:
    return (
        cell.get("role") == "paired-secondary"
        and cell.get("driver") in PAIRED_SECONDARY_DRIVERS
    )


def validate_paired_secondary_shape(
    cell: dict[str, Any], node: str, source_fixture: str, phases: frozenset[str]
) -> None:
    """Exact fresh paired-secondary cell: uninitialized source, reachable peer."""

    placement = cell.get("placement", {})
    boundary = cell.get("boundary", {})
    phase, edge = boundary.get("phase"), boundary.get("edge")
    peer = cell.get("peer_reachability")
    expected_id = (
        f"{cell.get('driver')}__pre-intent__paired-secondary__peer-{peer}"
        if phase == "pre-intent"
        else f"{cell.get('driver')}__{phase}__{edge}__paired-secondary__peer-{peer}"
    )
    if source_fixture != "uninitialized":
        raise BootstrapError(
            "a paired-secondary cell is prepared only as a fresh install "
            "(--source-fixture uninitialized); a managed PowerDNS secondary source "
            "producer does not exist yet (register row 8)"
        )
    if placement.get("source_fixture_policy") not in {
        "driver-specific", "uninitialized-permitted-noncritical",
    }:
        raise BootstrapError(
            "paired-secondary managed-pdns-required cells need a managed PowerDNS "
            "secondary source producer (register row 8); nothing was prepared"
        )
    if phase not in phases:
        raise BootstrapError(
            f"paired-secondary {cell.get('driver')} phase {phase!r} has no pass "
            "definition yet; admitted: " + ", ".join(sorted(phases))
        )
    if peer != "reachable":
        raise BootstrapError(
            "paired-secondary peer-unreachable cells stay refused until they have "
            "their own pass definition (derived from the paired Reconcile behaviour)"
        )
    if cell.get("id") != expected_id:
        raise BootstrapError("paired-secondary cell ID differs from its boundary")
    peer_node = NODE_FOR_PLACEMENT.get(placement.get("dns_peer_host"))
    if peer_node is None or peer_node == node:
        raise BootstrapError("paired-secondary placement must name a distinct peer guest")


def validate_bind_cell(cell: dict[str, Any], node: str, source_fixture: str) -> None:
    if cell.get("driver") != "bind" or cell.get("role") not in {
        "standalone",
        "paired-primary",
        "paired-secondary",
    }:
        raise BootstrapError("BIND bootstrap supports standalone, paired-primary or paired-secondary only")
    placement = cell.get("placement", {})
    expected_node = NODE_FOR_PLACEMENT.get(placement.get("kill_host"))
    if node != expected_node:
        raise BootstrapError(
            f"cell placement requires node {expected_node!r}, not {node!r}"
        )
    phase = cell.get("boundary", {}).get("phase")
    source_policy = placement.get("source_fixture_policy")
    if source_policy not in SOURCE_FIXTURE_POLICIES:
        raise BootstrapError("BIND source fixture policy is not canonical")
    if cell.get("role") == "paired-secondary":
        validate_paired_secondary_shape(
            cell, node, source_fixture, FRESH_BIND_SECONDARY_PHASES
        )
        return
    if source_fixture in PROVENANCE_BIND_CELLS:
        if not (
            node == "debian13"
            and cell.get("id") == PROVENANCE_BIND_CELLS[source_fixture]
            and cell.get("role") == "standalone"
            and source_policy == "driver-specific"
            and cell.get("fault_selector") == {
                "phase": "target-staged", "point": "after_write",
            }
        ):
            raise BootstrapError(
                f"{source_fixture} is prepared only for the Debian cell "
                f"{PROVENANCE_BIND_CELLS[source_fixture]}"
            )
        return
    if cell.get("role") == "paired-primary":
        if node != "arch" or source_fixture != "uninitialized":
            raise BootstrapError("paired-primary bootstrap requires an uninitialized Arch source")
    if source_fixture == "uninitialized":
        allowed_phases = (
            FRESH_BIND_STANDALONE_PHASES
            if cell.get("role") == "standalone"
            else EARLY_UNINITIALIZED_PHASES
        )
        if (
            source_policy
            not in {"driver-specific", "uninitialized-permitted-noncritical"}
            or phase not in allowed_phases
        ):
            raise BootstrapError(
                "uninitialized source requires a driver-supported fixture policy "
                "and an early BIND phase or standalone target-verified"
            )
    elif source_fixture == "owner-bind":
        if not (
            node == "debian13"
            and source_policy == "driver-specific"
            and cell.get("id") == INDEPENDENT_BIND_HANDOFF_CELL
            and cell.get("role") == "standalone"
            and cell.get("peer_reachability") == "reachable"
            and phase == "rolling-back"
            and cell.get("boundary", {}).get("edge") == "after-write"
            and cell.get("fault_selector") == {
                "phase": "rolling-back", "point": "after_write",
            }
        ):
            raise BootstrapError("owner BIND requires the exact Debian standalone rollback handoff cell")
    elif source_fixture == "managed-pdns":
        if (
            node != "debian13"
            or not (
                (source_policy == "managed-pdns-required"
                 and phase in CRITICAL_MANAGED_PDNS_PHASES)
                or early_managed_pdns_bind_cell(cell, phase, source_policy)
                or (
                    source_policy == "driver-specific"
                    and phase == "rolling-back"
                    and cell.get("id") == INDEPENDENT_BIND_HANDOFF_CELL
                    and cell.get("role") == "standalone"
                    and cell.get("peer_reachability") == "reachable"
                    and cell.get("boundary", {}).get("edge") == "after-write"
                    and cell.get("fault_selector") == {
                        "phase": "rolling-back", "point": "after_write",
                    }
                )
            )
        ):
            raise BootstrapError(
                "managed PowerDNS source preinstall requires the managed fixture "
                "policy and a supported critical BIND cell, or a driver-specific "
                "standalone intent/target-staged BIND cell, on certified Debian"
            )
    else:
        raise BootstrapError(f"unsupported BIND source fixture {source_fixture!r}")


def validate_pdns_adopt_cell(
    cell: dict[str, Any], node: str, source_fixture: str
) -> None:
    if cell.get("driver") != "pdns-adopt" or cell.get("role") != "standalone":
        raise BootstrapError(
            "this preparation path requires a standalone PowerDNS adoption cell"
        )
    placement = cell.get("placement", {})
    expected_node = NODE_FOR_PLACEMENT.get(placement.get("kill_host"))
    if node != expected_node or node != "debian13":
        raise BootstrapError(
            "PowerDNS adoption placement requires certified Debian 13"
        )
    if (
        source_fixture != "external-pdns-adoption"
        or placement.get("source_fixture_policy") != "driver-specific"
    ):
        raise BootstrapError(
            "PowerDNS adoption requires its driver-specific external preimage"
        )
    phase = cell.get("boundary", {}).get("phase")
    if phase not in PDNS_ADOPT_PHASES:
        raise BootstrapError(
            "PowerDNS adoption cell has an unsupported runnable phase"
        )



def validate_pdns_switch_cell(
    cell: dict[str, Any], node: str, source_fixture: str
) -> None:
    if cell.get("driver") != "pdns-switch" or cell.get("role") not in {
        "standalone", "paired-primary", "paired-secondary",
    }:
        raise BootstrapError("PowerDNS switch fixture supports standalone, paired-primary or paired-secondary only")
    placement = cell.get("placement", {})
    if cell.get("role") == "paired-secondary":
        # Fresh install only: pdns.service inactive and no database, so the
        # Agent journals under pdns-switch, not pdns-secondary-reconfigure
        # (classifyPDNSPairSecondarySource).
        if node != "debian13" or NODE_FOR_PLACEMENT.get(placement.get("kill_host")) != node:
            raise BootstrapError("PowerDNS paired secondary requires the certified Debian placement")
        validate_paired_secondary_shape(
            cell, node, source_fixture, FRESH_PDNS_SECONDARY_PHASES
        )
        return
    if (
        node != "debian13"
        or NODE_FOR_PLACEMENT.get(placement.get("kill_host")) != node
        or placement.get("source_fixture_policy") != "driver-specific"
        or source_fixture not in {"managed-bind", "uninitialized"}
    ):
        raise BootstrapError("PowerDNS switch requires the certified Debian placement")
    phase = cell.get("boundary", {}).get("phase")
    if source_fixture == "uninitialized" and not (
        (
            cell.get("role") == "paired-primary"
            and phase in FRESH_PDNS_PRIMARY_PHASES
            and cell.get("peer_reachability") != "unreachable"
        )
        or (cell.get("role") == "standalone" and phase in FRESH_PDNS_STANDALONE_PHASES)
    ):
        # Fresh paired-primary runs on the V3 journal: only its hooked writes
        # are cells, and only against a reachable native secondary.
        raise BootstrapError(
            "fresh PowerDNS fixture is limited to standalone cells and the peer-reachable "
            "paired-primary V3 phases " + ", ".join(sorted(FRESH_PDNS_PRIMARY_PHASES))
        )
    if phase not in PDNS_SWITCH_PHASES:
        raise BootstrapError("PowerDNS switch fixture has an unsupported matrix phase")


def secondary_pair_identity(node: str) -> dict[str, str]:
    """The Panel's secondary mapping (cmd/panel/dns_engine.go:1498-1502)."""

    if node not in {"arch", "debian13"}:
        raise BootstrapError("unsupported paired-secondary node")
    local_ip = "192.0.2.11" if node == "arch" else "192.0.2.10"
    peer_ip = "192.0.2.10" if node == "arch" else "192.0.2.11"
    return {
        "pair_role": "secondary",
        "local_ip": local_ip,
        "local_ns": SECONDARY_LOCAL_NS,
        "peer_ip": peer_ip,
        "peer_ns": SECONDARY_PEER_NS,
    }


def paired_secondary_scenario(driver: str, node: str) -> dict[str, Any]:
    """Fresh paired secondary: empty 0/0 source, epoch 1, zero zones.

    A secondary holds no local live zones (cmd/panel/dns_engine.go:1690-1696);
    the member arrives from the native primary peer by catalog transfer.
    """

    if driver not in PAIRED_SECONDARY_DRIVERS:
        raise BootstrapError("unsupported paired-secondary driver")
    if driver == "pdns-switch" and node != "debian13":
        raise BootstrapError("PowerDNS paired secondary requires certified Debian 13")
    return {
        "schema": SCENARIO_SCHEMA,
        "driver": driver,
        "source_fixture": "uninitialized",
        "mode": "switch",
        "source_engine": "",
        "target_engine": "bind" if driver == "bind" else "pdns",
        "source_epoch": 0,
        "target_epoch": 1,
        "source_revision": 0,
        "topology": "paired",
        **secondary_pair_identity(node),
        "zones": [],
    }


def pdns_switch_scenario(*, role: str = "standalone", authority_acceptance: bool = False,
                         source_fixture: str = "managed-bind") -> dict[str, Any]:
    if role == "paired-secondary":
        if source_fixture != "uninitialized" or authority_acceptance:
            raise BootstrapError("a PowerDNS paired secondary is prepared only as a fresh install")
        return paired_secondary_scenario("pdns-switch", "debian13")
    if role not in {"standalone", "paired-primary"}:
        raise BootstrapError("unsupported PowerDNS switch source role")
    if source_fixture not in {"managed-bind", "uninitialized"}:
        raise BootstrapError("unsupported PowerDNS switch source fixture")
    source = bind_scenario(
        "uninitialized", role=role, node="debian13",
        allow_debian_paired_source=role == "paired-primary",
        authority_acceptance=authority_acceptance,
    )
    zones = source["zones"]
    if source_fixture == "uninitialized" and role == "paired-primary":
        # Only a fresh paired producer requires transferable MASTER members;
        # a fresh standalone install keeps the ordinary NATIVE snapshot.
        zones = [{**zone, "zone_type": "MASTER"} for zone in zones]
    return {
        "schema": SCENARIO_SCHEMA,
        "driver": "pdns-switch",
        "source_fixture": source_fixture,
        "mode": "switch",
        "source_engine": "bind" if source_fixture == "managed-bind" else "",
        "target_engine": "pdns",
        "source_epoch": 1 if source_fixture == "managed-bind" else 0,
        "target_epoch": 2 if source_fixture == "managed-bind" else 1,
        "source_revision": 0,
        "topology": source["topology"],
        **({
            key: source[key]
            for key in ("pair_role", "local_ip", "local_ns", "peer_ip", "peer_ns")
        } if role == "paired-primary" else {}),
        "zones": zones,
    }

def validate_supported_cell(
    cell: dict[str, Any], node: str, source_fixture: str
) -> None:
    driver = cell.get("driver")
    if driver == "bind":
        validate_bind_cell(cell, node, source_fixture)
    elif driver == "pdns-adopt":
        validate_pdns_adopt_cell(cell, node, source_fixture)
    elif driver == "pdns-switch":
        validate_pdns_switch_cell(cell, node, source_fixture)
    else:
        raise BootstrapError(
            f"guest bootstrap does not yet prepare driver {driver!r}"
        )


def zone_snapshot() -> dict[str, Any]:
    return {
        "ordinal": 0,
        "domain": ZONE_NAME,
        "desired_generation": 1,
        "delete": False,
        "zone_type": "NATIVE",
        "records": [
            {
                "name": ZONE_NAME,
                "type": "SOA",
                "content": (
                    "ns1.s1-kill.test hostmaster.s1-kill.test "
                    "2026083101 10800 3600 604800 3600"
                ),
                "ttl": 3600,
                "prio": 0,
                "disabled": False,
            },
            {
                "name": ZONE_NAME,
                "type": "NS",
                "content": "ns1.s1-kill.test",
                "ttl": 3600,
                "prio": 0,
                "disabled": False,
            },
            {
                "name": "ns1.s1-kill.test",
                "type": "A",
                "content": "192.0.2.10",
                "ttl": 300,
                "prio": 0,
                "disabled": False,
            },
            {
                "name": QUERY_NAME,
                "type": "A",
                "content": "192.0.2.10",
                "ttl": 300,
                "prio": 0,
                "disabled": False,
            },
        ],
        "zone_qualifier": "",
    }


def authority_zone_snapshot() -> dict[str, Any]:
    """Additional isolated zone modelling Frankfurt/Boston delegation."""
    return {
        "ordinal": 1, "domain": "celikhost.com", "desired_generation": 1,
        "delete": False, "zone_type": "NATIVE", "zone_qualifier": "",
        "records": [
            {"name": "celikhost.com", "type": "SOA",
             "content": "ns1.celikhost.com hostmaster.celikhost.com 2026092701 10800 3600 604800 3600",
             "ttl": 3600, "prio": 0, "disabled": False},
            *({"name": "celikhost.com", "type": "NS", "content": ns,
               "ttl": 3600, "prio": 0, "disabled": False}
              for ns in ("ns1.celikhost.com", "ns2.celikhost.com")),
            *({"name": name + ".celikhost.com", "type": "A", "content": address,
               "ttl": 300, "prio": 0, "disabled": False}
              for name, address in (("ns1", "192.0.2.10"), ("ns2", "192.0.2.11"),
                                    ("frankfurt", "192.0.2.10"), ("boston", "192.0.2.11"))),
        ],
    }


def bind_scenario(
    source_fixture: str, *, role: str = "standalone", node: str = "debian13",
    allow_debian_paired_source: bool = False, authority_acceptance: bool = False,
) -> dict[str, Any]:
    if role == "paired-secondary":
        if source_fixture != "uninitialized" or authority_acceptance:
            raise BootstrapError("a BIND paired secondary is prepared only as a fresh install")
        return paired_secondary_scenario("bind", node)
    if source_fixture in PROVENANCE_BIND_CELLS:
        if role != "standalone" or node != "debian13" or authority_acceptance:
            raise BootstrapError(f"{source_fixture} is a Debian standalone BIND fixture")
        reinstall = source_fixture == MANAGED_BIND_ABSENT
        return {
            "schema": SCENARIO_SCHEMA,
            "driver": "bind",
            "source_fixture": source_fixture,
            # Takeover: on the wire exactly a fresh install (mode switch, empty
            # source, 0 -> 1); the Agent selects it from host state. Reinstall:
            # the managed BIND identity is kept, source = target = bind, equal
            # epochs (the Panel's reinstall_active manifest).
            "mode": "reinstall" if reinstall else "switch",
            "source_engine": "bind" if reinstall else "",
            "target_engine": "bind",
            "source_epoch": 1 if reinstall else 0,
            "target_epoch": 1,
            "source_revision": 0,
            "topology": "standalone",
            "zones": [zone_snapshot()],
        }
    if source_fixture == "uninitialized":
        source_engine, source_epoch, target_epoch, revision = "", 0, 1, 0
    elif source_fixture == "managed-pdns":
        source_engine, source_epoch, target_epoch, revision = "pdns", 1, 2, 0
    elif source_fixture == "owner-bind":
        source_engine, source_epoch, target_epoch, revision = "", 0, 1, 0
    else:
        raise BootstrapError("unsupported source fixture")
    if role not in {"standalone", "paired-primary"}:
        raise BootstrapError("unsupported BIND topology in guest scenario")
    if node not in {"arch", "debian13"}:
        raise BootstrapError("unsupported BIND fixture node")
    if role == "paired-primary" and (
        source_fixture != "uninitialized"
        or (node != "arch" and not (node == "debian13" and allow_debian_paired_source))
    ):
        raise BootstrapError("paired-primary scenario requires an uninitialized Arch source")
    if authority_acceptance and (role != "paired-primary" or node != "debian13"):
        raise BootstrapError("authority acceptance requires the Debian paired primary")
    local_ip = "192.0.2.11" if node == "arch" else "192.0.2.10"
    peer_ip = "192.0.2.10" if node == "arch" else "192.0.2.11"
    zone = zone_snapshot()
    if role == "paired-primary":
        for record in zone["records"]:
            if record["type"] == "A":
                record["content"] = local_ip
        zone["records"].extend(
            [
                {
                    "name": ZONE_NAME,
                    "type": "NS",
                    "content": "ns2.s1-kill.test",
                    "ttl": 3600,
                    "prio": 0,
                    "disabled": False,
                },
                {
                    "name": "ns2.s1-kill.test",
                    "type": "A",
                    "content": peer_ip,
                    "ttl": 300,
                    "prio": 0,
                    "disabled": False,
                },
            ]
        )
    return {
        "schema": SCENARIO_SCHEMA,
        "driver": "bind",
        "source_fixture": source_fixture,
        "mode": "switch",
        "source_engine": source_engine,
        "target_engine": "bind",
        "source_epoch": source_epoch,
        "target_epoch": target_epoch,
        "source_revision": revision,
        "topology": "paired" if role == "paired-primary" else "standalone",
        **(
            {
                "pair_role": "primary",
                "local_ip": local_ip,
                "local_ns": "ns1.s1-kill.test",
                "peer_ip": peer_ip,
                "peer_ns": "ns2.s1-kill.test",
            }
            if role == "paired-primary"
            else {}
        ),
        "zones": [zone, authority_zone_snapshot()] if authority_acceptance else [zone],
    }


def pdns_adoption_source_setup_scenario(
    *, include_deleted_child: bool = False
) -> dict[str, Any]:
    zones = [zone_snapshot()]
    if include_deleted_child:
        zones.append({
            "ordinal": 1,
            "domain": "old.s1-kill.test",
            "desired_generation": 1,
            "delete": True,
            "zone_type": "NATIVE",
            "records": [],
            "zone_qualifier": "",
        })
    return {
        "schema": SCENARIO_SCHEMA,
        "driver": "pdns-adopt",
        "source_fixture": "external-pdns-adoption",
        "mode": "adopt",
        "source_engine": "",
        "target_engine": "pdns",
        "source_epoch": 0,
        "target_epoch": 1,
        "source_revision": 0,
        "topology": "standalone",
        "zones": zones,
    }


def json_bytes(value: Any) -> bytes:
    return (json.dumps(value, indent=2, sort_keys=True) + "\n").encode("utf-8")


def stage_name(cell_id: str) -> str:
    identity = hashlib.sha256(cell_id.encode("utf-8")).hexdigest()[:16]
    return STAGE_PREFIX + identity


def identity_file(path: Path) -> Path:
    return regular_file(path, "SSH identity")


def ssh_base(node: dict[str, Any], identity: Path) -> list[str]:
    # Reuse fixture.py's exact SSH policy instead of growing a subtly different
    # host-key or destination contract here. The final element is its readiness
    # command, which bootstrap actions replace with a bounded fixed command.
    command = fixture.ssh_command(node, identity)
    if len(command) < 2 or command[0] != "ssh":
        raise BootstrapError("fixture returned an invalid SSH command")
    return command[:-1]


def scp_base(node: dict[str, Any], identity: Path) -> list[str]:
    management = node["management"]
    known_hosts = Path(node["paths"]["directory"]).parent / "ssh-known-hosts"
    return [
        "scp",
        "-q",
        "-o",
        "BatchMode=yes",
        "-o",
        "ConnectTimeout=5",
        "-o",
        "StrictHostKeyChecking=accept-new",
        "-o",
        "HashKnownHosts=no",
        "-o",
        f"UserKnownHostsFile={known_hosts}",
        "-i",
        str(identity),
        "-P",
        str(management["ssh_port"]),
    ]


def remote_destination(node: dict[str, Any], path: str) -> str:
    management = node["management"]
    if not path.startswith("/") or " " in path or "'" in path:
        raise BootstrapError("remote path is not safe for OpenSSH's remote shell")
    return f"celik@{management['ssh_host']}:{path}"


def run(command: list[str], *, execute: bool) -> None:
    if execute:
        subprocess.run(command, check=True)
    else:
        print(json.dumps(command))


def load_plan(args: argparse.Namespace) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any]]:
    root = args.work_root.resolve(strict=True)
    plan = fixture.load_cell_plan(root, args.cell_id)
    cell = load_manifest_cell(args.manifest, args.cell_id)
    node = plan["nodes"].get(args.node)
    if not isinstance(node, dict):
        raise BootstrapError("fixture plan does not contain the selected node")
    return plan, cell, node


def copy_guest_script(source: Path, destination: Path) -> None:
    """Make Linux shebangs deterministic even from a CRLF checkout."""
    raw = source.read_bytes()
    normalized = raw.replace(b"\r\n", b"\n")
    if b"\r" in normalized or not normalized.startswith(b"#!"):
        raise BootstrapError(f"guest script has unsupported line endings or shebang: {source.name}")
    destination.write_bytes(normalized)


def bundle_files(args: argparse.Namespace, output: Path) -> tuple[list[Path], str]:
    sources = {
        "agent": regular_file(args.agent, "untagged agent", executable=True),
        "agent.kill": regular_file(args.tagged_agent, "tagged agent", executable=True),
        "panel": regular_file(args.panel, "panel", executable=True),
        "dns-kill-trigger": regular_file(args.trigger, "scenario trigger", executable=True),
        "celikpanel-agent.service": regular_file(args.agent_unit, "agent unit"),
        "celikpanel-panel.service": regular_file(args.panel_unit, "panel unit"),
        "guest_bootstrap.sh": regular_file(
            Path(__file__).with_name("guest_bootstrap.sh"), "guest bootstrap"
        ),
        "guest_recovery_probe.py": regular_file(
            Path(__file__).with_name("guest_recovery_probe.py"), "guest recovery probe"
        ),
        "dns-kill-run-cell.py": regular_file(
            Path(__file__).with_name("run_cell.py"), "cell controller"
        ),
        "manifest.json": regular_file(
            args.manifest, "exact matrix manifest"
        ),
    }
    staged: list[Path] = []
    for name, source in sources.items():
        destination = output / name
        if name in {"guest_bootstrap.sh", "guest_recovery_probe.py", "dns-kill-run-cell.py"}:
            copy_guest_script(source, destination)
        else:
            shutil.copyfile(source, destination)
        os.chmod(destination, 0o700 if name == "guest_bootstrap.sh" else 0o600)
        staged.append(destination)
    web_tar = output / "web.tar"
    write_deterministic_web_tar(args.web_dir, web_tar)
    os.chmod(web_tar, 0o600)
    staged.append(web_tar)
    staged.sort(key=lambda path: path.name)
    manifest = output / "SHA256SUMS"
    manifest.write_text(
        "".join(f"{sha256_file(path)}  {path.name}\n" for path in staged),
        encoding="ascii",
        newline="\n",
    )
    os.chmod(manifest, 0o600)
    return staged + [manifest], sha256_file(manifest)


def install(args: argparse.Namespace) -> None:
    _, cell, node = load_plan(args)
    validate_supported_cell(cell, args.node, args.source_fixture)
    identity = identity_file(args.identity_file)
    stage = stage_name(args.cell_id)
    with tempfile.TemporaryDirectory(prefix="celikpanel-s1-bundle-") as temporary:
        paths, manifest_sha = bundle_files(args, Path(temporary))
        run(
            ssh_base(node, identity)
            + [f"test ! -e {stage} && install -d -m 0700 {stage}"],
            execute=args.execute,
        )
        run(
            scp_base(node, identity)
            + [str(path) for path in paths]
            + [remote_destination(node, stage + "/")],
            execute=args.execute,
        )
        remote = (
            f"sudo /bin/bash {stage}/guest_bootstrap.sh install {stage} "
            f"{manifest_sha} {args.cell_id} {args.node}"
        )
        run(ssh_base(node, identity) + [remote], execute=args.execute)
        print(
            json.dumps(
                {
                    "action": "install",
                    "cell_id": args.cell_id,
                    "node": args.node,
                    "stage": stage,
                    "bundle_manifest_sha256": manifest_sha,
                    "tagged_agent": "/opt/celikpanel/bin/agent.kill",
                    "untagged_agent": "/opt/celikpanel/bin/agent",
                    "panel": "/opt/celikpanel/bin/panel",
                    "trigger": "/opt/celikpanel/bin/dns-kill-trigger",
                    "controller": (
                        "/opt/celikpanel/libexec/dns-kill-run-cell.py"
                    ),
                    "guest_manifest": (
                        "/var/lib/celikpanel-dns-kill-matrix/manifest.json"
                    ),
                    "systemd_agent_exec_is_untagged": True,
                    "tagged_agent_expected_uid_gid": "root:celikpanel",
                },
                sort_keys=True,
            )
        )


RECOVERY_RUNTIME_ENROLL_SCRIPT = r"""set -euo pipefail
stage=$1
kit_sha=$2
test -f /etc/celikpanel-dns-kill-matrix
kit=$stage/recovery-kit.tar.gz
[[ -f $kit && ! -L $kit ]]
test "$(sha256sum -- "$kit" | cut -d' ' -f1)" = "$kit_sha"
root=/root/celikpanel-recovery-kit-${kit_sha:0:16}
test ! -e "$root"
install -d -m 0700 "$root"
tar -xzf "$kit" -C "$root" --no-same-owner
lock=/var/lib/celikpanel-release-transaction/transaction.lock
[[ -f $lock && ! -L $lock ]]
exec 9<>"$lock"
flock -x -w 60 9
"$root/recovery-runtime/bin/recovery" enroll-runtime --source "$root/recovery-runtime" --transaction-fd 9
exec 9>&-
marker=$(/usr/libexec/celikpanel/recovery check-bind-source-inverse-v1)
test "$marker" = celikpanel-bind-source-inverse/v1
if [[ $# -ge 4 ]]; then
    [[ $3 == check-bind-adoption-inverse-v1 && $4 == celikpanel-bind-adoption-inverse/v1 ]]
    extra=$(/usr/libexec/celikpanel/recovery "$3")
    test "$extra" = "$4"
fi
sha256sum /usr/libexec/celikpanel/recovery "$root/recovery-runtime/runtime.manifest"
"""


def validate_recovery_runtime_dir(path: Path) -> Path:
    try:
        clean = path.resolve(strict=True)
        info = path.lstat()
    except OSError as exc:
        raise BootstrapError(f"inspect recovery runtime: {exc}") from exc
    if (
        clean != path.absolute()
        or stat.S_ISLNK(info.st_mode)
        or not stat.S_ISDIR(info.st_mode)
        or clean.name != "recovery-runtime"
    ):
        raise BootstrapError(
            "recovery runtime must be a clean real directory named recovery-runtime"
        )
    regular_file(clean / "runtime.manifest", "recovery runtime manifest")
    regular_file(clean / "bin" / "recovery", "recovery runtime binary", executable=True)
    return clean


def write_recovery_kit(runtime: Path, target: Path) -> None:
    def root_owned(member: tarfile.TarInfo) -> tarfile.TarInfo:
        member.uid = 0
        member.gid = 0
        member.uname = "root"
        member.gname = "root"
        return member

    with tarfile.open(target, "w:gz") as archive:
        archive.add(runtime, arcname="recovery-runtime", filter=root_owned)


def validate_enrollment_cell(cell: dict[str, Any], node: str, source_fixture: str) -> None:
    """Any supported standalone cell may carry the recovery runtime."""

    if cell.get("role") != "standalone" and not fresh_pdns_primary_cell(cell):
        raise BootstrapError(
            "enroll-recovery-runtime applies to standalone cells and the fresh paired "
            "PowerDNS primary cells (only the kill guest is enrolled); "
            f"{cell.get('id')} is {cell.get('role')}"
        )
    validate_supported_cell(cell, node, source_fixture)


def enroll_recovery_runtime(args: argparse.Namespace) -> None:
    """Select the built recovery runtime in the guest, as the 2026-09-27 trial did.

    The kit is enrolled through the product's own `enroll-runtime` under the
    native release-transaction lock; the launcher must then advertise the BIND
    source inverse and, for the running-BIND adoption cell, the BIND adoption
    inverse. recover-dns-pdns-adoption has no capability probe; for it the BIND
    source marker only shows that the enrolled runtime is the selected one.

    Admitted for every supported STANDALONE cell, because an installed server
    always has the recovery launcher; the socket-recovery flows then record the
    read-only `recovery dns-switch-status` before and after recovery. The
    adoption capability is additionally required only for the running-BIND
    adoption cell with its owner-bind fixture. Dry-run unless --execute.
    """

    _, cell, node = load_plan(args)
    validate_enrollment_cell(cell, args.node, args.source_fixture)
    admission = OWNER_INVERSE_ADMISSIONS.get(cell["id"])
    adoption = (
        admission is not None
        and admission[0] == "owner-bind"
        and args.source_fixture == "owner-bind"
    )
    runtime = validate_recovery_runtime_dir(args.recovery_runtime)
    identity = identity_file(args.identity_file)
    stage = stage_name(args.cell_id)
    with tempfile.TemporaryDirectory(prefix="celikpanel-s1-recovery-kit-") as temporary:
        kit = Path(temporary) / RECOVERY_KIT_NAME
        write_recovery_kit(runtime, kit)
        os.chmod(kit, 0o600)
        kit_sha = sha256_file(kit)
        run(
            ssh_base(node, identity)
            + [f"test -d {stage} && test ! -e {stage}/{RECOVERY_KIT_NAME}"],
            execute=args.execute,
        )
        run(
            scp_base(node, identity) + [str(kit), remote_destination(node, stage + "/")],
            execute=args.execute,
        )
        remote = (
            "sudo /bin/bash -c "
            + shlex.quote(RECOVERY_RUNTIME_ENROLL_SCRIPT)
            + " enroll-recovery-runtime "
            + shlex.quote(stage)
            + " "
            + kit_sha
            + (
                f" {BIND_ADOPTION_INVERSE_COMMAND} {BIND_ADOPTION_INVERSE_MARKER}"
                if adoption
                else ""
            )
        )
        run(ssh_base(node, identity) + [remote], execute=args.execute)
    print(
        json.dumps(
            {
                "action": "enroll-recovery-runtime",
                "cell_id": args.cell_id,
                "node": args.node,
                "stage": stage,
                "recovery_kit_sha256": kit_sha,
                "launcher": OWNER_RECOVERY_LAUNCHER,
                "required_capability": BIND_SOURCE_INVERSE_MARKER,
                **(
                    {"additional_capability": BIND_ADOPTION_INVERSE_MARKER}
                    if adoption
                    else {}
                ),
            },
            sort_keys=True,
        )
    )


def controller_commands(cell_id: str) -> tuple[list[str], list[str], list[str]]:
    if CELL_RE.fullmatch(cell_id) is None:
        raise BootstrapError("cell ID is not canonical")
    scenario_guest = "/var/lib/celikpanel-dns-kill-matrix/scenario.json"
    identity_guest = (
        "/var/lib/celikpanel-dns-kill-matrix/measured/trigger-identity.json"
    )
    trigger_command = [
        "/opt/celikpanel/bin/dns-kill-trigger", "rpc-switch",
        "--scenario", scenario_guest,
        "--identity-receipt", identity_guest,
        "--timeout", "45m",
    ]
    recovery_command = list(trigger_command)
    recovery_command[1] = "rpc-retry"
    recovery_probe_command = [
        "/opt/celikpanel/libexec/dns-kill-recovery-probe.py",
        "--cell-id", cell_id,
        "--scenario", scenario_guest,
        "--identity-receipt", identity_guest,
        "--ledger", "/var/lib/celikpanel-agent-private/service-mutations.json",
        "--state", "/var/lib/celikpanel-agent-private/dns-engine-state.json",
        "--journal", "/var/lib/celikpanel-agent-private/dns-engine-switch-journal.json",
    ]
    return trigger_command, recovery_command, recovery_probe_command


PEER_EVIDENCE_DIRECTORY = "paired-secondary-peer"


def require_peer_engine(cell: dict[str, Any], peer_engine: Any) -> str | None:
    """--peer-engine is required for, and limited to, paired-secondary cells."""

    if not isinstance(peer_engine, str):
        peer_engine = None  # argparse default (or an absent attribute)
    if paired_secondary_cell(cell):
        if peer_engine not in PEER_ENGINES:
            raise BootstrapError(
                "a paired-secondary cell needs --peer-engine bind or --peer-engine pdns "
                "(the native primary peer flavour); nothing was prepared"
            )
        return str(peer_engine)
    if peer_engine is not None:
        raise BootstrapError("--peer-engine applies only to paired-secondary cells")
    return None


def require_peer_catalog_format(
    cell: dict[str, Any], peer_engine: str | None, value: Any
) -> str | None:
    """--peer-catalog-format: paired-secondary only; default per peer engine.

    A BIND peer serves only the bind format. A PowerDNS peer defaults to
    pdns-native (its own PRODUCER catalog); bind stays selectable so both
    producers can be run against a PowerDNS primary.
    """

    if not isinstance(value, str):
        value = None  # argparse default (or an absent attribute)
    if peer_engine is None:
        if value is not None:
            raise BootstrapError(
                "--peer-catalog-format applies only to paired-secondary cells "
                "(with --peer-engine)"
            )
        return None
    selected = DEFAULT_PEER_CATALOG_FORMAT[peer_engine] if value is None else value
    if selected not in PEER_CATALOG_FORMATS:
        raise BootstrapError("--peer-catalog-format must be bind or pdns-native")
    if peer_engine == "bind" and selected != "bind":
        raise BootstrapError(
            "a native BIND primary peer serves only the bind catalog format; "
            "--peer-catalog-format pdns-native needs --peer-engine pdns. Nothing was prepared"
        )
    return selected


def peer_namespace(
    args: argparse.Namespace, engine: str, *, require_transfer: bool,
    catalog_format: str | None = None,
) -> argparse.Namespace:
    return argparse.Namespace(
        engine=engine,
        catalog_format=catalog_format,
        work_root=args.work_root,
        cell_id=args.cell_id,
        identity_file=args.identity_file,
        manifest=args.manifest,
        require_secondary_transfer=require_transfer,
        execute=args.execute,
    )


def write_peer_evidence(
    plan: dict[str, Any], name: str, value: Any, *, execute: bool,
    directory_name: str = PEER_EVIDENCE_DIRECTORY,
) -> str | None:
    """Create-new host-side evidence beside the fixture plan (never replaced)."""

    if not execute:
        print(json.dumps({"peer_evidence": name, "value": value}, sort_keys=True))
        return None
    if re.fullmatch(r"[a-z0-9-]{1,64}\.json", name) is None:
        raise BootstrapError("peer evidence name is not canonical")
    if directory_name not in (PEER_EVIDENCE_DIRECTORY, FRESH_PRIMARY_EVIDENCE_DIRECTORY):
        raise BootstrapError("peer evidence directory is not canonical")
    directory = Path(plan["cell_directory"]) / directory_name
    try:
        directory.mkdir(mode=0o700)
    except FileExistsError:
        info = directory.lstat()
        if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
            raise BootstrapError("peer evidence directory is not a real directory")
    path = directory / name
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "wb") as handle:
        handle.write(json_bytes(value))
        handle.flush()
        os.fsync(handle.fileno())
    return str(path)


def read_peer_evidence(plan: dict[str, Any], name: str) -> Any:
    return read_json(Path(plan["cell_directory"]) / PEER_EVIDENCE_DIRECTORY / name, name)


def prepare_native_primary_peer(
    args: argparse.Namespace, plan: dict[str, Any], engine: str,
    catalog_format: str | None = None,
) -> None:
    """Run native_primary_peer.py prepare, then a baseline observe, on the peer.

    Order from PAIRED-SECONDARY-FIXTURE.md: the native primary must serve and
    NOTIFY/AXFR before the guest's secondary install starts. Its observation
    here cannot show a transfer yet (the guest has no secondary). The catalog
    format the peer serves is recorded; run-prepared must use the same one.
    """

    import native_primary_peer  # noqa: PLC0415 - imports this module

    catalog_format = catalog_format or DEFAULT_PEER_CATALOG_FORMAT[engine]
    prepared = native_primary_peer.prepare(peer_namespace(
        args, engine, require_transfer=False, catalog_format=catalog_format))
    print(json.dumps(prepared, sort_keys=True))
    observed = native_primary_peer.observe(peer_namespace(
        args, engine, require_transfer=False, catalog_format=catalog_format))
    print(json.dumps(observed, sort_keys=True))
    write_peer_evidence(
        plan, "peer-prepared.json",
        {"engine": engine, "catalog_format": catalog_format, "cell_id": args.cell_id,
         "prepare": prepared, "observe": observed},
        execute=args.execute,
    )


def prepare(args: argparse.Namespace) -> None:
    plan, cell, node = load_plan(args)
    peer_engine = require_peer_engine(cell, getattr(args, "peer_engine", None))
    peer_catalog_format = require_peer_catalog_format(
        cell, peer_engine, getattr(args, "peer_catalog_format", None)
    )
    if args.action == "prepare-bind":
        validate_bind_cell(cell, args.node, args.source_fixture)
        scenario = bind_scenario(
            args.source_fixture, role=cell["role"], node=args.node
        )
        guest_action = "prepare-bind"
    elif args.action == "prepare-pdns-adopt":
        validate_pdns_adopt_cell(cell, args.node, args.source_fixture)
        scenario = pdns_adoption_source_setup_scenario(
            include_deleted_child=args.include_deleted_child
        )
        guest_action = "prepare-pdns-adopt"
    elif args.action == "prepare-pdns-switch":
        validate_pdns_switch_cell(cell, args.node, args.source_fixture)
        scenario = pdns_switch_scenario(
            role=cell["role"], authority_acceptance=args.authority_acceptance,
            source_fixture=args.source_fixture)
        guest_action = "prepare-pdns-switch"
    else:
        raise BootstrapError("unsupported preparation action")
    source_policy = cell["placement"]["source_fixture_policy"]
    identity = identity_file(args.identity_file)
    stage = stage_name(args.cell_id)
    phase = cell["boundary"]["phase"]
    trigger_command, recovery_command, recovery_probe_command = controller_commands(
        args.cell_id
    )
    scenario_guest = trigger_command[3]
    identity_guest = trigger_command[5]
    if peer_engine is not None:
        # The native primary must serve before the guest's secondary install.
        prepare_native_primary_peer(args, plan, peer_engine, peer_catalog_format)
    with tempfile.TemporaryDirectory(prefix="celikpanel-s1-scenario-") as temporary:
        temporary_path = Path(temporary)
        scenario_path = temporary_path / "scenario.json"
        scenario_path.write_bytes(json_bytes(scenario))
        uploads = [scenario_path]
        if getattr(args, "owner_directives", False) is True:
            if not (
                args.action == "prepare-bind"
                and args.source_fixture == UNMANAGED_BIND_STOPPED
                and args.cell_id == PROVENANCE_BIND_CELLS[UNMANAGED_BIND_STOPPED]
            ):
                raise BootstrapError(
                    "--owner-directives applies only to prepare-bind of the stopped-BIND "
                    "takeover cell; nothing was prepared"
                )
            # The guest inserts OWNER_BIND_DIRECTIVES itself; this empty file
            # only asks for it (guest_bootstrap.sh add_owner_bind_directives).
            request = temporary_path / OWNER_DIRECTIVES_STAGE_NAME
            request.write_bytes(b"")
            uploads.append(request)
        if args.source_fixture == MANAGED_BIND_ABSENT:
            # The managed BIND is produced by a real untagged fresh BIND switch
            # with the same zone, then its engine is removed (receipts kept).
            source_setup = temporary_path / "source-setup-bind.json"
            source_setup.write_bytes(json_bytes(bind_scenario("uninitialized", node="debian13")))
            uploads.append(source_setup)
        elif args.source_fixture == "managed-pdns":
            source_setup = temporary_path / "source-setup-pdns.json"
            source_setup.write_bytes(json_bytes(pdns_adoption_source_setup_scenario()))
            uploads.append(source_setup)
        elif args.source_fixture == "managed-bind":
            source_setup = temporary_path / "source-setup-bind.json"
            source_setup.write_bytes(json_bytes(bind_scenario(
                "uninitialized", role=cell["role"], node="debian13",
                allow_debian_paired_source=cell["role"] == "paired-primary",
                authority_acceptance=args.authority_acceptance,
            )))
            uploads.append(source_setup)
        names = " ".join(path.name for path in uploads)
        run(
            ssh_base(node, identity)
            + [f"test -d {stage} && test ! -e {stage}/scenario.json && test ! -e {stage}/source-setup-pdns.json"],
            execute=args.execute,
        )
        run(
            scp_base(node, identity)
            + [str(path) for path in uploads]
            + [remote_destination(node, stage + "/")],
            execute=args.execute,
        )
        remote = (
            f"sudo /bin/bash {stage}/guest_bootstrap.sh {guest_action} "
            f"{args.cell_id} {args.node} {phase} {args.source_fixture} "
            f"{source_policy} {stage}"
        )
        run(ssh_base(node, identity) + [remote], execute=args.execute)
        print(
            json.dumps(
                {
                    "action": guest_action,
                    "cell_id": args.cell_id,
                    "node": args.node,
                    "boundary_phase": phase,
                    "source_fixture": args.source_fixture,
                    "source_fixture_policy": source_policy,
                    "uploaded": names,
                    "scenario": scenario_guest,
                    "source_proof": "/var/lib/celikpanel-dns-kill-matrix/source-proof.json",
                    "source_preinstall_proof": (
                        "/var/lib/celikpanel-dns-kill-matrix/source-preinstall-pdns.json"
                        if args.source_fixture
                        in {"managed-pdns", "external-pdns-adoption"}
                        else "/var/lib/celikpanel-dns-kill-matrix/source-preinstall-bind.json"
                        if args.source_fixture in PROVENANCE_BIND_CELLS
                        else None
                    ),
                    "native_primary_peer_engine": peer_engine,
                    "native_primary_peer_catalog_format": peer_catalog_format,
                    "source_adoption_proof": (
                        "/var/lib/celikpanel-dns-kill-matrix/source-adoption-pdns.json"
                        if args.source_fixture == "managed-pdns"
                        else None
                    ),
                    "external_pdns_preimage": (
                        "/var/lib/celikpanel-dns-kill-matrix/"
                        "source-external-pdns-preimage.json"
                        if args.source_fixture == "external-pdns-adoption"
                        else None
                    ),
                    "setup_adoption_rpc_used": (
                        args.source_fixture == "managed-pdns"
                    ),
                    "state_dir": "/var/lib/celikpanel-agent-private",
                    "mutation_lock": "/run/celikpanel/service-mutation.lock",
                    "agent_socket": "/run/celikpanel/agent.sock",
                    "agent_token": "/etc/celikpanel/agent.token",
                    "identity_receipt": identity_guest,
                    "trigger_command": trigger_command,
                    "recovery_command": recovery_command,
                    "recovery_probe_command": recovery_probe_command,
                    "recovery_probe": (
                        "/opt/celikpanel/libexec/dns-kill-recovery-probe.py"
                    ),
                    "controller_argv": (
                        "/var/lib/celikpanel-dns-kill-matrix/controller-argv.json"
                    ),
                    "controller_expected_uid_gid": "root:celikpanel",
                    "production_agent_service_stopped": True,
                },
                sort_keys=True,
            )
        )


def validate_owner_inverse_cell(
    cell: dict[str, Any],
    node: str,
    source_fixture: str,
    *,
    later_precursor: bool | None = None,
) -> None:
    """Exact Debian standalone cell admitted to the owner-inverse flow.

    Each admitted cell names its source fixture and manifest source policy in
    OWNER_INVERSE_ADMISSIONS. ``later_precursor`` (None: not judged, as for
    enrollment) must equal the admission's target-started precursor rule.
    """

    admission = OWNER_INVERSE_ADMISSIONS.get(cell.get("id"))
    boundary = cell.get("boundary", {})
    phase = boundary.get("phase")
    edge = boundary.get("edge")
    if cell.get("id") in STARTUP_ROLLBACK_CELLS:
        raise BootstrapError(
            f"{cell.get('id')} is not an owner-inverse cell: the restarted Agent "
            "rolls this PowerDNS adoption back and retires the journal by itself "
            f"(since 3cc2de22 also from rolled-back); run it with {STARTUP_ROLLBACK_FLAG}. "
            "Nothing was started"
        )
    if admission is None or not isinstance(edge, str):
        raise BootstrapError(
            "owner inverse after restart requires an admitted Debian standalone cell: "
            + ", ".join(sorted(OWNER_INVERSE_CELLS))
        )
    fixture_name, policy, later = admission
    peer = cell.get("peer_reachability")
    if not (
        cell.get("role") == "standalone"
        and cell.get("id") == f"{cell.get('driver')}__{phase}__{edge}__standalone__peer-{peer}"
        and boundary == {"edge": edge, "name": f"{phase}:{edge}", "phase": phase}
        and cell.get("fault_selector") == {"phase": phase, "point": edge.replace("-", "_")}
        and cell.get("placement", {}).get("source_fixture_policy") == policy
        and node == "debian13"
        and source_fixture == fixture_name
        and (later_precursor is None or later_precursor == later)
    ):
        raise BootstrapError(
            f"owner inverse after restart requires the exact Debian standalone cell "
            f"{cell.get('id')} with the {fixture_name} source fixture"
            + (" and the target-started rollback precursor" if later else "")
        )
    validate_supported_cell(cell, node, source_fixture)


def refuse_v2_managed_pdns_without_owner_flow(
    cell: dict[str, Any], source_fixture: str, owner_inverse: bool, handoff: bool
) -> None:
    """A managed PowerDNS source makes the producer write the V2 journal.

    The controller would expect V1 without the owner-inverse flow and fail at
    the boundary marker after a real mutation, so the host refuses first.
    """

    if (
        source_fixture != "managed-pdns"
        or cell.get("driver") != "bind"
        or cell.get("role") != "standalone"
        or owner_inverse
        or handoff
    ):
        return
    admission = OWNER_INVERSE_ADMISSIONS.get(cell.get("id"))
    if admission is not None and admission[0] == "managed-pdns":
        raise BootstrapError(
            f"{cell.get('id')} with a managed PowerDNS source writes the V2 journal, "
            "which the restarted Agent never executes; run it with "
            f"{OWNER_INVERSE_FLAG}. Nothing was started"
        )
    raise BootstrapError(
        f"{cell.get('id')} with a managed PowerDNS source has no V2 pass definition "
        f"(it is not admitted to {OWNER_INVERSE_FLAG}); nothing was started"
    )


def prepared_flags(
    args: argparse.Namespace, peer_catalog_format: str | None = None
) -> list[str]:
    """The guest-program flags of this run, in PREPARED_FLAG_ORDER."""

    selected = {
        INDEPENDENT_PDNS_HANDOFF_FLAG: args.stop_after_kill_for_independent_recovery is True,
        LATER_BIND_ROLLBACK_FLAG: getattr(args, "bind_rollback_after_target_started", False) is True,
        OWNER_INVERSE_FLAG: getattr(args, "owner_inverse_after_restart", False) is True,
        STARTUP_ROLLBACK_FLAG: getattr(args, "expect_agent_startup_rollback", False) is True,
        OWNER_EDIT_FLAGS["config"]: getattr(args, "owner_edit", None) == "config",
        OWNER_EDIT_FLAGS["sql"]: getattr(args, "owner_edit", None) == "sql",
        OWNER_RELEASE_FLAG: getattr(args, "owner_release_recovery", False) is True,
        OWNER_DIRECTIVES_FLAG: getattr(args, "owner_directives", False) is True,
        PEER_CATALOG_BIND_FLAG: peer_catalog_format == "bind",
        PEER_CATALOG_PDNS_FLAG: peer_catalog_format == "pdns-native",
        RETRY_SWITCH_FLAG: getattr(args, "retry_switch_after_rollback", False) is True,
        REBOOT_BEFORE_OWNER_FLAG: getattr(args, "reboot_before_owner_command", False) is True,
        REBOOT_AFTER_RECOVERY_FLAG: getattr(args, "reboot_after_recovery", False) is True,
        REBOOT_EVEN_IF_FAILED_FLAG: getattr(args, "reboot_even_if_failed", False) is True,
        DISABLE_MANAGEMENT_FLAG: (
            getattr(args, "disable_management_before_reboot", False) is True
        ),
    }
    return [flag for flag in PREPARED_FLAG_ORDER if selected.get(flag)]


def prepared_remote(cell_id: str, flags: Iterable[str]) -> str:
    return (
        "sudo /usr/sbin/runuser -u root -g celikpanel -- /usr/bin/env -i "
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin "
        "LANG=C.UTF-8 /usr/bin/python3 -c "
        + shlex.quote(RUN_PREPARED_CODE)
        + " "
        + shlex.quote(cell_id)
        + "".join(" " + shlex.quote(flag) for flag in flags)
    )


def run_prepared(args: argparse.Namespace) -> int:
    plan, cell, node = load_plan(args)
    validate_supported_cell(cell, args.node, args.source_fixture)
    owner_inverse = getattr(args, "owner_inverse_after_restart", False) is True
    later = getattr(args, "bind_rollback_after_target_started", False) is True
    startup = getattr(args, "expect_agent_startup_rollback", False) is True
    reboot_before = getattr(args, "reboot_before_owner_command", False) is True
    reboot_after = getattr(args, "reboot_after_recovery", False) is True
    if owner_inverse:
        if args.stop_after_kill_for_independent_recovery:
            raise BootstrapError(
                "owner inverse after restart excludes the independent handoff flags"
            )
        validate_owner_inverse_cell(
            cell, args.node, args.source_fixture, later_precursor=later
        )
    if args.source_fixture == "owner-bind" and not (
        later and (args.stop_after_kill_for_independent_recovery or owner_inverse)
    ):
        raise BootstrapError("owner BIND handoff requires the target-started rollback precursor")
    if args.stop_after_kill_for_independent_recovery and not (
        args.cell_id in INDEPENDENT_HANDOFF_CELLS
        and cell.get("id") == args.cell_id
        and cell.get("driver") == (
            "pdns-adopt" if args.cell_id == INDEPENDENT_PDNS_HANDOFF_CELL else "bind"
        )
        and cell.get("role") == "standalone"
        and cell.get("peer_reachability") == "reachable"
        and cell.get("boundary", {}).get("phase") == "rolling-back"
        and cell.get("boundary", {}).get("edge") == "after-write"
        and cell.get("fault_selector")
        == {"phase": "rolling-back", "point": "after_write"}
        and args.node == "debian13"
        and (
            args.source_fixture == "external-pdns-adoption"
            if args.cell_id == INDEPENDENT_PDNS_HANDOFF_CELL
            else args.source_fixture in {"managed-pdns", "owner-bind"}
        )
    ):
        raise BootstrapError(
            "independent recovery handoff requires an exact Debian PowerDNS "
            "adoption or BIND switch rollback cell"
        )
    if later and not (
        (args.stop_after_kill_for_independent_recovery or owner_inverse)
        and args.cell_id == INDEPENDENT_BIND_HANDOFF_CELL
        and cell.get("id") == args.cell_id
        and cell.get("driver") == "bind"
        and cell.get("role") == "standalone"
        and cell.get("peer_reachability") == "reachable"
        and cell.get("boundary") == {
            "edge": "after-write", "name": "rolling-back:after-write",
            "phase": "rolling-back",
        }
        and cell.get("fault_selector") == {
            "phase": "rolling-back", "point": "after_write",
        }
        and args.node == "debian13"
        and (
            args.source_fixture == "owner-bind"
            if owner_inverse
            else args.source_fixture in {"managed-pdns", "owner-bind"}
        )
    ):
        raise BootstrapError("later BIND rollback requires the exact Debian handoff cell")
    if startup and not (
        args.cell_id in STARTUP_ROLLBACK_CELLS
        and cell.get("id") == args.cell_id
        and cell.get("driver") == "pdns-adopt"
        and args.source_fixture == "external-pdns-adoption"
        and not owner_inverse
        and not args.stop_after_kill_for_independent_recovery
    ):
        raise BootstrapError(
            f"{STARTUP_ROLLBACK_FLAG} applies only to "
            + ", ".join(sorted(STARTUP_ROLLBACK_CELLS))
            + " with the external PowerDNS adoption fixture"
        )
    if reboot_before and not owner_inverse:
        raise BootstrapError(f"{REBOOT_BEFORE_OWNER_FLAG} requires {OWNER_INVERSE_FLAG}")
    retry_switch = getattr(args, "retry_switch_after_rollback", False) is True
    if retry_switch and not (owner_inverse and args.cell_id in V2_SWITCH_OWNER_CELLS):
        raise BootstrapError(
            f"{RETRY_SWITCH_FLAG} applies only to the PowerDNS -> BIND switch cells "
            f"with {OWNER_INVERSE_FLAG}: " + ", ".join(sorted(V2_SWITCH_OWNER_CELLS))
            + ". Nothing was started"
        )
    if getattr(args, "reboot_even_if_failed", False) is True and not reboot_after:
        raise BootstrapError(
            f"{REBOOT_EVEN_IF_FAILED_FLAG} requires {REBOOT_AFTER_RECOVERY_FLAG}"
        )
    disable_management = getattr(args, "disable_management_before_reboot", False) is True
    if disable_management and (not reboot_after or owner_inverse):
        raise BootstrapError(
            f"{DISABLE_MANAGEMENT_FLAG} requires {REBOOT_AFTER_RECOVERY_FLAG} on the "
            "rpc-retry flow (not the owner-inverse flow)"
        )
    # A paired secondary reboots only itself; its native primary peer keeps
    # serving, which is exactly the "secondary keeps serving" check. A fresh
    # paired PowerDNS primary reboots both guests: the native secondary first.
    fresh_primary = fresh_pdns_primary_cell(cell)
    if (reboot_before or reboot_after) and (
        args.stop_after_kill_for_independent_recovery
        or (
            cell.get("role") != "standalone"
            and not paired_secondary_cell(cell)
            and not fresh_primary
        )
        or (reboot_before and cell.get("role") != "standalone")
    ):
        raise BootstrapError(
            "reboot steps need a standalone, admitted paired-secondary or fresh paired "
            "PowerDNS primary cell without the independent handoff"
        )
    validate_fresh_primary_run(args, cell, reboot_after or reboot_before)
    peer_engine = require_peer_engine(cell, getattr(args, "peer_engine", None))
    peer_catalog_format = require_peer_catalog_format(
        cell, peer_engine, getattr(args, "peer_catalog_format", None)
    )
    refuse_v2_managed_pdns_without_owner_flow(
        cell, args.source_fixture, owner_inverse,
        bool(args.stop_after_kill_for_independent_recovery is True),
    )
    identity = identity_file(args.identity_file)
    flags = prepared_flags(args, peer_catalog_format)
    reboots_allowed = int(reboot_before) + int(reboot_after)
    # A PowerDNS secondary stores the member's unique label of the peer's
    # catalog on the consumed member row; the controller judges that value
    # against the label the peer probe read right before it (execute only).
    needs_member_label = (
        peer_engine is not None and cell.get("driver") == "pdns-switch"
    )

    def commands(label: str | None) -> tuple[list[str], list[str] | None]:
        selected = with_member_label(flags, label) if needs_member_label else list(flags)
        command = ssh_base(node, identity) + [prepared_remote(args.cell_id, selected)]
        resume = (
            ssh_base(node, identity)
            + [prepared_remote(args.cell_id, selected + [RESUME_FLAG])]
            if reboots_allowed
            else None
        )
        return command, resume

    if not args.execute:
        command, resume_command = commands(
            MEMBER_LABEL_PLACEHOLDER if needs_member_label else None
        )
        if fresh_primary:
            print(json.dumps(command))
            if resume_command is not None:
                print(json.dumps({
                    "on_exit": REBOOT_REQUESTED_EXIT,
                    "reboot": [
                        {"node": "arch", "role": "native BIND secondary", "first": True},
                        {"node": args.node, "role": "fresh PowerDNS primary"},
                    ],
                    "method": fixture.REBOOT_METHOD,
                    "then": resume_command,
                }))
            print(json.dumps({
                "on_exit": GATE_CLOSED_EXIT,
                "result": "gate closed in this build; nothing after the probe runs",
            }))
            if not isinstance(getattr(args, "owner_edit", None), str):
                print(json.dumps({
                    "after_controller": "native_pdns_bind_peer.py observe --address 192.0.2.10",
                    "evidence": f"{FRESH_PRIMARY_EVIDENCE_DIRECTORY}/peer-verdict.json",
                }))
            if getattr(args, "zone_lifecycle", False) is True:
                for step in ZONE_LIFECYCLE_STEPS:
                    print(json.dumps({
                        "zone_lifecycle_step": step,
                        "primary": ssh_base(node, identity) + [zone_lifecycle_remote(step)],
                        "observe": f"native_pdns_bind_peer.py observe-child --step {step}",
                    }))
            return 0
        if peer_engine is not None:
            print(json.dumps({
                "before_controller": "native_primary_peer.py observe",
                "engine": peer_engine,
                "catalog_format": peer_catalog_format,
                "require_secondary_transfer": False,
                "evidence": f"{PEER_EVIDENCE_DIRECTORY}/peer-before-kill.json",
            }))
        print(json.dumps(command))
        if resume_command is not None:
            print(json.dumps({
                "on_exit": REBOOT_REQUESTED_EXIT,
                "reboot": {
                    "action": "reboot",
                    "method": fixture.REBOOT_METHOD,
                    "node": args.node,
                    "at_most": reboots_allowed,
                },
                "then": resume_command,
            }))
        if peer_engine is not None:
            print(json.dumps({
                "after_controller": "native_primary_peer.py observe",
                "engine": peer_engine,
                "catalog_format": peer_catalog_format,
                "require_secondary_transfer": True,
                "evidence": f"{PEER_EVIDENCE_DIRECTORY}/peer-verdict.json",
            }))
        return 0
    peer_before: dict[str, Any] | None = None
    member_label: str | None = None
    if peer_engine is not None:
        peer_before = observe_peer_before_controller(
            args, plan, peer_engine, peer_catalog_format
        )
        if needs_member_label:
            member_label = peer_member_label(peer_before)
    command, resume_command = commands(member_label)
    returncode = subprocess.run(command, check=False).returncode
    reboots = 0
    while returncode == REBOOT_REQUESTED_EXIT and resume_command is not None:
        if reboots >= reboots_allowed:
            print(
                "guest bootstrap: the controller requested more reboots than the "
                "selected reboot flags allow; nothing more was run",
                file=sys.stderr,
            )
            return 1
        if fresh_primary:
            # Both guests: the panel-free native secondary first, while this
            # primary still serves, then the primary itself.
            peer_receipt = fixture.reboot_guest(
                plan, "arch", identity, getattr(args, "reboot_timeout", 600)
            )
            print(json.dumps({"peer_reboot": peer_receipt}, sort_keys=True))
        receipt = fixture.reboot_guest(
            plan, args.node, identity, getattr(args, "reboot_timeout", 600)
        )
        print(json.dumps(receipt, sort_keys=True))
        reboots += 1
        returncode = subprocess.run(resume_command, check=False).returncode
    if fresh_primary:
        return finish_fresh_primary_run(args, plan, returncode)
    if peer_engine is not None:
        return finish_peer_verdict(
            args, plan, peer_engine, peer_before, returncode, peer_catalog_format
        )
    return returncode


MEMBER_LABEL_PLACEHOLDER = "<label from peer-before-kill.json>"


def peer_member_label(observation: Any) -> str:
    """The member's unique label in the catalog the peer serves (peer probe)."""

    labels = observation.get("catalog_member_labels") if isinstance(observation, dict) else None
    label = labels.get(ZONE_NAME) if isinstance(labels, dict) else None
    if not isinstance(label, str) or MEMBER_LABEL_RE.fullmatch(label) is None:
        raise BootstrapError(
            "the native primary peer observation names no unique catalog label for "
            f"{ZONE_NAME}; the PowerDNS secondary's consumed member row cannot be "
            "judged. Nothing was started"
        )
    return label


def with_member_label(flags: list[str], label: str | None) -> list[str]:
    """Insert --peer-catalog-member-label=<label> right after the format flag."""

    if label is None:
        raise BootstrapError("a PowerDNS paired secondary needs the peer's member label")
    token = PEER_CATALOG_MEMBER_LABEL_PREFIX + label
    selected = list(flags)
    for index, flag in enumerate(selected):
        if flag in (PEER_CATALOG_BIND_FLAG, PEER_CATALOG_PDNS_FLAG):
            selected.insert(index + 1, token)
            return selected
    raise BootstrapError("the member label needs a peer catalog format flag")


def observe_peer_before_controller(
    args: argparse.Namespace, plan: dict[str, Any], engine: str,
    catalog_format: str | None = None,
) -> dict[str, Any] | None:
    """Peer baseline right before the controller (the guest is not a secondary yet)."""

    import native_primary_peer  # noqa: PLC0415 - imports this module

    catalog_format = catalog_format or DEFAULT_PEER_CATALOG_FORMAT[engine]
    prepared = read_peer_evidence(plan, "peer-prepared.json")
    if not isinstance(prepared, dict) or prepared.get("engine") != engine:
        raise BootstrapError(
            f"the native primary peer was prepared as {prepared.get('engine') if isinstance(prepared, dict) else None!r}, "
            f"not {engine!r}; run-prepared must use the prepared --peer-engine. Nothing was started"
        )
    # Evidence written before --peer-catalog-format existed was always bind.
    prepared_format = prepared.get("catalog_format", "bind")
    if prepared_format != catalog_format:
        raise BootstrapError(
            f"the native primary peer was prepared with --peer-catalog-format "
            f"{prepared_format}, not {catalog_format}; run-prepared must use the "
            "prepared --peer-catalog-format. Nothing was started"
        )
    observed = native_primary_peer.observe(peer_namespace(
        args, engine, require_transfer=False, catalog_format=catalog_format))
    write_peer_evidence(plan, "peer-before-kill.json", observed, execute=True)
    return observed.get("observation")


def _peer_observation_or_error(
    args: argparse.Namespace, engine: str, *, require_transfer: bool,
    catalog_format: str | None = None,
) -> tuple[dict[str, Any] | None, str | None]:
    import native_primary_peer  # noqa: PLC0415 - imports this module

    try:
        observed = native_primary_peer.observe(
            peer_namespace(args, engine, require_transfer=require_transfer,
                           catalog_format=catalog_format)
        )
    except (BootstrapError, fixture.FixtureError, OSError, ValueError, KeyError,
            json.JSONDecodeError, subprocess.SubprocessError) as exc:
        return None, f"{type(exc).__name__}: {exc}"
    return observed.get("observation"), None


PEER_PRODUCER_FOR_FORMAT = {"bind": "bind", "pdns-native": "powerdns"}


def judge_peer_observations(
    before: dict[str, Any] | None,
    after: dict[str, Any] | None,
    required_transfer_passed: bool,
    catalog_format: str | None = None,
) -> dict[str, Any]:
    """Peer half of the paired-secondary pass definition (pure; offline-tested).

    Verified failures: the after-recovery observation shows no logged transfer
    of the catalog or the member to this guest; the peer serves its catalog in
    another producer format than the one prepared (``catalog_format``); or the
    peer's native config digests, catalog producer, serial/members or member
    SOA changed across the run. Unknown: an observation that could not be taken.
    """

    failures: list[str] = []
    unknown: list[str] = []
    if before is None:
        unknown.append("no peer observation before the controller")
    if after is None:
        unknown.append("no peer observation after recovery")
    else:
        if catalog_format is not None:
            expected = PEER_PRODUCER_FOR_FORMAT[catalog_format]
            served = after.get("catalog_producer")
            if served is None:
                unknown.append("the peer observation does not name its catalog producer")
            elif served != expected:
                failures.append(
                    f"the native primary serves its catalog as {served!r}, not the "
                    f"prepared {catalog_format!r} format"
                )
        transfers = after.get("transfers_to_secondary")
        if not isinstance(transfers, dict) or not transfers:
            unknown.append("peer observation has no transfer record")
        else:
            missing = sorted(name for name, seen in transfers.items() if seen is not True)
            if missing:
                failures.append(
                    "the native primary logged no transfer to this guest for: "
                    + ", ".join(missing)
                )
            elif not required_transfer_passed:
                unknown.append(
                    "--require-secondary-transfer did not pass although transfers were logged"
                )
    if before is not None and after is not None:
        for key in ("config_sha256", "catalog_producer", "catalog_serial", "catalog_members",
                    "member_soa", "www_a"):
            if before.get(key) != after.get(key):
                failures.append(
                    f"peer {key} changed across the run: {before.get(key)!r} -> {after.get(key)!r}"
                )
    return {
        "status": "failed" if failures else ("unverified" if unknown else "passed"),
        "failures": failures,
        "unknown": unknown,
    }


def combine_paired_secondary_exit(guest_returncode: int, peer_status: str) -> int:
    """Guest controller exit (0/1/2/64) combined with the host-side peer verdict."""

    if guest_returncode not in (0, 1, 2):
        return guest_returncode
    if guest_returncode == 1 or peer_status == "failed":
        return 1
    if guest_returncode == 2 or peer_status != "passed":
        return 2
    return 0


def finish_peer_verdict(
    args: argparse.Namespace,
    plan: dict[str, Any],
    engine: str,
    before: dict[str, Any] | None,
    guest_returncode: int,
    catalog_format: str | None = None,
) -> int:
    catalog_format = catalog_format or DEFAULT_PEER_CATALOG_FORMAT[engine]
    after, error = _peer_observation_or_error(
        args, engine, require_transfer=True, catalog_format=catalog_format)
    required_passed = error is None
    after_error = None
    if after is None:
        # The strict probe failed; take one read-only observation without the
        # requirement so a missing transfer (failed) is told apart from an
        # unreachable peer (unknown).
        after, after_error = _peer_observation_or_error(
            args, engine, require_transfer=False, catalog_format=catalog_format)
    verdict = judge_peer_observations(before, after, required_passed, catalog_format)
    record = {
        "schema": "celikpanel/dns-kill-paired-secondary-peer-verdict/v1",
        "cell_id": args.cell_id,
        "peer_engine": engine,
        "peer_catalog_format": catalog_format,
        "peer_catalog_producer_expected": PEER_PRODUCER_FOR_FORMAT[catalog_format],
        "guest_controller_exit": guest_returncode,
        "require_secondary_transfer": {"passed": required_passed, "error": error},
        "observation_error": after_error,
        "before_controller": before,
        "after_recovery": after,
        **verdict,
        "combined_exit": combine_paired_secondary_exit(guest_returncode, verdict["status"]),
        "note": (
            "The peer serves the catalog in the format named by peer_catalog_format "
            "(pdns-native: PowerDNS's own PRODUCER catalog). The guest controller "
            "separately judges that the Agent logged accepting that same format."
        ),
    }
    write_peer_evidence(plan, "peer-verdict.json", record, execute=True)
    print(json.dumps(record, sort_keys=True))
    return record["combined_exit"]

def validate_fresh_primary_run(
    args: argparse.Namespace, cell: dict[str, Any], reboot: bool
) -> None:
    """Fresh paired PowerDNS primary flags, refused before anything runs."""

    owner_edit = getattr(args, "owner_edit", None)
    if not isinstance(owner_edit, str):
        owner_edit = None  # argparse default (or an absent attribute)
    release = getattr(args, "owner_release_recovery", False) is True
    lifecycle = getattr(args, "zone_lifecycle", False) is True
    directives = getattr(args, "owner_directives", False) is True
    if directives and not (
        args.source_fixture == UNMANAGED_BIND_STOPPED
        and cell.get("id") == PROVENANCE_BIND_CELLS[UNMANAGED_BIND_STOPPED]
    ):
        raise BootstrapError(
            "--owner-directives applies only to the stopped-BIND takeover cell it was "
            "prepared for; nothing was started"
        )
    if not fresh_pdns_primary_cell(cell):
        if owner_edit is not None or release or lifecycle:
            raise BootstrapError(
                "--owner-edit, --owner-release-recovery and --zone-lifecycle apply only to "
                "the fresh paired PowerDNS primary cells; nothing was started"
            )
        return
    if args.source_fixture != "uninitialized" or args.node != "debian13":
        raise BootstrapError(
            "the fresh paired PowerDNS primary runs only on Debian 13 from the uninitialized "
            "source; nothing was started"
        )
    variant = fresh_pdns_primary_variant(cell)
    if owner_edit is not None:
        if variant == "pre-journal":
            raise BootstrapError("intent:before-write has no journal to hold; nothing was started")
        if owner_edit == "sql" and variant != "post-start":
            raise BootstrapError(
                "--owner-edit sql needs the live database of a started PowerDNS (post-start "
                "cells); nothing was started")
        if reboot or lifecycle:
            raise BootstrapError(
                "the owner-edit hold has no reboot or zone lifecycle step; nothing was started")
    if release and (owner_edit != "config" or variant != "pre-start"):
        raise BootstrapError(
            "--owner-release-recovery needs --owner-edit config on a pre-start cell; "
            "nothing was started")
    if lifecycle and getattr(args, "disable_management_before_reboot", False) is True:
        raise BootstrapError(
            "--zone-lifecycle needs the running Agent; it cannot follow "
            f"{DISABLE_MANAGEMENT_FLAG}; nothing was started")


ZONE_LIFECYCLE_STEPS = ("add", "edit", "delete", "re-add")


def observe_until_converged(
    observe: Any, *, attempts: int = 30, delay: float = 2.0, sleep: Any = time.sleep
) -> tuple[dict[str, Any] | None, str | None]:
    """Read-only peer observation, repeated while the secondary may still be
    transferring (NOTIFY/refresh). Only the last result counts: a mismatch
    still present after every attempt is a verified deviation ("mismatch:"),
    an observation that could not be taken is unknown."""

    error: str | None = None
    for attempt in range(attempts):
        try:
            return observe(), None
        except ValueError as exc:
            error = f"mismatch: {exc}"
        except (BootstrapError, fixture.FixtureError, OSError, KeyError,
                subprocess.SubprocessError) as exc:
            error = f"{type(exc).__name__}: {exc}"
        if attempt + 1 < attempts:
            sleep(delay)
    return None, error


def zone_lifecycle_remote(step: str, *, recover: bool = False) -> str:
    """The Panel's zone-sync V3 RPCs through the trigger, as root:celikpanel."""

    if step not in ZONE_LIFECYCLE_STEPS or (recover and step != "delete"):
        raise BootstrapError(f"unsupported zone lifecycle step {step!r}")
    command = "rpc-pdns-primary-zone-v3-recover" if recover else "rpc-pdns-primary-zone-v3"
    return (
        "sudo /usr/sbin/runuser -u root -g celikpanel -- /usr/bin/env -i "
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin LANG=C.UTF-8 "
        "CELIKPANEL_AGENT_SOCKET=/run/celikpanel/agent.sock "
        "CELIKPANEL_AGENT_TOKEN_FILE=/etc/celikpanel/agent.token "
        f"/opt/celikpanel/bin/dns-kill-trigger {command} "
        "--scenario /var/lib/celikpanel-dns-kill-matrix/scenario.json "
        "--identity-receipt /var/lib/celikpanel-dns-kill-matrix/measured/trigger-identity.json "
        f"--step {shlex.quote(step)} --timeout 2m"
    )


def decode_zone_step(stdout: str) -> dict[str, Any]:
    lines = [line for line in stdout.splitlines() if line.strip()]
    if not lines:
        return {"outcome": "unknown", "error": "the zone step printed no result"}
    try:
        value = json.loads(lines[-1])
    except json.JSONDecodeError as exc:
        return {"outcome": "unknown", "error": f"zone step result is not JSON: {exc}"}
    if not isinstance(value, dict) or value.get("schema") != (
        "celikpanel/dns-kill-matrix-pdns-primary-zone-v3/v1"
    ):
        return {"outcome": "unknown", "error": "zone step result has another schema", "raw": value}
    return value


def judge_zone_step(step: str, value: dict[str, Any], observation_error: str | None) -> str:
    """passed / pending / failed / unverified for one lifecycle step (pure)."""

    outcome = value.get("outcome")
    if outcome == "verified_published":
        if observation_error is None:
            return "passed"
        return "failed" if observation_error.startswith("mismatch:") else "unverified"
    if outcome == "pending_exact_operation" and step == "delete":
        return "pending"
    if outcome in ("refused", "mixed", "refused_predecessor", "refused_existing"):
        return "failed"
    return "unverified"


def run_zone_lifecycle(
    args: argparse.Namespace, plan: dict[str, Any], *, recover_delete: bool = False
) -> int:
    """Add, edit, delete, re-add one child zone of the accepted primary.

    Each step is one exact V3 request; after each the native BIND secondary
    must answer as the primary does (deletion: both authoritative NXDOMAIN
    from the served parent, and the child gone from both catalogs). A pending
    deletion stops the lifecycle and names the owner's next step.
    """

    import native_pdns_bind_peer  # noqa: PLC0415 - imports this module

    node = plan["nodes"][args.node]
    identity = identity_file(args.identity_file)
    steps = ["delete", "re-add"] if recover_delete else list(ZONE_LIFECYCLE_STEPS)
    record: dict[str, Any] = {
        "schema": "celikpanel/dns-kill-fresh-primary-zone-lifecycle/v1",
        "cell_id": args.cell_id, "recover_delete": recover_delete, "steps": [],
    }
    status = "passed"
    for step in steps:
        recover = recover_delete and step == "delete"
        remote = zone_lifecycle_remote(step, recover=recover)
        completed = subprocess.run(
            ssh_base(node, identity) + [remote], check=False, capture_output=True,
            text=True, timeout=300,
        )
        value = decode_zone_step(completed.stdout)
        observation, observation_error = None, None
        if value.get("outcome") == "verified_published":
            observation, observation_error = observe_until_converged(
                lambda: native_pdns_bind_peer.observe_child(argparse.Namespace(
                    work_root=args.work_root, cell_id=args.cell_id,
                    source_fixture="uninitialized", identity_file=args.identity_file,
                    manifest=args.manifest, step=step, execute=True,
                ))
            )
        verdict = judge_zone_step(step, value, observation_error)
        entry = {
            "step": step, "recover": recover, "returncode": completed.returncode,
            "result": value, "observation": observation,
            "observation_error": observation_error, "verdict": verdict,
        }
        if verdict == "pending":
            entry["next_step"] = (
                "the Agent kept the deletion pending (" + str(value.get("job_error_code", ""))
                + "): the server owner enrolls the native BIND secondary for inspection "
                "(dns-peer-enroll --engine bind on both guests, as the Agent's message "
                "names), then runs guest_bootstrap.py zone-lifecycle --recover-delete; no "
                "second deletion is ever requested"
            )
        record["steps"].append(entry)
        print(json.dumps(entry, sort_keys=True))
        if verdict != "passed":
            status = {"failed": "failed", "pending": "pending"}.get(verdict, "unverified")
            break
    record["status"] = status
    write_peer_evidence(
        plan, "zone-lifecycle-recover.json" if recover_delete else "zone-lifecycle.json",
        record, execute=True, directory_name=FRESH_PRIMARY_EVIDENCE_DIRECTORY,
    )
    return {"passed": 0, "failed": 1}.get(status, 2)


def finish_fresh_primary_run(
    args: argparse.Namespace, plan: dict[str, Any], guest_returncode: int
) -> int:
    """Gate-closed pass-through, host-side pair verdict, optional lifecycle."""

    if guest_returncode == GATE_CLOSED_EXIT:
        print(json.dumps({
            "cell_id": args.cell_id, "status": "gate-closed",
            "message": (
                "gate closed in this build: the Agent under test refused the fresh paired "
                "PowerDNS primary before any mutation. This is not a failure; rebuild from "
                "the commit that opens the gate and run the cell again on a fresh fixture."
            ),
        }, sort_keys=True))
        return GATE_CLOSED_EXIT
    if isinstance(getattr(args, "owner_edit", None), str):
        # The hold flow is judged on the guest; the install is deliberately
        # not completed, so there is no pair to transfer.
        return guest_returncode
    combined = finish_fresh_primary_peer_verdict(args, plan, guest_returncode)
    if combined == 0 and getattr(args, "zone_lifecycle", False) is True:
        return run_zone_lifecycle(args, plan)
    return combined


def finish_fresh_primary_peer_verdict(
    args: argparse.Namespace, plan: dict[str, Any], guest_returncode: int
) -> int:
    import native_pdns_bind_peer  # noqa: PLC0415 - imports this module

    observation, error = observe_until_converged(
        lambda: native_pdns_bind_peer.observe(argparse.Namespace(
            work_root=args.work_root, cell_id=args.cell_id, source_fixture="uninitialized",
            identity_file=args.identity_file, manifest=args.manifest,
            address="192.0.2.10", execute=True,
        ))
    )
    status = "passed" if error is None else (
        "failed" if error.startswith("mismatch:") else "unverified")
    record = {
        "schema": "celikpanel/dns-kill-fresh-primary-peer-verdict/v1",
        "cell_id": args.cell_id,
        "guest_controller_exit": guest_returncode,
        "status": status,
        "error": error,
        "observation": observation,
        "combined_exit": combine_paired_secondary_exit(guest_returncode, status),
        "note": (
            "The native BIND secondary loaded this primary's PowerDNS PRODUCER catalog and "
            "member and answers them authoritatively over UDP and TCP exactly as the "
            "primary does."
        ),
    }
    write_peer_evidence(plan, "peer-verdict.json", record, execute=True,
                        directory_name=FRESH_PRIMARY_EVIDENCE_DIRECTORY)
    print(json.dumps(record, sort_keys=True))
    return record["combined_exit"]


def zone_lifecycle_action(args: argparse.Namespace) -> int:
    plan, cell, _ = load_plan(args)
    if not fresh_pdns_primary_cell(cell) or args.source_fixture != "uninitialized":
        raise BootstrapError("zone-lifecycle applies only to the fresh paired PowerDNS primary cells")
    if not args.execute:
        node = plan["nodes"][args.node]
        identity = identity_file(args.identity_file)
        steps = ["delete", "re-add"] if args.recover_delete else list(ZONE_LIFECYCLE_STEPS)
        for step in steps:
            print(json.dumps(ssh_base(node, identity) + [zone_lifecycle_remote(
                step, recover=args.recover_delete and step == "delete")]))
        return 0
    return run_zone_lifecycle(args, plan, recover_delete=args.recover_delete)


def common_parser(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--work-root", required=True, type=Path)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--manifest", type=Path, default=Path(__file__).with_name("manifest.json"))
    parser.add_argument("--node", required=True, choices=("arch", "debian13"))
    parser.add_argument("--identity-file", required=True, type=Path)
    parser.add_argument(
        "--source-fixture",
        required=True,
        choices=(
            "uninitialized",
            "managed-pdns",
            "owner-bind",
            "managed-bind",
            "external-pdns-adoption",
            UNMANAGED_BIND_STOPPED,
            MANAGED_BIND_ABSENT,
        ),
    )
    parser.add_argument(
        "--peer-engine", choices=PEER_ENGINES, default=None,
        help=(
            "paired-secondary cells only: flavour of the panel-free native primary "
            "peer (native_primary_peer.py --engine)"
        ),
    )
    parser.add_argument(
        "--peer-catalog-format", choices=PEER_CATALOG_FORMATS, default=None,
        help=(
            "paired-secondary cells only: catalog producer the native primary peer "
            "serves (native_primary_peer.py --catalog-format); default bind for a "
            "BIND peer and pdns-native for a PowerDNS peer"
        ),
    )
    parser.add_argument("--execute", action="store_true")


def parse_args(argv: Iterable[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="action", required=True)
    current = subparsers.add_parser("install")
    common_parser(current)
    current.add_argument("--agent", required=True, type=Path)
    current.add_argument("--tagged-agent", required=True, type=Path)
    current.add_argument("--panel", required=True, type=Path)
    current.add_argument("--trigger", required=True, type=Path)
    current.add_argument("--web-dir", required=True, type=Path)
    current.add_argument(
        "--agent-unit", type=Path, default=Path("deploy/systemd/celikpanel-agent.service").absolute()
    )
    current.add_argument(
        "--panel-unit", type=Path, default=Path("deploy/systemd/celikpanel-panel.service").absolute()
    )
    current = subparsers.add_parser("prepare-bind")
    common_parser(current)
    current.add_argument(
        "--owner-directives", action="store_true",
        help=(
            "stopped-BIND takeover only: the owner writes `recursion no;` and "
            "`allow-transfer { none; };` into the stopped BIND's options before the "
            "measured operation"
        ),
    )
    current = subparsers.add_parser("prepare-pdns-adopt")
    common_parser(current)
    current.add_argument("--include-deleted-child", action="store_true")
    current = subparsers.add_parser("prepare-pdns-switch")
    common_parser(current)
    current.add_argument("--authority-acceptance", action="store_true")
    current = subparsers.add_parser("run-prepared")
    common_parser(current)
    current.add_argument(INDEPENDENT_PDNS_HANDOFF_FLAG, action="store_true")
    current.add_argument(LATER_BIND_ROLLBACK_FLAG, action="store_true")
    current.add_argument(OWNER_INVERSE_FLAG, action="store_true")
    current.add_argument(STARTUP_ROLLBACK_FLAG, action="store_true")
    current.add_argument(
        RETRY_SWITCH_FLAG, action="store_true",
        help=(
            f"with {OWNER_INVERSE_FLAG} on a PowerDNS -> BIND switch cell: after a "
            "passed rollback (and optional reboot) request the same switch again as a "
            "new request; it must complete forward"
        ),
    )
    current.add_argument(REBOOT_BEFORE_OWNER_FLAG, action="store_true")
    current.add_argument(REBOOT_AFTER_RECOVERY_FLAG, action="store_true")
    current.add_argument(
        REBOOT_EVEN_IF_FAILED_FLAG, action="store_true",
        help=(
            f"with {REBOOT_AFTER_RECOVERY_FLAG}: reboot even when the pre-reboot verdict "
            "is not a complete pass, as a diagnostic; the post-reboot state is recorded "
            "with judged: false and never turns a failure into a pass"
        ),
    )
    current.add_argument(
        DISABLE_MANAGEMENT_FLAG, action="store_true",
        help=(
            f"with {REBOOT_AFTER_RECOVERY_FLAG}: stop and disable the Panel and Agent "
            "units before the reboot; the DNS daemon must then serve alone"
        ),
    )
    current.add_argument(
        "--reboot-timeout", type=int, default=600,
        help="seconds for each guest reboot (fixture.reboot_guest)",
    )
    current.add_argument(
        "--owner-edit", choices=tuple(OWNER_EDIT_FLAGS), default=None,
        help=(
            "fresh paired PowerDNS primary: the owner edits pdns.conf (config) or inserts "
            "a member-zone row into the live database (sql, post-start cells) between the "
            "kill and the Agent restart; the Agent must refuse and hold only DNS"
        ),
    )
    current.add_argument(
        "--owner-release-recovery", action="store_true",
        help=(
            "with --owner-edit config on a pre-start cell: the owner reverts the edit and "
            "finishes the Agent-released install with recover-dns-pdns-fresh-prestart"
        ),
    )
    current.add_argument(
        "--zone-lifecycle", action="store_true",
        help=(
            "fresh paired PowerDNS primary: after a passed run, add, edit, delete and "
            "re-add one child zone through the Agent's zone-sync V3 RPCs"
        ),
    )
    current.add_argument(
        "--owner-directives", action="store_true",
        help="stopped-BIND takeover prepared with --owner-directives",
    )
    current = subparsers.add_parser("zone-lifecycle")
    common_parser(current)
    current.add_argument(
        "--recover-delete", action="store_true",
        help=(
            "resume the pending deletion (same request, RecoverDNSZoneV3) after the owner "
            "enrollment, then re-add"
        ),
    )
    current = subparsers.add_parser("enroll-recovery-runtime")
    common_parser(current)
    current.add_argument("--recovery-runtime", required=True, type=Path)
    return parser.parse_args(argv)


def main(argv: Iterable[str] | None = None) -> int:
    try:
        args = parse_args(argv)
        if sys.platform != "linux" and args.execute:
            raise BootstrapError("fixture guest bootstrap execution requires the Linux QEMU host")
        if args.action == "install":
            install(args)
        elif args.action in {"prepare-bind", "prepare-pdns-adopt", "prepare-pdns-switch"}:
            prepare(args)
        elif args.action == "run-prepared":
            return run_prepared(args)
        elif args.action == "zone-lifecycle":
            return zone_lifecycle_action(args)
        elif args.action == "enroll-recovery-runtime":
            enroll_recovery_runtime(args)
        else:
            raise BootstrapError("unsupported action")
        return 0
    except (BootstrapError, fixture.FixtureError, OSError, subprocess.CalledProcessError) as exc:
        print(f"guest bootstrap: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
