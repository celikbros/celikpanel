#!/usr/bin/env python3

import argparse
import hashlib
from pathlib import Path
import subprocess
import sqlite3
import tempfile
import unittest
from unittest import mock

import guest_bootstrap as bootstrap
import native_pdns_peer as peer
import native_pdns_peer_probe as probe


def catalog_axfr(member: str | None) -> str:
    catalog = "catalog-c000020a.celikpanel.invalid."
    soa = f"{catalog} 60 IN SOA invalid. invalid. 1 60 30 3600 30\n"
    ptr = (hashlib.sha224(member.encode()).hexdigest() + ".zones." + catalog
           + f" 60 IN PTR {member}.\n") if member else ""
    return (";; ->>HEADER<<- opcode: QUERY, status: NOERROR\n" + soa
            + f"{catalog} 60 IN NS invalid.\n"
            + f"version.{catalog} 60 IN TXT \"2\"\n" + ptr + soa)


def pdns_catalog_axfr(member: str | None, owner: str = "tlhnkturltuku63bhersj40lbi0vkdvt") -> str:
    catalog = "catalog-c000020a.celikpanel.invalid."
    soa = f"{catalog} 60 IN SOA invalid. invalid. 1 60 30 3600 30\n"
    ptr = (f"{owner}.zones.{catalog} 60 IN PTR {member}.\n" if member else "")
    return (";; ->>HEADER<<- opcode: QUERY, status: NOERROR\n" + soa
            + f"{catalog} 60 IN NS invalid.\n"
            + f'version.{catalog} 60 IN TXT "2"\n' + ptr + soa)


