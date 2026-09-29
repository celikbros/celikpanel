#!/usr/bin/env python3
"""Offline tests: D-024 guidance reconstruction from API payloads."""

from __future__ import annotations

import json
import re
import sys
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import guidance as gd  # noqa: E402

CATALOG = gd.load_catalog()
T = gd.Translator(CATALOG)


def context(role: str = "primary") -> dict:
    return {
        "dns_mode": "local", "dns_role": role, "dns_engine": "bind",
        "local_nameserver": "ns1.ns-accept.example" if role == "primary" else "ns2.ns-accept.example",
        "local_ip": "192.0.2.11" if role == "primary" else "192.0.2.10",
        "peer_nameserver": "ns2.ns-accept.example" if role == "primary" else "ns1.ns-accept.example",
        "peer_ip": "192.0.2.10" if role == "primary" else "192.0.2.11",
        "panel_domain": "panel-arch.ns-accept.example", "mail_hostname": "", "dns_hosting_management": "",
    }


class CatalogTest(unittest.TestCase):
    def test_every_key_the_port_uses_exists_in_both_languages(self) -> None:
        source = (HERE / "guidance.py").read_text(encoding="utf-8")
        keys = set(re.findall(r'"((?:setup|err|dnsEngine|domains)\.[A-Za-z0-9_.]+)"', source))
        keys.discard("dnsEngine.blocker.")
        self.assertGreater(len(keys), 40)
        missing = sorted(key for key in keys if key not in CATALOG["en"] or key not in CATALOG["tr"])
        self.assertEqual(missing, [])

    def test_interpolation(self) -> None:
        text = T.text("setup.guide.primary", {"local": "ns1.x", "localIP": "192.0.2.10", "peer": "ns2.x", "peerIP": "192.0.2.11"})
        self.assertIn("192.0.2.11", text)
        self.assertNotIn("{", text)


class ApiErrorTest(unittest.TestCase):
    def test_license_lock_is_actionable_prerequisite(self) -> None:
        body = {"error": "Panel access requires an active server license.", "code": "license_required"}
        item = gd.api_error_guidance(T, 403, body)
        self.assertEqual(item["state"], "unmet-prerequisite")
        self.assertTrue(item["actionable"])
        self.assertEqual(item["message_keys"], ["err.license_required"])
        self.assertIn("administrator", item["actor"])
        self.assertTrue(item["shown"]["tr"][0].startswith("Paneli"))

    def test_uncoded_failure_is_not_actionable(self) -> None:
        item = gd.api_error_guidance(T, 409, {"error": "the preview was blocked: pdns_primary_switch_paused"})
        self.assertEqual(item["state"], "verified-failure")
        self.assertFalse(item["actionable"])
        self.assertEqual(item["shown"]["en"], ["the preview was blocked: pdns_primary_switch_paused"])

    def test_publication_pending_with_reviewed_reason(self) -> None:
        body = {"error": "saved", "code": "DNS_PUBLICATION_FAILED", "reason": "dns_peer_enrollment_required"}
        item = gd.api_error_guidance(T, 409, body, pending=True)
        self.assertEqual(item["state"], "pending")
        self.assertTrue(item["actionable"])
        self.assertEqual(item["message_keys"], ["err.DNS_PUBLICATION_FAILED.dns_peer_enrollment_required"])

    def test_unknown_license_observation(self) -> None:
        item = gd.api_error_guidance(T, 503, {"error": "x", "code": "LICENSE_VERIFICATION_UNAVAILABLE"})
        self.assertEqual(item["state"], "unknown")


