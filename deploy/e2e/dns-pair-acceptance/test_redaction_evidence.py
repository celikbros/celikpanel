#!/usr/bin/env python3
"""Offline tests: capture-time redaction, the evidence writer and topology rules."""

from __future__ import annotations

import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import evidence  # noqa: E402
import topology  # noqa: E402
from redaction import REDACTED, Redactor, secret_key  # noqa: E402

PASSWORD = "Fixture-Pa55word-0123456789abcdef"
COOKIE = "c0ffee-session-value-9f8e7d6c5b4a"
CSRF = "csrf-token-value-00112233445566"


class RedactionTest(unittest.TestCase):
    def setUp(self) -> None:
        self.redactor = Redactor([PASSWORD, COOKIE, CSRF])

    def test_secret_keys_and_public_metadata(self) -> None:
        for name in ("password", "new_password", "csrf_token", "session", "license_key",
                     "private_key", "api_key", "totp_code", "Authorization", "rndc_key"):
            self.assertTrue(secret_key(name), name)
        for name in ("public_key", "host_key_sha256", "credential_id", "code", "error_code",
                     "reason", "next_action", "license_state", "session_count", "credential_state",
                     "password_required", "totp_enabled", "state"):
            self.assertFalse(secret_key(name), name)

    def test_json_structure_is_kept_and_scalars_redacted(self) -> None:
        value = {
            "username": "s1-admin",
            "password": PASSWORD,
            "code": "pdns_primary_switch_paused",
            "guidance": {"reason": "paused", "next_action": "wait"},
            "recovery_codes": ["abc123def", "zzz999yyy"],
            "credentials": {"username": "x", "password": "anything-at-all"},
            "totp_enabled": True,
            "token": None,
        }
        out = self.redactor.value(value)
        self.assertEqual(out["password"], REDACTED)
        self.assertEqual(out["code"], "pdns_primary_switch_paused")
        self.assertEqual(out["guidance"], {"reason": "paused", "next_action": "wait"})
        self.assertEqual(out["recovery_codes"], [REDACTED, REDACTED])
        self.assertEqual(out["credentials"], {"username": "x", "password": REDACTED})
        self.assertIs(out["totp_enabled"], True)
        self.assertIsNone(out["token"])

    def test_registered_values_anywhere(self) -> None:
        text = f"url=/api/x?next=1 note {PASSWORD} cookie {COOKIE} header {CSRF}"
        out = self.redactor.text(text)
        for secret in (PASSWORD, COOKIE, CSRF):
            self.assertNotIn(secret, out)
        self.assertFalse(self.redactor.contains_secret(out))

    def test_shapes(self) -> None:
        pem = "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----"
        out = self.redactor.text(f"x {pem} Authorization: Bearer abcdefghijkl cp_session=zzzzzzzz;")
        self.assertNotIn("AAAA", out)
        self.assertNotIn("abcdefghijkl", out)
        self.assertNotIn("zzzzzzzz", out)
        self.assertIn("cp_session=" + REDACTED, out)
        self.assertEqual(self.redactor.text("GET /api?token=abc123&x=1"), f"GET /api?token={REDACTED}&x=1")

    def test_headers(self) -> None:
        out = self.redactor.headers([("Cookie", "a=b"), ("Set-Cookie", "s=1; HttpOnly"),
                                     ("X-CSRF-Token", CSRF), ("Content-Type", "application/json")])
        self.assertEqual(out[0], ["Cookie", REDACTED])
        self.assertEqual(out[1], ["Set-Cookie", REDACTED])
        self.assertEqual(out[2], ["X-CSRF-Token", REDACTED])
        self.assertEqual(out[3], ["Content-Type", "application/json"])

    def test_short_secret_refused(self) -> None:
        with self.assertRaises(ValueError):
            self.redactor.register("abc")


class EvidenceTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name).resolve()
        self.redactor = Redactor([PASSWORD, COOKIE])

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def test_create_new_and_sums(self) -> None:
        writer = evidence.EvidenceWriter(self.root, "run-1", self.redactor)
        writer.write_json("run.json", {"schema": evidence.RUN_SCHEMA})
        writer.api_exchange("steps/01-login", {
            "request": {"method": "POST", "path": "/api/v1/auth/login",
                        "body": {"username": "a", "password": PASSWORD}},
            "response": {"status": 200, "headers": [["Set-Cookie", f"cp_session={COOKIE}"]]},
        })
        writer.write_text("guests/debian13/journal-x.txt", f"line with {COOKIE}\r\n")
        with self.assertRaises(FileExistsError):
            writer._write_bytes("run.json", b"{}")
        result = {
            "schema": evidence.RESULT_SCHEMA,
            "steps": [{"id": "login", "verdict": "passed"}],
            "overall": "passed",
        }
        writer.finalize(result)
        with self.assertRaises(evidence.EvidenceError):
            writer.write_json("late.json", {})
        self.assertEqual(evidence.verify_sums(writer.directory), [])
        blob = b"".join(path.read_bytes() for path in writer.directory.rglob("*") if path.is_file())
        self.assertNotIn(PASSWORD.encode(), blob)
        self.assertNotIn(COOKIE.encode(), blob)
        with self.assertRaises(FileExistsError):
            evidence.EvidenceWriter(self.root, "run-1", self.redactor)
        sums = (writer.directory / "SHA256SUMS").read_text()
        self.assertIn("result.json", sums)
        self.assertIn("steps/01-login/api/0001-post-api-v1-auth-login.json", sums)

    def test_tamper_detected(self) -> None:
        writer = evidence.EvidenceWriter(self.root, "run-2", self.redactor)
        writer.write_json("a.json", {"x": 1})
        writer.finalize({"schema": evidence.RESULT_SCHEMA, "steps": [], "overall": "incomplete"})
        (writer.directory / "a.json").write_text("{}\n")
        (writer.directory / "extra.txt").write_text("x\n")
        self.assertEqual(sorted(evidence.verify_sums(writer.directory)), ["a.json", "unlisted:extra.txt"])

    def test_unsafe_paths_and_overall_consistency(self) -> None:
        writer = evidence.EvidenceWriter(self.root, "run-3", self.redactor)
        for bad in ("../x.json", "/abs.json", "a//b.json", "a/./b.json", "a/.hidden"):
            with self.assertRaises(evidence.EvidenceError, msg=bad):
                writer.write_json(bad, {})
        with self.assertRaises(evidence.EvidenceError):
            writer.finalize({"schema": evidence.RESULT_SCHEMA,
                             "steps": [{"verdict": "failed"}], "overall": "passed"})

    def test_secret_that_escapes_redaction_is_refused(self) -> None:
        # A value that is registered after the fact still cannot reach disk.
        redactor = Redactor()
        writer = evidence.EvidenceWriter(self.root, "run-4", redactor)
        redactor.register("late-secret-value")
        # write_text redacts registered values, so force the raw path:
        with self.assertRaises(evidence.EvidenceError):
            writer._write_bytes("raw.txt", b"late-secret-value")

    def test_overall_status(self) -> None:
        self.assertEqual(evidence.overall_status(["passed", "passed"]), "passed")
        self.assertEqual(evidence.overall_status(["passed", "refused-by-product-gate", "skipped"]),
                         "refused-by-product-gate")
        self.assertEqual(evidence.overall_status(["passed", "blocked-product", "refused-by-product-gate"]),
                         "blocked-product")
        self.assertEqual(evidence.overall_status(["passed", "failed", "blocked-product"]), "failed")
        self.assertEqual(evidence.overall_status(["passed", "not-run"]), "incomplete")
        self.assertEqual(evidence.overall_status(["skipped"]), "incomplete")
        with self.assertRaises(evidence.EvidenceError):
            evidence.overall_status(["green"])

    def test_run_directory_mode(self) -> None:
        writer = evidence.EvidenceWriter(self.root, "run-5", self.redactor)
        if os.name == "posix":
            self.assertEqual(writer.directory.stat().st_mode & 0o777, 0o700)
        with self.assertRaises(evidence.EvidenceError):
            evidence.EvidenceWriter(Path("relative"), "run-6", self.redactor)


