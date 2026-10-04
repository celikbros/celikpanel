#!/usr/bin/env python3
"""Disposable Arch PowerDNS catalog consumer for a Debian managed-BIND primary.

This fixture is deliberately separate from installed CelikPanel hosts.  Its
observation reports native database/catalog and authoritative DNS evidence;
neither a REFUSED reply nor an absent management process proves deletion.
"""

from __future__ import annotations

import argparse
import ipaddress
import json
from pathlib import Path
import subprocess
import tempfile

import fixture
import guest_bootstrap as bootstrap


ZONE = "s1-kill.test"


def pair_addresses(plan: dict, cell: dict) -> tuple[str, str, dict]:
    if (cell.get("driver"), cell.get("role"), cell.get("placement", {}).get("kill_host")) != (
        "pdns-switch", "paired-primary", "debian-13"
    ):
        raise bootstrap.BootstrapError("PowerDNS peer requires a Debian paired-primary switch cell")
    primary = plan["nodes"]["debian13"]
    secondary = plan["nodes"]["arch"]
    primary_ip = str(ipaddress.ip_interface(primary["peer"]["address"]).ip)
    secondary_ip = str(ipaddress.ip_interface(secondary["peer"]["address"]).ip)
    if (primary_ip, secondary_ip) != ("192.0.2.10", "192.0.2.11"):
        raise bootstrap.BootstrapError("fixture addresses differ from paired scenario")
    return primary_ip, secondary_ip, secondary


def catalog_name(primary_ip: str) -> str:
    return "catalog-" + ipaddress.IPv4Address(primary_ip).packed.hex() + ".celikpanel.invalid"


def peer_config(primary_ip: str, secondary_ip: str) -> str:
    ipaddress.IPv4Address(primary_ip)
    ipaddress.IPv4Address(secondary_ip)
    if primary_ip == secondary_ip:
        raise bootstrap.BootstrapError("PowerDNS AXFR peer must differ from its local address")
    return (
        "launch=gsqlite3\n"
        "gsqlite3-database=/var/lib/powerdns/pdns.sqlite3\n"
        f"local-address=127.0.0.1,{secondary_ip}\n"
        "local-port=53\n"
        "primary=no\n"
        "secondary=yes\n"
        "xfr-cycle-interval=1\n"
        "autosecondary=no\n"
        # Production V3 proof requires catalog AXFR from the bound primary.
        f"allow-axfr-ips={primary_ip}/32,127.0.0.1/32\n"
        "disable-axfr=no\n"
        "setuid=powerdns\n"
        "setgid=powerdns\n"
    )


