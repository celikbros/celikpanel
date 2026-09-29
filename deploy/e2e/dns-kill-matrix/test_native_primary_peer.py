#!/usr/bin/env python3
"""Offline tests for the panel-free native primary peer and its probe.

Nothing here contacts a guest. The catalog checks feed DNS wire messages built
from the rendered BIND zone text and from the rendered PowerDNS seed rows into
a line-by-line port of cmd/agent/dns_catalog_axfr.go; the port itself is pinned
to the Go test vectors so a drift in either direction fails here.
"""

import argparse
import contextlib
import io
import ipaddress
import json
from pathlib import Path
import re
import sqlite3
import struct
import subprocess
import tempfile
import unittest
from unittest import mock

import guest_bootstrap as bootstrap
import native_primary_peer as peer
import native_primary_peer_probe as probe


MANIFEST = Path(__file__).with_name("manifest.json")
BIND_CELL = "bind__intent__after-write__paired-secondary__peer-reachable"
ARCH_BIND_CELL = "bind__intent__before-write__paired-secondary__peer-reachable"
PDNS_CELL = "pdns-switch__intent__after-write__paired-secondary__peer-reachable"
PLAN = {
    "nodes": {
        "debian13": {"peer": {"address": "192.0.2.10/24"}},
        "arch": {"peer": {"address": "192.0.2.11/24"}},
    }
}
TYPES = {"SOA": probe.DNS_TYPE_SOA, "NS": probe.DNS_TYPE_NS, "TXT": probe.DNS_TYPE_TXT,
         "PTR": probe.DNS_TYPE_PTR, "A": probe.DNS_TYPE_A, "APL": probe.DNS_TYPE_APL}
QUERY_ID = 0x4217


def manifest_cell(cell_id: str) -> dict:
    cells = json.loads(MANIFEST.read_text(encoding="utf-8"))["cells"]
    return next(cell for cell in cells if cell["id"] == cell_id)


# --- zone text / seed rows -> RRs -> wire ---------------------------------

def parse_zone_text(text: str) -> list[tuple[str, str, str, int]]:
    origin, default_ttl, rows = "", None, []

    def absolute(name: str) -> str:
        if name == "@":
            return origin
        return name[:-1] if name.endswith(".") else name + "." + origin

    for line in text.splitlines():
        fields = line.split()
        if fields[0] == "$ORIGIN":
            origin = fields[1].rstrip(".")
            continue
        if fields[0] == "$TTL":
            default_ttl = int(fields[1])
            continue
        owner, rest = absolute(fields[0]), fields[1:]
        ttl = default_ttl
        if rest[0].isdigit():
            ttl, rest = int(rest[0]), rest[1:]
        assert rest[0] == "IN"
        kind, data = rest[1], rest[2:]
        if kind in {"NS", "PTR"}:
            content = absolute(data[0])
        elif kind == "SOA":
            content = " ".join([absolute(data[0]), absolute(data[1])] + data[2:])
        else:
            content = " ".join(data)
        rows.append((owner, kind, content, ttl))
    return rows


def rdata(kind: str, content: str) -> bytes:
    if kind in {"NS", "PTR"}:
        return probe.encode_name(content.rstrip("."))
    if kind == "SOA":
        fields = content.split()
        names = b"".join(probe.encode_name(name.rstrip(".")) for name in fields[:2])
        return names + struct.pack("!IIIII", *(int(value) for value in fields[2:]))
    if kind == "TXT":
        if len(content) >= 2 and content[0] == content[-1] == '"':
            content = content[1:-1]
        text = content.encode("ascii")
        return bytes([len(text)]) + text
    if kind == "A":
        return ipaddress.IPv4Address(content).packed
    return b"\x00"


def rr(owner: str, kind: str, content: str, ttl: int, klass: int = probe.DNS_CLASS_IN) -> bytes:
    data = rdata(kind, content)
    return probe.encode_name(owner) + struct.pack("!HHIH", TYPES[kind], klass, ttl, len(data)) + data