class GenericGuidanceTest(unittest.TestCase):
    """Item 4 (pair1): generic error text is never actionable guidance (D-024)."""

    PAIR1_PLAN_500 = json.loads((HERE / "fixtures" / "pair1" / "setup-plan-500-internal.json").read_text(encoding="utf-8"))

    def test_err_internal_is_not_actionable(self) -> None:
        item = gd.api_error_guidance(T, 500, {"code": "INTERNAL", "error": "internal server error"})
        self.assertEqual(item["message_keys"], ["err.INTERNAL"])
        self.assertEqual(item["shown"]["en"], [CATALOG["en"]["err.INTERNAL"]])
        self.assertEqual(item["state"], "verified-failure")
        self.assertFalse(item["actionable"])
        self.assertIn("generic error code INTERNAL", item["actionable_reason"])

    def test_exact_pair1_plan_500_shows_setup_plan_failed(self) -> None:
        exchange = self.PAIR1_PLAN_500
        self.assertEqual((exchange["method"], exchange["path"], exchange["status"]), ("POST", "/api/v1/setup/plan", 500))
        self.assertEqual(exchange["body"], {"code": "INTERNAL", "error": "internal server error"})
        item = gd.setup_plan_failure_guidance(T, exchange["status"], exchange["body"])
        # ServerSetup.tsx review(): the wizard never reads the body of a failed plan.
        self.assertEqual(item["message_keys"], ["setup.planFailed"])
        self.assertEqual(item["shown"]["en"], [CATALOG["en"]["setup.planFailed"]])
        self.assertEqual(item["shown"]["tr"], [CATALOG["tr"]["setup.planFailed"]])
        self.assertEqual(item["api_error_keys"], ["err.INTERNAL"])
        self.assertEqual(item["api_error_text"]["tr"], [CATALOG["tr"]["err.INTERNAL"]])
        self.assertEqual((item["code"], item["http_status"]), ("INTERNAL", 500))
        self.assertFalse(item["actionable"])
        self.assertIn("setup.planFailed", item["actionable_reason"])

    def test_plan_failure_is_generic_even_with_a_specific_code(self) -> None:
        item = gd.setup_plan_failure_guidance(T, 409, {"code": "setup_conflict", "error": "x"})
        self.assertFalse(item["actionable"])

    def test_bare_http_status_and_generic_server_text(self) -> None:
        bare = gd.api_error_guidance(T, 500, None)
        self.assertFalse(bare["actionable"])
        self.assertIn("bare HTTP status 500", bare["actionable_reason"])
        self.assertEqual(bare["message_keys"], ["common.error"])
        uncoded = gd.api_error_guidance(T, 500, {"error": "internal server error"})
        self.assertFalse(uncoded["actionable"])
        self.assertIn("generic server text", uncoded["actionable_reason"])

    def test_generic_blocker_and_empty_reason(self) -> None:
        blocker = gd.plan_blocker_guidance(T, ["something_new"], gd.wizard_code_keys())
        self.assertEqual(blocker["message_keys"], ["setup.blocker.unknown"])
        self.assertFalse(blocker["actionable"])
        pending = gd.deletion_pending_guidance(T, 202, {"status": "deletion_pending", "stage": "dns_cleanup",
                                                        "reason": "", "message": "m"})
        self.assertFalse(pending["actionable"])

    def test_start_failure_views(self) -> None:
        internal = gd.setup_start_failure_guidance(T, 500, {"code": "INTERNAL", "error": "internal server error"})
        self.assertEqual((internal["state"], internal["title_key"]), ("unknown", "setup.reconnecting"))
        self.assertEqual(internal["message_keys"], ["setup.uncertain"])
        self.assertFalse(internal["actionable"])
        review = gd.setup_start_failure_guidance(T, 409, {"code": "server_setup_review_stale", "error": "x"})
        self.assertEqual(review["message_keys"], ["setup.conflict"])
        self.assertTrue(review["actionable"])


class SetupExecutionTest(unittest.TestCase):
    def test_failed_dns_step_with_guidance(self) -> None:
        execution = {"status": "failed", "phase": "01-dns",
                     "steps": [{"id": "01-dns", "kind": "dns", "status": "failed"}],
                     "error": {"code": "server_setup_dns_failed", "message": "DNS setup requires attention."},
                     "context": context("primary")}
        item = gd.setup_execution_guidance(T, execution)
        self.assertEqual(item["state"], "verified-failure")
        self.assertTrue(item["actionable"])
        self.assertEqual(item["message_keys"], ["setup.guide.failed", "setup.guide.primary", "setup.guide.startSecondary"])
        self.assertIn("192.0.2.10", " ".join(item["shown"]["en"]))

    def test_failed_without_guidance_is_flagged(self) -> None:
        execution = {"status": "failed", "phase": "mystery", "steps": [],
                     "error": {"code": "server_setup_step_failed", "message": "x"}}
        item = gd.setup_execution_guidance(T, execution)
        self.assertEqual(item["message_keys"], ["setup.guide.failed"])
        # "Read the error below" alone is framing, not specific guidance.
        self.assertFalse(item["actionable"])
        execution["error"] = None
        execution["status"] = "waiting"
        execution["phase"] = None
        item = gd.setup_execution_guidance(T, execution)
        self.assertEqual(item["message_keys"], ["setup.guide.unknown"])
        self.assertFalse(item["actionable"])

    def test_secondary_waits_for_primary(self) -> None:
        execution = {"status": "waiting", "phase": "primary_dns",
                     "steps": [{"id": "01-dns", "kind": "dns", "status": "running"}],
                     "error": {"code": "server_setup_primary_dns_required", "message": "x"},
                     "context": context("secondary")}
        item = gd.setup_execution_guidance(T, execution)
        self.assertTrue(item["actionable"])
        self.assertEqual(item["state"], "unmet-prerequisite")
        self.assertIn("setup.guide.primaryDNSWaiting", item["message_keys"])
        self.assertIn("ns1.ns-accept.example", item["shown"]["en"][0])
        self.assertEqual(item["actor"], "the primary server's owner")
        self.assertIn("setup.guide.monitoring", item["detail_keys"])

    def test_waiting_access_dns_uses_phase_as_code(self) -> None:
        execution = {"status": "waiting", "phase": "access_dns",
                     "steps": [{"id": "05-access_dns", "kind": "access_dns", "status": "running",
                                "target": "panel-arch.ns-accept.example", "qualifier": "192.0.2.11"}],
                     "context": context("primary")}
        item = gd.setup_execution_guidance(T, execution)
        self.assertEqual(item["code"], "phase:access_dns")
        self.assertTrue(item["actionable"])
        self.assertIn("setup.guide.accessDNSRecord", item["message_keys"])

    def test_license_phase_and_progress(self) -> None:
        item = gd.setup_execution_guidance(T, {"status": "waiting", "phase": "license", "steps": [],
                                               "error": {"code": "license_required", "message": "x"}})
        self.assertEqual(item["title_key"], "setup.licenseWaiting")
        self.assertTrue(item["actionable"])
        running = gd.setup_execution_guidance(T, {"status": "running", "phase": "01-dns", "steps": [
            {"id": "01-dns", "kind": "dns", "status": "running"}], "context": context()})
        self.assertEqual(running["state"], "progress")
        self.assertTrue(running["actionable"])


