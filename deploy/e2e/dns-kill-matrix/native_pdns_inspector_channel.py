#!/usr/bin/env python3
"""Enroll and probe the optional PowerDNS-native SSH peer in a disposable pair.

This tool accepts one manifest cell and its QEMU marker. It installs only an
inspector, owner policy, and restricted SSH credential on a panel-free peer.
It does not infer a successful mutation from a probe; Agent V3 must consume
the exact response and retire its own challenge journal.
"""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import secrets
import subprocess
import tempfile

import fixture
import guest_bootstrap as bootstrap
import native_bind_inspector_channel as bind_channel
import native_pdns_peer


CELL = "pdns-switch__intent__after-write__paired-primary__peer-reachable"
PRIMARY_IP = "192.0.2.10"
PEER_IP = "192.0.2.11"
CATALOG = "catalog-c000020a.celikpanel.invalid"
STAGE = "/home/celik/celikpanel-pdns-inspector"
INSPECTOR = "/opt/celikpanel/bin/pdns-peer-inspect"
WRAPPER = "/opt/celikpanel/libexec/pdns-peer-inspect-forced"
KEY_DIR = "/var/lib/celikpanel-agent-private/pdns-peer-inspection-keys"
RECORD = "/var/lib/celikpanel-agent-private/pdns-peer-inspection-v1.json"


def canonical(value: dict) -> bytes:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode("ascii")


def selected(args: argparse.Namespace) -> tuple[dict, dict, Path]:
    if args.cell_id != CELL:
        raise ValueError("only the exact disposable paired PowerDNS switch cell is accepted")
    root = args.work_root.resolve(strict=True)
    plan = fixture.load_cell_plan(root, args.cell_id)
    cell = bootstrap.load_manifest_cell(args.manifest, args.cell_id)
    bootstrap.validate_pdns_switch_cell(cell, "debian13", "managed-bind")
    if native_pdns_peer.pair_addresses(plan, cell)[:2] != (PRIMARY_IP, PEER_IP):
        raise ValueError("disposable peer addresses changed")
    identity = bootstrap.identity_file(args.identity_file)
    primary, peer = plan["nodes"]["debian13"], plan["nodes"]["arch"]
    for node, name in ((primary, "debian13"), (peer, "arch")):
        expected = ("schema=celikpanel/dns-kill-fixture-plan/v1\n"
                    f"cell_id={CELL}\nnode={name}\n")
        if remote(node, identity, "cat /etc/celikpanel-dns-kill-matrix") != expected:
            raise ValueError(f"disposable {name} guest marker changed")
    remote(peer, identity, "test ! -e /opt/celikpanel/bin/agent && test ! -e /opt/celikpanel/bin/panel")
    return primary, peer, identity


def remote(node: dict, identity: Path, command: str) -> str:
    return subprocess.run(bootstrap.ssh_base(node, identity) + [command],
                          check=True, capture_output=True, text=True, timeout=45).stdout


def transfer(node: dict, identity: Path, source: Path, destination: str) -> None:
    subprocess.run(bootstrap.scp_base(node, identity) +
                   [str(source), bootstrap.remote_destination(node, destination)],
                   check=True, timeout=45)


def local_key_dir(args: argparse.Namespace) -> Path:
    root = args.work_root.resolve(strict=True)
    plan = fixture.load_cell_plan(root, args.cell_id)
    directory = Path(plan["cell_directory"]) / "pdns-peer-inspector"
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    if directory.is_symlink() or directory.stat().st_mode & 0o077:
        raise ValueError("unsafe disposable credential directory")
    return directory


