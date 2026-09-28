#!/usr/bin/env python3
"""Prepare and observe an Arch BIND consumer of a Debian PowerDNS catalog.

Only the exact disposable paired-primary switch cell is accepted. Preparation
does not require the primary to exist yet; observation requires both native
servers to publish the same catalog and authoritative member answers.
"""

from __future__ import annotations

import argparse
import ipaddress
import json
from pathlib import Path
import re
import subprocess
import tempfile

import fixture
import guest_bootstrap as bootstrap
from native_pdns_peer_probe import parse_catalog_axfr


CELL = "pdns-switch__intent__after-write__paired-primary__peer-reachable"
ZONE = "s1-kill.test"
QUERY = "www." + ZONE


def pair_addresses(plan: dict, cell: dict) -> tuple[str, str, dict]:
    if cell.get("id") != CELL or (
        cell.get("driver"), cell.get("role"), cell.get("placement", {}).get("kill_host")
    ) != ("pdns-switch", "paired-primary", "debian-13"):
        raise bootstrap.BootstrapError("fixture requires the exact Debian PowerDNS primary cell")
    primary = plan["nodes"]["debian13"]
    secondary = plan["nodes"]["arch"]
    primary_ip = str(ipaddress.ip_interface(primary["peer"]["address"]).ip)
    secondary_ip = str(ipaddress.ip_interface(secondary["peer"]["address"]).ip)
    if (primary_ip, secondary_ip) != ("192.0.2.10", "192.0.2.11"):
        raise bootstrap.BootstrapError("fixture peer addresses differ from the paired cell")
    return primary_ip, secondary_ip, secondary


def catalog_name(primary_ip: str) -> str:
    return "catalog-" + ipaddress.IPv4Address(primary_ip).packed.hex() + ".celikpanel.invalid"


def secondary_config(primary_ip: str, secondary_ip: str) -> str:
    primary = ipaddress.IPv4Address(primary_ip)
    secondary = ipaddress.IPv4Address(secondary_ip)
    if primary == secondary:
        raise bootstrap.BootstrapError("DNS pair must have distinct addresses")
    catalog = catalog_name(primary_ip)
    return (
        "options {\n"
        '    directory "/var/named";\n'
        f"    listen-on port 53 {{ 127.0.0.1; {secondary}; }};\n"
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
        '    file "celikpanel-fixture-catalog.zone";\n'
        f"    allow-transfer {{ {primary}; 127.0.0.1; }};\n"
        "};\n"
    )


def selected(args: argparse.Namespace) -> tuple[str, str, dict, Path]:
    if args.cell_id != CELL:
        raise bootstrap.BootstrapError("only the exact disposable cross-engine cell is supported")
    root = args.work_root.resolve(strict=True)
    plan = fixture.load_cell_plan(root, args.cell_id)
    cell = bootstrap.load_manifest_cell(args.manifest, args.cell_id)
    bootstrap.validate_pdns_switch_cell(cell, "debian13", args.source_fixture)
    primary_ip, secondary_ip, peer = pair_addresses(plan, cell)
    return primary_ip, secondary_ip, peer, bootstrap.identity_file(args.identity_file)


def verify_guest(ssh: list[str], cell_id: str, execute: bool) -> None:
    command = ssh + ["cat /etc/celikpanel-dns-kill-matrix"]
    if execute:
        marker = subprocess.run(command, check=True, capture_output=True, text=True).stdout
        expected = (
            "schema=celikpanel/dns-kill-fixture-plan/v1\n"
            f"cell_id={cell_id}\nnode=arch\n"
        )
        if marker != expected:
            raise bootstrap.BootstrapError("Arch guest marker differs from the disposable cell")
    else:
        bootstrap.run(command, execute=False)
    bootstrap.run(
        ssh + ["test ! -e /opt/celikpanel/bin/agent && test ! -e /opt/celikpanel/bin/panel"],
        execute=execute,
    )


def prepare(args: argparse.Namespace) -> dict:
    primary_ip, secondary_ip, peer, identity = selected(args)
    ssh = bootstrap.ssh_base(peer, identity)
    verify_guest(ssh, args.cell_id, args.execute)
    bootstrap.run(
        ssh + ["test ! -e /var/named/celikpanel-fixture-catalog.zone && ! systemctl is-active --quiet named.service"],
        execute=args.execute,
    )
    remote = bootstrap.stage_name(args.cell_id) + ".bind-peer.conf"
    with tempfile.TemporaryDirectory(prefix="celikpanel-pdns-bind-peer-") as temporary:
        config = Path(temporary) / "named.conf"
        config.write_text(secondary_config(primary_ip, secondary_ip), encoding="ascii", newline="\n")
        bootstrap.run(
            ssh + ["sudo /usr/bin/pacman -Syu --noconfirm bind bind-tools"],
            execute=args.execute,
        )
        bootstrap.run(
            bootstrap.scp_base(peer, identity)
            + [str(config), bootstrap.remote_destination(peer, remote)],
            execute=args.execute,
        )
        bootstrap.run(
            ssh + [
                f"sudo install -m 0640 -o root -g named {remote} /etc/named.conf"
                " && sudo named-checkconf /etc/named.conf"
                " && sudo systemctl enable --now named.service"
                " && systemctl is-active --quiet named.service"
                f" && rm -- {remote}"
            ],
            execute=args.execute,
        )
    return {
        "action": "prepare-native-pdns-bind-peer", "cell_id": args.cell_id,
        "primary_ip": primary_ip, "secondary_ip": secondary_ip,
        "catalog": catalog_name(primary_ip), "management_installed_on_secondary": False,
        "execute": args.execute,
    }