class NativePDNSPeerTest(unittest.TestCase):
    def test_pair_identity_and_config(self) -> None:
        plan = {"nodes": {
            "debian13": {"peer": {"address": "192.0.2.10/24"}},
            "arch": {"peer": {"address": "192.0.2.11/24"}},
        }}
        cell = {"driver": "pdns-switch", "role": "paired-primary",
                "placement": {"kill_host": "debian-13"}}
        self.assertEqual(peer.pair_addresses(plan, cell)[:2],
                         ("192.0.2.10", "192.0.2.11"))
        self.assertEqual(peer.catalog_name("192.0.2.10"),
                         "catalog-c000020a.celikpanel.invalid")
        config = peer.peer_config("192.0.2.10", "192.0.2.11")
        self.assertIn("launch=gsqlite3\n", config)
        self.assertIn("secondary=yes\n", config)
        self.assertIn("setuid=powerdns\n", config)
        self.assertIn("setgid=powerdns\n", config)
        self.assertIn("autosecondary=no\n", config)
        self.assertIn("allow-axfr-ips=192.0.2.10/32,127.0.0.1/32\n", config)
        self.assertIn("disable-axfr=no\n", config)
        self.assertNotIn("disable-axfr=yes", config)
        with self.assertRaises(bootstrap.BootstrapError):
            peer.peer_config("192.0.2.10", "192.0.2.10")
        self.assertNotIn("also-notify", config)
        with self.assertRaises(bootstrap.BootstrapError):
            peer.pair_addresses(plan, {**cell, "role": "standalone"})

    def test_probe_requires_catalog_and_loaded_member_for_presence(self) -> None:
        db = sqlite3.connect(":memory:")
        db.executescript("CREATE TABLE domains(id INTEGER PRIMARY KEY,name TEXT,type TEXT,master TEXT,account TEXT,catalog TEXT);"
                         "CREATE TABLE records(domain_id INTEGER,name TEXT,type TEXT,content TEXT);")
        db.execute("INSERT INTO domains VALUES(1,?,?,?,?,?)",
                   ("catalog-c000020a.celikpanel.invalid", "CONSUMER", "192.0.2.10", "fixture-pdns-peer", None))
        db.execute("INSERT INTO domains VALUES(2,?,?,?,?,?)",
                   ("s1-kill.test", "SLAVE", "192.0.2.10", None,
                    "catalog-c000020a.celikpanel.invalid"))
        db.execute("INSERT INTO records VALUES(2,'www.s1-kill.test','A','192.0.2.10')")
        db.commit()
        original_connect = sqlite3.connect
        with (mock.patch.object(probe.sqlite3, "connect", side_effect=lambda *a, **kw: db),
              mock.patch.object(probe.subprocess, "run", return_value=subprocess.CompletedProcess(["systemctl"], 0)),
              mock.patch.object(probe, "dig", side_effect=[
                  pdns_catalog_axfr("s1-kill.test"),
                  ";; flags: qr aa; status: NOERROR\nwww.s1-kill.test. A 192.0.2.10\n",
                  ";; flags: qr aa; status: NOERROR\nwww.s1-kill.test. A 192.0.2.10\n",
              ])):
            result = probe.observe("192.0.2.10", "192.0.2.11",
                "catalog-c000020a.celikpanel.invalid", "s1-kill.test", "present", "192.0.2.10")
            self.assertTrue(result["catalog_member"])
            self.assertTrue(result["authoritative_udp_tcp"])
        db.close()

    def test_catalog_parser_rejects_stray_ptr_owner_and_serial_change(self) -> None:
        catalog = "catalog-c000020a.celikpanel.invalid"
        serial, members = probe.parse_catalog_axfr(catalog_axfr("s1-kill.test"), catalog)
        self.assertEqual((serial, members), (1, {"s1-kill.test"}))
        forged = catalog_axfr("s1-kill.test").replace(
            "b076e9241974292fffe8ecc0209b7ace316d1d36063423d9ccb0849a.zones.",
            "0" * 56 + ".zones.")
        with self.assertRaisesRegex(ValueError, "PTR owner"):
            probe.parse_catalog_axfr(forged, catalog)
        changed = catalog_axfr(None).replace("invalid. invalid. 1 60", "invalid. invalid. 2 60", 1)
        with self.assertRaisesRegex(ValueError, "SOA framing"):
            probe.parse_catalog_axfr(changed, catalog)


    def test_catalog_parser_accepts_powerdns_base32hex_owner_only_when_selected(self) -> None:
        catalog = "catalog-c000020a.celikpanel.invalid"
        answer = pdns_catalog_axfr("s1-kill.test")
        with self.assertRaisesRegex(ValueError, "PTR owner"):
            probe.parse_catalog_axfr(answer, catalog)
        self.assertEqual(probe.parse_catalog_axfr(answer, catalog, producer="powerdns"),
                         (1, {"s1-kill.test"}))
        with self.assertRaisesRegex(ValueError, "PTR owner"):
            probe.parse_catalog_axfr(pdns_catalog_axfr("s1-kill.test", "z" * 32), catalog,
                                     producer="powerdns")
        duplicate = answer.replace("PTR s1-kill.test.", "PTR s1-kill.test.\n"
                                   + "tlhnkturltuku63bhersj40lbi0vkdvt.zones."
                                   + catalog + ". 60 IN PTR s1-kill.test.")
        with self.assertRaisesRegex(ValueError, "PTR owner"):
            probe.parse_catalog_axfr(duplicate, catalog, producer="powerdns")

    def test_catalog_parser_rejects_noncanonical_powerdns_member(self) -> None:
        with self.assertRaisesRegex(ValueError, "not canonical"):
            probe.parse_catalog_axfr(pdns_catalog_axfr("S1-kill.test"),
                                     "catalog-c000020a.celikpanel.invalid", producer="powerdns")

    def test_absent_probe_rejects_loaded_member_despite_negative_dns(self) -> None:
        db = sqlite3.connect(":memory:")
        db.executescript("CREATE TABLE domains(id INTEGER PRIMARY KEY,name TEXT,type TEXT,master TEXT,account TEXT,catalog TEXT);"
                         "CREATE TABLE records(domain_id INTEGER,name TEXT,type TEXT,content TEXT);")
        db.execute("INSERT INTO domains VALUES(1,?,?,?,?,?)",
                   ("catalog-c000020a.celikpanel.invalid", "CONSUMER", "192.0.2.10", "fixture-pdns-peer", None))
        db.execute("INSERT INTO domains VALUES(2,?,?,?,?,?)",
                   ("s1-kill.test", "SLAVE", "192.0.2.10", None,
                    "catalog-c000020a.celikpanel.invalid"))
        db.commit()
        with (mock.patch.object(probe.sqlite3, "connect", return_value=db),
              mock.patch.object(probe.subprocess, "run", return_value=subprocess.CompletedProcess(["systemctl"], 0)),
              mock.patch.object(probe, "dig", return_value=pdns_catalog_axfr(None))):
            with self.assertRaisesRegex(ValueError, "loaded member remains"):
                probe.observe("192.0.2.10", "192.0.2.11",
                    "catalog-c000020a.celikpanel.invalid", "s1-kill.test", "absent", None)
        db.close()

    def test_prepare_stages_consumer_without_requiring_primary_catalog(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            args = argparse.Namespace(action="prepare", work_root=Path(temporary),
                cell_id="pdns-switch__intent__after-write__paired-primary__peer-reachable",
                manifest=Path(temporary) / "manifest.json", identity_file=Path(temporary) / "key",
                execute=False, expect=None, address=None)
            plan = {"nodes": {
                "debian13": {"peer": {"address": "192.0.2.10/24"}},
                "arch": {"peer": {"address": "192.0.2.11/24"}},
            }}
            cell = {"driver": "pdns-switch", "role": "paired-primary",
                    "placement": {"kill_host": "debian-13"}}
            with (mock.patch.object(peer.fixture, "load_cell_plan", return_value=plan),
                  mock.patch.object(bootstrap, "load_manifest_cell", return_value=cell),
                  mock.patch.object(bootstrap, "validate_pdns_switch_cell"),
                  mock.patch.object(bootstrap, "identity_file", return_value=args.identity_file),
                  mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]),
                  mock.patch.object(bootstrap, "scp_base", return_value=["scp"]),
                  mock.patch.object(bootstrap, "remote_destination", return_value="celik@peer:fixture"),
                  mock.patch.object(bootstrap, "run") as executed):
                peer.host_action(args)
            commands = [item.args[0] for item in executed.call_args_list]
            remote = " ".join(commands[-1])
            self.assertIn("CONSUMER", remote)
            self.assertIn("-o powerdns -g powerdns", remote)
            self.assertIn("systemctl enable --now pdns.service", remote)
            self.assertNotIn("dig ", remote)
            self.assertNotIn("pdns_control retrieve", remote)

    def test_foreign_marker_refuses_before_mutation(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            args = argparse.Namespace(action="prepare", work_root=Path(temporary),
                cell_id="pdns-switch__intent__after-write__paired-primary__peer-reachable",
                manifest=Path(temporary) / "manifest.json", identity_file=Path(temporary) / "key",
                execute=True, expect=None, address=None)
            plan = {"nodes": {
                "debian13": {"peer": {"address": "192.0.2.10/24"}},
                "arch": {"peer": {"address": "192.0.2.11/24"}},
            }}
            cell = {"driver": "pdns-switch", "role": "paired-primary",
                    "placement": {"kill_host": "debian-13"}}
            with (mock.patch.object(peer.fixture, "load_cell_plan", return_value=plan),
                  mock.patch.object(bootstrap, "load_manifest_cell", return_value=cell),
                  mock.patch.object(bootstrap, "validate_pdns_switch_cell"),
                  mock.patch.object(bootstrap, "identity_file", return_value=args.identity_file),
                  mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]),
                  mock.patch.object(peer.subprocess, "run", return_value=subprocess.CompletedProcess(
                      ["ssh"], 0, stdout="foreign\n")),
                  mock.patch.object(bootstrap, "run") as mutation):
                with self.assertRaisesRegex(bootstrap.BootstrapError, "marker"):
                    peer.host_action(args)
                mutation.assert_not_called()


if __name__ == "__main__":
    unittest.main()
