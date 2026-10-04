#!/usr/bin/env python3
"""Offline tests: step sequencing, verdicts, gate refusal, enrollment and evidence."""

from __future__ import annotations

import json
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import evidence as ev  # noqa: E402
import fakes  # noqa: E402
import guidance as gd  # noqa: E402
import install_steps as inst  # noqa: E402
import pair_acceptance as pa  # noqa: E402
import topology as topo  # noqa: E402
from redaction import Redactor  # noqa: E402

TRANSLATOR = gd.Translator(gd.load_catalog())
DIST = {"sha256": "a" * 64, "root": "celikpanel-v0.0.0-test", "commit": "b" * 40, "tree": "c" * 40}


class Clock:
    def __init__(self) -> None:
        self.now = 0.0

    def __call__(self) -> float:
        return self.now

    def sleep(self, seconds: float) -> None:
        self.now += seconds


def primary_waits_script() -> list[dict]:
    ctx = {"dns_mode": "local", "dns_role": "secondary", "local_nameserver": "ns2.ns-accept.example",
           "local_ip": "192.0.2.11", "peer_nameserver": "ns1.ns-accept.example", "peer_ip": "192.0.2.10",
           "panel_domain": "panel-arch.ns-accept.example", "dns_hosting_management": ""}
    return [
        {"status": "running", "phase": "01-dns", "context": ctx, "steps": [{"id": "01-dns", "kind": "dns", "status": "running"}]},
        {"status": "waiting", "phase": "primary_dns", "context": ctx,
         "error": {"code": "server_setup_primary_dns_required", "message": "waiting for the primary"},
         "steps": [{"id": "01-dns", "kind": "dns", "status": "running"}]},
    ]


class Harness:
    def __init__(self, topology_name: str, *, primary_node: str | None = None, licensed: bool = True,
                 license_mode: str = "none", delete_script: list | None = None, edit_method: str = "ui-replace",
                 plan_can_start: bool = True, secondary_script: list | None = None,
                 primary_script: list | None = None, acceptance_build: bool | None = None,
                 acceptance_label: bool = True, pdns_primary_gate_open: bool = False,
                 refused_plan_side_effect: bool = False, api_secondary_ready: bool = True,
                 engine_scripts: dict | None = None, plan_responses: dict | None = None,
                 start_responses: dict | None = None, installer_stdout: dict | None = None,
                 journals: dict | None = None, setup_timeout: float = 600,
                 stable_stop_seconds: float | None = 300.0, unknown_state_limit: float = 300.0) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name).resolve()
        self.topology = topo.resolve(topology_name, primary_node=primary_node)
        self.world = fakes.World()
        P, S = self.topology.primary, self.topology.secondary
        cell = self.topology.cell_id("t1") if license_mode == "acceptance-fixture" else None
        # api_secondary_ready=True: the pair2 build's API (secondary_ready on the secondary only).
        # The pair1 build serializes it nowhere; tests of that exact API pass api_secondary_ready=False.
        common = {"acceptance_cell": cell, "acceptance_label": acceptance_label,
                  "pdns_primary_gate_open": pdns_primary_gate_open,
                  "refused_plan_side_effect": refused_plan_side_effect,
                  "api_secondary_ready": api_secondary_ready}
        engine_scripts, plan_responses, start_responses = engine_scripts or {}, plan_responses or {}, start_responses or {}
        self.panels = {
            P.node: fakes.FakePanel(P.node, "primary", P.engine, self.world, licensed=licensed,
                                    plan_can_start=plan_can_start, delete_script=delete_script,
                                    execution_script=primary_script, peer_node=S.node,
                                    engine_script=engine_scripts.get("primary"),
                                    plan_response=plan_responses.get("primary"),
                                    start_response=start_responses.get("primary"), **common),
            S.node: fakes.FakePanel(S.node, "secondary", S.engine, self.world, licensed=licensed,
                                    execution_script=secondary_script, peer_node=P.node,
                                    engine_script=engine_scripts.get("secondary"),
                                    plan_response=plan_responses.get("secondary"),
                                    start_response=start_responses.get("secondary"), **common),
        }
        if acceptance_build is None:
            acceptance_build = license_mode == "acceptance-fixture"
        dist = dict(DIST, acceptance_license_build=acceptance_build)
        self.guests = fakes.FakeGuests(self.world, self.topology, installer_stdout=installer_stdout, journals=journals)
        self.redactor = Redactor()
        self.writer = ev.EvidenceWriter(self.root, "run-test", self.redactor)
        self.clock = Clock()
        config = pa.Config(run_id="run-test", run_label="t1", license_mode=license_mode,
                           license_keys={"primary": "CPK-" + "1" * 64, "secondary": "CPK-" + "2" * 64}
                           if license_mode == "owner-key" else {},
                           edit_method=edit_method, dist_archive=Path("/nonexistent/celikpanel.tar.gz"),
                           dist_root=DIST["root"], dist_sha256=DIST["sha256"], dist_commit=DIST["commit"],
                           dist_tree=DIST["tree"], setup_timeout=setup_timeout, dns_timeout=20, deletion_timeout=60,
                           poll_interval=3, dns_interval=2, stable_stop_seconds=stable_stop_seconds,
                           unknown_state_limit=unknown_state_limit)
        if license_mode == "owner-key":
            for key in config.license_keys.values():
                self.redactor.register(key)
        self.driver = pa.Driver(
            config, self.topology, self.guests, self.writer, self.redactor, TRANSLATOR,
            fakes.panel_factory(self.panels, self.redactor, self.clock, self.clock.sleep),
            clock=self.clock, sleep=self.clock.sleep, archive_reader=lambda path: b"archive-bytes",
            dist_identity_reader=lambda path: dict(dist),
        )

    def run(self) -> dict:
        self.result = self.driver.execute()
        return self.result

    def verdicts(self) -> dict[str, str]:
        return {step["id"]: step["verdict"] for step in self.result["steps"]}

    def step(self, step_id: str) -> dict:
        return next(step for step in self.result["steps"] if step["id"] == step_id)

    def evidence_blob(self) -> bytes:
        return b"".join(path.read_bytes() for path in self.writer.directory.rglob("*") if path.is_file())

    def close(self) -> None:
        self.temporary.cleanup()