def remote_read(ssh: list[str], command: str, execute: bool) -> str:
    if not execute:
        bootstrap.run(ssh + [command], execute=False)
        return ""
    return subprocess.run(
        ssh + [command], check=True, capture_output=True, text=True, timeout=15,
    ).stdout


def dig_command(ip: str, name: str, kind: str, tcp: bool = False) -> str:
    ipaddress.IPv4Address(ip)
    if name not in {ZONE, QUERY, catalog_name("192.0.2.10")} or kind not in {"SOA", "A", "AXFR"}:
        raise bootstrap.BootstrapError("unsupported fixture DNS query")
    return (
        "dig +time=2 +tries=1 +norecurse +noall +comments +answer "
        + ("+tcp " if tcp else "") + f"@{ip} {name} {kind}"
    )


def authoritative_data(reply: str, name: str, kind: str) -> tuple[str, ...]:
    if "status: NOERROR" not in reply or re.search(r"flags: qr aa\b", reply) is None:
        raise ValueError("native member reply is not authoritative")
    rows = [line.split() for line in reply.splitlines() if line and not line.startswith(";")]
    if len(rows) != 1 or rows[0][0] != name + "." or "IN" not in rows[0]:
        raise ValueError("native member answer is not exact")
    index = rows[0].index("IN")
    if index + 2 >= len(rows[0]) or rows[0][index + 1] != kind:
        raise ValueError("native member record type differs")
    return tuple(rows[0][index + 2:])


def observe(args: argparse.Namespace) -> dict:
    primary_ip, secondary_ip, peer, identity = selected(args)
    expected_address = str(ipaddress.IPv4Address(args.address))
    ssh = bootstrap.ssh_base(peer, identity)
    verify_guest(ssh, args.cell_id, args.execute)
    remote_read(ssh, "systemctl is-active --quiet named.service", args.execute)
    catalog = catalog_name(primary_ip)
    source = remote_read(ssh, dig_command(primary_ip, catalog, "AXFR", True), args.execute)
    loaded = remote_read(ssh, dig_command("127.0.0.1", catalog, "AXFR", True), args.execute)
    if args.execute:
        source_serial, source_members = parse_catalog_axfr(source, catalog, producer="powerdns")
        loaded_serial, loaded_members = parse_catalog_axfr(loaded, catalog, producer="powerdns")
        if (source_serial, source_members) != (loaded_serial, loaded_members) or source_members != {ZONE}:
            raise ValueError("loaded BIND catalog differs from the PowerDNS primary")
    for name, kind in ((ZONE, "SOA"), (QUERY, "A")):
        source_data = None
        for ip in (primary_ip, secondary_ip):
            for tcp in (False, True):
                reply = remote_read(ssh, dig_command(ip, name, kind, tcp), args.execute)
                if not args.execute:
                    continue
                data = authoritative_data(reply, name, kind)
                if source_data is None:
                    source_data = data
                elif data != source_data:
                    raise ValueError("native member differs between primary and secondary")
                if kind == "A" and data != (expected_address,):
                    raise ValueError("loaded member A record differs from expectation")
    return {
        "action": "observe-native-pdns-bind-peer", "cell_id": args.cell_id,
        "primary_ip": primary_ip, "secondary_ip": secondary_ip, "catalog": catalog,
        "catalog_serial": source_serial if args.execute else None,
        "catalog_members": sorted(source_members) if args.execute else None,
        "member": ZONE, "address": expected_address,
        "authoritative_udp_tcp": args.execute,
        "management_installed_on_secondary": False, "execute": args.execute,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("prepare", "observe"))
    parser.add_argument("--work-root", type=Path, required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--source-fixture", choices=("managed-bind", "uninitialized"),
                        default="managed-bind")
    parser.add_argument("--identity-file", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, default=Path(__file__).with_name("manifest.json"))
    parser.add_argument("--address", help="expected member A address for observe")
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    if (args.action == "observe") != (args.address is not None):
        parser.error("--address is required exactly for observe")
    try:
        print(json.dumps(prepare(args) if args.action == "prepare" else observe(args), sort_keys=True))
    except (bootstrap.BootstrapError, fixture.FixtureError, OSError, ValueError,
            KeyError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as exc:
        parser.error(str(exc))


if __name__ == "__main__":
    main()
