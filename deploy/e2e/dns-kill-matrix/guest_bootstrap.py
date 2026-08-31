#!/usr/bin/env python3
"""Install matrix artifacts and prepare honest DNS source state in a fixture VM."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
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
CRITICAL_MANAGED_PDNS_PHASES = frozenset({"source-stopped", "target-started"})
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
    if cell.get("driver") != "bind" or cell.get("role") != "standalone":
        raise BootstrapError("this milestone prepares only standalone BIND target cells")
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
    elif source_fixture == "managed-pdns":
        if (
            source_policy != "managed-pdns-required"
            or node != "debian13"
            or phase not in CRITICAL_MANAGED_PDNS_PHASES
        ):
            raise BootstrapError(
                "managed PowerDNS source preinstall requires the managed fixture "
                "policy and a critical source-stopped/target-started BIND cell "
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


def validate_supported_cell(
    cell: dict[str, Any], node: str, source_fixture: str
) -> None:
    driver = cell.get("driver")
    if driver == "bind":
        validate_bind_cell(cell, node, source_fixture)
    elif driver == "pdns-adopt":
        validate_pdns_adopt_cell(cell, node, source_fixture)
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


def bind_scenario(source_fixture: str) -> dict[str, Any]:
    if source_fixture == "uninitialized":
        source_engine, source_epoch, target_epoch, revision = "", 0, 1, 0
    elif source_fixture == "managed-pdns":
        source_engine, source_epoch, target_epoch, revision = "pdns", 1, 2, 0
    else:
        raise BootstrapError("unsupported source fixture")
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
        "topology": "standalone",
        "zones": [zone_snapshot()],
    }


def pdns_adoption_source_setup_scenario() -> dict[str, Any]:
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
        "zones": [zone_snapshot()],
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
        scenario = bind_scenario(args.source_fixture)
        guest_action = "prepare-bind"
    elif args.action == "prepare-pdns-adopt":
        validate_pdns_adopt_cell(cell, args.node, args.source_fixture)
        scenario = pdns_adoption_source_setup_scenario()
        guest_action = "prepare-pdns-adopt"
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
    return parser.parse_args(argv)


def main(argv: Iterable[str] | None = None) -> int:
    try:
        args = parse_args(argv)
        if sys.platform != "linux" and args.execute:
            raise BootstrapError("fixture guest bootstrap execution requires the Linux QEMU host")
        if args.action == "install":
            install(args)
        elif args.action in {"prepare-bind", "prepare-pdns-adopt"}:
            prepare(args)
        else:
            raise BootstrapError("unsupported action")
        return 0
    except (BootstrapError, fixture.FixtureError, OSError, subprocess.CalledProcessError) as exc:
        print(f"guest bootstrap: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