class SequenceTest(unittest.TestCase):
    def harness(self, *args, **kwargs) -> Harness:  # noqa: ANN002, ANN003
        value = Harness(*args, **kwargs)
        self.addCleanup(value.close)
        return value

    def test_bind_bind_control_passes_end_to_end(self) -> None:
        h = self.harness("bind/bind", primary_node="arch")
        result = h.run()
        self.assertEqual(result["overall"], "passed", json.dumps(
            [(s["id"], s["verdict"], s["reason"]) for s in result["steps"]], indent=1))
        order = [step["id"] for step in result["steps"]]
        self.assertEqual(order, ["preflight", "install-primary", "install-secondary", "login-primary", "login-secondary",
                                 "license-primary", "license-secondary", "setup-review-primary", "setup-start-primary",
                                 "setup-review-secondary", "setup-start-secondary", "pair-ready", "zone-add",
                                 "record-add", "record-edit", "zone-delete", "zone-readd", "independence-reboot",
                                 "management-return", "collect"])
        for panel in h.panels.values():
            self.assertEqual(panel.start_count, 1, "setup start must be posted exactly once")
        # Every guest command that changes state was flagged mutating (identity re-check).
        installer = [c for c in h.guests.commands if "exec /bin/bash ./install.sh" in c[1]]
        self.assertEqual(len(installer), 2)
        self.assertTrue(all(c[2] for c in installer))
        self.assertEqual(ev.verify_sums(h.writer.directory), [])
        blob = h.evidence_blob()
        self.assertNotIn(fakes.COOKIE.encode(), blob)
        for password in h.driver.passwords.values():
            self.assertNotIn(password.encode(), blob)
        self.assertIn(b"setup-not-complete-offline-primary", (h.writer.directory / "result.json").read_bytes())
        self.assertFalse(result["native_evidence"])
        edit = h.step("record-edit")
        self.assertTrue(edit["checks"]["dns_record-edit"]["passed"])
        self.assertEqual(h.step("zone-delete")["checks"]["delete_first_outcome"]["state"], "succeeded")

    def test_api_put_edit(self) -> None:
        h = self.harness("bind/bind", primary_node="debian13", edit_method="api-put")
        self.assertEqual(h.run()["overall"], "passed")
        primary = h.panels["debian13"]
        self.assertIn(("PUT", f"/api/v1/domains/10/dns/records"), primary.unsafe_calls)

    def test_license_lock_blocks_offline(self) -> None:
        h = self.harness("bind/bind", primary_node="arch", licensed=False)
        result = h.run()
        self.assertEqual(result["overall"], "blocked-product")
        verdicts = h.verdicts()
        self.assertEqual(verdicts["license-primary"], "blocked-product")
        self.assertEqual(verdicts["zone-add"], "not-run")
        self.assertEqual(verdicts["collect"], "passed")
        guidance = h.step("license-primary")["guidance"][0]
        self.assertEqual(guidance["code"], "license_required")
        self.assertTrue(guidance["actionable"])
        self.assertEqual(result["product_blockers"][0]["id"], "L1-license-activation-offline")
        self.assertTrue(all(panel.start_count == 0 for panel in h.panels.values()))

    def test_owner_key_license_mode_activates_through_ui_endpoint(self) -> None:
        h = self.harness("bind/bind", primary_node="arch", licensed=False, license_mode="owner-key")
        result = h.run()
        self.assertEqual(result["overall"], "passed")
        self.assertIn(("POST", "/api/v1/panel/license"), h.panels["arch"].unsafe_calls)
        self.assertNotIn(b"CPK-", h.evidence_blob())

    def test_pdns_primary_refused_by_product_gate(self) -> None:
        h = self.harness("pdns-primary/bind-secondary", secondary_script=primary_waits_script())
        result = h.run()
        self.assertEqual(result["overall"], "refused-by-product-gate")
        verdicts = h.verdicts()
        self.assertEqual(verdicts["setup-review-primary"], "refused-by-product-gate")
        self.assertEqual(verdicts["setup-secondary-before-primary"], "passed")
        for step_id in ("zone-add", "zone-delete", "independence-reboot"):
            self.assertEqual(verdicts[step_id], "skipped")
        review = h.step("setup-review-primary")
        self.assertIn("PowerDNS primary in a DNS pair is not ready", review["reason"])
        self.assertIn("server plan", review["reason"])
        guidance = review["guidance"][0]
        self.assertEqual(guidance["source"], "setup-plan-blockers")
        self.assertEqual(guidance["code"], "pdns_primary_switch_paused")
        self.assertEqual(guidance["title_key"], "setup.planBlocked")
        self.assertEqual(guidance["message_keys"], ["setup.pdnsPrimaryPaused"])
        self.assertEqual(guidance["shown"]["tr"][0][:10], "DNS çiftin")
        self.assertTrue(guidance["actionable"])
        self.assertEqual(review["checks"]["wizard_code_key_pdns_primary"], "setup.pdnsPrimaryPaused")
        self.assertEqual(review["checks"]["plan"]["blockers"], ["pdns_primary_switch_paused"])
        self.assertIs(review["checks"]["refused_plan_left_nothing"], True)
        self.assertEqual(h.panels["debian13"].start_count, 0, "the driver must not start a gated plan")
        self.assertFalse(any(f["id"] == "pdns-primary-gate-client-side-only" for f in result["findings"]))
        waiting = [g for g in h.step("setup-secondary-before-primary")["guidance"] if g["state"] == "unmet-prerequisite"]
        self.assertTrue(waiting and waiting[-1]["actionable"])
        self.assertIn("setup.guide.primaryDNSWaiting", waiting[-1]["message_keys"])

    def test_refused_pdns_primary_plan_must_leave_nothing_behind(self) -> None:
        h = self.harness("pdns-primary/bind-secondary", secondary_script=primary_waits_script(),
                         refused_plan_side_effect=True)
        result = h.run()
        self.assertEqual(result["overall"], "failed")
        review = h.step("setup-review-primary")
        self.assertEqual(review["verdict"], "failed")
        self.assertIn("left changes behind", review["reason"])
        self.assertIs(review["checks"]["refused_plan_left_nothing"], False)
        self.assertEqual(h.panels["debian13"].start_count, 0)

    def test_pdns_primary_proceeds_when_the_server_gate_is_open(self) -> None:
        h = self.harness("pdns-primary/bind-secondary", pdns_primary_gate_open=True)
        result = h.run()
        verdicts = h.verdicts()
        self.assertEqual(verdicts["setup-review-primary"], "passed", h.step("setup-review-primary")["reason"])
        self.assertEqual(verdicts["setup-start-primary"], "passed")
        self.assertNotIn("setup-secondary-before-primary", verdicts)
        self.assertEqual(h.panels["debian13"].start_count, 1)
        self.assertNotEqual(result["overall"], "refused-by-product-gate")

    def test_acceptance_fixture_mode_activates_once_and_scopes_the_evidence(self) -> None:
        h = self.harness("bind/bind", primary_node="arch", licensed=False, license_mode="acceptance-fixture")
        result = h.run()
        self.assertEqual(result["overall"], "passed", json.dumps(
            [(s["id"], s["verdict"], s["reason"]) for s in result["steps"]], indent=1))
        self.assertEqual(result["license_mode"], "acceptance-fixture")
        self.assertIn("License behaviour is NOT evidenced", result["native_evidence_scope"])
        self.assertFalse(result["native_evidence"])
        cell = h.topology.cell_id("t1")
        for role, node in (("primary", "arch"), ("secondary", "debian13")):
            self.assertEqual(h.panels[node].activations, [fakes.FIXTURE_KEY], "activate exactly once, fixture key only")
            status = result["license_status"][role]
            self.assertEqual(status["license_label"], "ACCEPTANCE FIXTURE \u2014 NOT FOR PRODUCTION")
            self.assertEqual((status["state"], status["acceptance_cell"], status["acceptance_node"]), ("active", cell, node))
            step = h.step(f"license-{role}")
            self.assertEqual(step["checks"]["activation_posts"], 1)
            self.assertTrue((h.writer.directory / step["evidence_directory"] / f"license-status-{role}.json").is_file())
            self.assertTrue((h.writer.directory / step["evidence_directory"] / f"license-status-before-{role}.json").is_file())
        written = json.loads((h.writer.directory / "result.json").read_text(encoding="utf-8"))
        self.assertEqual(written["license_mode"], "acceptance-fixture")
        self.assertIn("NOT evidenced", written["native_evidence_scope"])
        self.assertNotIn(b"CPK-", h.evidence_blob())
        self.assertEqual(ev.verify_sums(h.writer.directory), [])

    def test_acceptance_fixture_mode_needs_the_acceptance_archive_and_label(self) -> None:
        customer_archive = self.harness("bind/bind", primary_node="arch", licensed=False,
                                        license_mode="acceptance-fixture", acceptance_build=False)
        result = customer_archive.run()
        self.assertEqual(customer_archive.verdicts()["preflight"], "failed")
        self.assertIn("build-dist.sh --acceptance-license", customer_archive.step("preflight")["reason"])
        self.assertTrue(all(not panel.activations for panel in customer_archive.panels.values()))
        self.assertEqual(result["license_mode"], "acceptance-fixture")

        tagged_in_customer_mode = self.harness("bind/bind", primary_node="arch", acceptance_build=True)
        tagged_in_customer_mode.run()
        self.assertEqual(tagged_in_customer_mode.verdicts()["preflight"], "failed")
        self.assertIn("acceptance_license test tag", tagged_in_customer_mode.step("preflight")["reason"])

        unlabelled = self.harness("bind/bind", primary_node="arch", licensed=False,
                                  license_mode="acceptance-fixture", acceptance_label=False)
        unlabelled.run()
        self.assertEqual(unlabelled.verdicts()["license-primary"], "failed")
        self.assertIn("does not label its license as the acceptance fixture",
                      unlabelled.step("license-primary")["reason"])
        self.assertEqual(unlabelled.verdicts()["setup-review-primary"], "not-run")

    def test_customer_modes_record_their_scope(self) -> None:
        h = self.harness("bind/bind", primary_node="arch", licensed=False)
        result = h.run()
        self.assertEqual(result["license_mode"], "none")
        self.assertIn("Customer panel build", result["native_evidence_scope"])
        self.assertNotIn("license_status", result)

    def test_pdns_secondary_enrollment_then_one_retry(self) -> None:
        # H2 (pair1): dns-peer-enroll --engine pdns replaces the old blocked-product E1.
        pending = (202, {"status": "deletion_pending", "domain": "pair-accept.example", "stage": "dns_cleanup",
                         "message": "pending", "reason": "dns_peer_enrollment_required"})
        h = self.harness("bind-primary/pdns-secondary", delete_script=[pending])
        result = h.run()
        self.assertEqual(result["overall"], "passed", h.step("zone-delete")["reason"])
        self.assertEqual(result["product_blockers"], [])
        commands = [c[1] for c in h.guests.commands if "dns-peer-enroll" in c[1]]
        self.assertEqual([c.split()[3] for c in commands],
                         ["primary-prepare", "secondary-install", "secondary-host-key", "primary-activate",
                          "primary-status", "secondary-status"])
        self.assertTrue(all("--engine pdns" in c for c in commands))
        install = next(c for c in commands if "secondary-install" in c)
        self.assertIn("--catalog-account celikpanel-peer-catalog-v1", install)
        self.assertIn("/dns-owner-tools/pdns-peer-inspect", install)
        self.assertNotIn("bind-peer-inspect", install)
        step = h.step("zone-delete")
        self.assertEqual(step["checks"]["owner_enrollment"], "completed with dns-peer-enroll (PowerDNS secondary)")
        self.assertEqual(step["checks"]["pdns_catalog_consumer_account"], ["celikpanel-peer-catalog-v1"])
        directory = h.writer.directory / step["evidence_directory"]
        self.assertTrue((directory / "native-pending-secondary.json").is_file())
        deletes = [c for c in h.panels["arch"].unsafe_calls if c[0] == "DELETE" and c[1] == "/api/v1/domains/10"]
        self.assertEqual(len(deletes), 2)
        self.assertTrue(any(o["step"] == "zone-delete" and "dns-peer-enroll --engine pdns" in o["action"]
                            for o in result["owner_steps"]))
        # Item 7: the shipped text names the engine selector since a5c4f80f, so no
        # D-024 observation is recorded for it.
        self.assertFalse(any(f["id"] == "d024-observation-pending-deletion-pdns-engine-selector"
                             for f in result["findings"]))
        self.assertEqual(step["verdict"], "passed")

    def test_bind_secondary_enrollment_has_no_engine_selector_observation(self) -> None:
        pending = (202, {"status": "deletion_pending", "domain": "pair-accept.example", "stage": "dns_cleanup",
                         "message": "pending", "reason": "dns_peer_enrollment_required"})
        h = self.harness("bind/bind", primary_node="arch", delete_script=[pending])
        result = h.run()
        self.assertEqual(result["overall"], "passed")
        self.assertFalse(any(f["id"].startswith("d024-observation") for f in result["findings"]))
        commands = [c[1] for c in h.guests.commands if "dns-peer-enroll" in c[1]]
        self.assertFalse(any("--engine" in c or "--catalog-account" in c for c in commands))

    def test_bind_secondary_enrollment_then_one_retry(self) -> None:
        pending = (202, {"status": "deletion_pending", "domain": "pair-accept.example", "stage": "dns_cleanup",
                         "message": "pending", "reason": "dns_peer_enrollment_required"})
        h = self.harness("bind/bind", primary_node="debian13", delete_script=[pending])
        result = h.run()
        self.assertEqual(result["overall"], "passed", h.step("zone-delete")["reason"])
        commands = [c[1] for c in h.guests.commands if "dns-peer-enroll" in c[1]]
        self.assertEqual([c.split()[3] for c in commands],
                         ["primary-prepare", "secondary-install", "secondary-host-key", "primary-activate",
                          "primary-status", "secondary-status"])
        self.assertTrue(all(c[2] for c in h.guests.commands if "dns-peer-enroll" in c[1]))
        deletes = [c for c in h.panels["debian13"].unsafe_calls if c[0] == "DELETE" and c[1] == "/api/v1/domains/10"]
        self.assertEqual(len(deletes), 2)
        review = h.step("zone-delete")["checks"]["host_key_review"]
        self.assertEqual(review["cli"], review["pub_file"])

    def test_setup_failure_without_guidance_fails_d024(self) -> None:
        script = [{"status": "running", "phase": "01-dns", "steps": [{"id": "01-dns", "kind": "dns", "status": "running"}]},
                  {"status": "failed", "phase": "mystery", "steps": [{"id": "01-dns", "kind": "dns", "status": "failed"}]}]
        h = self.harness("bind/bind", primary_node="arch", primary_script=script)
        result = h.run()
        self.assertEqual(result["overall"], "failed")
        step = h.step("setup-start-primary")
        self.assertEqual(step["verdict"], "failed")
        self.assertIn("D-024", step["reason"])
        self.assertEqual(h.verdicts()["setup-review-secondary"], "not-run")

    def test_setup_failure_with_guidance_is_verified_failure(self) -> None:
        ctx = {"dns_mode": "local", "dns_role": "primary", "local_nameserver": "ns1.x", "local_ip": "192.0.2.11",
               "peer_nameserver": "ns2.x", "peer_ip": "192.0.2.10", "panel_domain": "p.x", "dns_hosting_management": ""}
        script = [{"status": "failed", "phase": "01-dns", "context": ctx,
                   "error": {"code": "server_setup_dns_failed", "message": "DNS setup requires attention."},
                   "steps": [{"id": "01-dns", "kind": "dns", "status": "failed"}]}]
        h = self.harness("bind/bind", primary_node="arch", primary_script=script)
        h.run()
        step = h.step("setup-start-primary")
        self.assertEqual(step["verdict"], "failed")
        self.assertNotIn("D-024", step["reason"])
        self.assertTrue(step["guidance"][0]["actionable"])
        self.assertEqual(step["guidance"][0]["state"], "verified-failure")

    def test_management_return_detects_ledger_mutation(self) -> None:
        h = self.harness("bind/bind", primary_node="arch")
        h.world.mutate_ledger_on_return = True
        result = h.run()
        self.assertEqual(h.verdicts()["management-return"], "failed")
        self.assertIn("ledger changed", h.step("management-return")["reason"])
        self.assertEqual(result["overall"], "failed")

    def test_management_disabled_reboot_checks(self) -> None:
        h = self.harness("bind/bind", primary_node="arch")
        h.run()
        step = h.step("independence-reboot")
        self.assertEqual(step["verdict"], "passed")
        self.assertEqual(step["checks"]["boot_ids"]["arch"], ["1", "2"])
        disabled = [c for c in h.guests.commands if c[1] == "disable-management"]
        self.assertEqual(len(disabled), 2)


    # -- pair1 corrections ----------------------------------------------------

    def test_pair1_build_is_blocked_product_at_pair_ready_without_waiting(self) -> None:
        # The exact pair1 bodies: no secondary_ready anywhere. The primary satisfies the
        # corrected D1 (the key must be absent there); the secondary's build does not
        # report secondary readiness: blocked-product at once, not a failure after a wait.
        h = self.harness("bind/bind", primary_node="debian13", api_secondary_ready=False, setup_timeout=2700)
        result = h.run()
        self.assertEqual(result["overall"], "blocked-product")
        self.assertIsNone(result["overall_cause"])
        step = h.step("pair-ready")
        self.assertEqual(step["verdict"], "blocked-product")
        self.assertIn("this build does not report secondary readiness", step["reason"])
        self.assertLess(h.clock.now, 300, "an unreported field is final at once; no stable window is waited")
        primary = step["checks"]["pair_readiness_ready_primary"]
        self.assertTrue(primary["passed"], primary["reasons"])
        secondary = step["checks"]["pair_readiness_ready_secondary"]
        self.assertFalse(secondary["passed"])
        self.assertEqual(secondary["blocked"], "secondary-readiness-not-reported")
        self.assertEqual(secondary["poll"]["stop"], "done")
        for role in ("primary", "secondary"):
            recorded = json.loads((h.writer.directory / step["evidence_directory"] / f"engine-ready-{role}.json")
                                  .read_text(encoding="utf-8"))
            self.assertEqual(recorded, fakes.captured(fakes.CAPTURED_ENGINE[(role, "bind")])["body"] | {"zone_count": 0})
        self.assertEqual(h.verdicts()["zone-add"], "not-run")

    def test_pair2_build_passes_pair_ready_on_the_captured_shapes(self) -> None:
        for topology_name, node in (("bind/bind", "debian13"), ("bind-primary/pdns-secondary", None)):
            h = self.harness(topology_name, primary_node=node)
            result = h.run()
            self.assertEqual(result["overall"], "passed", h.step("pair-ready")["reason"])
            step = h.step("pair-ready")
            self.assertEqual(step["checks"]["pair_readiness_ready_primary"]["observed"]["secondary_ready"], "<absent>")
            self.assertIs(step["checks"]["pair_readiness_ready_secondary"]["observed"]["secondary_ready"], True)

    def test_primary_carrying_secondary_ready_fails_pair_ready(self) -> None:
        wrong = fakes.captured_engine("primary", "bind") | {"secondary_ready": False}
        h = self.harness("bind/bind", primary_node="debian13", engine_scripts={"primary": [wrong]},
                         stable_stop_seconds=30)
        result = h.run()
        self.assertEqual(result["overall"], "failed")
        step = h.step("pair-ready")
        self.assertEqual(step["verdict"], "failed")
        self.assertIn("contract violation", step["reason"])
        self.assertTrue(step["checks"]["pair_readiness_ready_secondary"]["passed"])

    def test_secondary_reporting_pair_ready_true_fails_d1(self) -> None:
        # The old fake's shape, which the real product never returns (dnsEngineContract.ts:377).
        wrong = fakes.captured_engine("secondary", "bind") | {"pair_ready": True}
        h = self.harness("bind/bind", primary_node="debian13", engine_scripts={"secondary": [wrong]})
        result = h.run()
        self.assertEqual(result["overall"], "failed")
        step = h.step("pair-ready")
        verdict = step["checks"]["pair_readiness_ready_secondary"]
        self.assertFalse(verdict["passed"])
        self.assertTrue(any("web client would refuse" in reason for reason in verdict["reasons"]))
        self.assertTrue(any("pair_ready is True, D1 requires pair_ready === false" in reason
                            for reason in verdict["reasons"]))
        self.assertTrue(step["checks"]["pair_readiness_ready_primary"]["passed"])

    def test_changing_state_is_waited_for_beyond_the_stable_window(self) -> None:
        base = fakes.captured_engine("secondary", "pdns")
        moving = [base | {"secondary_ready": False, "revision": 3 + index} for index in range(90)]
        h = self.harness("bind-primary/pdns-secondary", setup_timeout=2700,
                         engine_scripts={"secondary": moving + [base | {"secondary_ready": True, "revision": 99}]})
        result = h.run()
        verdicts = h.verdicts()
        self.assertEqual(verdicts["pair-ready"], "passed", h.step("pair-ready")["reason"])
        poll = h.step("pair-ready")["checks"]["pair_readiness_ready_secondary"]["poll"]
        self.assertEqual(poll["stop"], "done")
        self.assertGreater(poll["elapsed_seconds"], 300)
        self.assertEqual(result["overall"], "passed")

    def test_setup_plan_500_internal_is_a_d024_product_finding(self) -> None:
        # pair1 t1 r1: POST /api/v1/setup/plan -> 500 {"code":"INTERNAL","error":"internal server error"}.
        exchange = fakes.captured("setup-plan-500-internal.json")
        self.assertEqual((exchange["method"], exchange["path"], exchange["status"]), ("POST", "/api/v1/setup/plan", 500))
        h = self.harness("bind/bind", primary_node="debian13",
                         plan_responses={"secondary": (exchange["status"], exchange["body"])})
        result = h.run()
        self.assertEqual(result["overall"], "failed")
        step = h.step("setup-review-secondary")
        self.assertEqual(step["verdict"], "failed")
        self.assertIn("HTTP 500", step["reason"])
        self.assertIn("D-024", step["reason"])
        self.assertIn(TRANSLATOR.text("setup.planFailed"), step["reason"])
        item = step["guidance"][-1]
        self.assertEqual(item["message_keys"], ["setup.planFailed"])
        self.assertEqual(item["api_error_keys"], ["err.INTERNAL"])
        self.assertEqual(item["shown"]["tr"], [TRANSLATOR.text("setup.planFailed", language="tr")])
        self.assertFalse(item["actionable"])
        finding = next(f for f in result["findings"] if f["id"] == "d024-no-actionable-guidance-setup-review-secondary")
        self.assertEqual(finding["kind"], "product")
        self.assertEqual(finding["payload"]["body"], {"code": "INTERNAL", "error": "internal server error"})
        self.assertEqual(finding["http_status"], 500)
        self.assertEqual(h.verdicts()["setup-start-secondary"], "not-run")
        self.assertEqual(h.panels["arch"].start_count, 0)

    def test_setup_start_5xx_records_the_unknown_result_the_owner_sees(self) -> None:
        h = self.harness("bind/bind", primary_node="arch",
                         start_responses={"primary": (500, {"code": "INTERNAL", "error": "internal server error"})})
        result = h.run()
        step = h.step("setup-start-primary")
        self.assertEqual(step["verdict"], "failed")
        self.assertIn("D-024", step["reason"])
        item = step["guidance"][-1]
        self.assertEqual((item["source"], item["state"]), ("setup-start", "unknown"))
        self.assertEqual(item["message_keys"], ["setup.uncertain"])
        self.assertFalse(item["actionable"])
        self.assertEqual(step["checks"]["start_refused"]["http_status"], 500)
        self.assertEqual(h.panels["arch"].start_count, 1, "a refused start is never re-posted")
        self.assertTrue(any(f["id"] == "d024-no-actionable-guidance-setup-start-primary" for f in result["findings"]))

    def test_installer_restart_request_is_a_recorded_owner_step(self) -> None:
        # pair1 H3: the exact closing notice install.sh printed on Arch.
        notice = ("  Logs: journalctl -u celikpanel-panel -f\n\n"
                  "\x1b[1;31mRESTART THIS SERVER NOW / BU SUNUCUYU SIMDI YENIDEN BASLATIN\x1b[0m\n"
                  "    This server is running kernel 7.1.8-arch1-3, whose modules are no longer on disk:\n"
                  "    is restarted it cannot load nftables or WireGuard, so turning the firewall on\n"
                  "        reboot\n\n"
                  "    Bu sunucu, modulleri artik diskte olmayan 7.1.8-arch1-3 cekirdegiyle calisiyor:\n"
                  "        reboot\n\n")
        h = self.harness("bind/bind", primary_node="debian13", installer_stdout={"arch": notice})
        result = h.run()
        self.assertEqual(result["overall"], "passed", h.step("install-secondary")["reason"])
        step = h.step("install-secondary")
        self.assertEqual(step["checks"]["installer_restart_notice"], "required")
        owner = step["checks"]["owner_restart_after_install"]
        self.assertEqual(owner["actor"], "server owner")
        self.assertEqual(owner["boot_ids"], ["1", "2"])
        self.assertIn("nftables", owner["installer_text"])
        self.assertNotIn("\x1b", owner["installer_text"])
        self.assertEqual(result["owner_steps"][0]["step"], "install-secondary")
        reboots = [c for c in h.guests.commands if c[1] == "reboot"]
        self.assertEqual(reboots[0][0], "arch", "the restart happens at install, before setup")
        directory = h.writer.directory / step["evidence_directory"]
        self.assertIn("RESTART THIS SERVER NOW", (directory / "installer-restart-notice.txt").read_text(encoding="utf-8"))
        self.assertTrue((directory / "owner-restart-after-install.json").is_file())
        self.assertEqual(h.step("install-primary")["checks"]["installer_restart_notice"], "none")

    def test_installer_restart_recommendation_is_recorded_not_acted_on(self) -> None:
        notice = ("\x1b[1;33mA RESTART IS RECOMMENDED / YENIDEN BASLATMA ONERILIR\x1b[0m\n"
                  "    This server is running kernel 6.12 while its package manager has installed\n")
        h = self.harness("bind/bind", primary_node="debian13", installer_stdout={"arch": notice})
        result = h.run()
        self.assertEqual(result["overall"], "passed")
        self.assertEqual(h.step("install-secondary")["checks"]["installer_restart_notice"], "recommended")
        self.assertEqual(result["owner_steps"], [])
        self.assertEqual(h.step("independence-reboot")["checks"]["boot_ids"]["arch"], ["1", "2"])

    def test_license_service_journal_check(self) -> None:
        clean = self.harness("bind/bind", primary_node="arch", licensed=False, license_mode="acceptance-fixture",
                             journals={("arch", "celikpanel-panel.service"):
                                       "panel: acceptance_fixture: this panel was built with the acceptance_license "
                                       "test tag; it never contacts the license service\n"})
        result = clean.run()
        self.assertEqual(result["overall"], "passed")
        check = result["license_service_journal_check"]
        self.assertEqual((check["arch"]["state"], check["debian13"]["state"]), ("no-line-found", "no-line-found"))
        self.assertEqual(check["arch"]["acceptance_banner_count"], 1)
        self.assertIn("not exposed", check["arch"]["limit"])
        seen = self.harness("bind/bind", primary_node="arch", licensed=False, license_mode="acceptance-fixture",
                            journals={("debian13", "celikpanel-panel.service"):
                                      'license refresh: Post "https://celikpanel.net/account/": the acceptance '
                                      "test build never contacts the license service\n"})
        result = seen.run()
        self.assertEqual(seen.verdicts()["collect"], "failed")
        self.assertEqual(result["overall"], "failed")
        check = result["license_service_journal_check"]["debian13"]
        self.assertEqual((check["host_line_count"], check["refusal_line_count"]), (1, 1))

    # -- pair2 corrections ----------------------------------------------------

    def t3_script(self, reconciling_reads: int | None = None) -> list[dict]:
        running = fakes.captured("setup-operation-primary-running-dns.json", "pair2")["body"]
        reconciling = fakes.captured("setup-operation-primary-reconciling.json", "pair2")["body"]
        if reconciling_reads is None:
            return [running, reconciling]
        done = json.loads(json.dumps(reconciling))
        done.pop("error")
        done["steps"][0]["status"] = "succeeded"
        done["phase"] = "02-service"
        return [running] + [reconciling] * reconciling_reads + [done]

    def test_open_ended_unknown_setup_state_is_a_product_failure(self) -> None:
        # pair2 t3: server_setup_reconciling for 45 minutes; now bounded (default 300 s).
        h = self.harness("pdns-primary/bind-secondary", pdns_primary_gate_open=True, primary_script=self.t3_script(),
                         setup_timeout=2700)
        result = h.run()
        self.assertEqual(result["overall"], "failed")
        self.assertEqual(result["overall_cause"], "product")
        step = h.step("setup-start-primary")
        self.assertEqual((step["verdict"], step["cause"]), ("failed", "product"))
        self.assertIn("open-ended unknown", step["reason"])
        self.assertEqual(step["checks"]["poll_stop"]["stop"], "unknown-limit")
        self.assertLess(h.clock.now, 400, "the unknown state is bounded, not waited out for the setup timeout")
        finding = next(f for f in result["findings"] if f["id"] == "d024-open-ended-unknown-setup-start-primary-debian13")
        self.assertEqual((finding["kind"], finding["principle"], finding["code"]),
                         ("product", "D-024", "server_setup_reconciling"))
        self.assertGreaterEqual(finding["lasted_seconds"], 300)
        self.assertEqual(finding["limit_seconds"], 300)
        self.assertIsNotNone(finding["first_observed_at"])
        self.assertEqual(finding["shown"]["en"][0], TRANSLATOR.text("setup.guide.confirm"))
        self.assertEqual(finding["shown"]["tr"][0], TRANSLATOR.text("setup.guide.confirm", language="tr"))
        self.assertEqual(finding["payload"]["error"]["code"], "server_setup_reconciling")
        self.assertTrue((h.writer.directory / finding["payload_file"]).is_file())
        self.assertEqual(h.verdicts()["setup-review-secondary"], "not-run")
        self.assertEqual(h.panels["debian13"].start_count, 1, "polling never re-starts setup")

    def test_unknown_state_limit_is_configurable_and_a_resolved_unknown_passes(self) -> None:
        h = self.harness("pdns-primary/bind-secondary", pdns_primary_gate_open=True, primary_script=self.t3_script(),
                         setup_timeout=2700, unknown_state_limit=60)
        h.run()
        finding = next(f for f in h.result["findings"] if f["id"].startswith("d024-open-ended-unknown"))
        self.assertGreaterEqual(finding["lasted_seconds"], 60)
        self.assertLess(finding["lasted_seconds"], 70)
        # 20 reads x 3 s of reconciling that then resolves: within the bound, no finding.
        h = self.harness("pdns-primary/bind-secondary", pdns_primary_gate_open=True,
                         primary_script=self.t3_script(reconciling_reads=20), setup_timeout=2700)
        result = h.run()
        self.assertEqual(h.verdicts()["setup-start-primary"], "passed", h.step("setup-start-primary")["reason"])
        self.assertFalse(any(f["id"].startswith("d024-open-ended-unknown") for f in result["findings"]))

    def test_primary_start_secondary_text_while_its_dns_result_is_unknown(self) -> None:
        # t3: the captured reconciling read is a blocking DNS state -> finding; the
        # ordinary first running read before it is recorded only.
        h = self.harness("pdns-primary/bind-secondary", pdns_primary_gate_open=True, primary_script=self.t3_script(),
                         setup_timeout=2700)
        result = h.run()
        finding = next(f for f in result["findings"]
                       if f["id"] == "d024-contradictory-peer-start-setup-start-primary-debian13")
        self.assertEqual((finding["kind"], finding["principle"]), ("product", "D-024"))
        self.assertEqual(finding["keys"], ["setup.guide.startSecondary"])
        self.assertEqual(finding["shown"]["en"], [TRANSLATOR.text("setup.guide.startSecondary")])
        self.assertEqual(finding["shown"]["tr"], [TRANSLATOR.text("setup.guide.startSecondary", language="tr")])
        states = [(o["dns_step_status"], o["error_code"], o["blocking"]) for o in finding["observations"]]
        self.assertEqual(states, [("running", "server_setup_reconciling", "unknown:server_setup_reconciling")])
        step = h.step("setup-start-primary")
        recorded = [(o["dns_step_status"], o["error_code"], o["blocking"])
                    for o in step["checks"]["peer_start_guidance_observations"]]
        self.assertEqual(recorded, [("running", None, None)] + states)
        check = step["checks"]["peer_start_guidance_keys"]
        self.assertEqual(check["keys"], ["setup.guide.startSecondary"])
        self.assertIn("setup.guide.startSecondary", check["role_keys"])

    def test_primary_start_secondary_text_during_ordinary_progress_is_not_a_finding(self) -> None:
        # The role text deliberately allows starting the secondary while the primary's
        # setup is progressing: a running DNS step is recorded, never a finding.
        h = self.harness("bind/bind", primary_node="arch")
        result = h.run()
        self.assertEqual(result["overall"], "passed")
        self.assertFalse(any(f["id"].startswith("d024-contradictory-peer-start") for f in result["findings"]))
        observed = h.step("setup-start-primary")["checks"]["peer_start_guidance_observations"]
        self.assertEqual([(o["dns_step_status"], o["blocking"]) for o in observed], [("running", None)])

    def test_primary_start_secondary_text_after_its_dns_step_failed(self) -> None:
        reconciling = fakes.captured("setup-operation-primary-reconciling.json", "pair2")["body"]
        for code, blocking in (("server_setup_dns_failed", "dns-step-failed:server_setup_dns_failed"),
                               ("server_setup_dns_rolled_back", "dns-rolled-back:server_setup_dns_rolled_back")):
            failed = json.loads(json.dumps(reconciling))
            failed.update({"status": "failed", "error": {"code": code, "message": "DNS setup did not complete."}})
            failed["steps"][0]["status"] = "failed"
            h = self.harness("pdns-primary/bind-secondary", pdns_primary_gate_open=True, primary_script=[failed])
            result = h.run()
            self.assertEqual(h.verdicts()["setup-start-primary"], "failed")
            finding = next(f for f in result["findings"]
                           if f["id"] == "d024-contradictory-peer-start-setup-start-primary-debian13")
            self.assertEqual([(o["status"], o["dns_step_status"], o["error_code"], o["blocking"])
                              for o in finding["observations"]], [("failed", "failed", code, blocking)])

    def secondary_license_script(self, license_state: str | None = None) -> list[dict]:
        script = fakes.default_execution_script("secondary")
        required = fakes.captured("setup-operation-secondary-license-required.json", "pair2")["body"]
        if license_state:
            required = dict(required, _license_state=license_state)
        return script[:2] + [required] + script[2:]

    def test_license_required_while_the_license_is_active_is_recorded_and_the_run_continues(self) -> None:
        h = self.harness("bind/bind", primary_node="debian13", licensed=False, license_mode="acceptance-fixture",
                         secondary_script=self.secondary_license_script())
        result = h.run()
        self.assertEqual(result["overall"], "passed", json.dumps(
            [(s["id"], s["verdict"], s["reason"]) for s in result["steps"]], indent=1))
        observations = result["license_required_observations"]
        self.assertEqual(len(observations), 1)
        entry = observations[0]
        self.assertEqual((entry["step"], entry["node"], entry["source"]), ("pair-ready", "arch", "setup execution"))
        self.assertEqual(entry["setup_state"]["error"]["code"], "license_required")
        self.assertEqual((entry["license_status_http"], entry["license_status"]["state"]), (200, "active"))
        self.assertLessEqual(entry["observed_at"], entry["license_status_read_at"])
        finding = next(f for f in result["findings"] if f["id"] == "d024-license-required-while-active-pair-ready-arch")
        self.assertEqual((finding["kind"], finding["principle"]), ("product", "D-024"))
        self.assertEqual(finding["text"], "setup reported license required while the license was active")
        self.assertEqual(finding["shown"]["en"], [TRANSLATOR.text("setup.guide.license")])
        self.assertEqual(len(finding["observations"]), 1)
        self.assertNotIn(b"CPK-", h.evidence_blob())

    def test_license_required_with_an_unverified_license_is_recorded_without_a_finding(self) -> None:
        h = self.harness("bind/bind", primary_node="debian13", licensed=False, license_mode="acceptance-fixture",
                         secondary_script=self.secondary_license_script("verification_unavailable"))
        result = h.run()
        self.assertEqual(len(result["license_required_observations"]), 1)
        self.assertEqual(result["license_required_observations"][0]["license_status"]["state"],
                         "verification_unavailable")
        self.assertFalse(any(f["id"].startswith("d024-license-required-while-active") for f in result["findings"]))