class TopologyTest(unittest.TestCase):
    def test_forced_placement(self) -> None:
        value = topology.resolve("bind-primary/pdns-secondary")
        self.assertEqual((value.primary.node, value.secondary.node), ("arch", "debian13"))
        self.assertEqual(value.secondary.engine, "pdns")
        value = topology.resolve("pdns-primary/bind-secondary", primary_node="debian13")
        self.assertEqual((value.primary.address, value.secondary.address), ("192.0.2.10", "192.0.2.11"))

    def test_pdns_never_on_arch(self) -> None:
        with self.assertRaisesRegex(topology.TopologyError, "Debian 13"):
            topology.resolve("pdns-primary/bind-secondary", primary_node="arch")
        with self.assertRaisesRegex(topology.TopologyError, "Debian 13"):
            topology.resolve("bind-primary/pdns-secondary", primary_node="debian13")

    def test_control_requires_explicit_placement(self) -> None:
        with self.assertRaisesRegex(topology.TopologyError, "required"):
            topology.resolve("bind/bind")
        for node in ("debian13", "arch"):
            value = topology.resolve("bind/bind", primary_node=node)
            self.assertTrue(value.control)
            self.assertEqual(value.primary.node, node)
            self.assertNotEqual(value.secondary.node, node)

    def test_names_and_cell_id(self) -> None:
        with self.assertRaises(topology.TopologyError):
            topology.resolve("pdns/pdns", primary_node="debian13")
        with self.assertRaises(topology.TopologyError):
            topology.resolve("bind/bind", primary_node="freebsd")
        with self.assertRaises(topology.TopologyError):
            topology.resolve("bind/bind", primary_node="arch", zone="bad_label.example")
        with self.assertRaises(topology.TopologyError):
            topology.resolve("bind/bind", primary_node="arch", zone="a.example", infra_zone="example")
        value = topology.resolve("bind/bind", primary_node="arch")
        cell = value.cell_id("r1")
        self.assertIn("__", cell)
        self.assertEqual(value.primary.nameserver, "ns1." + value.infra_zone)
        self.assertEqual(value.secondary.peer_nameserver, value.primary.nameserver)
        self.assertEqual(json.loads(json.dumps(value.as_dict()))["secondary"]["os"], "debian-13")


class RunIdTest(unittest.TestCase):
    """H1 (pair1): the driver's run ID must be what EvidenceWriter admits."""

    def test_run_id_is_lowercase_and_accepted(self) -> None:
        import datetime as dt

        when = dt.datetime(2026, 9, 29, 17, 19, 7, tzinfo=dt.timezone.utc)
        run_id = evidence.make_run_id("r2", when)
        self.assertEqual(run_id, "r2-20260929t171907z")
        with tempfile.TemporaryDirectory() as directory:
            writer = evidence.EvidenceWriter(Path(directory).resolve(), run_id, Redactor())
            self.assertTrue(writer.directory.is_dir())
            with self.assertRaises(evidence.EvidenceError):
                evidence.EvidenceWriter(Path(directory).resolve(), "r2-20260929T171907Z", Redactor())
        with self.assertRaises(evidence.EvidenceError):
            evidence.make_run_id("R2", when)
        with self.assertRaises(evidence.EvidenceError):
            evidence.make_run_id("r2", when.replace(tzinfo=None))
        local = when.astimezone(dt.timezone(dt.timedelta(hours=3)))
        self.assertEqual(evidence.make_run_id("r2", local), run_id)


if __name__ == "__main__":
    unittest.main()