def enroll(args: argparse.Namespace) -> None:
    _, peer, identity = selected(args)
    if args.inspector is None or not args.inspector.is_file():
        raise ValueError("a built Linux pdns-peer-inspect executable is required")
    remote(peer, identity, "systemctl is-active --quiet pdns.service")
    local = local_key_dir(args)
    key = local / "id_ed25519"
    if key.exists() or key.with_suffix(".pub").exists():
        raise ValueError("disposable enrollment exists; refuse to replace it")
    subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(key)], check=True)
    public = key.with_suffix(".pub").read_text(encoding="ascii").strip()
    host_line = remote(peer, identity, "sudo cat /etc/ssh/ssh_host_ed25519_key.pub").strip()
    host_parts = host_line.split()
    if len(host_parts) not in (2, 3) or host_parts[0] != "ssh-ed25519":
        raise ValueError("peer SSH host identity is not Ed25519")
    host_digest = bind_channel.public_key_digest(host_line)
    policy = {"schema": "celikpanel-pdns-peer-inspector-policy/v1",
              "primary_ip": PRIMARY_IP, "peer_ip": PEER_IP,
              "catalog_name": CATALOG, "catalog_account": "fixture-pdns-peer"}
    staged = {
        ".policy.json": canonical(policy),
        ".authorized_key": ('restrict,from="' + PRIMARY_IP + '" ' +
                             " ".join(public.split()[:2]) + "\n").encode("ascii"),
        ".sudoers": ('celikpeer ALL=(root) NOPASSWD: ' + INSPECTOR + ' ""\n').encode("ascii"),
    }
    for suffix, data in staged.items():
        destination = local / ("staged" + suffix)
        destination.write_bytes(data)
        transfer(peer, identity, destination, STAGE + suffix)
    for source, suffix in ((args.inspector, ".bin"),
                           (Path(__file__).with_name("native_pdns_inspector_forced.sh"), ".wrapper"),
                           (Path(__file__).with_name("native_pdns_inspector_sshd.conf"), ".sshd.conf")):
        transfer(peer, identity, source, STAGE + suffix)
    command = " && ".join((
        "sudo install -d -m 0755 -o root -g root /opt/celikpanel /opt/celikpanel/bin /opt/celikpanel/libexec",
        f"sudo install -m 0755 -o root -g root {STAGE}.bin {INSPECTOR}",
        f"sudo install -m 0755 -o root -g root {STAGE}.wrapper {WRAPPER}",
        "sudo install -d -m 0750 -o root -g root /etc/pdns-peer-inspector",
        f"sudo install -m 0600 -o root -g root {STAGE}.policy.json /etc/pdns-peer-inspector/policy.json",
        "sudo install -d -m 0755 -o root -g root /etc/ssh/authorized_keys",
        f"sudo install -m 0644 -o root -g root {STAGE}.authorized_key /etc/ssh/authorized_keys/celikpeer",
        "(id -u celikpeer >/dev/null 2>&1 || sudo useradd -m -s /bin/sh celikpeer)",
        "sudo passwd -l celikpeer",
        f"sudo install -m 0440 -o root -g root {STAGE}.sudoers /etc/sudoers.d/celikpanel-native-pdns-peer",
        "sudo visudo -cf /etc/sudoers.d/celikpanel-native-pdns-peer",
        f"sudo install -m 0644 -o root -g root {STAGE}.sshd.conf /etc/ssh/sshd_config.d/99-celikpanel-native-pdns-peer.conf",
        "sudo /usr/bin/sshd -t", "sudo systemctl reload sshd.service",
    ))
    remote(peer, identity, command)
    known = local / "known_hosts"
    known.write_text(PEER_IP + " " + " ".join(host_parts[:2]) + "\n", encoding="ascii", newline="\n")
    print(json.dumps({"fixture_enrolled": True, "peer_host_key_sha256": host_digest,
                      "policy_sha256": hashlib.sha256(canonical(policy)).hexdigest()}, sort_keys=True))


def agent_enroll(args: argparse.Namespace) -> None:
    primary, peer, identity = selected(args)
    local = local_key_dir(args)
    key, public, known = (local / "id_ed25519", local / "id_ed25519.pub", local / "known_hosts")
    if not key.is_file() or not public.is_file() or not known.is_file():
        raise ValueError("first enroll the isolated PowerDNS inspector")
    host_digest = bind_channel.public_key_digest(
        remote(peer, identity, "sudo cat /etc/ssh/ssh_host_ed25519_key.pub").strip())
    client_digest = bind_channel.public_key_digest(public.read_text(encoding="ascii"))
    credential_id = secrets.token_hex(16)
    record = {"schema": "celikpanel-pdns-peer-inspection/v1", "engine": "pdns",
              "enrollment_id": secrets.token_hex(16), "revision": 1,
              "primary_ip": PRIMARY_IP, "peer_ip": PEER_IP,
              "catalog_name": CATALOG, "view": "_default",
              "ssh_username": "celikpeer", "host_key_sha256": host_digest,
              "credential_id": credential_id, "client_public_key_sha256": client_digest}
    with tempfile.TemporaryDirectory(prefix="pdns-agent-enroll-") as directory:
        source = Path(directory) / "enrollment.json"
        source.write_bytes(canonical(record))
        transfer(primary, identity, source, STAGE + ".agent-enrollment")
    transfer(primary, identity, key, STAGE + ".key")
    remote(primary, identity, " && ".join((
        "sudo test -d /var/lib/celikpanel-agent-private",
        f"sudo test ! -e {RECORD}",
        f"sudo install -d -m 0700 -o root -g root {KEY_DIR}",
        f"sudo install -m 0600 -o root -g root {STAGE}.key {KEY_DIR}/{credential_id}.key",
        f"sudo install -m 0600 -o root -g root {STAGE}.agent-enrollment {RECORD}",
        f"rm -- {STAGE}.key {STAGE}.agent-enrollment",
        f"sudo test -s {RECORD}",
    )))
    print(json.dumps({"agent_enrolled": True, "record_sha256": hashlib.sha256(canonical(record)).hexdigest(),
                      "enrollment_id": record["enrollment_id"], "credential_id": credential_id}, sort_keys=True))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("enroll", "agent-enroll"))
    parser.add_argument("--work-root", type=Path, required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--identity-file", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, default=Path(__file__).with_name("manifest.json"))
    parser.add_argument("--inspector", type=Path)
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    if not args.execute:
        parser.error("disposable enrollment requires --execute")
    try:
        {"enroll": enroll, "agent-enroll": agent_enroll}[args.action](args)
    except (ValueError, OSError, KeyError, fixture.FixtureError,
            bootstrap.BootstrapError, subprocess.CalledProcessError) as error:
        parser.error(str(error))


if __name__ == "__main__":
    main()