class IdentityRefusalTest(unittest.TestCase):
    def test_unverified_guest_is_never_touched(self) -> None:
        h = Harness("bind/bind", primary_node="arch")
        self.addCleanup(h.close)

        def refuse(node: str) -> dict:
            raise RuntimeError("refusing reboot: guest product_uuid differs; this is not the cell's own disposable guest")

        h.guests.verify = refuse
        result = h.run()
        self.assertEqual(result["overall"], "failed")
        self.assertEqual(h.verdicts()["preflight"], "failed")
        self.assertEqual(h.verdicts()["install-primary"], "not-run")
        self.assertEqual(h.guests.commands, [])
        self.assertTrue((h.writer.directory / "guests/arch/not-collected.txt").is_file())


class InstallStepsTest(unittest.TestCase):
    def test_restart_notice_states(self) -> None:
        self.assertEqual(inst.installer_restart_notice("done\n")["state"], "none")
        required = inst.installer_restart_notice(
            "x\n\x1b[1;31mRESTART THIS SERVER NOW / BU SUNUCUYU SIMDI YENIDEN BASLATIN\x1b[0m\n"
            "    line one\n        reboot\n\n    satir\n        reboot\n\nINSTALL-DONE\n")
        self.assertEqual(required["state"], "required")
        self.assertEqual(required["text"].splitlines()[0], inst.RESTART_REQUIRED_BANNER)
        self.assertIn("satir", required["text"])
        self.assertNotIn("INSTALL-DONE", required["text"])
        recommended = inst.installer_restart_notice("A RESTART IS RECOMMENDED / YENIDEN BASLATMA ONERILIR\n    x\n")
        self.assertEqual(recommended["state"], "recommended")

    def test_enrollment_engine_selector(self) -> None:
        root = "celikpanel-v0.0.0-test"
        bind = inst.enroll_secondary_install(root, primary_ip="192.0.2.10", peer_ip="192.0.2.11",
                                             catalog="catalog-c000020a.celikpanel.invalid")
        self.assertNotIn("--engine", bind)
        self.assertTrue(bind.endswith("/dns-owner-tools/bind-peer-inspect"))
        pdns = inst.enroll_secondary_install(root, primary_ip="192.0.2.10", peer_ip="192.0.2.11",
                                             catalog="catalog-c000020a.celikpanel.invalid", engine="pdns",
                                             catalog_account="celikpanel-peer-catalog-v1")
        self.assertIn("secondary-install --engine pdns ", pdns)
        self.assertIn("--catalog-account celikpanel-peer-catalog-v1", pdns)
        self.assertTrue(pdns.endswith("/dns-owner-tools/pdns-peer-inspect"))
        with self.assertRaises(inst.InstallError):
            inst.enroll_secondary_install(root, primary_ip="192.0.2.10", peer_ip="192.0.2.11",
                                          catalog="c.invalid", engine="pdns", catalog_account="")
        with self.assertRaises(inst.InstallError):
            inst.enroll_secondary_install(root, primary_ip="192.0.2.10", peer_ip="192.0.2.11",
                                          catalog="c.invalid", engine="pdns", catalog_account="Bad Account")
        with self.assertRaises(inst.InstallError):
            inst.enroll_secondary_install(root, primary_ip="192.0.2.10", peer_ip="192.0.2.11",
                                          catalog="c.invalid", catalog_account="celikpanel-peer-catalog-v1")
        self.assertTrue(inst.enroll_status(root, "primary", "pdns").endswith("primary-status --engine pdns"))
        self.assertTrue(inst.enroll_primary_prepare(root).endswith("primary-prepare"))
        with self.assertRaises(inst.InstallError):
            inst.enroll_primary_prepare(root, "knot")