class DeletionAndGateTest(unittest.TestCase):
    def test_pending_deletion_with_and_without_reason(self) -> None:
        body = {"status": "deletion_pending", "domain": "pair-accept.example", "stage": "dns_cleanup",
                "message": "m", "reason": "dns_peer_enrollment_required"}
        item = gd.deletion_pending_guidance(T, 202, body)
        self.assertTrue(item["actionable"])
        self.assertEqual(item["reason"], "dns_peer_enrollment_required")
        body.pop("reason")
        generic = gd.deletion_pending_guidance(T, 202, body)
        self.assertFalse(generic["actionable"])
        self.assertEqual(generic["message_keys"], ["domains.deletionPending"])
        unreviewed = gd.deletion_pending_guidance(T, 202, {**body, "reason": "made_up"})
        self.assertFalse(unreviewed["actionable"])

    def test_saved_status(self) -> None:
        item = gd.deletion_pending_guidance(T, 200, {"status": "deletion_pending", "stage": "dns_cleanup",
                                                     "reason": "dns_peer_inspection_unknown", "message": "m"},
                                            saved=True)
        self.assertTrue(item["actionable"])
        unknown = gd.deletion_pending_guidance(T, 200, {"status": "unknown", "stage": "unknown", "message": "m"},
                                               saved=True)
        self.assertFalse(unknown["actionable"])

    def test_wizard_maps_the_server_gate_code_from_the_shipped_source(self) -> None:
        keys = gd.wizard_code_keys()
        self.assertEqual(keys["pdns_primary_switch_paused"], "setup.pdnsPrimaryPaused")
        self.assertEqual(keys["license_required"], "license.restricted")
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            unmapped = Path(directory) / "ServerSetup.tsx"
            unmapped.write_text("const codeKey: Record<string, TranslationKey> = {\n    other: 'x.y',\n};\n",
                                encoding="utf-8")
            self.assertEqual(gd.wizard_code_keys(unmapped), {"other": "x.y"})
            self.assertEqual(gd.wizard_code_keys(Path(directory) / "missing.tsx"), {})
            item = gd.plan_blocker_guidance(T, ["pdns_primary_switch_paused"], gd.wizard_code_keys(unmapped))
        # failureText's fallback is generic: an unmapped gate code is not actionable.
        self.assertEqual(item["message_keys"], ["setup.blocker.unknown"])
        self.assertFalse(item["actionable"])

    def test_selection_rule_and_server_gate_text(self) -> None:
        draft = {"purpose": "dns", "dns_mode": "local", "dns_role": "primary", "dns_engine": "pdns"}
        # The wizard no longer refuses a PowerDNS primary itself (6f2fb028).
        self.assertIsNone(gd.setup_selection_error(draft))
        self.assertEqual(gd.setup_selection_error({**draft, "dns_mode": "external"}), "setup.components.localDNSRequired")
        item = gd.plan_blocker_guidance(T, ["pdns_primary_switch_paused"], gd.wizard_code_keys())
        self.assertTrue(item["actionable"])
        self.assertEqual(item["code"], "pdns_primary_switch_paused")
        self.assertEqual(item["title_key"], "setup.planBlocked")
        self.assertEqual(item["title"], CATALOG["en"]["setup.planBlocked"])
        self.assertEqual(item["shown"]["en"], [CATALOG["en"]["setup.pdnsPrimaryPaused"]])
        self.assertEqual(item["shown"]["tr"], [CATALOG["tr"]["setup.pdnsPrimaryPaused"]])
        self.assertEqual(item["actor"], "this server's administrator (choose another engine or topology)")
        preview = gd.preview_blocker_guidance(T, [{"code": "pdns_primary_switch_paused"}])
        self.assertTrue(preview["actionable"])
        self.assertEqual(preview["shown"]["en"], [CATALOG["en"]["dnsEngine.blocker.pdnsPrimarySwitchPaused"]])

if __name__ == "__main__":
    unittest.main()