def message(catalog: str, records: list[bytes], *, question: bool = True,
            flags: int = probe.FLAG_QR | probe.FLAG_AA, authorities: list[bytes] = (),
            additionals: list[bytes] = ()) -> bytes:
    header = struct.pack("!HHHHHH", QUERY_ID, flags, 1 if question else 0, len(records),
                         len(authorities), len(additionals))
    body = b""
    if question:
        body = probe.encode_name(catalog) + struct.pack("!HH", probe.DNS_TYPE_AXFR, probe.DNS_CLASS_IN)
    return header + body + b"".join(records) + b"".join(authorities) + b"".join(additionals)


def framed(*messages: bytes) -> bytes:
    return b"".join(struct.pack("!H", len(item)) + item for item in messages)


def axfr_records(rows: list[tuple[str, str, str, int]]) -> list[bytes]:
    soa = [row for row in rows if row[1] == "SOA"]
    rest = [row for row in rows if row[1] != "SOA"]
    assert len(soa) == 1
    return [rr(*row) for row in soa + rest + soa]


def pdns_rows(seed: str, zone: str) -> list[tuple[str, str, str, int]]:
    db = sqlite3.connect(":memory:")
    db.executescript(
        "CREATE TABLE domains(id INTEGER PRIMARY KEY, name TEXT UNIQUE, master TEXT, "
        "last_check INTEGER, type TEXT, notified_serial INTEGER, account TEXT, "
        "options TEXT, catalog TEXT);"
        "CREATE TABLE records(id INTEGER PRIMARY KEY, domain_id INTEGER, name TEXT, "
        "type TEXT, content TEXT, ttl INTEGER, prio INTEGER, disabled INTEGER, "
        "ordername TEXT, auth INTEGER);"
    )
    db.executescript(seed)
    domain = db.execute(
        "SELECT type, master, notified_serial, account, catalog FROM domains WHERE name=?", (zone,)
    ).fetchall()
    assert domain == [("MASTER", None, None, peer.FIXTURE_ACCOUNT, None)], domain
    rows = db.execute(
        "SELECT r.name, r.type, r.content, r.ttl FROM records r JOIN domains d "
        "ON r.domain_id=d.id WHERE d.name=? AND r.disabled=0 AND r.auth=1 ORDER BY r.id",
        (zone,),
    ).fetchall()
    db.close()
    return [tuple(row) for row in rows]


