#!/usr/bin/env python3
"""Offline tests: the driver's readers against the real pair1 and pair2 API payloads, and D1.

The fixtures in ``fixtures/pair1/`` and ``fixtures/pair2/`` are minimal copies
of captured exchanges (redacted at capture; the source path is metadata only,
the evidence tree is never read here). They are what the product returned, so
a reader that refuses them, or a fake that differs from them, is wrong.

pair1 = build ``aa6b9380`` (no ``secondary_ready`` anywhere); pair2 = build
``916e1577`` (``secondary_ready`` exactly on an active paired secondary).
"""

from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import fakes  # noqa: E402
import guidance as gd  # noqa: E402
import pair_contract as pc  # noqa: E402

FIXTURES = HERE / "fixtures" / "pair1"
FIXTURES2 = HERE / "fixtures" / "pair2"
T = gd.Translator(gd.load_catalog())
ENGINE_FIXTURES = {
    "engine-bind-primary-paired.json": ("primary", "bind"),
    "engine-bind-primary-paired-arch.json": ("primary", "bind"),
    "engine-bind-secondary-paired.json": ("secondary", "bind"),
    "engine-pdns-secondary-paired.json": ("secondary", "pdns"),
}
ENGINE_FIXTURES2 = {
    "engine-bind-primary-paired.json": ("primary", "bind"),
    "engine-bind-secondary-paired.json": ("secondary", "bind"),
    "engine-pdns-secondary-paired.json": ("secondary", "pdns"),
}
FIXTURE_KEYS = {"source", "note", "panel", "method", "path", "request_body", "status", "body"}


def load(name: str) -> dict:
    return json.loads((FIXTURES / name).read_text(encoding="utf-8"))


def load2(name: str) -> dict:
    return json.loads((FIXTURES2 / name).read_text(encoding="utf-8"))


