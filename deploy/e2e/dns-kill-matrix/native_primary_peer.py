#!/usr/bin/env python3
"""Prepare and observe a panel-free native PRIMARY peer for a paired-SECONDARY cell.

The guest under test becomes a CelikPanel-managed secondary (BIND or PowerDNS)
of this peer. The peer runs only native packages and configuration: no
CelikPanel Panel, Agent, HTTPS API or remote record-management automation
(D-022: standard primary/secondary transfer must not require a remote
CelikPanel). Two flavours are selectable with ``--engine``:

* ``bind``: named with two ``type primary`` zones read from zone files;
* ``pdns``: PowerDNS gsqlite3 with two ordinary ``MASTER`` zones.

Both flavours serve the product catalog ``catalog-<hex(peer)>.celikpanel.invalid``
byte-for-byte in the format written by ``binddns.renderCatalogZone`` /
``binddns.CatalogZoneRecords`` (SOA/NS ``invalid.``, TTL 60, version "2",
SHA-224 member labels). The PowerDNS flavour deliberately does NOT use a native
PowerDNS ``PRODUCER`` catalog: its 32-character base32hex member labels and
TTL 0 properties are rejected by the Agent's paired-secondary catalog reader,
which applies the BIND producer policy (cmd/agent/dns_engine_host.go
probeDNSCatalogAXFR, cmd/agent/dns_engine_pdns_catalog.go peerPDNSCatalog).

AXFR is allowed only to the guest under test and the peer's own loopback (for
the read-only probe); NOTIFY is sent only to the guest under test. Dry-run is
the default: without ``--execute`` every command is printed, none is run.
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
import native_primary_peer_probe as probe


ZONE = probe.ZONE
CATALOG_SERIAL = probe.CATALOG_SERIAL
FIXTURE_ACCOUNT = probe.FIXTURE_ACCOUNT
NODE_ADDRESSES = {"debian13": "192.0.2.10", "arch": "192.0.2.11"}
ENGINES = ("bind", "pdns")
SUPPORTED_DRIVERS = frozenset({"bind", "pdns-switch"})
# Early fresh-secondary cells only. Managed-PowerDNS-source secondary cells have
# no guest-side producer yet; widen together with guest_bootstrap.
SUPPORTED_SOURCE_POLICIES = frozenset({"driver-specific", "uninitialized-permitted-noncritical"})
SAFE_TOKEN = re.compile(r'^[A-Za-z0-9 ."\-]+$')

LAYOUTS = {
    ("debian13", "bind"): {
        "unit": "named.service",
        "config": "/etc/bind/named.conf",
        "config_owner": "root:bind",
        "config_mode": "0644",
        "directory": "/var/cache/bind",
        "catalog_file": "/etc/bind/celikpanel-fixture-catalog.zone",
        "member_file": "/etc/bind/celikpanel-fixture-member.zone",
        "zone_owner": "root:bind",
        "zone_mode": "0644",
        "packages": ("bind9",),
    },
    ("arch", "bind"): {
        "unit": "named.service",
        "config": "/etc/named.conf",
        "config_owner": "root:named",
        "config_mode": "0640",
        "directory": "/var/named",
        "catalog_file": "/var/named/celikpanel-fixture-catalog.zone",
        "member_file": "/var/named/celikpanel-fixture-member.zone",
        "zone_owner": "root:named",
        "zone_mode": "0640",
        "packages": ("bind",),
    },
    ("debian13", "pdns"): {
        "unit": "pdns.service",
        "config": "/etc/powerdns/pdns.conf",
        "config_owner": "root:pdns",
        "config_mode": "0640",
        "database": "/var/lib/powerdns/pdns.sqlite3",
        "database_owner": "pdns:pdns",
        "database_directory_mode": "0755",
        "schema": "/usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql",
        # Debian's unit already runs as User=pdns.
        "setuid": None,
        "packages": ("pdns-server", "pdns-backend-sqlite3"),
    },
    ("arch", "pdns"): {
        "unit": "pdns.service",
        "config": "/etc/powerdns/pdns.conf",
        "config_owner": "root:root",
        "config_mode": "0644",
        "database": "/var/lib/powerdns/pdns.sqlite3",
        "database_owner": "powerdns:powerdns",
        "database_directory_mode": "0750",
        "schema": "/usr/share/doc/powerdns/schema.sqlite3.sql",
        "setuid": "powerdns",
        "packages": ("powerdns",),
    },
}


# ---------------------------------------------------------------------------
# Pure rendering (offline-tested).
# ---------------------------------------------------------------------------

def check_pair(primary_ip: str, secondary_ip: str) -> tuple[str, str]:
    primary = ipaddress.IPv4Address(primary_ip)
    secondary = ipaddress.IPv4Address(secondary_ip)
    if str(primary) != primary_ip or str(secondary) != secondary_ip:
        raise bootstrap.BootstrapError("peer addresses must be canonical IPv4")
    if primary == secondary:
        raise bootstrap.BootstrapError("DNS pair must use distinct peer addresses")
    if primary not in probe.PEER_NETWORK or secondary not in probe.PEER_NETWORK:
        raise bootstrap.BootstrapError("DNS pair must stay on the isolated 192.0.2.0/24 link")
    return primary_ip, secondary_ip


def catalog_members(members: list[str] | None = None) -> list[str]:
    selected = [ZONE] if members is None else list(members)
    if len(set(selected)) != len(selected) or any(not probe.canonical_fqdn(m) for m in selected):
        raise bootstrap.BootstrapError("catalog member set is not canonical")
    return sorted(selected)


def catalog_zone_text(primary_ip: str, serial: int = CATALOG_SERIAL,
                      members: list[str] | None = None) -> str:
    """Byte-for-byte mirror of internal/binddns/pairing.go renderCatalogZone."""

    if not 0 < serial <= 0xFFFFFFFF:
        raise bootstrap.BootstrapError("catalog serial must be positive")
    catalog = probe.catalog_name(primary_ip)
    lines = [
        f"$ORIGIN {catalog}.",
        "$TTL 60",
        f"@ IN SOA invalid. invalid. {serial} 60 30 3600 30",
        "@ IN NS invalid.",
        'version IN TXT "2"',
    ]
    for member in catalog_members(members):
        lines.append(f"{probe.catalog_member_label(member)}.zones IN PTR {member}.")
    return "\n".join(lines) + "\n"


def catalog_records(primary_ip: str, serial: int = CATALOG_SERIAL,
                    members: list[str] | None = None) -> list[tuple[str, str, str, int]]:
    """Mirror of binddns.CatalogZoneRecords: (name, type, content, ttl)."""

    if not 0 < serial <= 0xFFFFFFFF:
        raise bootstrap.BootstrapError("catalog serial must be positive")
    catalog = probe.catalog_name(primary_ip)
    records = [
        (catalog, "SOA", f"invalid. invalid. {serial} 60 30 3600 30", 60),
        (catalog, "NS", "invalid.", 60),
        ("version." + catalog, "TXT", '"2"', 60),
    ]
    for member in catalog_members(members):
        records.append((probe.catalog_member_label(member) + ".zones." + catalog, "PTR", member, 60))
    return records


def member_zone_records(primary_ip: str, secondary_ip: str) -> list[tuple[str, str, str, int]]:
    """The s1-kill.test records a paired primary at ``primary_ip`` publishes.

    Mirrors guest_bootstrap.bind_scenario(role="paired-primary") for the node
    that plays the primary: ns1/www point at the primary, ns2 at the secondary.
    """

    check_pair(primary_ip, secondary_ip)
    return [
        (ZONE, "SOA", "ns1.s1-kill.test hostmaster.s1-kill.test 2026083101 10800 3600 604800 3600", 3600),
        (ZONE, "NS", "ns1.s1-kill.test", 3600),
        ("ns1." + ZONE, "A", primary_ip, 300),
        (probe.QUERY, "A", primary_ip, 300),
        (ZONE, "NS", "ns2.s1-kill.test", 3600),
        ("ns2." + ZONE, "A", secondary_ip, 300),
    ]


def _absolute_content(kind: str, content: str) -> str:
    if kind in {"NS", "PTR", "CNAME"}:
        return content if content.endswith(".") else content + "."
    if kind == "SOA":
        fields = content.split()
        return " ".join(
            [field if field.endswith(".") else field + "." for field in fields[:2]] + fields[2:]
        )
    return content


def member_zone_text(primary_ip: str, secondary_ip: str) -> str:
    lines = [f"$ORIGIN {ZONE}.", "$TTL 3600"]
    for name, kind, content, ttl in member_zone_records(primary_ip, secondary_ip):
        lines.append(f"{name}. {ttl} IN {kind} {_absolute_content(kind, content)}")
    return "\n".join(lines) + "\n"


def bind_primary_config(node: str, primary_ip: str, secondary_ip: str) -> str:
    check_pair(primary_ip, secondary_ip)
    layout = LAYOUTS[(node, "bind")]
    catalog = probe.catalog_name(primary_ip)
    zone_policy = (
        f"    allow-transfer {{ {secondary_ip}; 127.0.0.1; }};\n"
        f"    also-notify {{ {secondary_ip}; }};\n"
        "    notify explicit;\n"
    )
    return (
        "// Disposable CelikPanel kill-matrix native primary peer; no management software.\n"
        "options {\n"
        f'    directory "{layout["directory"]}";\n'
        f"    listen-on port 53 {{ 127.0.0.1; {primary_ip}; }};\n"
        "    listen-on-v6 { none; };\n"
        "    recursion no;\n"
        "    allow-query { any; };\n"
        "    allow-transfer { none; };\n"
        "    notify explicit;\n"
        "};\n"
        f'zone "{catalog}" {{\n'
        "    type primary;\n"
        f'    file "{layout["catalog_file"]}";\n'
        + zone_policy +
        "};\n"
        f'zone "{ZONE}" {{\n'
        "    type primary;\n"
        f'    file "{layout["member_file"]}";\n'
        + zone_policy +
        "};\n"
    )


def pdns_primary_config(node: str, primary_ip: str, secondary_ip: str) -> str:
    check_pair(primary_ip, secondary_ip)
    layout = LAYOUTS[(node, "pdns")]
    lines = [
        "# Disposable CelikPanel kill-matrix native primary peer; no management software.",
        "launch=gsqlite3",
        f"gsqlite3-database={layout['database']}",
        f"local-address=127.0.0.1,{primary_ip}",
        "local-port=53",
        "primary=yes",
        "secondary=no",
        "autosecondary=no",
        "disable-axfr=no",
        f"allow-axfr-ips={secondary_ip}/32,127.0.0.1/32",
        f"also-notify={secondary_ip}",
        f"only-notify={secondary_ip}/32",
        "api=no",
        "webserver=no",
        # Notice/info level carries the AXFR-out and notification lines.
        "loglevel=6",
    ]
    if layout["setuid"]:
        lines += [f"setuid={layout['setuid']}", f"setgid={layout['setuid']}"]
    return "\n".join(lines) + "\n"


def _sql_text(value: str) -> str:
    if SAFE_TOKEN.fullmatch(value) is None:
        raise bootstrap.BootstrapError("PowerDNS seed value is outside the fixture alphabet")
    return "'" + value.replace("'", "''") + "'"


def pdns_seed_sql(primary_ip: str, secondary_ip: str) -> str:
    """Rows for two ordinary MASTER zones; notified_serial NULL forces NOTIFY."""

    check_pair(primary_ip, secondary_ip)
    catalog = probe.catalog_name(primary_ip)
    statements = ["BEGIN;"]
    zones = (
        (catalog, catalog_records(primary_ip)),
        (ZONE, member_zone_records(primary_ip, secondary_ip)),
    )
    for zone, _records in zones:
        statements.append(
            "INSERT INTO domains(name,type,account) VALUES("
            f"{_sql_text(zone)},'MASTER',{_sql_text(FIXTURE_ACCOUNT)});"
        )
    for zone, records in zones:
        for name, kind, content, ttl in records:
            statements.append(
                "INSERT INTO records(domain_id,name,type,content,ttl,prio,disabled,auth) "
                f"SELECT id,{_sql_text(name)},{_sql_text(kind)},{_sql_text(content)},"
                f"{int(ttl)},0,0,1 FROM domains WHERE name={_sql_text(zone)};"
            )
    statements.append("COMMIT;")
    return "\n".join(statements) + "\n"


# ---------------------------------------------------------------------------
# Cell selection and safety.
# ---------------------------------------------------------------------------

def pair_nodes(plan: dict, cell: dict) -> tuple[str, str, str, str]:
    """Return (primary_node, primary_ip, secondary_node, secondary_ip)."""

    if cell.get("role") != "paired-secondary" or cell.get("driver") not in SUPPORTED_DRIVERS:
        raise bootstrap.BootstrapError(
            "native primary peer requires a bind or pdns-switch paired-secondary cell"
        )
    placement = cell.get("placement", {})
    if placement.get("source_fixture_policy") not in SUPPORTED_SOURCE_POLICIES:
        raise bootstrap.BootstrapError(
            "managed-source secondary cells have no guest producer; only fresh cells are supported"
        )
    secondary_node = bootstrap.NODE_FOR_PLACEMENT.get(placement.get("kill_host"))
    primary_node = bootstrap.NODE_FOR_PLACEMENT.get(placement.get("dns_peer_host"))
    if secondary_node is None or primary_node is None or secondary_node == primary_node:
        raise bootstrap.BootstrapError("paired-secondary placement must name two distinct guests")
    if cell.get("driver") == "pdns-switch" and secondary_node != "debian13":
        raise bootstrap.BootstrapError("PowerDNS secondary under test requires certified Debian 13")
    addresses = {}
    for node in (primary_node, secondary_node):
        addresses[node] = str(ipaddress.ip_interface(plan["nodes"][node]["peer"]["address"]).ip)
        if addresses[node] != NODE_ADDRESSES[node]:
            raise bootstrap.BootstrapError("fixture peer addresses differ from the paired scenario")
    check_pair(addresses[primary_node], addresses[secondary_node])
    return primary_node, addresses[primary_node], secondary_node, addresses[secondary_node]


def selected(args: argparse.Namespace) -> tuple[str, str, str, dict, Path]:
    if args.engine not in ENGINES:
        raise bootstrap.BootstrapError("native primary engine must be bind or pdns")
    root = args.work_root.resolve(strict=True)
    plan = fixture.load_cell_plan(root, args.cell_id)
    cell = bootstrap.load_manifest_cell(args.manifest, args.cell_id)
    primary_node, primary_ip, _secondary_node, secondary_ip = pair_nodes(plan, cell)
    identity = bootstrap.identity_file(args.identity_file)
    return primary_node, primary_ip, secondary_ip, plan["nodes"][primary_node], identity


def remote_read(ssh: list[str], command: str, execute: bool) -> str:
    if not execute:
        bootstrap.run(ssh + [command], execute=False)
        return ""
    return subprocess.run(
        ssh + [command], check=True, capture_output=True, text=True, timeout=60,
    ).stdout


def verify_peer_guest(ssh: list[str], cell_id: str, node: str, execute: bool) -> None:
    marker = remote_read(ssh, "cat /etc/celikpanel-dns-kill-matrix", execute)
    expected = (
        "schema=celikpanel/dns-kill-fixture-plan/v1\n"
        f"cell_id={cell_id}\nnode={node}\n"
    )
    if execute and marker != expected:
        raise bootstrap.BootstrapError("peer guest marker does not match the selected disposable cell")


def fresh_peer_check(node: str, engine: str) -> str:
    layout = LAYOUTS[(node, engine)]
    checks = [
        "test ! -e /opt/celikpanel/bin/agent",
        "test ! -e /opt/celikpanel/bin/panel",
        "! systemctl is-active --quiet named.service",
        "! systemctl is-active --quiet bind9.service",
        "! systemctl is-active --quiet pdns.service",
    ]
    if engine == "bind":
        checks += [f"test ! -e {layout['catalog_file']}", f"test ! -e {layout['member_file']}"]
    else:
        checks.append(f"test ! -e {layout['database']}")
    return " && ".join(checks)


def install_command(node: str, engine: str) -> str:
    packages = " ".join(LAYOUTS[(node, engine)]["packages"])
    if node == "arch":
        return f"sudo /usr/bin/pacman -Syu --noconfirm {packages}"
    # Mask during install so the package default never serves before the
    # fixture configuration is in place (same pattern as guest_bootstrap.sh).
    units = "named.service bind9.service" if engine == "bind" else "pdns.service"
    return (
        "sudo apt-get update -qq"
        f" && sudo systemctl mask {units}"
        " && sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq"
        f" --no-install-recommends {packages}"
        f" && sudo systemctl unmask {units}"
        " && sudo systemctl daemon-reload"
    )


def activation_command(node: str, engine: str, staged: dict[str, str], catalog: str) -> str:
    layout = LAYOUTS[(node, engine)]
    owner, group = layout["config_owner"].split(":")
    parts = [
        f"sudo install -m {layout['config_mode']} -o {owner} -g {group} "
        f"{staged['config']} {layout['config']}"
    ]
    if engine == "bind":
        zone_owner, zone_group = layout["zone_owner"].split(":")
        for key in ("catalog_file", "member_file"):
            parts.append(
                f"sudo install -m {layout['zone_mode']} -o {zone_owner} -g {zone_group} "
                f"{staged[key]} {layout[key]}"
            )
        parts += [
            f"sudo named-checkconf {layout['config']}",
            f"sudo named-checkzone {catalog} {layout['catalog_file']}",
            f"sudo named-checkzone {ZONE} {layout['member_file']}",
        ]
    else:
        db_owner, db_group = layout["database_owner"].split(":")
        parts += [
            f"test -f {layout['schema']}",
            f"sudo install -d -m {layout['database_directory_mode']} -o {db_owner} -g {db_group} "
            "/var/lib/powerdns",
            "sudo /usr/bin/python3 -c 'import sqlite3,sys; "
            "db=sqlite3.connect(sys.argv[1]); "
            "db.executescript(open(sys.argv[2]).read()); "
            "db.executescript(open(sys.argv[3]).read()); db.close()' "
            f"{layout['database']} {layout['schema']} {staged['seed']}",
            f"sudo chown {db_owner}:{db_group} {layout['database']}",
            f"sudo chmod 0640 {layout['database']}",
        ]
    parts += [
        f"sudo systemctl enable {layout['unit']}",
        f"sudo systemctl restart {layout['unit']}",
        f"systemctl is-active --quiet {layout['unit']}",
        "rm -- " + " ".join(staged[key] for key in sorted(staged)),
    ]
    return " && ".join(parts)


def rendered_files(node: str, engine: str, primary_ip: str, secondary_ip: str) -> dict[str, str]:
    if engine == "bind":
        return {
            "config": bind_primary_config(node, primary_ip, secondary_ip),
            "catalog_file": catalog_zone_text(primary_ip),
            "member_file": member_zone_text(primary_ip, secondary_ip),
        }
    return {
        "config": pdns_primary_config(node, primary_ip, secondary_ip),
        "seed": pdns_seed_sql(primary_ip, secondary_ip),
    }


# ---------------------------------------------------------------------------
# Actions.
# ---------------------------------------------------------------------------

def prepare(args: argparse.Namespace) -> dict:
    node, primary_ip, secondary_ip, peer, identity = selected(args)
    ssh = bootstrap.ssh_base(peer, identity)
    verify_peer_guest(ssh, args.cell_id, node, args.execute)
    bootstrap.run(ssh + [fresh_peer_check(node, args.engine)], execute=args.execute)
    catalog = probe.catalog_name(primary_ip)
    files = rendered_files(node, args.engine, primary_ip, secondary_ip)
    stage = bootstrap.stage_name(args.cell_id) + ".primary-peer."
    staged = {key: stage + key for key in files}
    bootstrap.run(ssh + [install_command(node, args.engine)], execute=args.execute)
    with tempfile.TemporaryDirectory(prefix="celikpanel-primary-peer-") as temporary:
        for key, text in files.items():
            local = Path(temporary) / key
            local.write_text(text, encoding="ascii", newline="\n")
            bootstrap.run(
                bootstrap.scp_base(peer, identity)
                + [str(local), bootstrap.remote_destination(peer, staged[key])],
                execute=args.execute,
            )
        bootstrap.run(
            ssh + [activation_command(node, args.engine, staged, catalog)],
            execute=args.execute,
        )
    return {
        "action": "prepare-native-primary-peer",
        "cell_id": args.cell_id,
        "engine": args.engine,
        "primary_node": node,
        "primary_ip": primary_ip,
        "secondary_ip": secondary_ip,
        "catalog": catalog,
        "catalog_serial": CATALOG_SERIAL,
        "catalog_members": catalog_members(),
        "management_installed_on_primary": False,
        "execute": args.execute,
    }


def observe(args: argparse.Namespace) -> dict:
    node, primary_ip, secondary_ip, peer, identity = selected(args)
    ssh = bootstrap.ssh_base(peer, identity)
    verify_peer_guest(ssh, args.cell_id, node, args.execute)
    layout = LAYOUTS[(node, args.engine)]
    catalog = probe.catalog_name(primary_ip)
    remote_probe = bootstrap.stage_name(args.cell_id) + ".primary-peer-probe.py"
    bootstrap.run(
        bootstrap.scp_base(peer, identity)
        + [str(Path(probe.__file__).resolve()), bootstrap.remote_destination(peer, remote_probe)],
        execute=args.execute,
    )
    configs = [layout["config"]] + (
        [layout["catalog_file"], layout["member_file"]] if args.engine == "bind" else []
    )
    command = (
        f"sudo /usr/bin/python3 {remote_probe} --engine {args.engine}"
        f" --primary {primary_ip} --secondary {secondary_ip} --catalog {catalog}"
        + "".join(f" --config {path}" for path in configs)
        + (" --require-secondary-transfer" if args.require_secondary_transfer else "")
        + f"; status=$?; rm -f -- {remote_probe}; exit $status"
    )
    output = remote_read(ssh, command, args.execute)
    result = None
    if args.execute:
        result = json.loads(output)
        if (result.get("schema") != "celikpanel/native-primary-peer-observation/v1"
                or result.get("engine") != args.engine
                or result.get("primary_ip") != primary_ip
                or result.get("secondary_ip") != secondary_ip
                or result.get("catalog") != catalog
                or result.get("catalog_members") != catalog_members()):
            raise bootstrap.BootstrapError("native primary observation differs from the fixture")
    return {
        "action": "observe-native-primary-peer",
        "cell_id": args.cell_id,
        "engine": args.engine,
        "observation": result,
        "execute": args.execute,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("prepare", "observe"))
    parser.add_argument("--engine", choices=ENGINES, required=True)
    parser.add_argument("--work-root", type=Path, required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--identity-file", type=Path, required=True)
    parser.add_argument(
        "--manifest", type=Path, default=Path(__file__).with_name("manifest.json")
    )
    parser.add_argument("--require-secondary-transfer", action="store_true",
                        help="observe only: fail unless the native log shows the "
                             "catalog and member transferred to the guest")
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    if args.action == "prepare" and args.require_secondary_transfer:
        parser.error("--require-secondary-transfer applies to observe only")
    try:
        result = prepare(args) if args.action == "prepare" else observe(args)
        print(json.dumps(result, sort_keys=True))
    except (bootstrap.BootstrapError, fixture.FixtureError, OSError, ValueError,
            KeyError, json.JSONDecodeError, subprocess.CalledProcessError,
            subprocess.TimeoutExpired) as exc:
        parser.error(str(exc))


if __name__ == "__main__":
    main()