class CatalogFormatTest(unittest.TestCase):
    def test_member_label_matches_go_vector(self) -> None:
        # internal/binddns/pairing_test.go TestCatalogMemberLabelIsExactRFC1035SafeSHA224
        self.assertEqual(
            probe.catalog_member_label("example.test"),
            "6e00c7a8685d9d2629b0f7db1dfcd122fc79ab52b41cb04de3163ffa",
        )
        self.assertEqual(probe.catalog_name("192.0.2.10"), "catalog-c000020a.celikpanel.invalid")
        self.assertEqual(probe.catalog_name("192.0.2.11"), "catalog-c000020b.celikpanel.invalid")

    def test_catalog_text_is_byte_identical_to_go_renderer(self) -> None:
        # TestEmptyPrimaryCatalogUsesRFC9432BaseRecords (exact golden text).
        self.assertEqual(
            peer.catalog_zone_text("192.0.2.10", 1, []),
            "$ORIGIN catalog-c000020a.celikpanel.invalid.\n$TTL 60\n"
            "@ IN SOA invalid. invalid. 1 60 30 3600 30\n@ IN NS invalid.\n"
            'version IN TXT "2"\n',
        )
        # TestPrimaryPairingRendersCatalogAndTransferPolicy member line.
        self.assertIn(
            "6e00c7a8685d9d2629b0f7db1dfcd122fc79ab52b41cb04de3163ffa.zones IN PTR example.test.\n",
            peer.catalog_zone_text("192.0.2.10", 9, ["example.test"]),
        )

    def test_rendered_catalog_zone_passes_the_agent_reader(self) -> None:
        for primary in ("192.0.2.10", "192.0.2.11"):
            catalog = probe.catalog_name(primary)
            rows = parse_zone_text(peer.catalog_zone_text(primary))
            # Zone text and CatalogZoneRecords rows must be the same wire RRs.
            self.assertEqual([rr(*row) for row in rows],
                             [rr(*row) for row in peer.catalog_records(primary)])
            records = axfr_records(rows)
            single = framed(message(catalog, records))
            split = framed(message(catalog, records[:1]), message(catalog, records[1:3], question=False),
                           message(catalog, records[3:]))
            for stream in (single, split):
                self.assertEqual(
                    probe.read_catalog_axfr_stream(stream, QUERY_ID, catalog),
                    (1, ["s1-kill.test"]),
                )

    def test_pdns_seed_rows_pass_the_agent_reader(self) -> None:
        for primary, secondary in (("192.0.2.10", "192.0.2.11"), ("192.0.2.11", "192.0.2.10")):
            catalog = probe.catalog_name(primary)
            seed = peer.pdns_seed_sql(primary, secondary)
            rows = pdns_rows(seed, catalog)
            self.assertEqual(rows, peer.catalog_records(primary))
            self.assertEqual(
                probe.read_catalog_axfr_stream(framed(message(catalog, axfr_records(rows))),
                                               QUERY_ID, catalog),
                (1, ["s1-kill.test"]),
            )
            self.assertEqual(pdns_rows(seed, "s1-kill.test"),
                             peer.member_zone_records(primary, secondary))

    def test_reader_follows_name_compression(self) -> None:
        catalog = probe.catalog_name("192.0.2.10")
        records = axfr_records(peer.catalog_records("192.0.2.10"))
        plain = message(catalog, records)
        # Replace the first apex owner with a pointer to the question name.
        apex = probe.encode_name(catalog)
        index = plain.index(apex, 12 + len(apex))
        compressed = plain[:index] + b"\xc0\x0c" + plain[index + len(apex):]
        self.assertEqual(probe.read_catalog_axfr_stream(framed(compressed), QUERY_ID, catalog),
                         (1, ["s1-kill.test"]))

    def test_native_powerdns_producer_catalog_is_refused_by_the_secondary_policy(self) -> None:
        # Mirrors cmd/agent TestPowerDNSCatalogAXFRProducerIsExplicitAndBounded:
        # this is why the PowerDNS flavour serves an ordinary MASTER catalog.
        catalog = probe.catalog_name("192.0.2.10")
        rows = [
            (catalog, "SOA", "invalid. invalid. 1790542951 60 30 3600 30", 60),
            (catalog, "NS", "invalid.", 60),
            ("version." + catalog, "TXT", '"2"', 0),
            ("lf5eijnqp9ob8kmq5mv0vhjaevtcfuus.zones." + catalog, "PTR", "s1-kill.test", 0),
        ]
        stream = framed(message(catalog, axfr_records(rows)))
        with self.assertRaises(probe.CatalogAXFRError):
            probe.read_catalog_axfr_stream(stream, QUERY_ID, catalog, probe.PRODUCER_BIND)
        self.assertEqual(
            probe.read_catalog_axfr_stream(stream, QUERY_ID, catalog, probe.PRODUCER_POWERDNS),
            (1790542951, ["s1-kill.test"]),
        )

    def test_reader_port_rejects_the_go_negative_cases(self) -> None:
        catalog = probe.catalog_name("192.0.2.10")
        label = probe.catalog_member_label("s1-kill.test")
        base = [
            (catalog, "SOA", "invalid. invalid. 17 60 30 3600 30", 60),
            (catalog, "NS", "invalid.", 60),
            ("version." + catalog, "TXT", '"2"', 60),
            (label + ".zones." + catalog, "PTR", "s1-kill.test", 60),
            (catalog, "SOA", "invalid. invalid. 17 60 30 3600 30", 60),
        ]
        self.assertEqual(
            probe.read_catalog_axfr_stream(framed(message(catalog, [rr(*row) for row in base])),
                                           QUERY_ID, catalog),
            (17, ["s1-kill.test"]),
        )

        def replace(index: int, row: tuple) -> list[bytes]:
            rows = list(base)
            rows[index] = row
            return [rr(*item) for item in rows]

        soa = lambda content: (catalog, "SOA", content, 60)  # noqa: E731
        cases = {
            "zero serial": replace(0, soa("invalid. invalid. 0 60 30 3600 30")),
            "mismatched serial": replace(4, soa("invalid. invalid. 18 60 30 3600 30")),
            "SOA MNAME": replace(0, soa("ns.example. invalid. 17 60 30 3600 30")),
            "SOA RNAME": replace(0, soa("invalid. hostmaster.example. 17 60 30 3600 30")),
            "SOA timers": replace(0, soa("invalid. invalid. 17 60 30 86400 30")),
            "NS target": replace(1, (catalog, "NS", "ns.example.", 60)),
            "quoted version": replace(2, ("version." + catalog, "TXT", '""2""', 60)),
            "TTL 61": replace(3, (label + ".zones." + catalog, "PTR", "s1-kill.test", 61)),
            "hash mismatch": replace(3, ("0" * 56 + ".zones." + catalog, "PTR", "s1-kill.test", 60)),
            "short hash": replace(3, ("a" * 55 + ".zones." + catalog, "PTR", "s1-kill.test", 60)),
            "APL": [rr(*base[0]), rr(catalog, "APL", "", 60)] + [rr(*row) for row in base[1:]],
            "duplicate NS": [rr(*row) for row in base[:2]] + [rr(*row) for row in base[1:]],
            "missing version": [rr(*row) for row in base[:2] + base[3:]],
            "no closing SOA": [rr(*row) for row in base[:4]],
            "class": [rr(*row) for row in base[:2]] + [rr(*base[2], klass=3)]
                     + [rr(*row) for row in base[3:]],
        }
        for name, records in cases.items():
            with self.subTest(name), self.assertRaises(probe.CatalogAXFRError):
                probe.read_catalog_axfr_stream(framed(message(catalog, records)), QUERY_ID, catalog)
        records = [rr(*row) for row in base]
        for name, raw in {
            "AA absent": message(catalog, records, flags=probe.FLAG_QR),
            "unexpected RD": message(catalog, records, flags=probe.FLAG_QR | probe.FLAG_AA | 0x0100),
            "first question absent": message(catalog, records, question=False),
            "authority": message(catalog, records, authorities=[records[1]]),
            "additional": message(catalog, records, additionals=[records[1]]),
        }.items():
            with self.subTest(name), self.assertRaises(probe.CatalogAXFRError):
                probe.read_catalog_axfr_stream(framed(raw), QUERY_ID, catalog)

    def test_catalog_members_and_serial_are_canonical(self) -> None:
        for members in (["S1-kill.test"], ["s1-kill.test", "s1-kill.test"], ["single"],
                        ["192.0.2.1"], ["-bad.test"]):
            with self.subTest(members), self.assertRaises(bootstrap.BootstrapError):
                peer.catalog_zone_text("192.0.2.10", 1, members)
        with self.assertRaises(bootstrap.BootstrapError):
            peer.catalog_zone_text("192.0.2.10", 0)


