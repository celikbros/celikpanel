#!/usr/bin/env python3
"""Prepare and observe the disposable PowerDNS/PowerDNS native pair.

The Arch peer has no Panel or Agent. Preparation only stages its CONSUMER;
this script never starts a production source switch or retries a DNS mutation.
Observation is accepted only after the Debian source is native PowerDNS.
"""

from __future__ import annotations

import argparse
import ipaddress
import json
from pathlib import Path
import subprocess

import fixture
import guest_bootstrap as bootstrap
import native_pdns_peer


CELL = "pdns-switch__intent__after-write__paired-primary__peer-reachable"
PRIMARY = "192.0.2.10"
SECONDARY = "192.0.2.11"


def selected(args: argparse.Namespace) -> tuple[dict, dict, Path]:
    if args.cell_id != CELL:
        raise bootstrap.BootstrapError("only the exact disposable paired-primary cell is accepted")
    root = args.work_root.resolve(strict=True)
    plan = fixture.load_cell_plan(root, CELL)
    cell = bootstrap.load_manifest_cell(args.manifest, CELL)
    bootstrap.validate_pdns_switch_cell(cell, "debian13", "managed-bind")
    if native_pdns_peer.pair_addresses(plan, cell)[:2] != (PRIMARY, SECONDARY):
        raise bootstrap.BootstrapError("disposable pair addresses differ")
    return plan["nodes"]["debian13"], plan["nodes"]["arch"], bootstrap.identity_file(args.identity_file)


def remote(node: dict, identity: Path, command: str, execute: bool) -> str:
    argv = bootstrap.ssh_base(node, identity) + [command]
    if not execute:
        bootstrap.run(argv, execute=False)
        return ""
    return subprocess.run(argv, check=True, capture_output=True, text=True, timeout=30).stdout


def exact_marker(node: dict, identity: Path, name: str, execute: bool) -> None:
    actual = remote(node, identity, "cat /etc/celikpanel-dns-kill-matrix", execute)
    expected = ("schema=celikpanel/dns-kill-fixture-plan/v1\n"
                f"cell_id={CELL}\nnode={name}\n")
    if execute and actual != expected:
        raise bootstrap.BootstrapError(f"disposable {name} marker differs")


def observe(args: argparse.Namespace) -> dict:
    primary, secondary, identity = selected(args)
    address = str(ipaddress.IPv4Address(args.address))
    exact_marker(primary, identity, "debian13", args.execute)
    exact_marker(secondary, identity, "arch", args.execute)
    remote(secondary, identity,
           "test ! -e /opt/celikpanel/bin/agent && test ! -e /opt/celikpanel/bin/panel",
           args.execute)
    # Install only observation code in disposable guest home. No service, DNS,
    # database or Agent state is changed by this action.
    stage = bootstrap.stage_name(CELL) + "-pdns-pair"
    remote(primary, identity, "install -d -m 0700 " + stage, args.execute)
    scripts = ("native_pdns_peer_probe.py", "native_pdns_pdns_primary_probe.py")
    for source_name in scripts:
        source = Path(__file__).with_name(source_name)
        remote_name = stage + "/" + source_name
        bootstrap.run(bootstrap.scp_base(primary, identity) +
                      [str(source), bootstrap.remote_destination(primary, remote_name)],
                      execute=args.execute)
    source_probe = stage + "/native_pdns_pdns_primary_probe.py"
    source_result = remote(primary, identity,
                           "sudo /usr/bin/python3 " + source_probe +
                           f" --primary {PRIMARY} --secondary {SECONDARY} --address {address}",
                           args.execute)
    peer_probe = Path(__file__).with_name("native_pdns_peer_probe.py")
    peer_stage = bootstrap.stage_name(CELL) + ".native_pdns_peer_probe.py"
    bootstrap.run(bootstrap.scp_base(secondary, identity) +
                  [str(peer_probe), bootstrap.remote_destination(secondary, peer_stage)],
                  execute=args.execute)
    catalog = native_pdns_peer.catalog_name(PRIMARY)
    peer_result = remote(secondary, identity,
                         "sudo /usr/bin/python3 " + peer_stage +
                         f" --primary {PRIMARY} --secondary {SECONDARY} --catalog {catalog}"
                         f" --expect present --address {address}", args.execute)
    if args.execute:
        source = json.loads(source_result)
        peer = json.loads(peer_result)
        if (source["catalog_serial"], source["catalog_members"]) != (
            peer["catalog_serial"], peer["catalog_members"]
        ) or source["catalog_members"] != ["s1-kill.test"]:
            raise ValueError("native PowerDNS producer and CONSUMER catalog differ")
    return {
        "schema": "celikpanel/native-pdns-pdns-pair-observation/v1",
        "cell_id": CELL, "execute": args.execute,
        "source": source if args.execute else None,
        "peer": peer if args.execute else None,
        "management_installed_on_secondary": False,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("prepare", "observe"))
    parser.add_argument("--work-root", type=Path, required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--identity-file", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, default=Path(__file__).with_name("manifest.json"))
    parser.add_argument("--address", help="expected www.s1-kill.test A address; observe only")
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    if (args.action == "observe") != (args.address is not None):
        parser.error("--address is required exactly for observe")
    try:
        if args.action == "prepare":
            selected(args)
            native_pdns_peer.host_action(argparse.Namespace(**{
                **vars(args), "expect": None,
            }))
        else:
            print(json.dumps(observe(args), sort_keys=True))
    except (bootstrap.BootstrapError, fixture.FixtureError, OSError, ValueError,
            KeyError, json.JSONDecodeError, subprocess.CalledProcessError,
            subprocess.TimeoutExpired) as exc:
        parser.error(str(exc))


if __name__ == "__main__":
    main()
