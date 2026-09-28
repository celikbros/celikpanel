#!/usr/bin/env python3
"""Prepare a panel-free BIND catalog secondary in a disposable DNS kill cell."""

from __future__ import annotations

import argparse
import ipaddress
import json
from pathlib import Path
import subprocess
import tempfile

import fixture
import guest_bootstrap as bootstrap


def secondary_config(primary_ip: str, secondary_ip: str, authoritative_parent: bool = False) -> str:
    primary = ipaddress.IPv4Address(primary_ip)
    secondary = ipaddress.IPv4Address(secondary_ip)
    if primary == secondary:
        raise bootstrap.BootstrapError("DNS pair must use distinct peer addresses")
    catalog = "catalog-" + primary.packed.hex() + ".celikpanel.invalid"
    base = (
        "options {\n"
        '    directory "/var/cache/bind";\n'
        f"    listen-on {{ 127.0.0.1; {secondary}; }};\n"
        "    listen-on-v6 { none; };\n"
        "    recursion no;\n"
        "    allow-query { any; };\n"
        "    allow-transfer { none; };\n"
        "    catalog-zones {\n"
        f'        zone "{catalog}" default-primaries {{ {primary}; }} in-memory yes;\n'
        "    };\n"
        "};\n"
        f'zone "{catalog}" {{\n'
        "    type secondary;\n"
        f"    primaries {{ {primary}; }};\n"
        f"    allow-transfer {{ {primary}; 127.0.0.1; }};\n"
        "};\n"
    )
    if not authoritative_parent:
        return base
    return base + (
        'zone "test" {\n'
        "    type primary;\n"
        '    file "/etc/bind/celikpanel-fixture-parent.zone";\n'
        "    allow-transfer { none; };\n"
        "};\n"
    )


def parent_zone(secondary_ip: str) -> str:
    secondary = ipaddress.IPv4Address(secondary_ip)
    return (
        "$TTL 60\n"
        "@ IN SOA ns.test. hostmaster.test. ( 1 3600 600 604800 60 )\n"
        "@ IN NS ns.test.\n"
        f"ns IN A {secondary}\n"
    )


def pair_addresses(plan: dict, cell: dict) -> tuple[str, str, dict]:
    if (
        cell.get("driver") != "bind"
        or cell.get("role") != "paired-primary"
        or cell.get("placement", {}).get("kill_host") != "arch"
    ):
        raise bootstrap.BootstrapError("native peer bootstrap requires an Arch BIND primary cell")
    primary = plan["nodes"]["arch"]
    secondary = plan["nodes"]["debian13"]
    primary_ip = str(ipaddress.ip_interface(primary["peer"]["address"]).ip)
    secondary_ip = str(ipaddress.ip_interface(secondary["peer"]["address"]).ip)
    if primary_ip != "192.0.2.11" or secondary_ip != "192.0.2.10":
        raise bootstrap.BootstrapError("fixture peer addresses differ from the paired scenario")
    return primary_ip, secondary_ip, secondary


def prepare(args: argparse.Namespace) -> None:
    root = args.work_root.resolve(strict=True)
    plan = fixture.load_cell_plan(root, args.cell_id)
    cell = bootstrap.load_manifest_cell(args.manifest, args.cell_id)
    bootstrap.validate_bind_cell(cell, "arch", "uninitialized")
    primary_ip, secondary_ip, secondary = pair_addresses(plan, cell)
    authoritative_parent = getattr(args, "authoritative_parent", False)
    if authoritative_parent and args.cell_id != (
        "bind__intent__after-write__paired-primary__peer-reachable"
    ):
        raise bootstrap.BootstrapError(
            "the parent-negative proof belongs only to the exact paired deletion cell"
        )
    identity = bootstrap.identity_file(args.identity_file)
    remote_path = bootstrap.stage_name(args.cell_id) + ".peer.conf"
    ssh = bootstrap.ssh_base(secondary, identity)
    marker_command = ssh + ["cat /etc/celikpanel-dns-kill-matrix"]
    expected_marker = (
        "schema=celikpanel/dns-kill-fixture-plan/v1\n"
        f"cell_id={args.cell_id}\nnode=debian13\n"
    )
    if args.execute:
        marker = subprocess.run(
            marker_command, check=True, capture_output=True, text=True
        )
        if marker.stdout != expected_marker:
            raise bootstrap.BootstrapError(
                "peer guest marker does not match the selected disposable cell"
            )
    else:
        bootstrap.run(marker_command, execute=False)
    bootstrap.run(
        ssh + ["test ! -e /opt/celikpanel/bin/agent && test ! -e /opt/celikpanel/bin/panel"],
        execute=args.execute,
    )
    with tempfile.TemporaryDirectory(prefix="celikpanel-bind-peer-") as temporary:
        config_path = Path(temporary) / "named.conf"
        parent_path = Path(temporary) / "celikpanel-fixture-parent.zone"
        config_path.write_text(
            secondary_config(primary_ip, secondary_ip, authoritative_parent),
            encoding="ascii", newline="\n"
        )
        if authoritative_parent:
            parent_path.write_text(parent_zone(secondary_ip), encoding="ascii", newline="\n")
        bootstrap.run(ssh + ["sudo apt-get update -qq"], execute=args.execute)
        bootstrap.run(
            ssh + ["sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq bind9"],
            execute=args.execute,
        )
        bootstrap.run(
            bootstrap.scp_base(secondary, identity)
            + [str(config_path), bootstrap.remote_destination(secondary, remote_path)],
            execute=args.execute,
        )
        parent_remote_path = remote_path + ".parent.zone"
        if authoritative_parent:
            bootstrap.run(
                bootstrap.scp_base(secondary, identity)
                + [str(parent_path), bootstrap.remote_destination(secondary, parent_remote_path)],
                execute=args.execute,
            )
        parent_install = ""
        if authoritative_parent:
            parent_install = (
                f" && sudo install -m 0644 -o root -g root {parent_remote_path} "
                "/etc/bind/celikpanel-fixture-parent.zone"
                " && sudo named-checkzone test /etc/bind/celikpanel-fixture-parent.zone"
            )
        bootstrap.run(
            ssh + [
                f"sudo install -m 0644 -o root -g root {remote_path} /etc/bind/named.conf"
                + parent_install
                + " && sudo named-checkconf /etc/bind/named.conf"
                + " && sudo systemctl enable --now named.service"
                + " && sudo systemctl restart named.service"
                + " && systemctl is-active --quiet named.service"
                + f" && rm -- {remote_path}"
                + (f" {parent_remote_path}" if authoritative_parent else "")
            ],
            execute=args.execute,
        )
    print(json.dumps({
        "action": "prepare-native-bind-peer",
        "cell_id": args.cell_id,
        "primary_ip": primary_ip,
        "secondary_ip": secondary_ip,
        "engine": "bind",
        "management_installed_on_secondary": False,
        "execute": args.execute,
    }, sort_keys=True))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--work-root", type=Path, required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--identity-file", type=Path, required=True)
    parser.add_argument("--authoritative-parent", action="store_true")
    parser.add_argument(
        "--manifest", type=Path, default=Path(__file__).with_name("manifest.json")
    )
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    try:
        prepare(args)
    except (
        bootstrap.BootstrapError, fixture.FixtureError, OSError, ValueError,
        KeyError, subprocess.CalledProcessError,
    ) as exc:
        parser.error(str(exc))


if __name__ == "__main__":
    main()