class RenderingTest(unittest.TestCase):
    def test_member_zone_matches_the_existing_paired_primary_scenario(self) -> None:
        for node, other in (("debian13", "arch"), ("arch", "debian13")):
            primary, secondary = peer.NODE_ADDRESSES[node], peer.NODE_ADDRESSES[other]
            scenario = bootstrap.bind_scenario(
                "uninitialized", role="paired-primary", node=node,
                allow_debian_paired_source=True,
            )
            expected = [(r["name"], r["type"], r["content"], r["ttl"])
                        for r in scenario["zones"][0]["records"]]
            self.assertEqual(peer.member_zone_records(primary, secondary), expected)
            self.assertEqual(parse_zone_text(peer.member_zone_text(primary, secondary)), expected)
            self.assertIn(f"www.s1-kill.test. 300 IN A {primary}\n",
                          peer.member_zone_text(primary, secondary))

    def test_bind_primary_restricts_transfer_and_notify_to_the_guest(self) -> None:
        for node, other in (("debian13", "arch"), ("arch", "debian13")):
            primary, secondary = peer.NODE_ADDRESSES[node], peer.NODE_ADDRESSES[other]
            config = peer.bind_primary_config(node, primary, secondary)
            layout = peer.LAYOUTS[(node, "bind")]
            self.assertEqual(set(re.findall(r"\d+\.\d+\.\d+\.\d+", config)),
                             {primary, secondary, "127.0.0.1"})
            self.assertEqual(config.count(f"allow-transfer {{ {secondary}; 127.0.0.1; }};"), 2)
            self.assertEqual(config.count(f"also-notify {{ {secondary}; }};"), 2)
            self.assertEqual(config.count("notify explicit;"), 3)
            self.assertEqual(config.count("type primary;"), 2)
            self.assertIn("allow-transfer { none; };", config)
            self.assertIn(f"listen-on port 53 {{ 127.0.0.1; {primary}; }};", config)
            self.assertIn("recursion no;", config)
            self.assertIn(f'zone "{probe.catalog_name(primary)}"', config)
            self.assertIn(f'file "{layout["catalog_file"]}"', config)
            self.assertIn(f'file "{layout["member_file"]}"', config)
            self.assertNotIn("catalog-zones", config)
            self.assertNotIn("celikpanel/bin", config)

    def test_pdns_primary_restricts_transfer_and_notify_to_the_guest(self) -> None:
        for node, other in (("debian13", "arch"), ("arch", "debian13")):
            primary, secondary = peer.NODE_ADDRESSES[node], peer.NODE_ADDRESSES[other]
            lines = peer.pdns_primary_config(node, primary, secondary).splitlines()
            self.assertIn(f"allow-axfr-ips={secondary}/32,127.0.0.1/32", lines)
            self.assertIn(f"also-notify={secondary}", lines)
            self.assertIn(f"only-notify={secondary}/32", lines)
            self.assertIn(f"local-address=127.0.0.1,{primary}", lines)
            for line in ("primary=yes", "secondary=no", "autosecondary=no", "api=no",
                         "webserver=no", "launch=gsqlite3"):
                self.assertIn(line, lines)
            self.assertEqual("setuid=powerdns" in lines, node == "arch")
            self.assertFalse(any(line.startswith(("include-dir", "api-key")) for line in lines))
            self.assertEqual(set(re.findall(r"\d+\.\d+\.\d+\.\d+", "\n".join(lines))),
                             {primary, secondary, "127.0.0.1"})

    def test_pair_addresses_are_refused_outside_the_isolated_link(self) -> None:
        for primary, secondary in (("192.0.2.10", "192.0.2.10"), ("198.51.100.1", "192.0.2.10"),
                                   ("192.0.2.10", "10.0.2.15"), ("192.000.2.10", "192.0.2.11")):
            with self.subTest((primary, secondary)), self.assertRaises(
                (bootstrap.BootstrapError, ValueError)
            ):
                peer.bind_primary_config("debian13", primary, secondary)


