#!/usr/bin/env python3
"""Prepare and observe an Arch BIND consumer of a Debian PowerDNS catalog.

Accepted: the exact disposable paired-primary intent cell (either source), and
every admitted fresh paired PowerDNS primary cell (journal V3,
guest_bootstrap.fresh_pdns_primary_cell) with the uninitialized source.
Preparation does not require the primary to exist yet; observation requires
both native servers to publish the same catalog and authoritative member
answers. observe-child follows the zone lifecycle of the accepted primary.

--zero-zones (the primary was prepared with guest_bootstrap.py
prepare-pdns-switch --zero-zones): the primary publishes no member, so the
catalogs must list no zone (or only the lifecycle's child), the catalog SOA is
judged on both servers over UDP and TCP instead of member records, and after
the child's deletion both servers refuse the child (REFUSED): no served
parent exists to deny it authoritatively.
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
# The child zone of the fresh primary's zone lifecycle (cmd/dns-kill-matrix-
# trigger rpc-pdns-primary-zone-v3): serial per step; edit adds `changed`.
CHILD = "s2." + ZONE
CHILD_QUERY = "www." + CHILD
CHILD_CHANGED = "changed." + CHILD
CHILD_STEPS = {
    "add": {"present": True, "serial": "2026092801", "changed": False},
    "edit": {"present": True, "serial": "2026092802", "changed": True},
    "delete": {"present": False},
    "re-add": {"present": True, "serial": "2026092803", "changed": False},
}


def accepted_cell(cell: dict, source_fixture: str) -> bool:
    if cell.get("id") == CELL:
        return True
    return source_fixture == "uninitialized" and bootstrap.fresh_pdns_primary_cell(cell)


def pair_addresses(plan: dict, cell: dict, source_fixture: str = "managed-bind") -> tuple[str, str, dict]:
    if not accepted_cell(cell, source_fixture) or (
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
    if args.cell_id != CELL and args.source_fixture != "uninitialized":
        raise bootstrap.BootstrapError("only the exact disposable cross-engine cell is supported")
    root = args.work_root.resolve(strict=True)
    plan = fixture.load_cell_plan(root, args.cell_id)
    cell = bootstrap.load_manifest_cell(args.manifest, args.cell_id)
    if not accepted_cell(cell, args.source_fixture):
        raise bootstrap.BootstrapError(
            "only the exact disposable cross-engine cell or an admitted fresh paired "
            "PowerDNS primary cell is supported")
    bootstrap.validate_pdns_switch_cell(cell, "debian13", args.source_fixture)
    primary_ip, secondary_ip, peer = pair_addresses(plan, cell, args.source_fixture)
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


RNDC_KEY_PREPARED = "created"
RNDC_KEY_ALREADY_PRESENT = "present"


def rndc_key_prepare_command() -> str:
    """The owner's documented step for a panel-free Arch BIND secondary,
    before named is first started (cmd/bind-peer-inspect/README.md): Arch's
    ``bind`` package creates no rndc key, so the deletion inspector's
    ``rndc -s 127.0.0.1 zonestatus`` cannot work until one exists (observed in
    evidence/batch10-zero-zone-resume-20261001, dns_peer_inspection_unknown).

    Mirrors the product's own rule for an installed BIND
    (cmd/agent/dns_engine_bind_rndc_key.go, prepareBINDRNDCKeyWithOps): leave
    everything alone when a key, an ``/etc/rndc.conf`` or the owner's own
    ``controls`` statement already exists; otherwise run the native
    ``rndc-confgen -a`` once and give the created key ``root:named 0640``,
    the ownership Arch's ``named -u named`` needs after it drops privileges.
    Prints exactly "created" or "present" on its own last line; never the key
    material itself.
    """

    return (
        "sudo sh -c \""
        "if test -e /etc/rndc.key || test -e /etc/rndc.conf || grep -q '^controls' /etc/named.conf; "
        f"then echo {RNDC_KEY_ALREADY_PRESENT}; "
        "else rndc-confgen -a >/dev/null"
        " && chown root:named /etc/rndc.key && chmod 0640 /etc/rndc.key"
        f" && echo {RNDC_KEY_PREPARED}; "
        "fi\""
    )


def rndc_status_probe(ssh: list[str], execute: bool) -> bool | None:
    """Whether the loopback control channel the deletion inspector also needs
    (``rndc -s 127.0.0.1 ...``) currently works: the exit code only, never
    rndc's own output, so a later run can see the prerequisite is already met
    instead of only learning about a missing key after a pending deletion.
    """

    output = remote_read(ssh, "sudo rndc -s 127.0.0.1 status >/dev/null 2>&1; echo $?", execute)
    if not execute:
        return None
    code = output.strip()
    if not code.isdigit():
        raise ValueError("rndc status probe returned a non-numeric exit code")
    return code == "0"


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
            ],
            execute=args.execute,
        )
        # Owner-prepared rndc key: before named is first started, never after.
        rndc_key_result = remote_read(ssh, rndc_key_prepare_command(), args.execute)
        rndc_key_status = rndc_key_result.strip() if args.execute else None
        if args.execute and rndc_key_status not in (RNDC_KEY_PREPARED, RNDC_KEY_ALREADY_PRESENT):
            raise bootstrap.BootstrapError(
                "owner-prepared rndc key step on the Arch secondary returned an unexpected result")
        bootstrap.run(
            ssh + [
                "sudo systemctl enable --now named.service"
                " && systemctl is-active --quiet named.service"
                f" && rm -- {remote}"
            ],
            execute=args.execute,
        )
    return {
        "action": "prepare-native-pdns-bind-peer", "cell_id": args.cell_id,
        "primary_ip": primary_ip, "secondary_ip": secondary_ip,
        "catalog": catalog_name(primary_ip), "management_installed_on_secondary": False,
        "owner_prepared_rndc_key": rndc_key_status,
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
    if name not in {ZONE, QUERY, catalog_name("192.0.2.10"), CHILD, CHILD_QUERY, CHILD_CHANGED} or kind not in {"SOA", "A", "AXFR"}:
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


def authoritative_negative(reply: str) -> None:
    """An authoritative NXDOMAIN (the served parent denies the child)."""

    if "status: NXDOMAIN" not in reply or re.search(r"flags: qr aa\b", reply) is None:
        raise ValueError("the negative answer is not an authoritative NXDOMAIN")


def not_served(reply: str) -> None:
    """REFUSED: the server holds no zone for the name (zero-zone primary,
    child deleted, no parent). Not an authoritative answer by design."""

    if "status: REFUSED" not in reply:
        raise ValueError("the server does not refuse a name it holds no zone for")


def zero_zones(args: argparse.Namespace) -> bool:
    return getattr(args, "zero_zones", False) is True


def catalog_soa_answers(ssh: list[str], catalog: str, primary_ip: str, secondary_ip: str,
                        execute: bool) -> dict:
    """The catalog SOA serial from both servers over UDP and TCP (must agree)."""

    answers: dict = {}
    for ip in (primary_ip, secondary_ip):
        for tcp in (False, True):
            reply = remote_read(ssh, dig_command(ip, catalog, "SOA", tcp), execute)
            if execute:
                answers[f"{ip}/{'tcp' if tcp else 'udp'}"] = soa_serial(
                    authoritative_data(reply, catalog, "SOA"))
    if execute and len(set(answers.values())) != 1:
        raise ValueError(f"the catalog SOA serial differs between servers or transports: {answers}")
    return answers


def soa_serial(data: tuple[str, ...]) -> str:
    if len(data) != 7:
        raise ValueError("SOA data is not exact")
    return data[2]


def observe_child(args: argparse.Namespace) -> dict:
    """After one lifecycle step: both servers answer the child as expected.

    Present: identical authoritative SOA (the step's serial) and www A from
    primary and secondary over UDP and TCP; `changed` only after edit; the
    child in both the primary's and the loaded catalog. Absent: both servers
    return an authoritative NXDOMAIN for the child SOA from the served parent,
    and neither catalog lists the child. Catalog serials are recorded.
    """

    expected = CHILD_STEPS.get(args.step)
    if expected is None:
        raise bootstrap.BootstrapError(f"unsupported lifecycle step {args.step!r}")
    primary_ip, secondary_ip, peer, identity = selected(args)
    ssh = bootstrap.ssh_base(peer, identity)
    verify_guest(ssh, args.cell_id, args.execute)
    remote_read(ssh, "systemctl is-active --quiet named.service", args.execute)
    catalog = catalog_name(primary_ip)
    source = remote_read(ssh, dig_command(primary_ip, catalog, "AXFR", True), args.execute)
    loaded = remote_read(ssh, dig_command("127.0.0.1", catalog, "AXFR", True), args.execute)
    base = set() if zero_zones(args) else {ZONE}
    members = base | {CHILD} if expected["present"] else base
    report: dict = {
        "action": "observe-child-native-pdns-bind-peer", "cell_id": args.cell_id,
        "step": args.step, "child": CHILD, "expected": expected, "execute": args.execute,
        "zero_zones": zero_zones(args), "catalog_members_expected": sorted(members),
    }
    if args.execute:
        source_serial, source_members = parse_catalog_axfr(source, catalog, producer="powerdns")
        loaded_serial, loaded_members = parse_catalog_axfr(loaded, catalog, producer="powerdns")
        report["catalog"] = {"primary_serial": source_serial, "loaded_serial": loaded_serial,
                             "primary_members": sorted(source_members),
                             "loaded_members": sorted(loaded_members)}
        if (source_serial, source_members) != (loaded_serial, loaded_members) or source_members != members:
            raise ValueError("the loaded BIND catalog differs from the PowerDNS primary or the step")
    answers: dict = {}
    for ip in (primary_ip, secondary_ip):
        for tcp in (False, True):
            key = f"{ip}/{'tcp' if tcp else 'udp'}"
            soa = remote_read(ssh, dig_command(ip, CHILD, "SOA", tcp), args.execute)
            if not args.execute:
                continue
            if not expected["present"]:
                if zero_zones(args):
                    not_served(soa)
                    answers[key] = {"soa": "REFUSED"}
                else:
                    authoritative_negative(soa)
                    answers[key] = {"soa": "NXDOMAIN"}
                continue
            serial = soa_serial(authoritative_data(soa, CHILD, "SOA"))
            www = authoritative_data(
                remote_read(ssh, dig_command(ip, CHILD_QUERY, "A", tcp), args.execute),
                CHILD_QUERY, "A")
            changed_reply = remote_read(ssh, dig_command(ip, CHILD_CHANGED, "A", tcp), args.execute)
            if expected["changed"]:
                changed = authoritative_data(changed_reply, CHILD_CHANGED, "A")
            else:
                authoritative_negative(changed_reply)
                changed = None
            if serial != expected["serial"] or www != ("192.0.2.10",) or (
                expected["changed"] and changed != ("192.0.2.10",)
            ):
                raise ValueError(f"{key} child answers differ from the step: {serial} {www} {changed}")
            answers[key] = {"soa_serial": serial, "www_a": list(www),
                            "changed_a": list(changed) if changed else None}
    report["answers"] = answers
    return report


def observe(args: argparse.Namespace) -> dict:
    """Both servers serve the same catalog and member answers.

    ``args.address`` is the scenario's www A (the expected RDATA, not a query
    target). With ``args.with_child`` (after the zone lifecycle's re-add) the
    catalogs must list the child zone as well; observe-child judges its answers.
    """

    primary_ip, secondary_ip, peer, identity = selected(args)
    zero = zero_zones(args)
    expected_address = None if zero else str(ipaddress.IPv4Address(args.address))
    members_expected = set() if zero else {ZONE}
    if getattr(args, "with_child", False) is True:
        members_expected |= {CHILD}
    ssh = bootstrap.ssh_base(peer, identity)
    verify_guest(ssh, args.cell_id, args.execute)
    remote_read(ssh, "systemctl is-active --quiet named.service", args.execute)
    # Whether the deletion inspector's own control-channel prerequisite is
    # currently met (exit code only; see rndc_status_probe), so a run after
    # the owner's rndc key preparation shows it, instead of only surfacing a
    # dns_peer_inspection_unknown after a real deletion attempt.
    rndc_status_ok = rndc_status_probe(ssh, args.execute)
    catalog = catalog_name(primary_ip)
    source = remote_read(ssh, dig_command(primary_ip, catalog, "AXFR", True), args.execute)
    loaded = remote_read(ssh, dig_command("127.0.0.1", catalog, "AXFR", True), args.execute)
    if args.execute:
        source_serial, source_members = parse_catalog_axfr(source, catalog, producer="powerdns")
        loaded_serial, loaded_members = parse_catalog_axfr(loaded, catalog, producer="powerdns")
        if (source_serial, source_members) != (loaded_serial, loaded_members) or (
            source_members != members_expected
        ):
            raise ValueError(
                "loaded BIND catalog differs from the PowerDNS primary or the expected "
                f"members {sorted(members_expected)}: primary {source_serial} "
                f"{sorted(source_members)}, loaded {loaded_serial} {sorted(loaded_members)}")
    answers: dict = {}
    if zero:
        # No member: the catalog zone itself, identical on both servers.
        answers = {f"{key}/{catalog}/SOA": value for key, value in catalog_soa_answers(
            ssh, catalog, primary_ip, secondary_ip, args.execute).items()}
        if args.execute and set(answers.values()) != {str(source_serial)}:
            raise ValueError(
                f"the served catalog SOA differs from the transferred serial {source_serial}: "
                f"{answers}")
    for name, kind in (() if zero else ((ZONE, "SOA"), (QUERY, "A"))):
        source_data = None
        for ip in (primary_ip, secondary_ip):
            for tcp in (False, True):
                reply = remote_read(ssh, dig_command(ip, name, kind, tcp), args.execute)
                if not args.execute:
                    continue
                data = authoritative_data(reply, name, kind)
                # Server queried and the answer's RDATA, for expected-vs-observed.
                answers[f"{ip}/{'tcp' if tcp else 'udp'}/{name}/{kind}"] = list(data)
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
        "member": None if zero else ZONE, "address": expected_address, "answers": answers,
        "zero_zones": zero,
        "catalog_members_expected": sorted(members_expected),
        "authoritative_udp_tcp": args.execute,
        "management_installed_on_secondary": False, "rndc_status_ok": rndc_status_ok,
        "execute": args.execute,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("prepare", "observe", "observe-child"))
    parser.add_argument("--work-root", type=Path, required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--source-fixture", choices=("managed-bind", "uninitialized"),
                        default="managed-bind")
    parser.add_argument("--identity-file", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, default=Path(__file__).with_name("manifest.json"))
    parser.add_argument("--address", help="expected member A address for observe")
    parser.add_argument("--step", choices=tuple(CHILD_STEPS), help="lifecycle step for observe-child")
    parser.add_argument("--with-child", action="store_true",
                        help="observe: the catalogs also list the lifecycle's child zone")
    parser.add_argument("--zero-zones", action="store_true",
                        help="the primary was prepared with no zones (prepare-pdns-switch "
                             "--zero-zones): no member, catalog judged by itself")
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    if (args.action == "observe" and not args.zero_zones) != (args.address is not None):
        parser.error("--address is required exactly for observe without --zero-zones")
    if (args.action == "observe-child") != (args.step is not None):
        parser.error("--step is required exactly for observe-child")
    actions = {"prepare": prepare, "observe": observe, "observe-child": observe_child}
    try:
        print(json.dumps(actions[args.action](args), sort_keys=True))
    except (bootstrap.BootstrapError, fixture.FixtureError, OSError, ValueError,
            KeyError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as exc:
        parser.error(str(exc))


if __name__ == "__main__":
    main()
