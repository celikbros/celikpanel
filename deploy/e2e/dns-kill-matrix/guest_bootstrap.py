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
from typing import Any, Iterable

import fixture


SOURCE_PROOF_SCHEMA = "celikpanel/dns-kill-source-proof/v1"
SCENARIO_SCHEMA = "celikpanel-dns-kill-matrix-trigger/v1"
CELL_RE = re.compile(r"[a-z0-9][a-z0-9_.-]{0,239}")
SHA256_RE = re.compile(r"[0-9a-f]{64}")
EARLY_UNINITIALIZED_PHASES = frozenset({"pre-intent", "intent", "target-staged"})
CRITICAL_MANAGED_PDNS_PHASES = frozenset({"source-stopped", "target-started", "rolled-back"})
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
if len(sys.argv) not in (2, 3, 4):
    raise SystemExit("unexpected prepared controller invocation")
if len(sys.argv) >= 3:
    handoff_flag = "--stop-after-kill-for-independent-recovery"
    if (sys.argv[1] not in {
                "pdns-adopt__rolling-back__after-write__standalone__peer-reachable",
                "bind__rolling-back__after-write__standalone__peer-reachable",
            }
            or sys.argv[2] != handoff_flag
            or handoff_flag in argv
            or argv.count("--trigger-mode") != 1):
        raise SystemExit("independent recovery handoff is not valid for prepared cell")
    trigger_index = argv.index("--trigger-mode")
    if argv[trigger_index + 1:trigger_index + 2] != ["socket"]:
        raise SystemExit("independent recovery handoff requires a socket trigger")
    argv.append(handoff_flag)
if len(sys.argv) == 4:
    later_flag = "--bind-rollback-after-target-started"
    if (sys.argv[1] != "bind__rolling-back__after-write__standalone__peer-reachable"
            or sys.argv[3] != later_flag or later_flag in argv):
        raise SystemExit("later BIND rollback requires the exact handoff cell")
    argv.append(later_flag)
os.execv(argv[0], argv)
"""

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


def validate_bind_cell(cell: dict[str, Any], node: str, source_fixture: str) -> None:
    if cell.get("driver") != "bind" or cell.get("role") not in {
        "standalone",
        "paired-primary",
    }:
        raise BootstrapError("BIND bootstrap supports standalone or paired-primary only")
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
    if cell.get("role") == "paired-primary":
        if node != "arch" or source_fixture != "uninitialized":
            raise BootstrapError("paired-primary bootstrap requires an uninitialized Arch source")
    if source_fixture == "uninitialized":
        if (
            source_policy
            not in {"driver-specific", "uninitialized-permitted-noncritical"}
            or phase not in EARLY_UNINITIALIZED_PHASES
        ):
            raise BootstrapError(
                "uninitialized source requires a driver-supported fixture policy "
                "and an early BIND phase"
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
                "policy and a supported critical BIND cell "
                "on certified Debian"
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
    if cell.get("driver") != "pdns-switch" or cell.get("role") not in {"standalone", "paired-primary"}:
        raise BootstrapError("PowerDNS switch fixture supports standalone or paired-primary only")
    placement = cell.get("placement", {})
    if (
        node != "debian13"
        or NODE_FOR_PLACEMENT.get(placement.get("kill_host")) != node
        or placement.get("source_fixture_policy") != "driver-specific"
        or source_fixture not in {"managed-bind", "uninitialized"}
    ):
        raise BootstrapError("PowerDNS switch requires the certified Debian placement")
    if source_fixture == "uninitialized" and (
        cell.get("role") != "paired-primary" or cell.get("boundary", {}).get("phase") != "intent"
    ):
        raise BootstrapError("fresh PowerDNS fixture is limited to paired-primary intent")
    if cell.get("boundary", {}).get("phase") not in {
        "pre-intent", "intent", "target-staged", "source-stopped",
        "target-started", "target-verified", "committed",
        "rolling-back", "rolled-back",
    }:
        raise BootstrapError("PowerDNS switch fixture has an unsupported matrix phase")


def pdns_switch_scenario(*, role: str = "standalone", authority_acceptance: bool = False,
                         source_fixture: str = "managed-bind") -> dict[str, Any]:
    if role not in {"standalone", "paired-primary"}:
        raise BootstrapError("unsupported PowerDNS switch source role")
    if source_fixture not in {"managed-bind", "uninitialized"} or (
        source_fixture == "uninitialized" and role != "paired-primary"
    ):
        raise BootstrapError("unsupported PowerDNS switch source fixture")
    source = bind_scenario(
        "uninitialized", role=role, node="debian13",
        allow_debian_paired_source=role == "paired-primary",
        authority_acceptance=authority_acceptance,
    )
    zones = source["zones"]
    if source_fixture == "uninitialized":
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


def prepare(args: argparse.Namespace) -> None:
    _, cell, node = load_plan(args)
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
    with tempfile.TemporaryDirectory(prefix="celikpanel-s1-scenario-") as temporary:
        temporary_path = Path(temporary)
        scenario_path = temporary_path / "scenario.json"
        scenario_path.write_bytes(json_bytes(scenario))
        uploads = [scenario_path]
        if args.source_fixture == "managed-pdns":
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
                        else None
                    ),
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


def run_prepared(args: argparse.Namespace) -> int:
    _, cell, node = load_plan(args)
    validate_supported_cell(cell, args.node, args.source_fixture)
    if args.source_fixture == "owner-bind" and not (
        args.stop_after_kill_for_independent_recovery
        and getattr(args, "bind_rollback_after_target_started", False) is True
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
    if getattr(args, "bind_rollback_after_target_started", False) is True and not (
        args.stop_after_kill_for_independent_recovery
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
        and args.source_fixture in {"managed-pdns", "owner-bind"}
    ):
        raise BootstrapError("later BIND rollback requires the exact Debian handoff cell")
    identity = identity_file(args.identity_file)
    remote = (
        "sudo /usr/sbin/runuser -u root -g celikpanel -- /usr/bin/env -i "
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin "
        "LANG=C.UTF-8 /usr/bin/python3 -c "
        + shlex.quote(RUN_PREPARED_CODE)
        + " "
        + shlex.quote(args.cell_id)
        + (
            " " + shlex.quote(INDEPENDENT_PDNS_HANDOFF_FLAG)
            if args.stop_after_kill_for_independent_recovery
            else ""
        )
        + (
            " " + shlex.quote(LATER_BIND_ROLLBACK_FLAG)
            if getattr(args, "bind_rollback_after_target_started", False) is True
            else ""
        )
    )
    command = ssh_base(node, identity) + [remote]
    if not args.execute:
        print(json.dumps(command))
        return 0
    return subprocess.run(command, check=False).returncode

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
        else:
            raise BootstrapError("unsupported action")
        return 0
    except (BootstrapError, fixture.FixtureError, OSError, subprocess.CalledProcessError) as exc:
        print(f"guest bootstrap: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