def host_action(args: argparse.Namespace) -> None:
    root = args.work_root.resolve(strict=True)
    plan = fixture.load_cell_plan(root, args.cell_id)
    cell = bootstrap.load_manifest_cell(args.manifest, args.cell_id)
    bootstrap.validate_pdns_switch_cell(cell, "debian13", "managed-bind")
    primary_ip, secondary_ip, peer = pair_addresses(plan, cell)
    identity = bootstrap.identity_file(args.identity_file)
    ssh = bootstrap.ssh_base(peer, identity)
    marker = ("schema=celikpanel/dns-kill-fixture-plan/v1\n"
              f"cell_id={args.cell_id}\nnode=arch\n")
    check = ssh + ["cat /etc/celikpanel-dns-kill-matrix"]
    if args.execute:
        actual = subprocess.run(check, check=True, capture_output=True, text=True).stdout
        if actual != marker:
            raise bootstrap.BootstrapError("Arch peer marker differs from selected disposable cell")
    else:
        bootstrap.run(check, execute=False)
    if args.action == "prepare":
        bootstrap.run(ssh + ["test ! -e /opt/celikpanel/bin/agent && test ! -e /opt/celikpanel/bin/panel && test ! -e /var/lib/powerdns/pdns.sqlite3"], execute=args.execute)
        with tempfile.TemporaryDirectory(prefix="celikpanel-pdns-peer-") as temporary:
            config = Path(temporary) / "pdns.conf"
            config.write_text(peer_config(primary_ip, secondary_ip), encoding="ascii", newline="\n")
            remote = bootstrap.stage_name(args.cell_id) + ".pdns.conf"
            bootstrap.run(bootstrap.scp_base(peer, identity) + [str(config), bootstrap.remote_destination(peer, remote)], execute=args.execute)
            # The exact CONSUMER is staged before the BIND primary exists.
            # Production BIND setup requires a loaded peer member before it
            # can commit, so the peer must be ready to consume its first NOTIFY.
            command = (
                "sudo /usr/bin/pacman -Syu --noconfirm powerdns bind-tools "
                f"&& sudo install -m 0644 -o root -g root {remote} /etc/powerdns/pdns.conf "
                "&& sudo install -d -m 0750 -o powerdns -g powerdns /var/lib/powerdns "
                "&& sudo /usr/bin/python3 -c 'import sqlite3; "
                "db=sqlite3.connect(\"/var/lib/powerdns/pdns.sqlite3\"); "
                "db.executescript(open(\"/usr/share/doc/powerdns/schema.sqlite3.sql\").read()); "
                f"db.execute(\"INSERT INTO domains(name,type,master,account) VALUES(?,?,?,?)\", "
                f"(\"{catalog_name(primary_ip)}\",\"CONSUMER\",\"{primary_ip}\",\"fixture-pdns-peer\")); "
                "db.commit(); db.close()' "
                "&& sudo chown powerdns:powerdns /var/lib/powerdns/pdns.sqlite3 "
                "&& sudo chmod 0640 /var/lib/powerdns/pdns.sqlite3 "
                "&& sudo systemctl enable --now pdns.service "
                "&& systemctl is-active --quiet pdns.service "
                f"&& rm -- {remote}"
            )
            bootstrap.run(ssh + [command], execute=args.execute)
    elif args.action == "observe":
        probe = Path(__file__).with_name("native_pdns_peer_probe.py")
        remote = bootstrap.stage_name(args.cell_id) + ".pdns-probe.py"
        bootstrap.run(bootstrap.scp_base(peer, identity) + [str(probe), bootstrap.remote_destination(peer, remote)], execute=args.execute)
        command = (f"sudo /usr/bin/python3 {remote} --primary {primary_ip} "
                   f"--secondary {secondary_ip} --catalog {catalog_name(primary_ip)} "
                   f"--expect {args.expect}" +
                   (f" --address {ipaddress.IPv4Address(args.address)}" if args.address else "") +
                   f" && rm -- {remote}")
        bootstrap.run(ssh + [command], execute=args.execute)
    else:
        raise bootstrap.BootstrapError("unsupported PowerDNS peer action")
    print(json.dumps({"action": args.action + "-native-pdns-peer", "cell_id": args.cell_id,
                      "primary_ip": primary_ip, "secondary_ip": secondary_ip,
                      "catalog": catalog_name(primary_ip), "management_installed_on_secondary": False,
                      "execute": args.execute}, sort_keys=True))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("prepare", "observe"))
    parser.add_argument("--work-root", type=Path, required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--identity-file", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, default=Path(__file__).with_name("manifest.json"))
    parser.add_argument("--execute", action="store_true")
    parser.add_argument("--expect", choices=("present", "absent"))
    parser.add_argument("--address")
    args = parser.parse_args()
    if args.action == "observe" and (args.expect is None or (args.expect == "present") != (args.address is not None)):
        parser.error("observe requires --expect and an A --address only when present")
    try:
        host_action(args)
    except (bootstrap.BootstrapError, fixture.FixtureError, OSError, KeyError,
            ValueError, subprocess.CalledProcessError) as exc:
        parser.error(str(exc))


if __name__ == "__main__":
    main()