class CapturedFixtureTest(unittest.TestCase):
    def check_minimal(self, directory: Path, run_dir: str, count: int) -> None:
        names = sorted(path.name for path in directory.glob("*.json"))
        self.assertEqual(len(names), count)
        for name in names:
            raw = (directory / name).read_bytes()
            self.assertNotIn(b"\r", raw)
            text = raw.decode("utf-8")
            for forbidden in ("Cookie", "celikpanel_session", "password", "CPK-"):
                self.assertNotIn(forbidden, text, name)
            document = json.loads(text)
            self.assertTrue(document["source"].startswith(f"deploy/e2e/dns-pair-acceptance/evidence/{run_dir}/"))
            self.assertEqual(set(document), FIXTURE_KEYS)

    def test_fixtures_are_minimal_redacted_and_self_describing(self) -> None:
        self.check_minimal(FIXTURES, "pair1-20260930", 7)
        self.check_minimal(FIXTURES2, "pair2-20260930", 7)

    def test_reader_accepts_every_captured_engine_snapshot(self) -> None:
        for name, (role, engine) in ENGINE_FIXTURES.items():
            facts = pc.read_engine_snapshot(load(name)["body"])
            self.assertEqual((facts["pair_role"], facts["active_engine"], facts["topology"], facts["state"]),
                             (role, engine, "paired", "ready"), name)
            self.assertIs(facts["pair_ready"], role == "primary", name)
            self.assertEqual(facts["secondary_ready"], pc.ABSENT, name)  # pair1 build: never serialized
        for name, (role, engine) in ENGINE_FIXTURES2.items():
            facts = pc.read_engine_snapshot(load2(name)["body"])
            self.assertEqual((facts["pair_role"], facts["active_engine"]), (role, engine), name)
            self.assertIs(facts["pair_ready"], role == "primary", name)
            # pair2 build (Decision D): only the active paired secondary carries the key.
            self.assertEqual(facts["secondary_ready"], True if role == "secondary" else pc.ABSENT, name)
        unconfigured = pc.read_engine_snapshot(load("engine-unconfigured.json")["body"])
        self.assertEqual((unconfigured["active_engine"], unconfigured["state"], unconfigured["pair_role"]),
                         (None, "unconfigured", pc.ABSENT))

    def test_d1_passes_on_every_pair2_capture(self) -> None:
        for name, (role, engine) in ENGINE_FIXTURES2.items():
            verdict = pc.pair_readiness(load2(name)["body"], role=role, engine=engine)
            self.assertTrue(verdict["passed"], (name, verdict["reasons"]))
            self.assertIsNone(verdict["blocked"])
            self.assertTrue(pc.readiness_final(verdict))

    def test_d1_on_the_pair1_build(self) -> None:
        # The primary rule never needed the field; the secondaries lack it: this build
        # does not report secondary readiness (blocked-product, final at once).
        for name, (role, engine) in ENGINE_FIXTURES.items():
            verdict = pc.pair_readiness(load(name)["body"], role=role, engine=engine)
            if role == "primary":
                self.assertTrue(verdict["passed"], (name, verdict["reasons"]))
                continue
            self.assertFalse(verdict["passed"], name)
            self.assertEqual(verdict["blocked"], pc.BLOCKED_NOT_REPORTED)
            self.assertEqual(verdict["absent_fields"], ["secondary_ready"])
            self.assertEqual(verdict["reasons"], [pc.NOT_REPORTED])
            self.assertIn("does not report secondary readiness", verdict["reasons"][0])
            self.assertTrue(pc.readiness_final(verdict))
            self.assertTrue(pc.engine_settled(load(name)["body"]))

    def test_primary_carrying_secondary_ready_is_a_contract_violation(self) -> None:
        primary = load2("engine-bind-primary-paired.json")["body"]
        for value in (False, True):
            body = primary | {"secondary_ready": value}
            with self.assertRaises(pc.ContractError):
                pc.read_engine_snapshot(body)
            verdict = pc.pair_readiness(body, role="primary", engine="bind")
            self.assertFalse(verdict["passed"])
            self.assertIsNone(verdict["blocked"], "a violation is a failure, never blocked-product")
            self.assertFalse(pc.readiness_final(verdict))
            self.assertTrue(any("contract violation" in reason for reason in verdict["reasons"]), verdict["reasons"])
            self.assertTrue(any("web client would refuse" in reason for reason in verdict["reasons"]))
        with self.assertRaisesRegex(pc.ContractError, "both true"):
            pc.read_engine_snapshot(primary | {"secondary_ready": True})
        with self.assertRaisesRegex(pc.ContractError, "outside an active paired secondary"):
            pc.read_engine_snapshot(primary | {"secondary_ready": False})
        unconfigured = load("engine-unconfigured.json")["body"]
        with self.assertRaisesRegex(pc.ContractError, "outside an active paired secondary"):
            pc.read_engine_snapshot(unconfigured | {"secondary_ready": False})

    def test_negative_secondary_with_pair_ready_true(self) -> None:
        body = load2("engine-bind-secondary-paired.json")["body"] | {"pair_ready": True}
        with self.assertRaisesRegex(pc.ContractError, "non-primary"):
            pc.read_engine_snapshot(body)
        verdict = pc.pair_readiness(body, role="secondary", engine="bind")
        self.assertFalse(verdict["passed"])
        self.assertTrue(any("pair_ready is True, D1 requires pair_ready === false" in r for r in verdict["reasons"]))
        self.assertTrue(any("web client would refuse" in r for r in verdict["reasons"]))

    def test_other_d1_combinations_fail(self) -> None:
        primary = load2("engine-bind-primary-paired.json")["body"]
        secondary = load2("engine-pdns-secondary-paired.json")["body"]
        cases = [
            (primary | {"pair_ready": False}, "primary", "bind"),
            (secondary | {"secondary_ready": False}, "secondary", "pdns"),
            (secondary, "secondary", "bind"),                                     # wrong engine
            (secondary, "primary", "pdns"),                                       # wrong role
            (secondary | {"state": "degraded"}, "secondary", "pdns"),
            (secondary | {"secondary_ready": 1}, "secondary", "pdns"),            # never truthiness
            ({key: value for key, value in secondary.items() if key != "pair_ready"}, "secondary", "pdns"),
            ({"poll_error": "HTTP 500"}, "secondary", "pdns"),
        ]
        for body, role, engine in cases:
            verdict = pc.pair_readiness(body, role=role, engine=engine)
            self.assertFalse(verdict["passed"], (role, engine, body))
            self.assertIsNone(verdict["blocked"], (role, engine))
        # Absent secondary_ready plus another fault is a failure, not blocked-product.
        stripped = {key: value for key, value in secondary.items() if key != "secondary_ready"}
        verdict = pc.pair_readiness(stripped | {"state": "degraded"}, role="secondary", engine="pdns")
        self.assertFalse(verdict["passed"])
        self.assertIsNone(verdict["blocked"])
        self.assertTrue(pc.pair_readiness(stripped, role="secondary", engine="pdns")["blocked"])

    def test_in_progress_switch_is_not_settled(self) -> None:
        body = load("engine-bind-primary-paired.json")["body"]
        switching = body | {"state": "switching", "operation_id": body["operation"]["id"],
                            "operation": body["operation"] | {"status": "running"}}
        self.assertFalse(pc.engine_settled(switching))
        self.assertFalse(pc.setup_settled({"status": "running"}))
        self.assertTrue(pc.setup_settled({"status": "waiting"}))