class CellSelectionTest(unittest.TestCase):
    def test_every_supported_manifest_cell_resolves_the_opposite_guest(self) -> None:
        cells = json.loads(MANIFEST.read_text(encoding="utf-8"))["cells"]
        count = 0
        for cell in cells:
            if (cell["role"] != "paired-secondary" or cell["driver"] not in peer.SUPPORTED_DRIVERS
                    or cell["placement"]["source_fixture_policy"] not in peer.SUPPORTED_SOURCE_POLICIES):
                continue
            count += 1
            node, primary, other, secondary = peer.pair_nodes(PLAN, cell)
            self.assertEqual(bootstrap.NODE_FOR_PLACEMENT[cell["placement"]["dns_peer_host"]], node)
            self.assertEqual(bootstrap.NODE_FOR_PLACEMENT[cell["placement"]["kill_host"]], other)
            self.assertEqual((primary, secondary),
                             (peer.NODE_ADDRESSES[node], peer.NODE_ADDRESSES[other]))
        self.assertEqual(count, 22 + 34)

    def test_unsupported_cells_are_refused(self) -> None:
        refused = [
            "bind__intent__after-write__paired-primary__peer-reachable",
            "bind__pre-intent__standalone__peer-reachable",
            "pdns-secondary-reconfigure__intent__after-write__paired-secondary__peer-reachable",
            "bind__source-stopped__after-write__paired-secondary__peer-reachable",
        ]
        for cell_id in refused:
            with self.subTest(cell_id), self.assertRaises(bootstrap.BootstrapError):
                peer.pair_nodes(PLAN, manifest_cell(cell_id))
        arch_pdns = json.loads(json.dumps(manifest_cell(PDNS_CELL)))
        arch_pdns["placement"]["kill_host"], arch_pdns["placement"]["dns_peer_host"] = "arch", "debian-13"
        with self.assertRaises(bootstrap.BootstrapError):
            peer.pair_nodes(PLAN, arch_pdns)
        same = json.loads(json.dumps(manifest_cell(BIND_CELL)))
        same["placement"]["dns_peer_host"] = same["placement"]["kill_host"]
        with self.assertRaises(bootstrap.BootstrapError):
            peer.pair_nodes(PLAN, same)
        moved = {"nodes": {"debian13": {"peer": {"address": "192.0.2.20/24"}},
                           "arch": {"peer": {"address": "192.0.2.11/24"}}}}
        with self.assertRaises(bootstrap.BootstrapError):
            peer.pair_nodes(moved, manifest_cell(BIND_CELL))


