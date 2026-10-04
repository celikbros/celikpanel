#!/usr/bin/env python3
"""Observe the isolated BIND-primary/PowerDNS-secondary authority pair."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import subprocess
import sys

import fixture
import guest_bootstrap as bootstrap
import native_pdns_peer

CELL = "pdns-switch__intent__after-write__paired-primary__peer-reachable"


def remote(node: dict, identity: Path, command: str, execute: bool) -> str:
    argv = bootstrap.ssh_base(node, identity) + [command]
    if not execute:
        print(json.dumps(argv))
        return ""
    return subprocess.run(argv, check=True, capture_output=True, text=True,
                          timeout=30).stdout.strip()


def observe(args: argparse.Namespace) -> dict:
    root = args.work_root.resolve(strict=True)
    plan = fixture.load_cell_plan(root, args.cell_id)
    cell = bootstrap.load_manifest_cell(args.manifest, args.cell_id)
    bootstrap.validate_pdns_switch_cell(cell, "debian13", "managed-bind")
    primary_ip, secondary_ip, secondary = native_pdns_peer.pair_addresses(plan, cell)
    if args.cell_id != CELL or (primary_ip, secondary_ip) != ("192.0.2.10", "192.0.2.11"):
        raise bootstrap.BootstrapError("authority witness requires the exact disposable pair")
    primary = plan["nodes"]["debian13"]
    identity = bootstrap.identity_file(args.identity_file)
    for node, name in ((primary, "debian13"), (secondary, "arch")):
        marker = remote(node, identity, "cat /etc/celikpanel-dns-kill-matrix", args.execute)
        expected = (f"schema=celikpanel/dns-kill-fixture-plan/v1\n"
                    f"cell_id={CELL}\nnode={name}")
        if args.execute and marker != expected:
            raise ValueError("disposable guest marker differs")
    boot_ids = {
        "primary": remote(primary, identity, "cat /proc/sys/kernel/random/boot_id", args.execute),
        "secondary": remote(secondary, identity, "cat /proc/sys/kernel/random/boot_id", args.execute),
    }
    post_reboot = args.previous_primary_boot_id is not None
    if post_reboot and args.execute and (
        boot_ids["primary"] == args.previous_primary_boot_id or
        boot_ids["secondary"] == args.previous_secondary_boot_id
    ):
        raise ValueError("both native guests must have new boot IDs")
    remote(primary, identity, "systemctl is-active --quiet named.service && systemctl is-enabled --quiet named.service", args.execute)
    remote(secondary, identity, "systemctl is-active --quiet pdns.service && systemctl is-enabled --quiet pdns.service", args.execute)
    remote(secondary, identity, "test ! -e /opt/celikpanel/bin/agent && test ! -e /opt/celikpanel/bin/panel", args.execute)
    if post_reboot:
        remote(primary, identity,
               "! systemctl is-active --quiet celikpanel-agent.service && "
               "! systemctl is-active --quiet celikpanel-panel.service && "
               "! systemctl is-enabled --quiet celikpanel-agent.service && "
               "! systemctl is-enabled --quiet celikpanel-panel.service", args.execute)
    probe = Path(__file__).with_name("native_bind_pdns_authority_probe.py")
    stage = bootstrap.stage_name(CELL) + ".authority-probe.py"
    bootstrap.run(bootstrap.scp_base(secondary, identity) +
                  [str(probe), bootstrap.remote_destination(secondary, stage)],
                  execute=args.execute)
    result = remote(secondary, identity,
                    f"sudo /usr/bin/python3 {stage}", args.execute)
    proof = json.loads(result) if args.execute else None
    return {
        "schema": "celikpanel/native-bind-pdns-authority-pair/v1",
        "cell_id": CELL, "execute": args.execute,
        "primary_boot_id": boot_ids["primary"],
        "secondary_boot_id": boot_ids["secondary"],
        "post_reboot": post_reboot,
        "authority": proof,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--work-root", type=Path, required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--identity-file", type=Path, required=True)
    parser.add_argument("--manifest", type=Path,
                        default=Path(__file__).with_name("manifest.json"))
    parser.add_argument("--previous-primary-boot-id")
    parser.add_argument("--previous-secondary-boot-id")
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    if (args.previous_primary_boot_id is None) != (args.previous_secondary_boot_id is None):
        parser.error("post-reboot observation requires both previous boot IDs")
    try:
        print(json.dumps(observe(args), sort_keys=True))
        return 0
    except (bootstrap.BootstrapError, fixture.FixtureError, OSError, ValueError,
            KeyError, json.JSONDecodeError, subprocess.SubprocessError) as exc:
        print(f"native authority pair unavailable: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