class CapturedSetupStateTest(unittest.TestCase):
    def test_t3_reconciling_is_a_listed_unknown_state(self) -> None:
        exchange = load2("setup-operation-primary-reconciling.json")
        execution = exchange["body"]
        self.assertEqual((exchange["path"].split("?")[0], exchange["status"]), ("/api/v1/setup/operation", 200))
        self.assertEqual(gd.unknown_state_code(execution), "server_setup_reconciling")
        self.assertEqual(gd.dns_step_status(execution), "running")
        item = gd.setup_execution_guidance(T, execution)
        self.assertEqual((item["state"], item["title_key"]), ("unknown", "setup.guide.confirmTitle"))
        self.assertEqual(item["message_keys"][:3], ["setup.guide.confirm", "setup.guide.primary",
                                                    "setup.guide.startSecondary"])
        # The driver's per-read model rates it actionable; only the time bound catches it.
        self.assertTrue(item["actionable"])
        self.assertIsNone(gd.unknown_state_code(load2("setup-operation-primary-running-dns.json")["body"]))
        self.assertIsNone(gd.unknown_state_code(execution | {"status": "failed"}))
        self.assertIsNone(gd.unknown_state_code({"poll_error": "HTTP 502"}))

    def test_t1_license_required_read_and_active_license(self) -> None:
        execution = load2("setup-operation-secondary-license-required.json")["body"]
        self.assertTrue(gd.license_required_state(execution))
        self.assertFalse(gd.license_required_state(load2("setup-operation-primary-reconciling.json")["body"]))
        item = gd.setup_execution_guidance(T, execution)
        self.assertEqual(item["message_keys"], ["setup.guide.license"])
        status = load2("license-status-secondary-active.json")
        self.assertEqual((status["path"], status["body"]["state"], status["body"]["can_provision"]),
                         ("/api/v1/panel/license", "active", True))

    def test_dns_blocking_state_for_the_contradictory_guidance_check(self) -> None:
        running = load2("setup-operation-primary-running-dns.json")["body"]
        reconciling = load2("setup-operation-primary-reconciling.json")["body"]
        self.assertIsNone(gd.dns_blocking_state(running), "ordinary progress is not blocking")
        pending = json.loads(json.dumps(running))
        pending["steps"][0]["status"] = "pending"
        self.assertIsNone(gd.dns_blocking_state(pending))
        self.assertEqual(gd.dns_blocking_state(reconciling), "unknown:server_setup_reconciling")
        failed = reconciling | {"status": "failed", "error": {"code": "server_setup_dns_failed"}}
        failed["steps"] = [dict(failed["steps"][0], status="failed")] + failed["steps"][1:]
        self.assertEqual(gd.dns_blocking_state(failed), "dns-step-failed:server_setup_dns_failed")
        rolled = failed | {"error": {"code": "server_setup_dns_rolled_back"}}
        self.assertEqual(gd.dns_blocking_state(rolled), "dns-rolled-back:server_setup_dns_rolled_back")
        waiting = running | {"status": "waiting", "phase": "dns_readiness",
                             "error": {"code": "server_setup_dns_readiness_required"}}
        self.assertEqual(gd.dns_blocking_state(waiting), "dns-waiting:server_setup_dns_readiness_required")
        at_dns_step = running | {"status": "waiting", "error": {"code": "server_setup_dns_peer_required"}}
        self.assertEqual(gd.dns_blocking_state(at_dns_step), "dns-waiting:server_setup_dns_peer_required")
        # A wait elsewhere (license before the DNS step) is not the DNS step's code.
        license_wait = running | {"status": "waiting", "phase": "license", "error": {"code": "license_required"}}
        license_wait["steps"] = [dict(license_wait["steps"][0], status="pending")] + license_wait["steps"][1:]
        self.assertIsNone(gd.dns_blocking_state(license_wait))
        # Once the DNS step succeeded nothing is blocking, whatever follows.
        after = load2("setup-operation-secondary-license-required.json")["body"]
        self.assertEqual(gd.dns_step_status(after), "succeeded")
        self.assertIsNone(gd.dns_blocking_state(after | {"status": "failed", "error": {"code": "x"}}))

    def test_role_guidance_keys_are_read_from_the_product_source(self) -> None:
        keys = gd.role_guidance_keys()
        self.assertIn("setup.guide.startSecondary", keys["primary"])
        self.assertIn("setup.guide.startPrimary", keys["secondary"])
        resolved = gd.peer_start_keys()
        self.assertEqual((resolved["state"], resolved["keys"]), ("resolved", ["setup.guide.startSecondary"]))
        self.assertEqual(gd.peer_start_keys(HERE / "no-such-file.ts")["state"], "not-used-by-this-build")
        self.assertEqual(gd.missing_web_src_files(gd.WEB_SRC), [])
        self.assertEqual(sorted(gd.missing_web_src_files(HERE)), sorted(gd.WEB_SRC_FILES.values()))