class ActionTest(unittest.TestCase):
    def args(self, temporary: str, cell_id: str, engine: str, execute: bool) -> argparse.Namespace:
        return argparse.Namespace(
            work_root=Path(temporary), cell_id=cell_id, engine=engine,
            manifest=MANIFEST, identity_file=Path(temporary) / "key",
            require_secondary_transfer=False, execute=execute,
        )

    def patched(self, args: argparse.Namespace, cell_id: str):
        stack = contextlib.ExitStack()
        stack.enter_context(mock.patch.object(peer.fixture, "load_cell_plan", return_value=PLAN))
        stack.enter_context(mock.patch.object(bootstrap, "load_manifest_cell",
                                              return_value=manifest_cell(cell_id)))
        stack.enter_context(mock.patch.object(bootstrap, "identity_file",
                                              return_value=args.identity_file))
        stack.enter_context(mock.patch.object(bootstrap, "ssh_base", return_value=["ssh", "peer"]))
        stack.enter_context(mock.patch.object(bootstrap, "scp_base", return_value=["scp"]))
        stack.enter_context(mock.patch.object(bootstrap, "remote_destination",
                                              side_effect=lambda _node, path: "peer:" + path))
        return stack

    def test_foreign_peer_marker_refuses_before_any_mutation(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            for engine in peer.ENGINES:
                args = self.args(temporary, BIND_CELL, engine, True)
                foreign = subprocess.CompletedProcess(["ssh"], 0, stdout="foreign guest\n")
                with self.patched(args, BIND_CELL), \
                        mock.patch.object(peer.subprocess, "run", return_value=foreign), \
                        mock.patch.object(bootstrap, "run") as mutating_run:
                    with self.assertRaises(bootstrap.BootstrapError):
                        peer.prepare(args)
                    mutating_run.assert_not_called()

    def test_marker_names_the_peer_node_not_the_guest_under_test(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            args = self.args(temporary, BIND_CELL, "bind", True)
            # BIND_CELL kills on debian-13, so the peer (primary) is arch.
            guest = subprocess.CompletedProcess(
                ["ssh"], 0,
                stdout=f"schema=celikpanel/dns-kill-fixture-plan/v1\ncell_id={BIND_CELL}\nnode=debian13\n",
            )
            with self.patched(args, BIND_CELL), \
                    mock.patch.object(peer.subprocess, "run", return_value=guest), \
                    mock.patch.object(bootstrap, "run") as mutating_run:
                with self.assertRaises(bootstrap.BootstrapError):
                    peer.prepare(args)
                mutating_run.assert_not_called()

    def run_dry(self, cell_id: str, engine: str) -> tuple[dict, list[list[str]]]:
        with tempfile.TemporaryDirectory() as temporary:
            args = self.args(temporary, cell_id, engine, False)
            calls: list[list[str]] = []

            def record(command: list[str], *, execute: bool) -> None:
                self.assertFalse(execute)
                calls.append(command)

            with self.patched(args, cell_id), \
                    mock.patch.object(peer.subprocess, "run",
                                      side_effect=AssertionError("dry run executed a command")), \
                    mock.patch.object(bootstrap, "run", side_effect=record), \
                    contextlib.redirect_stdout(io.StringIO()):
                result = peer.prepare(args)
            return result, calls

    def test_dry_run_bind_primary_on_arch(self) -> None:
        result, calls = self.run_dry(BIND_CELL, "bind")
        self.assertEqual((result["primary_node"], result["primary_ip"], result["secondary_ip"]),
                         ("arch", "192.0.2.11", "192.0.2.10"))
        self.assertEqual(result["catalog"], "catalog-c000020b.celikpanel.invalid")
        self.assertFalse(result["execute"])
        commands = [call[-1] for call in calls if call[0] == "ssh"]
        self.assertEqual(commands[0], "cat /etc/celikpanel-dns-kill-matrix")
        self.assertIn("test ! -e /opt/celikpanel/bin/agent", commands[1])
        self.assertIn("test ! -e /var/named/celikpanel-fixture-catalog.zone", commands[1])
        self.assertEqual(commands[2], "sudo /usr/bin/pacman -Syu --noconfirm bind")
        self.assertEqual(len([call for call in calls if call[0] == "scp"]), 3)
        activation = commands[3]
        for fragment in ("-o root -g named", "/etc/named.conf", "named-checkconf",
                         "named-checkzone catalog-c000020b.celikpanel.invalid",
                         "named-checkzone s1-kill.test", "systemctl restart named.service",
                         "systemctl is-active --quiet named.service", "rm -- "):
            self.assertIn(fragment, activation)

    def test_dry_run_pdns_primary_on_arch_for_debian_pdns_secondary(self) -> None:
        result, calls = self.run_dry(PDNS_CELL, "pdns")
        self.assertEqual((result["primary_node"], result["primary_ip"]), ("arch", "192.0.2.11"))
        commands = [call[-1] for call in calls if call[0] == "ssh"]
        self.assertIn("test ! -e /var/lib/powerdns/pdns.sqlite3", commands[1])
        self.assertEqual(commands[2], "sudo /usr/bin/pacman -Syu --noconfirm powerdns")
        self.assertEqual(len([call for call in calls if call[0] == "scp"]), 2)
        for fragment in ("/usr/share/doc/powerdns/schema.sqlite3.sql",
                         "sudo chown powerdns:powerdns /var/lib/powerdns/pdns.sqlite3",
                         "systemctl restart pdns.service"):
            self.assertIn(fragment, commands[3])

    def test_dry_run_debian_primary_masks_package_defaults(self) -> None:
        for engine, units in (("bind", "named.service bind9.service"), ("pdns", "pdns.service")):
            result, calls = self.run_dry(ARCH_BIND_CELL, engine)
            self.assertEqual((result["primary_node"], result["primary_ip"], result["secondary_ip"]),
                             ("debian13", "192.0.2.10", "192.0.2.11"))
            install = [call[-1] for call in calls if call[0] == "ssh"][2]
            self.assertIn(f"sudo systemctl mask {units}", install)
            self.assertIn(f"sudo systemctl unmask {units}", install)
            self.assertIn("--no-install-recommends", install)
            self.assertLess(install.index("mask"), install.index("apt-get install"))


class ProbeTest(unittest.TestCase):
    def test_transfer_log_detection_for_both_native_formats(self) -> None:
        catalog = probe.catalog_name("192.0.2.11")
        bind_lines = [
            f"client @0x7f00 192.0.2.10#40123 ({catalog}): transfer of '{catalog}/IN': AXFR started (serial 1)",
            "client @0x7f01 192.0.2.10#40124 (s1-kill.test): transfer of 's1-kill.test/IN': AXFR ended",
            "zone s1-kill.test/IN: sending notifies (serial 2026083101)",
            "unrelated line",
        ]
        self.assertTrue(probe.transfers_to(bind_lines, catalog, "192.0.2.10"))
        self.assertTrue(probe.transfers_to(bind_lines, "s1-kill.test", "192.0.2.10"))
        self.assertFalse(probe.transfers_to(bind_lines, "s1-kill.test", "192.0.2.1"))
        self.assertEqual(len(probe.transfer_log_lines(bind_lines, [catalog, "s1-kill.test"])), 3)
        pdns_lines = [
            "AXFR-out zone 's1-kill.test.', client '192.0.2.10:53001', transfer initiated",
            "Queued notification of domain 's1-kill.test' to 192.0.2.10:53",
        ]
        self.assertTrue(probe.transfers_to(pdns_lines, "s1-kill.test", "192.0.2.10"))
        self.assertFalse(probe.transfers_to(pdns_lines, catalog, "192.0.2.10"))
        self.assertEqual(len(probe.transfer_log_lines(pdns_lines, ["s1-kill.test"])), 2)

    def test_soa_response_parser(self) -> None:
        answer = rr("s1-kill.test", "SOA",
                    "ns1.s1-kill.test. hostmaster.s1-kill.test. 2026083101 10800 3600 604800 3600", 3600)
        raw = (struct.pack("!HHHHHH", 7, probe.FLAG_QR | probe.FLAG_AA, 1, 1, 0, 0)
               + probe.encode_name("s1-kill.test") + struct.pack("!HH", probe.DNS_TYPE_SOA, 1) + answer)
        self.assertEqual(
            probe.parse_rrset_response(raw, 7, "s1-kill.test", probe.DNS_TYPE_SOA),
            {"authoritative": True, "rcode": 0, "answers": 1, "values": [2026083101]},
        )
        with self.assertRaises(ValueError):
            probe.parse_rrset_response(raw + b"\x00", 7, "s1-kill.test", probe.DNS_TYPE_SOA)
        with self.assertRaises(ValueError):
            probe.parse_rrset_response(raw, 8, "s1-kill.test", probe.DNS_TYPE_SOA)

    def test_probe_identity_refusals(self) -> None:
        catalog = probe.catalog_name("192.0.2.11")
        probe.validate_identity("bind", "192.0.2.11", "192.0.2.10", catalog)
        for engine, primary, secondary, name in (
            ("knot", "192.0.2.11", "192.0.2.10", catalog),
            ("bind", "192.0.2.11", "192.0.2.11", catalog),
            ("pdns", "192.0.2.11", "10.0.2.15", catalog),
            ("bind", "192.0.2.11", "192.0.2.10", probe.catalog_name("192.0.2.10")),
        ):
            with self.subTest((engine, primary, secondary, name)), self.assertRaises(ValueError):
                probe.validate_identity(engine, primary, secondary, name)


if __name__ == "__main__":
    unittest.main()
