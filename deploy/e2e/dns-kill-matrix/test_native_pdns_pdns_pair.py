#!/usr/bin/env python3

import argparse
import hashlib
import json
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import unittest
from unittest import mock

import guest_bootstrap as bootstrap
import native_pdns_pdns_pair as pair
import native_pdns_pdns_primary_probe as primary_probe


CATALOG = "catalog-c000020a.celikpanel.invalid"


def catalog_axfr() -> str:
    apex = CATALOG + "."
    member = "s1-kill.test"
    ptr = hashlib.sha224(member.encode()).hexdigest() + ".zones." + apex
    soa = f"{apex} 60 IN SOA invalid. invalid. 1 60 30 3600 30\n"
    return (";; status: NOERROR\n" + soa + f"{apex} 60 IN NS invalid.\n"
            + f"version.{apex} 60 IN TXT \"2\"\n"
            + f"{ptr} 60 IN PTR {member}.\n" + soa)


class NativePowerDNSPairTest(unittest.TestCase):
    def test_primary_requires_real_producer_sql_and_authoritative_answers(self) -> None:
        db = sqlite3.connect(":memory:")
        db.executescript("CREATE TABLE domains(id INTEGER PRIMARY KEY,name TEXT,type TEXT,master TEXT,account TEXT,catalog TEXT);"
                         "CREATE TABLE records(domain_id INTEGER,name TEXT,type TEXT,content TEXT);")
        db.execute("INSERT INTO domains VALUES(1,?,?,?,?,?)",
                   (CATALOG, "PRODUCER", None, primary_probe.ACCOUNT, None))
        db.execute("INSERT INTO domains VALUES(2,?,?,?,?,?)",
                   ("s1-kill.test", "NATIVE", None, None, CATALOG))
        db.execute("INSERT INTO records VALUES(2,?,?,?)",
                   ("www.s1-kill.test", "A", "192.0.2.10"))
        db.commit()
        def service(argv, **_):
            return subprocess.CompletedProcess(argv, 0 if argv[-1] == "pdns.service" else 3)
        state = {"schema": "celikpanel-dns-engine-state/v1", "engine": "pdns",
                 "pair_role": "primary", "pair_local_ip": "192.0.2.10",
                 "pair_peer_ip": "192.0.2.11", "primary_catalog_serial": 1}
        with (mock.patch.object(primary_probe.Path, "read_text", return_value=json.dumps(state)),
              mock.patch.object(primary_probe.sqlite3, "connect", return_value=db),
              mock.patch.object(primary_probe.subprocess, "run", side_effect=service),
              mock.patch.object(primary_probe, "dig", side_effect=[
                  catalog_axfr(),
                  ";; flags: qr aa; status: NOERROR\nwww.s1-kill.test. 60 IN A 192.0.2.10\n",
                  ";; flags: qr aa; status: NOERROR\nwww.s1-kill.test. 60 IN A 192.0.2.10\n",
              ])):
            result = primary_probe.observe("192.0.2.10", "192.0.2.11", "192.0.2.10")
            self.assertEqual(result["catalog_members"], ["s1-kill.test"])
            self.assertTrue(result["authoritative_udp_tcp"])
        db.close()

    def test_primary_rejects_stale_bind_receipt_before_sql(self) -> None:
        def service(argv, **_):
            return subprocess.CompletedProcess(argv, 0 if argv[-1] == "pdns.service" else 3)
        receipt = {"schema": "celikpanel-dns-engine-state/v1", "engine": "bind",
                   "pair_role": "primary", "pair_local_ip": "192.0.2.10",
                   "pair_peer_ip": "192.0.2.11", "primary_catalog_serial": 1}
        with (mock.patch.object(primary_probe.subprocess, "run", side_effect=service),
              mock.patch.object(primary_probe.Path, "read_text", return_value=json.dumps(receipt)),
              mock.patch.object(primary_probe.sqlite3, "connect") as database):
            with self.assertRaisesRegex(ValueError, "not this PowerDNS primary"):
                primary_probe.observe("192.0.2.10", "192.0.2.11", "192.0.2.10")
            database.assert_not_called()

    def test_primary_answer_rejects_address_only_in_comment(self) -> None:
        reply = ";; flags: qr aa; status: NOERROR; expected 192.0.2.10\n"
        self.assertFalse(primary_probe.authoritative_a(reply, "192.0.2.10"))

    def test_primary_rejects_bind_server_before_sql(self) -> None:
        def service(argv, **_):
            return subprocess.CompletedProcess(argv, 0)
        with (mock.patch.object(primary_probe.subprocess, "run", side_effect=service),
              mock.patch.object(primary_probe.sqlite3, "connect") as database):
            with self.assertRaisesRegex(ValueError, "BIND still serves"):
                primary_probe.observe("192.0.2.10", "192.0.2.11", "192.0.2.10")
            database.assert_not_called()

    def test_pair_rejects_wrong_catalog_serial(self) -> None:
        args = argparse.Namespace(action="observe", work_root=Path("."), cell_id=pair.CELL,
                                  identity_file=Path("key"), manifest=Path("manifest"),
                                  address="192.0.2.10", execute=True)
        source = {"catalog_serial": 2, "catalog_members": ["s1-kill.test"]}
        peer = {"catalog_serial": 1, "catalog_members": ["s1-kill.test"]}
        with (mock.patch.object(pair, "selected", return_value=({"peer": {}}, {"peer": {}}, Path("key"))),
              mock.patch.object(pair, "exact_marker"),
              mock.patch.object(pair, "remote", side_effect=["", "", json.dumps(source), json.dumps(peer)]),
              mock.patch.object(bootstrap, "scp_base", return_value=["scp"]),
              mock.patch.object(bootstrap, "remote_destination", return_value="celik@peer:/var/tmp/probe"),
              mock.patch.object(bootstrap, "run")):
            with self.assertRaisesRegex(ValueError, "producer and CONSUMER catalog differ"):
                pair.observe(args)

    def test_foreign_cell_refuses_before_peer_preparation(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            args = argparse.Namespace(action="prepare", work_root=Path(root),
                                      cell_id="foreign", identity_file=Path(root) / "key",
                                      manifest=Path(root) / "manifest", address=None, execute=False)
            with self.assertRaisesRegex(bootstrap.BootstrapError, "exact disposable"):
                pair.selected(args)


if __name__ == "__main__":
    unittest.main()