class FakeMirrorsCaptureTest(unittest.TestCase):
    def test_fake_engine_bodies_are_the_captured_ones(self) -> None:
        world = fakes.World()
        for (role, engine), name in fakes.CAPTURED_ENGINE.items():
            pair1 = fakes.FakePanel("debian13", role, engine, world, api_secondary_ready=False)
            self.assertEqual(pair1.engine_body(), load("engine-unconfigured.json")["body"])
            pair1.started = {"request_id": "r"}
            self.assertEqual(pair1.engine_body(), load(name)["body"], name)
            pair2 = fakes.FakePanel("debian13", role, engine, world, api_secondary_ready=True)
            pair2.started = {"request_id": "r"}
            self.assertEqual(pair2.engine_body(), load2(name)["body"], name)
            self.assertEqual("secondary_ready" in pair2.engine_body(), role == "secondary")
        derived = fakes.captured_engine("primary", "pdns")
        self.assertNotIn("secondary_ready", derived)
        self.assertTrue(pc.pair_readiness(derived, role="primary", engine="pdns")["passed"])

    def test_captured_setup_operation_guidance_is_actionable(self) -> None:
        execution = load("setup-operation-primary-waiting-access-dns.json")["body"]
        item = gd.setup_execution_guidance(T, execution)
        self.assertEqual((item["state"], item["code"]), ("unmet-prerequisite", "server_setup_access_dns_required"))
        self.assertTrue(item["actionable"])


if __name__ == "__main__":
    unittest.main()