class CliTest(unittest.TestCase):
    def test_plan_is_dry_run_and_validates_topology(self) -> None:
        import contextlib
        import io

        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = pa.main(["plan", "--topology", "pdns-primary/bind-secondary", "--run-label", "r1",
                            "--work-root", "/var/tmp/cp-pair"])
        self.assertEqual(code, 0)
        plan = json.loads(out.getvalue())
        self.assertTrue(plan["dry_run"])
        self.assertEqual(plan["cell_id"], "pair-accept__pdns-debian13__bind-arch__r1")
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            self.assertEqual(pa.main(["plan", "--topology", "pdns-primary/bind-secondary", "--primary-node", "arch",
                                      "--run-label", "r1", "--work-root", "/var/tmp/cp-pair"]), 2)
        self.assertIn("Debian 13", err.getvalue())
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            self.assertEqual(pa.main(["plan", "--topology", "bind/bind", "--primary-node", "arch", "--run-label", "R1",
                                      "--work-root", "/var/tmp/cp-pair"]), 2)
        self.assertIn("run label must be lowercase", err.getvalue())

    def product_web_src(self, commit: str | None) -> Path:
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name) / "product-web-src"
        shutil.copytree(gd.WEB_SRC / "i18n", root / "i18n")
        for relative in ("components/ServerSetup.tsx", "lib/serverSetupGuidance.ts"):
            (root / relative).parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(gd.WEB_SRC / relative, root / relative)
        if commit is not None:
            (root / pa.PRODUCT_COMMIT_MARKER).write_text(commit + "\n", encoding="ascii")
        return root

    def base_run(self) -> list[str]:
        return ["run", "--topology", "bind/bind", "--primary-node", "arch", "--run-label", "r1",
                "--work-root", "/var/tmp/cp-pair", "--identity-file", "/nonexistent/id",
                "--dist-archive", "/nonexistent/a.tar.gz", "--dist-sha256", "a" * 64, "--dist-commit", "b" * 40,
                "--dist-tree", "c" * 40, "--evidence-root", "/nonexistent/evidence"]

    def test_run_without_execute_is_dry_and_license_flags_are_checked(self) -> None:
        import contextlib
        import io

        base = self.base_run() + ["--product-web-src", str(self.product_web_src("b" * 40))]
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            self.assertEqual(pa.main(base), 0)
        plan = json.loads(out.getvalue())
        self.assertTrue(plan["dry_run"])
        self.assertEqual((plan["web_src"]["source"], plan["web_src"]["commit"]), ("--product-web-src", "b" * 40))
        self.assertEqual(plan["unknown_state_limit_seconds"], 300)
        self.assertEqual(plan["unknown_state_codes"], ["server_setup_reconciling"])
        with self.assertRaises(SystemExit) as refused:
            pa.main(base + ["--license-mode", "owner-key"])
        self.assertIn("allow-license-service", str(refused.exception))
        with self.assertRaises(SystemExit):
            pa.main(base + ["--license-key-file-primary", "/x"])
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            self.assertEqual(pa.main(base + ["--license-mode", "acceptance-fixture"]), 0)
        plan = json.loads(out.getvalue())
        self.assertEqual(plan["license_mode"], "acceptance-fixture")
        self.assertIn("NOT evidenced", plan["native_evidence_scope"])
        with self.assertRaises(SystemExit) as refused:
            pa.main(base + ["--license-mode", "acceptance-fixture", "--allow-license-service"])
        self.assertIn("never contacts the license service", str(refused.exception))
        with self.assertRaises(SystemExit):
            pa.main(base + ["--license-mode", "acceptance-fixture", "--license-key-file-primary", "/x"])
        with self.assertRaises(SystemExit) as refused:
            pa.main(base + ["--stable-stop-seconds", "-1"])
        self.assertIn("stable-stop-seconds", str(refused.exception))
        for bad in ("0", "-5"):
            with self.assertRaises(SystemExit) as refused:
                pa.main(base + ["--unknown-state-limit-seconds", bad])
            self.assertIn("unknown-state-limit-seconds", str(refused.exception))
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            self.assertEqual(pa.main(base + ["--unknown-state-limit-seconds", "120"]), 0)
        self.assertEqual(json.loads(out.getvalue())["unknown_state_limit_seconds"], 120)

    def test_guidance_texts_come_from_the_product_commit(self) -> None:
        import contextlib
        import io

        original = pa.driver_checkout
        self.addCleanup(setattr, pa, "driver_checkout", original)
        # A driver from another commit (or from git archive: unknown) needs the product's web/src.
        for checkout in ({"commit": "d" * 40, "web_src_clean": True}, {"commit": None, "web_src_clean": None}):
            pa.driver_checkout = lambda checkout=checkout: checkout
            with self.assertRaises(SystemExit) as refused:
                pa.main(self.base_run())
            self.assertIn("--product-web-src", str(refused.exception))
        # Same commit but a modified web/src: still refused.
        pa.driver_checkout = lambda: {"commit": "b" * 40, "web_src_clean": False}
        with self.assertRaises(SystemExit) as refused:
            pa.main(self.base_run())
        self.assertIn("modified web/src", str(refused.exception))
        # Same commit, clean: the driver's own texts are the product's.
        pa.driver_checkout = lambda: {"commit": "b" * 40, "web_src_clean": True}
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            self.assertEqual(pa.main(self.base_run()), 0)
        self.assertEqual(json.loads(out.getvalue())["web_src"]["source"], "driver checkout")
        # --product-web-src must name the product commit and hold the files the driver reads.
        pa.driver_checkout = lambda: {"commit": "d" * 40, "web_src_clean": True}
        with self.assertRaises(SystemExit) as refused:
            pa.main(self.base_run() + ["--product-web-src", str(self.product_web_src("e" * 40))])
        self.assertIn("is from commit " + "e" * 40, str(refused.exception))
        with self.assertRaises(SystemExit) as refused:
            pa.main(self.base_run() + ["--product-web-src", str(self.product_web_src(None))])
        self.assertIn("PRODUCT-COMMIT", str(refused.exception))
        incomplete = self.product_web_src("b" * 40)
        (incomplete / "lib/serverSetupGuidance.ts").unlink()
        with self.assertRaises(SystemExit) as refused:
            pa.main(self.base_run() + ["--product-web-src", str(incomplete)])
        self.assertIn("lib/serverSetupGuidance.ts", str(refused.exception))

    def test_driver_reads_texts_from_its_configured_web_src(self) -> None:
        # A product whose wizard no longer shows the start-secondary key for the primary:
        # the contradictory-guidance check has nothing to match (keys come from web/src).
        root = self.product_web_src("b" * 40)
        guidance = root / "lib/serverSetupGuidance.ts"
        guidance.write_text(guidance.read_text(encoding="utf-8").replace(
            "'setup.guide.startSecondary'", "'setup.guide.pairUnverified'"), encoding="utf-8")
        h = Harness("bind/bind", primary_node="arch")
        self.addCleanup(h.close)
        h.driver.web_src = root
        result = h.run()
        self.assertEqual(result["overall"], "passed")
        check = h.step("setup-start-primary")["checks"]["peer_start_guidance_keys"]
        self.assertEqual((check["state"], check["keys"]), ("not-used-by-this-build", []))
        self.assertFalse(any(f["id"].startswith("d024-contradictory-peer-start") for f in result["findings"]))


