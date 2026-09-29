#!/usr/bin/env python3
"""Offline tests: the driver's readers against the real pair1 API payloads, and D1.

The fixtures in ``fixtures/pair1/`` are minimal copies of captured exchanges
(redacted at capture; the source path is metadata only, the evidence tree is
never read here). They are what the product returned, so a reader that refuses
them, or a fake that differs from them, is wrong.
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
T = gd.Translator(gd.load_catalog())
ENGINE_FIXTURES = {
    "engine-bind-primary-paired.json": ("primary", "bind"),
    "engine-bind-primary-paired-arch.json": ("primary", "bind"),
    "engine-bind-secondary-paired.json": ("secondary", "bind"),
    "engine-pdns-secondary-paired.json": ("secondary", "pdns"),
}


def load(name: str) -> dict:
    return json.loads((FIXTURES / name).read_text(encoding="utf-8"))


class CapturedFixtureTest(unittest.TestCase):
    def test_fixtures_are_minimal_redacted_and_self_describing(self) -> None:
        names = sorted(path.name for path in FIXTURES.glob("*.json"))
        self.assertEqual(len(names), 7)
        for name in names:
            raw = (FIXTURES / name).read_bytes()
            self.assertNotIn(b"\r", raw)
            text = raw.decode("utf-8")
            for forbidden in ("Cookie", "celikpanel_session", "password", "CPK-"):
                self.assertNotIn(forbidden, text, name)
            document = json.loads(text)
            self.assertTrue(document["source"].startswith("deploy/e2e/dns-pair-acceptance/evidence/pair1-20260930/"))
            self.assertEqual(set(document), {"source", "note", "panel", "method", "path", "request_body", "status", "body"})

    def test_reader_accepts_every_captured_engine_snapshot(self) -> None:
        for name, (role, engine) in ENGINE_FIXTURES.items():
            body = load(name)["body"]
            facts = pc.read_engine_snapshot(body)
            self.assertEqual((facts["pair_role"], facts["active_engine"], facts["topology"], facts["state"]),
                             (role, engine, "paired", "ready"), name)
            # What the product serializes: pair_ready as the primary publication proof only.
            self.assertIs(facts["pair_ready"], role == "primary", name)
            self.assertEqual(facts["secondary_ready"], pc.ABSENT, name)
        unconfigured = pc.read_engine_snapshot(load("engine-unconfigured.json")["body"])
        self.assertEqual((unconfigured["active_engine"], unconfigured["state"], unconfigured["pair_role"]),
                         (None, "unconfigured", pc.ABSENT))

    def test_d1_on_the_captured_snapshots_fails_only_for_the_unserialized_field(self) -> None:
        for name, (role, engine) in ENGINE_FIXTURES.items():
            body = load(name)["body"]
            verdict = pc.pair_readiness(body, role=role, engine=engine)
            self.assertFalse(verdict["passed"], name)
            self.assertEqual(verdict["absent_fields"], ["secondary_ready"], name)
            self.assertEqual(len(verdict["reasons"]), 1, verdict["reasons"])
            self.assertIn("does not serialize it", verdict["reasons"][0])
            # The same body with the D1 field as the product contract defines it passes.
            complete = body | {"secondary_ready": role == "secondary"}
            self.assertTrue(pc.pair_readiness(complete, role=role, engine=engine)["passed"], name)
            self.assertTrue(pc.engine_settled(body))

    def test_negative_secondary_with_pair_ready_true(self) -> None:
        body = load("engine-bind-secondary-paired.json")["body"] | {"pair_ready": True}
        with self.assertRaisesRegex(pc.ContractError, "non-primary"):
            pc.read_engine_snapshot(body)
        verdict = pc.pair_readiness(body | {"secondary_ready": True}, role="secondary", engine="bind")
        self.assertFalse(verdict["passed"])
        self.assertTrue(any("pair_ready is True, D1 requires pair_ready === false" in r for r in verdict["reasons"]))
        self.assertTrue(any("web client would refuse" in r for r in verdict["reasons"]))

    def test_other_d1_combinations_fail(self) -> None:
        primary = load("engine-bind-primary-paired.json")["body"]
        secondary = load("engine-pdns-secondary-paired.json")["body"]
        cases = [
            (primary | {"secondary_ready": True}, "primary", "bind"),              # both proofs
            (primary | {"pair_ready": False, "secondary_ready": False}, "primary", "bind"),
            (secondary | {"secondary_ready": False}, "secondary", "pdns"),
            (secondary | {"secondary_ready": True}, "secondary", "bind"),          # wrong engine
            (secondary | {"secondary_ready": True}, "primary", "pdns"),            # wrong role
            (secondary | {"secondary_ready": True, "state": "degraded"}, "secondary", "pdns"),
            (secondary | {"secondary_ready": 1}, "secondary", "pdns"),             # never truthiness
            ({"poll_error": "HTTP 500"}, "secondary", "pdns"),
        ]
        for body, role, engine in cases:
            self.assertFalse(pc.pair_readiness(body, role=role, engine=engine)["passed"], (role, engine, body))
        with self.assertRaisesRegex(pc.ContractError, "both true"):
            pc.read_engine_snapshot(primary | {"secondary_ready": True})

    def test_in_progress_switch_is_not_settled(self) -> None:
        body = load("engine-bind-primary-paired.json")["body"]
        switching = body | {"state": "switching", "operation_id": body["operation"]["id"],
                            "operation": body["operation"] | {"status": "running"}}
        self.assertFalse(pc.engine_settled(switching))
        self.assertFalse(pc.setup_settled({"status": "running"}))
        self.assertTrue(pc.setup_settled({"status": "waiting"}))


class FakeMirrorsCaptureTest(unittest.TestCase):
    def test_fake_engine_bodies_are_the_captured_ones(self) -> None:
        world = fakes.World()
        for (role, engine), name in fakes.CAPTURED_ENGINE.items():
            panel = fakes.FakePanel("debian13", role, engine, world)
            self.assertEqual(panel.engine_body(), load("engine-unconfigured.json")["body"])
            panel.started = {"request_id": "r"}
            self.assertEqual(panel.engine_body(), load(name)["body"], name)
            exposed = fakes.FakePanel("debian13", role, engine, world, api_secondary_ready=True)
            exposed.started = {"request_id": "r"}
            self.assertIs(exposed.engine_body()["secondary_ready"], role == "secondary")
        derived = fakes.captured_engine("primary", "pdns")
        self.assertTrue(pc.pair_readiness(derived | {"secondary_ready": False}, role="primary", engine="pdns")["passed"])

    def test_captured_setup_operation_guidance_is_actionable(self) -> None:
        execution = load("setup-operation-primary-waiting-access-dns.json")["body"]
        item = gd.setup_execution_guidance(T, execution)
        self.assertEqual((item["state"], item["code"]), ("unmet-prerequisite", "server_setup_access_dns_required"))
        self.assertTrue(item["actionable"])


if __name__ == "__main__":
    unittest.main()
