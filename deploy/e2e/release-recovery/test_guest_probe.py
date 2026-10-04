#!/usr/bin/env python3
"""Offline guard/parser tests; no VM, network, service, or installed state access.

Çevrimdışı koruma/ayrıştırıcı testleri; VM, ağ, servis veya kurulu duruma erişmez.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import sqlite3
import sys
import os
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("guest_probe", Path(__file__).with_name("guest_probe.py"))
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)

NONCE = "a" * 64
UUID = "67139a2c-7b23-4387-95ad-45f9e2b831ea"
IDENTITY = {"schema": probe.MARKER_SCHEMA, "nonce": NONCE, "vm_uuid": UUID,
            "cell_id": "alpha75-to-alpha80", "node": "debian13"}


class IdentityTests(unittest.TestCase):
    def validate(self, raw=None, **kw):
        args = {"nonce": NONCE, "vm_uuid": UUID, "cell_id": "alpha75-to-alpha80",
                "node": "debian13", "dmi_uuid": UUID.upper(),
                "vendor": "QEMU", "product": "Standard PC (Q35 + ICH9, 2009)"}
        args.update(kw)
        return probe.validate_identity(json.dumps(IDENTITY).encode() if raw is None else raw, **args)

    def test_exact_qemu_marker(self):
        self.assertEqual(self.validate()["nonce"], NONCE)

    def test_wrong_identity_real_hardware_extra_fields_and_duplicate_fields_refused(self):
        for changes in ({"nonce": "b" * 64}, {"dmi_uuid": "3c7b156c-7b6e-4f32-9a2d-750174e41f4d"},
                        {"vendor": "Dell Inc."}, {"product": "PowerEdge"}, {"node": "arch"},
                        {"cell_id": "../live"}):
            with self.subTest(changes=changes), self.assertRaises(probe.ProbeError):
                self.validate(**changes)
        for raw in (json.dumps(dict(IDENTITY, production=True)).encode(), b'{"schema":1,"schema":2}', b'[]'):
            with self.subTest(raw=raw), self.assertRaises(probe.ProbeError):
                self.validate(raw)

    def test_argument_cannot_select_remote_dns_server(self):
        for value in ("@72.62.38.15", "; id", "--server", "bad..test", "bad_name.test"):
            with self.subTest(value=value), self.assertRaises(argparse.ArgumentTypeError):
                probe.valid_dns_name(value)
        self.assertEqual(probe.valid_dns_name("Panel.Lab.Example."), "panel.lab.example")


class ParserTests(unittest.TestCase):
    def test_service_and_timer_properties_are_distinct(self):
        raw = "Id=celikpanel-agent.service\nLoadState=loaded\nActiveState=active\nSubState=running\nMainPID=123\n"
        self.assertEqual(probe.parse_properties(raw)["MainPID"], "123")
        timer = "Id=certbot.timer\nLoadState=loaded\nActiveState=active\nSubState=waiting\nLastTriggerUSec=\n"
        self.assertEqual(probe.parse_properties(timer, timer=True)["SubState"], "waiting")
        for bad in (raw + "MainPID=999\n", raw.replace("123", "error"), "ActiveState=active\n"):
            with self.assertRaises(probe.ProbeError):
                probe.parse_properties(bad)

    def test_transaction_secrets_are_not_returned(self):
        observed = probe.parse_transaction(b"operation=update\nsnapshot=20260914T000000Z-test\ntoken=supersecret\nlock_token=othersecret\n")
        self.assertEqual(observed, {"operation": "update", "snapshot": "20260914T000000Z-test"})
        self.assertNotIn("secret", json.dumps(observed))
        for raw in (b"operation=update\nsnapshot=../bad\n", b"operation=update\nsnapshot=x\noperation=rollback\n"):
            with self.assertRaises(probe.ProbeError):
                probe.parse_transaction(raw)

    def test_dns_preserves_positive_negative_and_non_authoritative_responses(self):
        raw = ";; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 1\n;; flags: qr aa; QUERY: 1\npanel.lab.example. 300 IN A 127.0.0.2\n"
        value = probe.parse_dig(raw)
        self.assertTrue(value["authoritative"])
        self.assertEqual(value["answers"][0]["data"], "127.0.0.2")
        missing = probe.parse_dig(raw.split("panel.lab.example")[0].replace("NOERROR", "NXDOMAIN").replace("qr aa", "qr"))
        self.assertEqual(missing["rcode"], "NXDOMAIN")
        self.assertFalse(missing["authoritative"])
        with self.assertRaises(probe.ProbeError):
            probe.parse_dig(";; communications error: connection refused")

    def test_public_certificate_fields(self):
        raw = "sha256 Fingerprint=" + ":".join(["AB"] * 32) + "\nnotBefore=Jan 1 00:00:00 2026 GMT\nnotAfter=Jan 2 00:00:00 2026 GMT\nX509v3 Subject Alternative Name:\n    DNS:panel.lab.example, IP Address:127.0.0.1\n"
        value = probe.parse_certificate(raw)
        self.assertEqual(value["sha256_fingerprint"], "ab" * 32)
        self.assertEqual(value["subject_alt_name"], ["DNS:panel.lab.example", "IP Address:127.0.0.1"])
        with self.assertRaises(probe.ProbeError):
            probe.parse_certificate("sha256 Fingerprint=AA\n")

    def event(self, message, unit="celikpanel-release-recovery.service"):
        return probe.journal_event({"MESSAGE": message, "_SYSTEMD_UNIT": unit,
                                    "__REALTIME_TIMESTAMP": "1790000000000000"})

    def test_only_exact_native_rollback_end_is_terminal_text_evidence(self):
        value = self.event("==> Rollback complete / Geri alma tamamlandı")
        self.assertEqual(value["kind"], "rollback_completion_text")
        for message in ("Rollback complete? no", "!! rollback complete was not reached", "license_key=secret",
                        "==> Rollback runtime was already complete; Certbot scheduler restoration is complete."):
            self.assertIsNone(self.event(message))
        self.assertIsNone(self.event("==> Rollback complete / Geri alma tamamlandı", "ssh.service"))

    def test_failure_event_contains_only_public_code_and_state(self):
        value = self.event("!! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason=token=secret password=secret")
        self.assertEqual(value["safe_message"], "CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required")
        self.assertNotIn("secret", json.dumps(value))


class ObservationTests(unittest.TestCase):
    def test_malformed_subsystem_preserves_independent_observation(self):
        def malformed_tls():
            raise RuntimeError("symlink loop contains private fixture detail")
        values = {
            "tls": probe.collect_observation(malformed_tls),
            "database": probe.collect_observation(lambda: {"status": "ok", "integrity_check": ["ok"]}),
        }
        self.assertEqual(values["tls"]["status"], "unknown")
        self.assertNotIn("private", json.dumps(values))
        self.assertEqual(values["database"]["status"], "ok")
        for interruption in (KeyboardInterrupt, SystemExit):
            def interrupted():
                raise interruption()
            with self.assertRaises(interruption):
                probe.collect_observation(interrupted)

    @unittest.skipUnless(sys.platform == "linux" and getattr(os, "geteuid", lambda: -1)() == 0, "native root file semantics")
    def test_database_nonempty_wal_is_unknown_without_sqlite_open(self):
        with tempfile.TemporaryDirectory() as root:
            database = Path(root) / "celikpanel.db"
            database.write_bytes(b"fixture")
            Path(str(database) + "-wal").write_bytes(b"uncheckpointed")
            before = sorted((p.name, p.read_bytes()) for p in Path(root).iterdir())
            with patch.object(probe, "PANEL_DB", database), patch.object(probe.sqlite3, "connect") as connect:
                result = probe.observe_database()
            connect.assert_not_called()
            self.assertEqual(result["status"], "unknown")
            self.assertEqual(before, sorted((p.name, p.read_bytes()) for p in Path(root).iterdir()))

    @unittest.skipUnless(sys.platform == "linux" and getattr(os, "geteuid", lambda: -1)() == 0, "native root file semantics")
    def test_database_checkpointed_fixture_integrity_and_schema_are_read_only(self):
        with tempfile.TemporaryDirectory() as root:
            database = Path(root) / "celikpanel.db"
            connection = sqlite3.connect(database)
            connection.executescript("CREATE TABLE schema_migrations(version INTEGER); INSERT INTO schema_migrations VALUES (1),(37);")
            connection.close()
            before = database.read_bytes()
            with patch.object(probe, "PANEL_DB", database):
                result = probe.observe_database()
            self.assertEqual(result["status"], "ok")
            self.assertEqual(result["integrity_check"], ["ok"])
            self.assertEqual(result["schema_version"], 37)
            self.assertEqual(before, database.read_bytes())
            self.assertEqual([p.name for p in Path(root).iterdir()], ["celikpanel.db"])

    def test_every_dns_transport_is_loopback_and_nonrecursive(self):
        calls = []
        def fake(argv, **kwargs):
            calls.append(argv)
            return probe.unknown("fixture unavailable")
        with patch.object(probe, "command", side_effect=fake):
            result = probe.observe_dns("lab.example", "panel.lab.example")
        self.assertEqual(len(calls), 4)
        self.assertTrue(all("@127.0.0.1" in argv and "+norecurse" in argv for argv in calls))
        self.assertTrue(all(result[key]["status"] == "unknown" for key in ("soa_udp", "soa_tcp", "a_udp", "a_tcp")))


if __name__ == "__main__":
    unittest.main()