class BuildScriptTest(unittest.TestCase):
    SCRIPTS = HERE / "scripts"

    def test_archive_observation_counts_evidence_and_e2e_entries(self) -> None:
        import io
        import subprocess
        import tarfile

        text = (self.SCRIPTS / "build-dist.sh").read_text(encoding="utf-8")
        start = text.index('> "$out/archive-evidence-entries.json" <<\'PY\'\n') + len(
            '> "$out/archive-evidence-entries.json" <<\'PY\'\n')
        snippet = text[start:text.index("\nPY\n", start)]
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        archive = Path(temporary.name) / "a.tar.gz"
        root = "celikpanel-v0.0.0-test"
        members = [f"{root}/", f"{root}/bin/panel", f"{root}/deploy/", f"{root}/deploy/e2e/",
                   f"{root}/deploy/e2e/dns-kill-matrix/fixture.py",
                   f"{root}/deploy/e2e/dns-kill-matrix/evidence/", f"{root}/deploy/e2e/dns-kill-matrix/evidence/b6/x.sqlite3",
                   f"{root}/docs/evidence/", f"{root}/docs/evidence/note.md", f"{root}/evidence.md"]
        with tarfile.open(archive, "w:gz") as bundle:
            for name in members:
                info = tarfile.TarInfo(name.rstrip("/"))
                if name.endswith("/"):
                    info.type = tarfile.DIRTYPE
                    bundle.addfile(info)
                else:
                    info.size = 1
                    bundle.addfile(info, io.BytesIO(b"x"))
        completed = subprocess.run([sys.executable, "-", str(archive), "f" * 64], input=snippet, text=True,
                                   capture_output=True, check=True)
        observed = json.loads(completed.stdout)
        self.assertEqual(observed["total_entries"], len(members))
        self.assertEqual((observed["under_evidence_dir"]["entries"], observed["under_evidence_dir"]["files"]), (2, 2))
        self.assertEqual(observed["under_evidence_dir"]["sample"],
                         ["deploy/e2e/dns-kill-matrix/evidence/b6/x.sqlite3", "docs/evidence/note.md"])
        self.assertEqual((observed["under_deploy_e2e"]["entries"], observed["under_deploy_e2e"]["files"]), (3, 2))
        self.assertIn("observation only", observed["note"])

    def test_build_exports_product_web_src_and_run_passes_it(self) -> None:
        build = (self.SCRIPTS / "build-dist.sh").read_text(encoding="utf-8")
        self.assertIn('cp -a "$src/web/src/." "$out/product-web-src/"', build)
        self.assertIn('> "$out/product-web-src/PRODUCT-COMMIT"', build)
        self.assertIn('"product_web_src": out + "/product-web-src"', build)
        self.assertEqual(pa.PRODUCT_COMMIT_MARKER, "PRODUCT-COMMIT")
        run = (self.SCRIPTS / "run-topology.sh").read_text(encoding="utf-8")
        self.assertIn("web_args=(--product-web-src \"$web_src\")", run)
        for path in self.SCRIPTS.iterdir():
            self.assertNotIn(b"\r", path.read_bytes(), path.name)


if __name__ == "__main__":
    unittest.main()
