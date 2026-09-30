"""Offline tests for the upd1 owner-started update trial (no guest, no network)."""
import ast
import base64
import fnmatch
import hashlib
import importlib.util
import inspect
import io
import json
import os
import re
import secrets
import shutil
import subprocess
import sys
import tarfile
import tempfile
import threading
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[2]
RUN_EVIDENCE = HERE / "evidence" / "upd1-20261001"


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    value = importlib.util.module_from_spec(spec)
    sys.modules[name] = value  # dataclasses resolve annotations through sys.modules
    spec.loader.exec_module(value)
    return value


t = load("tested_owner_update_trial", "owner_update_trial.py")
w = load("tested_upd1_workload", "guest_upd1_workload.py")
o = load("tested_owner_update_observer", "guest_owner_update_observer.py")

SNAPSHOT = "20261001T120000Z-from-unknown-to-" + "3" * 40 + "-" + "1" * 32
RID = "a" * 32


def artifacts(upd3=False):
    def item(version, sequence, commit, parent=None):
        value = {"version": version, "sequence": sequence, "commit": commit, "tree": "e" * 40, "sha256": "f" * 64,
                 "archive": "/nonexistent.tar.gz", "license_mode": "acceptance-fixture",
                 "product_web_src": str(REPO / "web" / "src")}
        if parent:
            value["parent"] = parent
        return value
    document = {"schema": t.ARTIFACTS_SCHEMA, "source_head": "9" * 40, "clone": "/var/tmp/cp-upd1-build/x/repo",
                "baseline": item("v0.1.0-alpha.81", 81, "1" * 40),
                "good": item("v0.1.0-alpha.82", 82, "2" * 40, "1" * 40),
                "defective": item("v0.1.0-alpha.82", 82, "3" * 40, "2" * 40)}
    if upd3:
        document["startcheck"] = item("v0.1.0-alpha.82", 82, "4" * 40, "2" * 40)
        document["realstart"] = item("v0.1.0-alpha.82", 82, "5" * 40, "2" * 40)
    return document


class PlanTests(unittest.TestCase):
    def test_twelve_cells_with_the_selected_second_faults(self):
        self.assertEqual(sorted(t.CELLS), ["upd1-arch-defective", "upd1-arch-good", "upd1-arch-mgmt-off-reboot",
                                           "upd1-arch-owner-continuation", "upd1-arch-realstart",
                                           "upd1-arch-startcheck", "upd1-debian13-defective", "upd1-debian13-good",
                                           "upd1-debian13-mgmt-off-reboot", "upd1-debian13-owner-continuation",
                                           "upd1-debian13-realstart", "upd1-debian13-startcheck"])
        self.assertEqual(t.CELLS["upd1-debian13-defective"].recovery_fault,
                         {"action": "reboot", "checkpoint": "payload_restored"})
        self.assertEqual(t.CELLS["upd1-arch-defective"].recovery_fault,
                         {"action": "kill", "checkpoint": "runtime_verified"})
        self.assertIsNone(t.CELLS["upd1-debian13-good"].recovery_fault)
        self.assertTrue(t.CELLS["upd1-debian13-good"].mail_required)
        self.assertFalse(t.CELLS["upd1-arch-good"].mail_required)
        for cell in t.CELLS.values():
            if cell.recovery_fault:
                self.assertIn(cell.recovery_fault, o.FAULTS)
        with self.assertRaises(ValueError):
            t.validate_cell("upd1-rhel9-defective")

    def test_artifacts_validation(self):
        document = artifacts()
        self.assertIs(t.validate_artifacts(document, check_files=False), document)
        for mutate, message in (
                (lambda d: d["good"].update(version="v0.1.0-alpha.81"), "labelled"),
                (lambda d: d["defective"].update(commit="2" * 40), "distinct"),
                (lambda d: d["defective"].update(parent="1" * 40), "lineage"),
                (lambda d: d["baseline"].pop("license_mode"), "acceptance-license"),
                (lambda d: d["good"].update(sha256="F" * 64), "exact"),
                (lambda d: d.update(schema="other"), "schema")):
            broken = artifacts()
            mutate(broken)
            with self.assertRaisesRegex(ValueError, message):
                t.validate_artifacts(broken, check_files=False)

    def test_dry_run_plan_validates_without_a_guest(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "upd1-artifacts.json"
            path.write_text(json.dumps(artifacts()))
            out = io.StringIO()
            with redirect_stdout(out):
                code = t.main(["plan", "--cell", "upd1-debian13-defective", "--artifacts", str(path),
                               "--work-root", "/var/tmp/cp-release-drill-upd1-d13-def", "--dry-run"])
            self.assertEqual(code, 0)
            plan = json.loads(out.getvalue())
            names = [step["name"] for step in plan["steps"]]
            self.assertEqual(names[:3], ["preflight", "origin", "baseline-install"])
            self.assertIn("owner-continuation (required)", names)
            self.assertFalse(plan["native_evidence"])
            self.assertEqual(plan["candidate"]["commit"], "3" * 40)
            self.assertIn("QMP system_reset", json.dumps(plan))
            with self.assertRaises(ValueError):
                t.main(["plan", "--cell", "upd1-arch-good", "--artifacts", str(path),
                        "--work-root", "/var/tmp/somewhere-else", "--dry-run"])

    def test_owner_start_without_execute_is_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "a.json"
            path.write_text(json.dumps(artifacts()))
            with self.assertRaises(SystemExit), redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
                t.main(["run", "--cell", "upd1-arch-good", "--artifacts", str(path),
                        "--work-root", "/var/tmp/cp-release-drill-x"])

    def test_acceptance_key_matches_the_pair_driver(self):
        source = (REPO / "deploy/e2e/dns-pair-acceptance/pair_acceptance.py").read_text()
        match = re.search(r'ACCEPTANCE_FIXTURE_KEY = "([^"]+)" \+ "0" \* (\d+)', source)
        self.assertEqual(t.ACCEPTANCE_FIXTURE_KEY, match.group(1) + "0" * int(match.group(2)))


class FixtureSourceTests(unittest.TestCase):
    def test_defect_applies_exactly_once_to_the_current_panel_main(self):
        text = (REPO / "cmd/panel/main.go").read_text()
        changed = t.apply_defect(text)
        self.assertEqual(changed.count("upd1 fixture defect:"), 1)
        self.assertNotIn('log.Println("Canonical panel database migrations completed")', changed)
        with self.assertRaises(ValueError):
            t.apply_defect(changed)

    def test_fixture_policies_satisfy_the_harness_release_policy_checks(self):
        archive = load("tested_upd1_candidate_archive", "candidate_archive.py")
        origin = load("tested_upd1_origin", "worker_fixture_origin.py")
        baseline = t.policy_text("v0.1.0-alpha.81", 81, 80, "v0.1.0-alpha.80", t.ALPHA80_COMMIT).encode()
        candidate = t.policy_text("v0.1.0-alpha.82", 82, 81, "v0.1.0-alpha.81", "1" * 40).encode()
        self.assertEqual(archive.verify_release_policy(baseline, {"version": "v0.1.0-alpha.81", "current": 81,
                                                                  "previous": 80, "previous_version": "v0.1.0-alpha.80"},
                                                       "v0.1.0-alpha.81")["current"], 81)
        self.assertEqual(archive.verify_release_policy(candidate, origin.RELEASE_POLICY, "v0.1.0-alpha.82")
                         ["previous_commit"], "1" * 40)
        with self.assertRaises(ValueError):
            t.policy_text("v0.1.0-alpha.82", 82, 82, "v0.1.0-alpha.81", "1" * 40)

    def test_fixture_edits_are_refused_outside_a_disposable_clone(self):
        with tempfile.TemporaryDirectory() as directory:
            (Path(directory) / ".git").mkdir()
            with self.assertRaisesRegex(ValueError, "disposable"):
                t.fixture_source(Path(directory), "baseline", None)

    def test_acceptance_notice_is_the_only_exemption(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "a.tar.gz"
            notice = t.ACCEPTANCE_NOTICE_PREFIX + b"\nrest\n"
            with tarfile.open(archive, "w:gz") as bundle:
                info = tarfile.TarInfo("celikpanel-v0.1.0-alpha.81/" + t.ACCEPTANCE_NOTICE)
                info.size = len(notice)
                bundle.addfile(info, io.BytesIO(notice))
            seen = {}

            def verify(candidate, repository):
                seen.update(files=dict(candidate["files"]), repository=repository)
                return {"commit": candidate["commit"]}
            wrapped = t.acceptance_source_proof(verify, archive, Path("/clone"))
            files = {"install.sh": "1" * 64, t.ACCEPTANCE_NOTICE: hashlib.sha256(notice).hexdigest()}
            proof = wrapped({"commit": "c" * 40, "files": files}, Path("/ignored"))
            self.assertEqual(seen["files"], {"install.sh": "1" * 64})
            self.assertEqual(seen["repository"], Path("/clone"))
            self.assertEqual(proof["exempted_from_git_proof"], [t.ACCEPTANCE_NOTICE])
            with self.assertRaisesRegex(ValueError, "digest"):
                wrapped({"commit": "c" * 40, "files": {t.ACCEPTANCE_NOTICE: "0" * 64}}, None)
            with self.assertRaisesRegex(ValueError, "digest"):
                wrapped({"commit": "c" * 40, "files": {"install.sh": "1" * 64}}, None)


class RuleTests(unittest.TestCase):
    def test_ui_backoff(self):
        delays, delay = [], t.POLL_MIN_MS
        for _ in range(7):
            delay = t.next_delay_ms(delay, False)
            delays.append(delay)
        self.assertEqual(delays, [2400, 3840, 6144, 9830, 15000, 15000, 15000])
        self.assertEqual(t.next_delay_ms(15000, True), 1500)

    def known(self, phase, proof="none", **extra):
        return dict({"schema": "celikpanel-recovery-status/v1", "request_id": RID, "observation": "known",
                     "phase": phase, "terminal_proof": proof, "reason": "x"}, **extra)

    def shell(self, rid=RID, lang="en"):
        return {"reference": rid, "status_command": t.shell_status_command(rid, lang)}

    def test_status_agreement(self):
        both = t.status_agreement(RID, self.known("recovering"), self.known("recovering"), self.shell())
        self.assertEqual(both["verdict"], "agree")
        self.assertEqual(t.status_agreement(RID, None, self.known("recovering"), self.shell(lang="tr"))["verdict"],
                         "single-source")
        phase = t.status_agreement(RID, self.known("recovered", "rollback_verified"), self.known("recovering"), self.shell())
        self.assertEqual(phase["verdict"], "disagree")
        self.assertTrue(any("phase differs" in r for r in phase["reasons"]))
        other = t.status_agreement(RID, None, dict(self.known("recovering"), request_id="b" * 32), self.shell())
        self.assertEqual(other["verdict"], "disagree")
        shell = t.status_agreement(RID, None, self.known("recovering"), {"reference": RID, "status_command": "rm -rf /"})
        self.assertEqual(shell["verdict"], "disagree")
        paused = dict(self.known("recovery_required"), automatic_recovery="paused_retry_limit")
        self.assertEqual(t.status_agreement(RID, dict(paused, automatic_recovery=None), paused, self.shell())["verdict"],
                         "disagree")
        unavailable = {"request_id": RID, "observation": "unavailable"}
        self.assertEqual(t.status_agreement(RID, None, unavailable, self.shell())["verdict"], "no-known-source")

    def test_agreement_verdict_tolerates_one_transition_sample_only(self):
        a, d, s = {"verdict": "agree"}, {"verdict": "disagree"}, {"verdict": "single-source"}
        self.assertEqual(t.agreement_verdict([a, d, a])["verdict"], "passed")
        self.assertEqual(t.agreement_verdict([a, d, d, a])["verdict"], "failed")
        self.assertEqual(t.agreement_verdict([a, a, d])["verdict"], "failed")
        self.assertEqual(t.agreement_verdict([s, s])["verdict"], "inconclusive")

    def test_status_and_outcome_classification(self):
        recovered = self.known("recovered", "rollback_verified", previous_failure="update_failed")
        paused = dict(self.known("recovery_required"), automatic_recovery="paused_retry_limit")
        self.assertEqual(t.classify_status(recovered), "terminal")
        self.assertEqual(t.classify_status(paused), "paused")
        self.assertEqual(t.classify_status(self.known("recovering")), "in-progress")
        self.assertEqual(t.classify_status({"observation": "unavailable"}), "unknown")
        self.assertEqual(t.classify_outcome("defective", recovered, False), "recovered-automatically")
        self.assertEqual(t.classify_outcome("defective", recovered, True), "recovered-after-owner-continuation")
        self.assertEqual(t.classify_outcome("defective", paused, False), "paused-owner-action-required")
        self.assertEqual(t.classify_outcome("good", self.known("succeeded", "update_verified"), False), "update-verified")
        self.assertEqual(t.classify_outcome("good", recovered, False), "good-candidate-rolled-back")
        self.assertEqual(t.classify_outcome("defective", self.known("succeeded", "update_verified"), False),
                         "defective-candidate-reported-success")
        self.assertEqual(t.classify_outcome("defective", None, False), "not-terminal")

    def test_recovery_guidance_uses_the_product_catalogue(self):
        guidance = load("tested_upd1_guidance", "../dns-pair-acceptance/guidance.py")
        translator = guidance.Translator(guidance.load_catalog(REPO / "web/src/i18n"))
        paused = dict(self.known("recovery_required"), automatic_recovery="paused_retry_limit",
                      previous_failure="recovery_incomplete")
        shown = t.recovery_guidance(translator, paused)
        self.assertTrue(shown["actionable"], shown)
        self.assertIn("sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50", shown["texts"]["en"])
        self.assertIn("sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50", shown["texts"]["tr"])
        self.assertNotEqual(shown["texts"]["en"], shown["texts"]["tr"])
        done = t.recovery_guidance(translator, self.known("recovered", "rollback_verified", previous_failure="update_failed"))
        self.assertEqual(done["missing_keys"], [])
        bogus = t.recovery_guidance(translator, self.known("mystery"))
        self.assertTrue(bogus["no_actor_or_action"])
        self.assertFalse(t.recovery_guidance(translator, None)["actionable"])
        # H10: the card follows the product's systemUpdateOutcome.ts (no recovery record read yet: unknown result).
        rules = t.load_card_rules(REPO / "web/src")
        card = t.update_card_guidance(translator, {"found": True, "request_id": RID, "status": "failed"}, None, rules)
        self.assertEqual((card["keys"], card["missing_keys"]),
                         (["panelUpdate.failed", "panelUpdate.outcome.unknownResult"], []))
        self.assertNotEqual(card["texts"]["en"], card["texts"]["tr"])
        summary = t.update_card_guidance(translator, {"found": True, "request_id": RID, "status": "failed",
                                                      "summary": "update failed"}, None, rules)
        self.assertEqual(summary["server_message"], "update failed")
        self.assertEqual(summary["texts"]["tr"][-1], "Sunucunun bildirdiği: update failed")
        self.assertEqual(t.update_card_guidance(translator, {"found": False}, None, rules)["texts"],
                         {"en": [], "tr": []})
        self.assertTrue(t.update_card_guidance(translator, {"found": True, "status": "failed"})["unavailable"])

    def test_outage_windows(self):
        def s(moment, ok):
            return {"t": moment, "web": {"ok": ok}}
        steady = [s(i * 5.0, True) for i in range(20)]
        self.assertEqual(t.outage_windows(steady, "web"), [])
        failing = [s(0, True), s(5, True), s(10, False), s(15, False), s(20, True), s(25, True)]
        (window,) = t.outage_windows(failing, "web")
        self.assertEqual((window["from"], window["first_bad"], window["last_bad"], window["to"]), (5, 10, 15, 20))
        self.assertEqual((window["lower_bound_s"], window["upper_bound_s"], window["kind"]), (5.0, 15.0, "failed"))
        gap = [s(0, True), s(5, True), s(100, True)]
        (window,) = t.outage_windows(gap, "web")
        self.assertEqual((window["kind"], window["upper_bound_s"], window["lower_bound_s"]), ("unobserved", 95.0, 0.0))
        tail = [s(0, True), s(5, False)]
        (window,) = t.outage_windows(tail, "web")
        self.assertIsNone(window["to"])
        self.assertIsNone(window["upper_bound_s"])
        skipped = [{"t": 0, "web": {"ok": None}}, s(5, True)]
        self.assertEqual(t.outage_windows(skipped, "web"), [])
        mixed = [s(0, True), s(5, False), s(90, False), s(95, True)]
        self.assertEqual(t.outage_windows(mixed, "web")[0]["kind"], "failed+unobserved")

    def test_cron_windows(self):
        def s(moment, mtime):
            return {"t": moment, "cron": {"ok": True, "mtime": mtime}}
        steady = [s(i * 5.0, 60.0 * (i * 5 // 60)) for i in range(60)]
        self.assertEqual(t.cron_windows(steady), [])
        stalled = [s(0, 0.0), s(60, 60.0), s(400, 60.0), s(420, 420.0), s(480, 480.0)]
        (window,) = t.cron_windows(stalled)
        self.assertEqual((window["from"], window["to"], window["upper_bound_s"]), (60.0, 420.0, 360.0))
        open_tail = [s(0, 0.0), s(300, 0.0)]
        self.assertIsNone(t.cron_windows(open_tail)[0]["to"])

    def test_windows_caused_by_the_recorded_reset(self):
        windows = [{"from": 100.0, "to": 180.0}, {"from": 400.0, "to": 420.0}]
        t.classify_windows(windows, [110.0])
        self.assertEqual([w["cause"] for w in windows], ["host-reset", "unexplained"])
        self.assertEqual(t.workload_verdict(windows), "interrupted")
        self.assertEqual(t.workload_verdict(windows[:1]), "interrupted-only-by-host-reset")
        self.assertEqual(t.workload_verdict([]), "never-interrupted")

    def test_panel_window_must_lie_inside_the_transaction(self):
        inside = [{"from": 105.0, "to": 300.0, "first_bad": 110.0}]
        self.assertEqual(t.panel_verdict(inside, 100.0, 290.0)["verdict"], "down-only-during-transaction")
        before = [{"from": 10.0, "to": 20.0, "first_bad": 15.0}]
        self.assertEqual(t.panel_verdict(before, 100.0, 290.0)["verdict"], "down-outside-transaction")
        still_down = [{"from": 105.0, "to": None, "first_bad": 110.0}]
        self.assertEqual(t.panel_verdict(still_down, 100.0, 290.0)["verdict"], "down-outside-transaction")

    def test_database_comparison(self):
        def db(tables, schema="s"):
            return {"semantic": {"schema_sha256": schema, "sha256": "x",
                                 "tables": [{"name": n, "sha256": v} for n, v in tables.items()]}}
        pre = db({"domains": "1", "sessions": "2"})
        self.assertEqual(t.compare_databases(pre, db({"domains": "1", "sessions": "2"}))["verdict"], "equal")
        self.assertEqual(t.compare_databases(pre, db({"domains": "1", "sessions": "9"}))["verdict"],
                         "equal-except-volatile")
        changed = t.compare_databases(pre, db({"domains": "8", "sessions": "2"}))
        self.assertEqual((changed["verdict"], changed["unexpected"]), ("different", ["domains"]))
        self.assertEqual(t.compare_databases(pre, db({"domains": "1", "sessions": "2"}, "t"))["verdict"], "different")
        self.assertEqual(t.compare_databases({"status": "unknown"}, pre)["verdict"], "inconclusive")

    def test_state_comparison_and_overall(self):
        before = {"certbot.timer": {"UnitFileState": "enabled", "ActiveState": "active"}}
        self.assertTrue(t.compare_states(before, json.loads(json.dumps(before)))["equal"])
        self.assertFalse(t.compare_states(before, {"certbot.timer": {"UnitFileState": "disabled",
                                                                       "ActiveState": "inactive"}})["equal"])
        self.assertEqual(t.overall(["passed", "observed", "skipped"]), "complete-for-review")
        self.assertEqual(t.overall(["passed", "inconclusive"]), "inconclusive")
        self.assertEqual(t.overall(["passed", "not-run"]), "incomplete")
        self.assertEqual(t.overall(["failed", "not-run"]), "failed")
        with self.assertRaises(ValueError):
            t.overall(["passed", "PASS"])

    def test_offline_copy_and_shell_command(self):
        copy = t.parse_offline_copy((REPO / "web/src/offline/copy.ts").read_text(encoding="utf-8"))
        self.assertEqual(set(copy["en"]), set(copy["tr"]))
        self.assertIn("status", copy["en"])
        self.assertIn("recovery status --request-id", (REPO / "web/src/offline/page.ts").read_text())
        self.assertEqual(t.shell_status_command(RID, "tr"),
                         f"sudo /usr/libexec/celikpanel/recovery status --request-id {RID} --lang tr")
        with self.assertRaises(ValueError):
            t.shell_status_command("A" * 32, "en")

    def test_attempt_counting(self):
        journal = ("2026-10-01T12:00:01.000000+00:00 host celikpanel-release-recovery-runner[1]: "
                   f"Recovery dispatch admitted: attempt=1 snapshot={SNAPSHOT}\n"
                   "2026-10-01T12:03:01.000000+00:00 host bash[2]: "
                   f"Recovery dispatch admitted: attempt=2 snapshot={SNAPSHOT}\n")
        admitted = t.parse_dispatch_journal(journal)
        self.assertEqual([a["attempt"] for a in admitted], ["1", "2"])
        self.assertEqual(admitted[0]["at"], "2026-10-01T12:00:01.000000+00:00")
        receipts = [{"snapshot": SNAPSHOT, "name": n, "mtime_utc": f"T{n}"} for n in ("2", "1", "3", "owner.AbCd1234")]
        receipts.append({"snapshot": "other", "name": "1", "mtime_utc": "x"})
        counted = t.attempts_from_receipts(receipts, SNAPSHOT)
        self.assertEqual((counted["automatic_count"], counted["owner_count"]), (3, 1))
        self.assertEqual([a["attempt"] for a in counted["automatic"]], ["1", "2", "3"])


class RedactionTests(unittest.TestCase):
    def test_secrets_never_reach_evidence_and_result_is_marked_non_native(self):
        pair = t.pair_modules()
        redactor = pair["redaction"].Redactor()
        password = "owner-password-" + "x" * 20
        mailbox = "mailbox-secret-" + "y" * 20
        for value in (password, mailbox, t.ACCEPTANCE_FIXTURE_KEY):
            redactor.register(value)
        writer_class = t.evidence_writer_class()
        with tempfile.TemporaryDirectory() as directory:
            writer = writer_class(Path(directory), "upd1-debian13-defective-20261001t120000z", redactor)
            cookie = "session-cookie-value-" + "z" * 24
            api = pair["panel_api"]

            def transport(method, path, headers, body, timeout):
                if path == "/api/v1/auth/login":
                    return api.Response(200, [("Set-Cookie", f"celikpanel_session={cookie}; Path=/; HttpOnly")],
                                        json.dumps({"ok": True}).encode())
                if path == "/api/v1/auth/me":
                    assert headers["Cookie"].endswith(cookie)
                    return api.Response(200, [], json.dumps({"username": "labadmin", "role": "admin"}).encode())
                return api.Response(200, [], json.dumps({"echo": mailbox, "auth": headers.get("Cookie")}).encode())
            client = api.PanelClient("owner", "https://127.0.0.1:18443", transport, redactor,
                                     lambda exchange: writer.api_exchange("steps/06-seed", exchange))
            client.login("labadmin", password)
            client.post("/api/v1/domains/1/mail/accounts", {"address": "owner@upd1-owner.test", "password": mailbox})
            with client.polling() as view:
                view.get("/api/v1/panel/update/status?request_id=" + RID)
                with self.assertRaises(api.PollMutationError):
                    client.post("/api/v1/panel/update/start", {"request_id": RID})
            writer.write_json("steps/06-seed/license.json", {"key": t.ACCEPTANCE_FIXTURE_KEY, "text": password})
            with self.assertRaises(Exception):
                writer._write_bytes("steps/06-seed/raw.txt", password.encode())
            result = {"schema": t.RESULT_SCHEMA, "native_evidence": False,
                      "steps": [{"name": "preflight", "verdict": "passed"}], "overall": "complete-for-review"}
            with self.assertRaises(ValueError):
                writer.finalize_upd1(dict(result, native_evidence=True))
            with self.assertRaises(ValueError):
                writer.finalize_upd1(dict(result, overall="failed"))
            writer.finalize_upd1(result)
            everything = "".join(p.read_text() for p in writer.directory.rglob("*") if p.is_file())
            for secret in (password, mailbox, t.ACCEPTANCE_FIXTURE_KEY, cookie):
                self.assertNotIn(secret, everything)
            self.assertIn('"native_evidence": false', (writer.directory / "result.json").read_text())
            self.assertEqual(pair["evidence"].verify_sums(writer.directory), [])


class GuestWorkloadTests(unittest.TestCase):
    def test_dns_query_and_response(self):
        packet = w.dns_query_packet("upd1-owner.test")
        self.assertEqual(packet[:2], b"\x55\x50")
        question = packet[12:]
        name = b"\x0aupd1-owner\x04test\x00"
        rdata = b"\x03ns1\xc0\x0c" + b"\x0ahostmaster\xc0\x0c" + (2026100101).to_bytes(4, "big") + b"\x00" * 16
        answer = b"\xc0\x0c" + (6).to_bytes(2, "big") + (1).to_bytes(2, "big") + (300).to_bytes(4, "big") \
            + len(rdata).to_bytes(2, "big") + rdata
        response = b"\x55\x50" + (0x8400).to_bytes(2, "big") + (1).to_bytes(2, "big") + (1).to_bytes(2, "big") \
            + b"\x00\x00\x00\x00" + question + answer
        self.assertTrue(question.startswith(name))
        self.assertEqual(w.parse_dns_response(response), {"rcode": 0, "authoritative": True, "answers": 1,
                                                          "soa_serial": 2026100101})
        with self.assertRaises(ValueError):
            w.parse_dns_response(b"\x00\x01" + response[2:])
        with self.assertRaises(ValueError):
            w.dns_query_packet("bad name")

    def test_owner_retry_command_is_exactly_the_printed_one(self):
        line = f"sudo /usr/libexec/celikpanel/recovery recover --retry --snapshot {SNAPSHOT}"
        journal = ("Automatic recovery paused after three admitted attempts.\n"
                   "After resolving the cause, the owner may authorize one same-snapshot retry: " + line + "\n") * 2
        self.assertEqual(w.extract_retry_command(journal, SNAPSHOT),
                         f"/usr/libexec/celikpanel/recovery recover --retry --snapshot {SNAPSHOT}")
        other = SNAPSHOT.replace("1" * 32, "2" * 32)
        with self.assertRaisesRegex(ValueError, "different snapshot"):
            w.extract_retry_command(journal + line.replace(SNAPSHOT, other) + "\n", SNAPSHOT)
        with self.assertRaisesRegex(ValueError, "does not show"):
            w.extract_retry_command("sudo /usr/libexec/celikpanel/recovery recover; rm -rf /", SNAPSHOT)
        with self.assertRaises(ValueError):
            w.extract_retry_command(journal, "not-a-snapshot")

    def test_budget_receipt_filter(self):
        good = ("schema=celikpanel-recovery-dispatch/v1\nsnapshot=" + SNAPSHOT + "\nattempt=2\n"
                "token_sha256=" + "4" * 64 + "\noperation=update\nphase=active\n").encode()
        self.assertTrue(w.budget_receipt_safe(good))
        self.assertFalse(w.budget_receipt_safe(good.replace(b"token_sha256=", b"token=")))


class ObserverTests(unittest.TestCase):
    def identity(self):
        return {"nonce": "b" * 64, "vm_uuid": "d61f4a8b-31dd-4bb0-b280-1741c539f4c3", "cell_id": "c", "node": "debian13"}

    def intent(self, mode="checkpoint", fault=None):
        return {"schema": o.INTENT_SCHEMA, "identity": self.identity(), "operation_id": RID, "mode": mode,
                "baseline": {"version": "v0.1.0-alpha.81", "commit": "c" * 40, "agent_sha256": "d" * 64,
                             "panel_sha256": "e" * 64},
                "target": {"version": "v0.1.0-alpha.82", "commit": "3" * 40, "agent_sha256": "f" * 64,
                           "panel_sha256": "0" * 64},
                "recovery_fault": fault if fault is not None or mode == "watch" else o.FAULTS[0]}

    def test_intent_validation(self):
        self.assertEqual(o.validate_intent(self.intent(), self.identity(), RID)["mode"], "checkpoint")
        self.assertEqual(o.validate_intent(self.intent(fault=o.FAULTS[1]), self.identity(), RID)["recovery_fault"],
                         {"action": "kill", "checkpoint": "runtime_verified"})
        self.assertIsNone(o.validate_intent(self.intent("watch"), self.identity(), RID)["recovery_fault"])
        for broken in (dict(self.intent("watch"), recovery_fault=o.FAULTS[0]),
                       dict(self.intent(), recovery_fault={"action": "kill", "checkpoint": "payload_restored"}),
                       dict(self.intent(), mode="kill")):
            with self.assertRaises(o.probe.ProbeError):
                o.validate_intent(broken, self.identity(), RID)

    def test_checkpoint_is_proved_fault_armed_and_worker_released_without_signal(self):
        worker = {"ActiveState": "active", "FreezerState": "running"}
        idle = {"ActiveState": "inactive"}
        marker = {"phase": "active", "operation": "update", "snapshot": SNAPSHOT}
        states = [{"worker": worker, "transaction": None, "recovery": idle}] * 2 \
            + [{"worker": worker, "transaction": marker, "recovery": idle}] * 40 \
            + [{"worker": {"ActiveState": "failed"}, "transaction": marker, "recovery": idle}] * 3
        calls = []

        class Native:
            def __init__(self):
                self.index = 0

            def observe(self):
                value = states[min(self.index, len(states) - 1)]
                self.index += 1
                return value

            def installed(self):
                return {"agent": "f" * 64, "panel": "0" * 64} if self.index >= 4 else None

            def worker_identity(self):
                return {"pid": 42}

            def freeze(self):
                calls.append("freeze")

            def revalidate(self, identity):
                calls.append("revalidate")

            def full_proof(self, snapshot, tick):
                calls.append("proof")
                return {"snapshot": snapshot, "manifest_sha256": "9" * 64}

            def recovery_handoff(self, identity, proof, tick):
                calls.append("handoff")
                return {"intent_sha256": "8" * 64}

            def thaw(self):
                calls.append("thaw")
                return 0

            def cancel_recovery_handoff(self):
                calls.append("cancel")

            def kill(self):  # pragma: no cover - must never be reached
                raise AssertionError("the observer must never kill")
        events = []
        code = o.run_observer(SimpleNamespace(operation_id=RID), self.intent(), lambda e, **f: events.append((e, f)),
                              Native(), pause=lambda _: None)
        kinds = [e for e, _ in events]
        self.assertEqual(code, 0)
        for expected in ("armed", "worker_frozen", "candidate_installed_checkpoint", "recovery_fault_armed",
                         "worker_released", "released"):
            self.assertIn(expected, kinds)
        self.assertLess(kinds.index("recovery_fault_armed"), kinds.index("worker_released"))
        self.assertEqual(calls.count("freeze"), 1)
        self.assertEqual(calls.count("thaw"), 1)
        self.assertNotIn("cancel", calls)
        released = dict(events[-1][1])
        self.assertFalse(released["kill_sent"])
        self.assertTrue(released["checkpoint_verified"])
        self.assertEqual(released["reason"], "worker-exited")

    def test_watch_mode_never_freezes(self):
        states = [{"worker": {"ActiveState": "active"}, "transaction": {"phase": "active", "operation": "update",
                                                                         "snapshot": SNAPSHOT},
                   "recovery": {"ActiveState": "inactive"}}] * 3 + \
                 [{"worker": {"ActiveState": "inactive"}, "transaction": None, "recovery": {"ActiveState": "inactive"}}]
        native = SimpleNamespace(observe=lambda: states.pop(0) if len(states) > 1 else states[0],
                                 installed=lambda: {"agent": "f" * 64}, cancel_recovery_handoff=lambda: None)
        events = []
        code = o.run_observer(SimpleNamespace(operation_id=RID), self.intent("watch"),
                              lambda e, **f: events.append(e), native, pause=lambda _: None)
        self.assertEqual(code, 0)
        self.assertNotIn("worker_frozen", events)
        self.assertEqual(events[-1], "released")


# ---------------------------------------------------------------------------
# upd1 2026-09-30 corrections (H1-H5, L1-L3, origin lookup, cron precondition)
# ---------------------------------------------------------------------------

@unittest.skipUnless(os.name == "posix" and shutil.which("bash"), "the wrapper runs under bash on the Linux host")
class WrapperTests(unittest.TestCase):
    """H1: run-upd1.sh keeps a repository path with spaces as one argument."""

    def harness(self, directory):
        target = Path(directory) / "CELIKBROS PROJECTS" / "celik panel" / "deploy" / "e2e" / "release-recovery"
        target.mkdir(parents=True)
        for name in ("run-upd1.sh", "owner_update_trial.py"):
            shutil.copy2(HERE / name, target / name)
        art = target.parent / "upd1 artifacts.json"
        art.write_text(json.dumps(artifacts(upd3=True)))
        return target, art

    def test_no_command_is_kept_in_an_unquoted_string(self):
        text = (HERE / "run-upd1.sh").read_text()
        self.assertIsNone(re.search(r'\$(DRIVER|LAB)\b(?!\[@\])', text))
        self.assertIsNone(re.search(r'^\s*(DRIVER|LAB)="', text, re.M))
        self.assertIn('DRIVER=(python3 "$HERE/owner_update_trial.py")', text)

    def test_dry_run_from_a_path_with_spaces(self):
        with tempfile.TemporaryDirectory() as directory:
            target, art = self.harness(directory)
            done = subprocess.run(["bash", str(target / "run-upd1.sh"), "dry-run", "upd1-debian13-defective", str(art),
                                   "upd1-d13-def-a"], capture_output=True, text=True, cwd=directory, timeout=60)
            self.assertEqual(done.returncode, 0, done.stderr)
            plan = json.loads(done.stdout)
            self.assertEqual(plan["dns"]["verdict"], t.DNS_NOT_PROVIDED)
            self.assertEqual(plan["draft_choices"], {"dns_mode": "external", "peer_ip": "", "peer_ns": ""})
            self.assertFalse(plan["native_evidence"])
            for cell in ("upd1-debian13-startcheck", "upd1-arch-startcheck", "upd1-debian13-realstart",
                         "upd1-arch-realstart", "upd1-debian13-owner-continuation", "upd1-arch-owner-continuation",
                         "upd1-debian13-mgmt-off-reboot", "upd1-arch-mgmt-off-reboot"):
                with self.subTest(cell=cell):
                    done = subprocess.run(["bash", str(target / "run-upd1.sh"), "dry-run", cell, str(art),
                                           "upd3-x"], capture_output=True, text=True, cwd=directory, timeout=60)
                    self.assertEqual(done.returncode, 0, done.stderr)
                    self.assertEqual(json.loads(done.stdout)["cell"]["name"], cell)

    @unittest.skipUnless(os.name == "posix" and os.geteuid() == 0, "the cell command refuses non-root")
    def test_cell_passes_every_path_and_owner_choice_as_one_argument(self):
        with tempfile.TemporaryDirectory() as directory:
            target, art = self.harness(directory)
            fake = Path(directory) / "fake bin"
            fake.mkdir()
            log = Path(directory) / "calls.log"
            (fake / "python3").write_text('#!/bin/sh\n{ for a in "$@"; do printf \'%s\\037\' "$a"; done; '
                                          'printf \'\\036\'; } >> "$UPD1_TEST_LOG"\n')
            (fake / "python3").chmod(0o755)
            draft = Path(directory) / "owner draft.json"
            draft.write_text(json.dumps({"peer_ip": "192.0.2.11", "peer_ns": "ns2.upd1-infra.test"}))
            name = "upd1-wrapper-test-" + secrets.token_hex(6)
            self.assertFalse(Path("/var/tmp/cp-release-drill-" + name).exists())
            env = dict(os.environ, PATH=str(fake) + os.pathsep + os.environ.get("PATH", ""), UPD1_TEST_LOG=str(log),
                       UPD1_DNS_MODE="local", UPD1_SETUP_DRAFT_JSON=str(draft))
            done = subprocess.run(["bash", str(target / "run-upd1.sh"), "cell", "upd1-debian13-good", str(art), name,
                                   "2371"], capture_output=True, text=True, cwd=directory, env=env, timeout=60)
            self.assertEqual(done.returncode, 0, done.stderr)
            calls = [record.split("\x1f")[:-1] for record in log.read_text().split("\x1e") if record]
            self.assertEqual([c[0] for c in calls], [str(target / "owner_update_trial.py")] + [str(target / "lab.py")] * 3
                             + [str(target / "owner_update_trial.py"), str(target / "lab.py")])
            self.assertEqual([c[1] for c in calls], ["plan", "prepare", "start", "status", "run", "stop"])
            for call in (calls[0], calls[4]):
                self.assertIn(str(art), call)
                self.assertEqual(call[call.index("--dns-mode") + 1], "local")
                self.assertEqual(call[call.index("--setup-draft-json") + 1], str(draft))
            self.assertIn("dns_mode=local", done.stdout)
            self.assertFalse(Path("/var/tmp/cp-release-drill-" + name).exists())


class DNSScopeTests(unittest.TestCase):
    """H3/H5: external DNS by default; local only with the paired identity."""

    def test_external_is_the_default_and_matches_the_run_copy_choice(self):
        run_copy = json.loads((RUN_EVIDENCE / "harness-run-copy" / "draft-external.json").read_text())
        self.assertEqual(t.DEFAULT_DNS_MODE, "external")
        self.assertEqual(t.setup_draft_choice("external", None), run_copy)
        trial = object.__new__(t.Trial)
        trial.node_name, trial.setup_draft_override = "debian13", t.setup_draft_choice("external", None)
        draft = trial.draft("web_mail", "192.0.2.10")
        self.assertEqual((draft["dns_mode"], draft["peer_ip"], draft["peer_ns"], draft["purpose"]),
                         ("external", "", "", "web_mail"))
        self.assertEqual(draft["panel_domain"], "panel-debian13.upd1-infra.test")

    def test_draft_choice_refusals(self):
        peers = json.loads((RUN_EVIDENCE / "harness-run-copy" / "draft-debian13.json").read_text())
        self.assertEqual(t.setup_draft_choice("local", peers), dict(peers, dns_mode="local"))
        for mode, override, message in (
                ("local", None, "server_setup_dns_identity_required"),
                ("local", {"peer_ip": "192.0.2.11"}, "peer_ip and peer_ns"),
                ("external", peers, "no peer identity"),
                ("external", {"dns_mode": "local"}, "conflicts"),
                ("external", {"purpose": "web"}, "other than purpose"),
                ("remote", None, "--dns-mode")):
            with self.subTest(mode=mode, override=override), self.assertRaisesRegex(ValueError, message):
                t.setup_draft_choice(mode, override)

    def test_plan_cli_records_the_dns_scope(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "a.json"
            path.write_text(json.dumps(artifacts()))
            base = ["plan", "--cell", "upd1-debian13-good", "--artifacts", str(path),
                    "--work-root", "/var/tmp/cp-release-drill-upd1-x", "--dry-run"]
            out = io.StringIO()
            with redirect_stdout(out):
                self.assertEqual(t.main(base), 0)
            plan = json.loads(out.getvalue())
            self.assertEqual(plan["dns"]["mode"], "external")
            self.assertIn("item 2", plan["dns"]["note"])
            with self.assertRaises(SystemExit), redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
                t.main(base + ["--dns-mode", "local"])
            draft = RUN_EVIDENCE / "harness-run-copy" / "draft-debian13.json"
            out = io.StringIO()
            with redirect_stdout(out):
                self.assertEqual(t.main(base + ["--dns-mode", "local", "--setup-draft-json", str(draft)]), 0)
            self.assertEqual(json.loads(out.getvalue())["draft_choices"]["peer_ip"], "192.0.2.11")

    def test_external_dns_is_never_counted_as_passed(self):
        def sample(moment, dns_ok):
            return {"t": moment, "web": {"ok": True}, "dns": {"ok": dns_ok}, "panel": {"ok": True}}
        for dns_ok in (True, False):
            samples = [sample(i * 5.0, dns_ok) for i in range(10)]
            per = t.workload_verdicts(samples, [], mail_listed=False, cron="measured", dns_mode="external")
            self.assertEqual(per["dns"]["verdict"], t.DNS_NOT_PROVIDED)
            self.assertNotIn("windows", per["dns"])
            self.assertEqual(per["web"]["verdict"], "never-interrupted")
        local = t.workload_verdicts([sample(0, True), sample(5, False), sample(10, True)], [], mail_listed=False,
                                    cron="measured", dns_mode="local")
        self.assertEqual(local["dns"]["verdict"], "interrupted")
        scope = t.result_scope("external", None, None, None)
        self.assertEqual(scope["dns"]["verdict"], t.DNS_NOT_PROVIDED)
        self.assertIn("stopped before seeding", scope["cron"]["verdict"])


class SetupWaitTests(unittest.TestCase):
    """H4: a stable wait at access_dns settles the setup step as observed."""

    def ex(self, status, phase="access_dns"):
        return {"status": status, "phase": phase}

    def test_settle_phases_match_the_pair_driver(self):
        source = (REPO / "deploy/e2e/dns-pair-acceptance/pair_acceptance.py").read_text()
        literal = re.search(r"NON_DNS_SETUP_PHASES = frozenset\((\{[^}]*\})\)", source).group(1)
        self.assertEqual(t.SETUP_SETTLE_PHASES, frozenset(ast.literal_eval(literal)))

    def test_retry_flips_to_running_do_not_restart_the_clock(self):
        wait = t.SetupWait()
        observed = [wait.observe(self.ex(s), now) for now, s in
                    ((0, "running"), (25, "waiting"), (50, "running"), (100, "waiting"), (119, "waiting"))]
        self.assertEqual(observed, ["continue"] * 5)
        self.assertEqual(wait.observe(self.ex("running"), 125), "continue")  # the latest read must say waiting
        self.assertEqual(wait.observe(self.ex("waiting"), 126), "settled")
        self.assertEqual(wait.seconds(126), 126.0)

    def test_phase_change_error_or_other_phase_restarts_or_never_settles(self):
        wait = t.SetupWait()
        wait.observe(self.ex("waiting"), 0)
        wait.observe(self.ex("waiting", "panel_certificate"), 60)
        self.assertEqual(wait.observe(self.ex("waiting", "panel_certificate"), 150), "continue")
        self.assertEqual(wait.observe(self.ex("waiting", "panel_certificate"), 181), "settled")
        other = t.SetupWait()
        for now in range(0, 1000, 25):
            self.assertEqual(other.observe(self.ex("waiting", "mail_profile"), now), "continue")
        reset = t.SetupWait()
        reset.observe(self.ex("waiting"), 0)
        reset.observe({"poll_error": "timeout"}, 60)
        self.assertEqual(reset.observe(self.ex("waiting"), 130), "continue")
        self.assertEqual(t.SetupWait().observe(self.ex("failed", "mail_profile"), 0), "terminal")
        few = t.SetupWait()
        few.observe(self.ex("waiting"), 0)
        self.assertEqual(few.observe(self.ex("waiting"), 500), "continue")  # two reads are not a stable wait

    def test_mail_requirement_follows_the_recorded_setup(self):
        waited_early = json.loads((RUN_EVIDENCE / "upd1-debian13-good/run-b/steps/06-setup/setup-execution.json").read_text())
        waited_late = json.loads((RUN_EVIDENCE / "upd1-debian13-defective/run-c/steps/06-setup/setup-execution.json").read_text())
        self.assertFalse(t.mail_steps_reached(waited_early))
        self.assertTrue(t.mail_steps_reached(waited_late))
        self.assertTrue(t.mail_steps_reached(None))


class DatabaseVolatileTests(unittest.TestCase):
    """L2: the waiting setup's table is excluded, and listed, only when the wait was recorded."""

    def db(self, tables):
        return {"semantic": {"schema_sha256": "s", "sha256": "x",
                             "tables": [{"name": n, "sha256": v} for n, v in tables.items()]}}

    def test_waiting_setup_rows_are_excluded_explicitly(self):
        pre = self.db({"domains": "1", "server_setup_executions": "a", "server_setup_state": "s"})
        post = self.db({"domains": "1", "server_setup_executions": "b", "server_setup_state": "s"})
        waited = t.compare_databases(pre, post, t.volatile_tables(True))
        self.assertEqual(waited["verdict"], "equal-except-volatile")
        self.assertIn("server_setup_executions", waited["volatile_excluded"])
        self.assertIn("claimServerSetupDNSRetry", waited["volatile_reasons"]["server_setup_executions"])
        self.assertNotIn("server_setup_state", waited["volatile_excluded"])
        plain = t.compare_databases(pre, post, t.volatile_tables(False))
        self.assertEqual((plain["verdict"], plain["unexpected"]), ("different", ["server_setup_executions"]))
        state = t.compare_databases(pre, self.db({"domains": "1", "server_setup_executions": "b",
                                                  "server_setup_state": "t"}), t.volatile_tables(True))
        self.assertEqual(state["unexpected"], ["server_setup_state"])

    def test_the_waiting_writer_is_the_product_table(self):
        retry = (REPO / "cmd/panel/server_setup_dns_retry.go").read_text()
        self.assertIn("UPDATE server_setup_executions SET status='running'", retry)


class CronPreconditionTests(unittest.TestCase):
    def test_absent_crontab_is_recorded_not_seeded(self):
        absent = t.cron_availability({"crontab": None, "units": {"cron.service": {"LoadState": "not-found"},
                                                                  "cronie.service": {"LoadState": "not-found"}}})
        self.assertEqual((absent["available"], absent["verdict"], absent["daemon_units"]),
                         (False, "not available on this baseline", {}))
        present = t.cron_availability({"crontab": "/usr/bin/crontab", "units": {
            "cron.service": {"LoadState": "loaded", "ActiveState": "active", "UnitFileState": "enabled"}}})
        self.assertEqual((present["available"], present["daemon_active"]), (True, True))
        trial = object.__new__(t.Trial)
        trial.state = {"cron_availability": absent}
        self.assertEqual(trial.cron_scope(), t.CRON_NOT_AVAILABLE)
        trial.state = {"cron_availability": present, "cron_precondition": True}
        self.assertEqual(trial.cron_scope(), "measured")
        per = t.workload_verdicts([], [], mail_listed=False, cron=t.CRON_NOT_AVAILABLE, dns_mode="external")
        self.assertEqual(per["cron"]["verdict"], "not-available-on-baseline")
        self.assertEqual(t.result_scope("external", absent, None, None)["cron"]["verdict"], t.CRON_NOT_AVAILABLE)

    def test_the_agent_gate_is_crontab_on_path(self):
        # The Agent refuses cron work when `crontab` is not on PATH (wording may change with the P1 fix).
        agent = (REPO / "cmd/agent/cron_rpc.go").read_text(encoding="utf-8")
        self.assertIn('LookPath("crontab")', agent)


class OriginTests(unittest.TestCase):
    """L1 (persistent origin unit) and the no-real-lookup rule."""

    NONCE = "b" * 64

    def test_origin_unit_is_enabled_persistent_and_named_outside_the_installer_globs(self):
        unit = w.origin_unit_text(self.NONCE)
        self.assertIn("\n[Install]\nWantedBy=multi-user.target\n", unit)
        self.assertIn("Restart=on-failure", unit)
        self.assertIn("ExecStart=/usr/bin/python3 -I /root/celikpanel-release-recovery-lab/worker-fixture-origin.py "
                      "guest-serve --nonce " + self.NONCE + "\n", unit)
        with self.assertRaises(ValueError):
            w.origin_unit_text("x; rm -rf /")
        self.assertEqual(t.ORIGIN_UNIT, w.ORIGIN_UNIT)
        self.assertEqual(t.SAMPLER_UNIT, w.SAMPLER_UNIT)
        getsh = (REPO / "download-portal/get.sh").read_text()
        globs = re.findall(r"(/[\w/]+/systemd/system/celikpanel-\*)", getsh)
        self.assertGreaterEqual(len(globs), 4)
        for pattern in globs:
            self.assertFalse(fnmatch.fnmatch("/etc/systemd/system/" + t.ORIGIN_UNIT, pattern))
            # The old unit name would have told the installer an install had already started.
            self.assertTrue(fnmatch.fnmatch(pattern.split("/celikpanel-")[0] + "/celikpanel-lab-upd1-origin.service",
                                            pattern))

    def test_driver_no_longer_starts_a_transient_origin_or_resolves_before_provisioning(self):
        self.assertNotIn("systemd-run", inspect.getsource(t.Trial.origin))
        self.assertNotIn("getent", inspect.getsource(t.Trial.preflight))
        self.assertIn("origin_check(\"before-arm\")", inspect.getsource(t.Trial.arm))
        self.assertIn("origin_check(\"after-owner-restart\")", inspect.getsource(t.Trial.baseline_install))

    def test_guest_lookup_and_unit_refuse_before_provisioning(self):
        with tempfile.TemporaryDirectory() as directory:
            calls = []
            missing = Path(directory) / "worker-origin-provisioned.json"
            with mock.patch.object(w, "ORIGIN_PROVISIONED", missing), \
                    mock.patch.object(w, "run", lambda *a, **k: calls.append(a) or {}), \
                    mock.patch.object(w, "private_root", lambda: Path(directory)):
                with self.assertRaisesRegex(ValueError, "not looked up"):
                    w.origin_check()
                with self.assertRaisesRegex(ValueError, "not provisioned"):
                    w.install_origin({"nonce": self.NONCE})
            self.assertEqual(calls, [])

    def test_origin_verdict(self):
        def check(getent, http):
            return {"getent": {"stdout": getent}, "https": {"stdout": http}, "unit": {"ActiveState": "active"},
                    "boot_id": "b"}
        self.assertTrue(t.origin_verdict(check("127.0.0.1       celikpanel.net\n", "200"))["ok"])
        for getent, http in (("185.95.0.123    celikpanel.net\n", "200"),
                             ("127.0.0.1 celikpanel.net\n185.95.0.123 celikpanel.net\n", "200"),
                             ("", "200"), ("127.0.0.1 celikpanel.net\n", "000")):
            with self.subTest(getent=getent, http=http):
                self.assertFalse(t.origin_verdict(check(getent, http))["ok"])

    def test_hosts_mappings_read_the_file_only(self):
        text = ("127.0.0.1 localhost\n# 185.95.0.123 celikpanel.net\n::1 ip6-localhost\n"
                "127.0.0.1 celikpanel.net # disposable CelikPanel worker fixture\n")
        self.assertEqual(t.hosts_mappings(text), ["127.0.0.1 celikpanel.net # disposable CelikPanel worker fixture"])
        self.assertEqual(t.hosts_mappings("127.0.0.1 localhost\n"), [])

    def test_journal_units_accept_globs_only_as_unit_patterns(self):
        seen = []
        with mock.patch.object(w, "run", lambda argv, **k: seen.append(argv) or {"status": "ok", "stdout": ""}):
            w.journal(["php*-fpm.service", t.ORIGIN_UNIT], "-12h", 100)
            with self.assertRaises(ValueError):
                w.journal(["x.service; rm -rf /"], "-12h", 100)
        self.assertIn("php*-fpm.service", seen[0])


class CollectAfterEarlyStopTests(unittest.TestCase):
    """L3: a cell stopped at seed (or earlier) still keeps its journals and observations."""

    class Fake(t.Trial):
        def __init__(self, directory, stop):  # noqa: D401 - no lab, no guest
            pair = t.pair_modules()
            self.redactor = pair["redaction"].Redactor()
            self.ev = t.evidence_writer_class()(Path(directory), "upd1-debian13-good-20261001t120000z", self.redactor)
            self.cell, self.node_name = t.CELLS["upd1-debian13-good"], "debian13"
            self.identity = {"cell_id": "c", "node": "debian13", "vm_uuid": "u", "nonce": "b" * 64}
            self.artifacts, self.dns_mode = artifacts(), "external"
            self.setup_draft_override = t.setup_draft_choice("external", None)
            self.steps, self.step_dir, self.state = [], "steps/00-run", {"findings": [], "resets": []}
            self.host_samples, self.stop_host_loop = [], threading.Event()
            self.tunnel = SimpleNamespace(close=lambda: None)
            self.stop, self.calls = stop, []

        def workload(self, mode, *args, timeout=120):
            self.calls.append((mode, args))
            if mode == "samples":
                return {"jsonl_base64": "", "next_offset": 0}
            if mode == "journal":
                return {"stdout": "journal of " + " ".join(args[4::2]) + "\n"}
            if mode == "budget":
                return {"present": False, "receipts": []}
            raise AssertionError(mode)

        def guest(self, body, timeout=120):
            self.calls.append(("guest", body))
            return SimpleNamespace(stdout=json.dumps({"directory_present": False, "records": {}}))

        def _stage(self, name):
            if name == self.stop:
                raise t.StepFailed(f"{name} stopped here")
            if name == "preflight":
                self.state["helpers_uploaded"] = True
            return "passed"

        def preflight(self, checks): return self._stage("preflight")
        def origin(self, checks): return self._stage("origin")
        def baseline_install(self, checks): return self._stage("baseline-install")
        def owner_login(self, checks): return self._stage("owner-login")
        def license(self, checks): return self._stage("license")
        def setup(self, checks): return self._stage("setup")
        def seed(self, checks): return self._stage("seed")

    def run_fake(self, stop):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        trial = self.Fake(directory.name, stop)
        with redirect_stdout(io.StringIO()):
            result = trial.execute()
        return trial, result, {s["name"]: s["verdict"] for s in result["steps"]}

    def test_collect_runs_after_a_stop_at_seed(self):
        trial, result, verdicts = self.run_fake("seed")
        self.assertEqual((verdicts["seed"], verdicts["collect"], verdicts["verdicts"]), ("failed", "passed", "not-run"))
        self.assertEqual(result["overall"], "failed")
        collect = next(p for p in trial.ev.directory.glob("steps/*-collect") if p.is_dir())
        journals = {p.name: p.read_text() for p in collect.glob("journal-*.txt")}
        self.assertEqual(sorted(journals), ["journal-lab.txt", "journal-product.txt", "journal-setup-services.txt"])
        self.assertIn(t.ORIGIN_UNIT, journals["journal-lab.txt"])
        self.assertIn(t.BASELINE_INSTALL_UNIT, journals["journal-lab.txt"])
        self.assertIn("celikpanel-panel.service celikpanel-agent.service", journals["journal-product.txt"])
        self.assertIn("php*-fpm.service", journals["journal-setup-services.txt"])
        self.assertTrue((collect / "observation-records.json").is_file())
        step = json.loads((collect / "step.json").read_text())
        self.assertEqual(step["checks"]["stopped_after"], ["seed"])
        self.assertEqual(result["scope"]["dns"]["verdict"], t.DNS_NOT_PROVIDED)

    def test_collect_runs_after_a_stop_at_origin_and_skips_only_an_unprepared_guest(self):
        trial, _, verdicts = self.run_fake("origin")
        self.assertEqual(verdicts["collect"], "passed")
        _, _, verdicts = self.run_fake("preflight")
        self.assertEqual(verdicts["collect"], "skipped")

    def test_observation_listing_without_a_request(self):
        script = t.observation_records_script(None)
        self.assertIn("root.glob('*')", script)
        self.assertIn("withheld", script)
        with self.assertRaises(ValueError):
            t.observation_records_script("x' ; rm -rf / #")


# ---------------------------------------------------------------------------
# upd2 corrections made permanent (H6, H7, O5)
# ---------------------------------------------------------------------------

class Upd2CorrectionTests(unittest.TestCase):
    def owner_start_trial(self, status, body):
        trial = object.__new__(t.Trial)
        saved = []
        trial.state = {"request_id": RID, "check": {"current_version": t.BASELINE_VERSION, "current_commit": "1" * 40,
                                                    "target": {"version": t.CANDIDATE_VERSION}}}
        trial.node_name, trial.root = "debian13", Path("/nonexistent")
        trial.trial = SimpleNamespace(save=lambda *a: saved.append(a))
        trial.p = {"panel_api": SimpleNamespace(UnknownOutcome=type("UnknownOutcome", (Exception,), {}))}
        calls = []

        def api(method, path, body_=None, **kwargs):
            calls.append((method, path))
            if path == "/api/v1/host-mutation-readiness":
                return SimpleNamespace(status=200, json=lambda: {"ready": True})
            return SimpleNamespace(status=status, json=lambda: body)
        trial.api = api
        return trial, calls

    def test_h6_owner_start_accepts_202_and_200_only_with_accepted_true(self):
        # The product's own status for an accepted start (cmd/panel/system_update_handlers.go).
        handlers = (REPO / "cmd/panel/system_update_handlers.go").read_text(encoding="utf-8")
        self.assertIn("http.StatusAccepted", handlers)
        self.assertIn('`json:"accepted"`', handlers)
        for status, body, verdict in ((202, {"accepted": True, "status": "queued"}, "passed"),
                                      (200, {"accepted": True, "status": "running"}, "passed"),
                                      (202, {"accepted": False}, "failed"), (202, None, "failed"),
                                      (409, {"code": "PANEL_UPDATE_BUSY"}, "failed"),
                                      (201, {"accepted": True}, "failed")):
            with self.subTest(status=status, body=body):
                trial, calls = self.owner_start_trial(status, body)
                checks = {}
                if verdict == "passed":
                    self.assertEqual(trial.owner_start(checks), "passed")
                    self.assertIn("start_answered_at", trial.state)
                else:
                    with self.assertRaises(t.StepFailed):
                        trial.owner_start(checks)
                self.assertEqual([c for c in calls if c[0] == "POST"], [("POST", "/api/v1/panel/update/start")])
                self.assertEqual(checks["start"]["http"], status)

    def test_h7_arch_getent_prints_the_loopback_under_localhost(self):
        def check(getent, http="200"):
            return {"getent": {"status": "ok", "returncode": 0, "stdout": getent}, "https": {"stdout": http},
                    "unit": {"ActiveState": "active"}, "boot_id": "b"}
        arch = t.origin_verdict(check("127.0.0.1       localhost\n"))
        self.assertTrue(arch["ok"], arch)
        self.assertEqual(arch["addresses"], ["127.0.0.1"])
        self.assertEqual(arch["getent_stdout"], "127.0.0.1       localhost\n")
        self.assertEqual((arch["getent_status"], arch["getent_returncode"]), ("ok", 0))
        for getent in ("185.95.0.123    celikpanel.net\n", "127.0.0.1 localhost\n185.95.0.123 celikpanel.net\n",
                       "", "::1 localhost\n"):
            with self.subTest(getent=getent):
                self.assertFalse(t.origin_verdict(check(getent))["ok"])
        self.assertFalse(t.origin_verdict(check("127.0.0.1 localhost\n", "000"))["ok"])

    def known(self, phase, **extra):
        return dict({"request_id": RID, "observation": "known", "phase": phase, "terminal_proof": "none"}, **extra)

    def test_o5_start_instant_lag_is_recorded_not_a_disagreement(self):
        shell = {"reference": RID, "status_command": t.shell_status_command(RID, "en")}
        lag = t.status_agreement(RID, self.known("accepted"), self.known("running"), shell, start_instant=True)
        self.assertEqual(lag["verdict"], "start-instant-lag")
        self.assertIn("start-instant lag", lag["lag"])
        self.assertEqual(lag["reasons"], [])
        later = t.status_agreement(RID, self.known("accepted"), self.known("running"), shell)
        self.assertEqual(later["verdict"], "disagree")
        for api, cli in ((self.known("running"), self.known("accepted")),       # reversed order
                         (self.known("accepted"), self.known("recovering")),    # not the adjacent step
                         (self.known("accepted"), self.known("running", previous_failure="update_failed"))):
            with self.subTest(api=api["phase"], cli=cli["phase"]):
                self.assertEqual(t.status_agreement(RID, api, cli, shell, start_instant=True)["verdict"], "disagree")
        bad_shell = t.status_agreement(RID, self.known("accepted"), self.known("running"),
                                       {"reference": RID, "status_command": "x"}, start_instant=True)
        self.assertEqual(bad_shell["verdict"], "disagree")
        a, lagged = {"verdict": "agree"}, {"verdict": "start-instant-lag"}
        summary = t.agreement_verdict([lagged, a, a])
        self.assertEqual((summary["verdict"], summary["lag_samples"], summary["agreed_samples"]), ("passed", 1, 2))
        self.assertEqual(t.agreement_verdict([lagged])["verdict"], "inconclusive")

    def test_o5_only_the_first_poll_interval_counts_as_the_start_instant(self):
        source = inspect.getsource(t.Trial.status_sample)
        self.assertIn("start_instant = index == 0 or", source)
        self.assertIn("POLL_MIN_MS / 1000.0", source)


# ---------------------------------------------------------------------------
# upd3: candidate-panel start kinds (start-check, real-start)
# ---------------------------------------------------------------------------

def cli_texts():
    return t.parse_cli_guidance((REPO / "cmd/recovery/main.go").read_text(encoding="utf-8"))


PHASE_REASON = {"recovered": "rollback_verified", "succeeded": "update_verified", "failed": "update_failed",
                "recovering": "recovery_running", "recovery_required": "recovery_incomplete",
                "running": "update_running", "accepted": "operation_accepted"}


def product_status(status):
    """A status as the product prints it (schema, observed_at, the phase's reason), so the build's own
    parseRecoveryObservation accepts it (H10: the screen is modelled by the build's functions)."""
    if not isinstance(status, dict):
        return status
    value = dict(status)
    value.setdefault("schema", "celikpanel-recovery-status/v1")
    value.setdefault("observed_at", "2026-10-01T12:00:00Z")
    if value.get("observation") == "known":
        value.setdefault("reason", PHASE_REASON.get(value.get("phase")))
    return value


def cli_sample(status, texts, utc="2026-10-01T12:00:00Z"):
    """A cli-status sample whose EN/TR output is the product text writeStatus would print."""
    status = product_status(status)
    key = t.cli_guidance_key(status)
    out = {}
    for lang in ("en", "tr"):
        lines = [texts[key][lang] if key else "generic"]
        if status.get("failure_code") and status.get("previous_failure") == "update_failed":
            lines.append(texts["cause_markers"][lang][0].format(code=status["failure_code"]))
        out[lang] = {"stdout": "\n".join(lines) + "\n"}
    return {"utc": utc, "observed": status, "cli": dict(out, json={"stdout": json.dumps(status)})}


class Upd3CellTests(unittest.TestCase):
    OLD = {
        "upd1-debian13-defective": t.Cell("upd1-debian13-defective", "debian13", "defective",
                                          {"action": "reboot", "checkpoint": "payload_restored"}, True),
        "upd1-debian13-good": t.Cell("upd1-debian13-good", "debian13", "good", None, True),
        "upd1-arch-defective": t.Cell("upd1-arch-defective", "arch", "defective",
                                      {"action": "kill", "checkpoint": "runtime_verified"}, False),
        "upd1-arch-good": t.Cell("upd1-arch-good", "arch", "good", None, False)}

    def plan(self, cell, document=None):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "a.json"
            path.write_text(json.dumps(document or artifacts(upd3=True)))
            out = io.StringIO()
            with redirect_stdout(out):
                self.assertEqual(t.main(["plan", "--cell", cell, "--artifacts", str(path),
                                         "--work-root", "/var/tmp/cp-release-drill-upd3-x", "--dry-run"]), 0)
            return json.loads(out.getvalue())

    def test_good_and_migrate_only_cells_are_unchanged(self):
        for name, cell in self.OLD.items():
            with self.subTest(cell=name):
                self.assertEqual(t.CELLS[name], cell)
                self.assertIn(t.candidate_role(cell), ("good", "defective"))
                self.assertEqual(t.cell_roles(cell), ("baseline", "good", "defective"))
                self.assertIs(t.provenance_for(cell.variant), t.PROVENANCE)
                plan = self.plan(name, artifacts())   # a pre-upd3 artifacts document still serves them
                self.assertNotIn("defect", plan)
                self.assertNotIn("kind-expectation", [s["name"] for s in plan["steps"]])
                steps = {s["name"]: s["does"] for s in plan["steps"]}
                self.assertTrue(steps["owner-continuation (required)"].startswith(
                    "only if the product reports paused_retry_limit"))
                self.assertTrue(steps["terminal"].startswith("installed/running identity"))
                self.assertEqual(plan["expected_outcome"], "succeeded (update_verified)" if cell.variant == "good"
                                 else "recovered (rollback_verified, previous_failure=update_failed), automatically "
                                      "or after the owner's one-time retry")
        self.assertEqual(t.apply_defect((REPO / "cmd/panel/main.go").read_text()).count("upd1 fixture defect:"), 1)

    def test_plan_validation_of_the_four_new_cells(self):
        expected = {"upd1-debian13-startcheck": ("start-check", "4" * 40, {"action": "reboot",
                                                                           "checkpoint": "payload_restored"}),
                    "upd1-arch-startcheck": ("start-check", "4" * 40, {"action": "kill", "checkpoint": "runtime_verified"}),
                    "upd1-debian13-realstart": ("real-start", "5" * 40, None),
                    "upd1-arch-realstart": ("real-start", "5" * 40, None)}
        for name, (variant, commit, fault) in expected.items():
            with self.subTest(cell=name):
                plan = self.plan(name)
                self.assertEqual(plan["cell"]["variant"], variant)
                self.assertEqual(plan["candidate"]["commit"], commit)
                self.assertEqual(plan["cell"]["recovery_fault"], fault)
                self.assertFalse(plan["native_evidence"])
                self.assertEqual(plan["defect"]["kind"], variant)
                self.assertEqual(plan["provenance"]["defect"], t.KIND_PROVENANCE[variant])
                steps = {s["name"]: s["does"] for s in plan["steps"]}
                self.assertIn("kind-expectation", steps)
                if variant == "start-check":
                    self.assertIn("completion.pending is never created", plan["expected_outcome"])
                    self.assertIn(t.START_CHECK_CODE, plan["expected_outcome"])
                    self.assertEqual(plan["defect"]["file"], "cmd/panel/server_lifecycle.go")
                else:
                    self.assertIn("no rollback", plan["expected_outcome"])
                    self.assertIn(t.REAL_START_CODE, plan["expected_outcome"])
                    self.assertEqual(plan["defect"]["owner_retry"], "not run")
                    self.assertIn("NOT run", steps["owner-continuation (required)"])
                    self.assertEqual(plan["defect"]["file"], "cmd/panel/main.go")
                    self.assertIn("expected down from the update until the end", steps["verdicts"])
                with self.assertRaisesRegex(ValueError, "rebuild with build-upd1-artifacts.sh"):
                    self.plan(name, artifacts())

    def test_artifact_rules_for_the_start_kinds(self):
        self.assertEqual(t.validate_artifacts(artifacts(upd3=True), check_files=False)["startcheck"]["commit"],
                         "4" * 40)
        for mutate, message in ((lambda d: d["startcheck"].update(parent="3" * 40), "good <- startcheck"),
                                (lambda d: d["realstart"].update(commit="4" * 40), "distinct"),
                                (lambda d: d["realstart"].update(version="v0.1.0-alpha.81"), "labelled"),
                                (lambda d: d["startcheck"].update(license_mode="customer"), "acceptance-license")):
            broken = artifacts(upd3=True)
            mutate(broken)
            with self.subTest(message=message), self.assertRaisesRegex(ValueError, message):
                t.validate_artifacts(broken, check_files=False)
        cell = t.CELLS["upd1-arch-realstart"]
        self.assertEqual(t.cell_roles(cell), ("baseline", "good", "defective", "realstart"))
        with self.assertRaisesRegex(ValueError, "artifact realstart is missing"):
            t.validate_cell_artifacts(artifacts(), cell, check_files=False)

    def test_classification_of_the_new_variants(self):
        recovered = {"observation": "known", "phase": "recovered", "terminal_proof": "rollback_verified"}
        succeeded = {"observation": "known", "phase": "succeeded", "terminal_proof": "update_verified"}
        paused = {"observation": "known", "phase": "recovery_required", "terminal_proof": "none",
                  "automatic_recovery": "paused_retry_limit"}
        self.assertEqual(t.classify_outcome("start-check", recovered, False), "recovered-automatically")
        self.assertEqual(t.classify_outcome("start-check", succeeded, False), "defective-candidate-reported-success")
        self.assertEqual(t.classify_outcome("real-start", paused, False), "paused-owner-action-required")
        self.assertEqual(t.classify_outcome("real-start", recovered, False), "real-start-candidate-rolled-back")
        self.assertEqual(t.classify_outcome("real-start", succeeded, False), "real-start-candidate-reported-success")


class Upd3FixturePatchTests(unittest.TestCase):
    def test_start_check_patch_sits_in_the_function_the_check_and_the_real_start_share(self):
        source = (REPO / t.START_CHECK_FILE).read_text(encoding="utf-8")
        changed = t.apply_kind_patch("start-check", source)
        self.assertEqual(changed.count("upd3 fixture defect"), 1)
        body = changed.split("func configurePanelHTTPTLS(", 1)[1].split("\n}\n", 1)[0]
        self.assertIn("upd3 fixture defect: this candidate cannot prepare its panel TLS listener", body)
        self.assertIn("tlsOn, err := configurePanelHTTPTLS(server, certPath, keyPath)", changed)   # real start
        readiness = (REPO / "cmd/panel/startup_readiness.go").read_text(encoding="utf-8")
        self.assertIn("configurePanelHTTPTLS(server, certPath, keyPath)", readiness)                # the check
        self.assertIn('"tls_pair_invalid"', readiness.split("configurePanelHTTPTLS(server, certPath, keyPath)", 1)[1]
                      .split("\n\t}\n", 1)[0])
        self.assertEqual(t.START_CHECK_FIXTURE_REASON, "tls_pair_invalid")
        with self.assertRaisesRegex(ValueError, "start-check"):
            t.apply_kind_patch("start-check", changed)

    def test_real_start_patch_is_after_every_early_exit_and_outside_the_check(self):
        source = (REPO / t.REAL_START_FILE).read_text(encoding="utf-8")
        changed = t.apply_kind_patch("real-start", source)
        fatal = changed.index("upd3 fixture defect: this candidate exits before its panel listener starts")
        main = changed.index("\nfunc main() {")
        for early in ("emitPanelBuildIdentity(os.Args[1:], os.Stdout)", "runStartupReadinessEntry(os.Args[1:]",
                      "if *migrateOnlyFlag {", "if *countUsersFlag {", "if *createAdmin {"):
            with self.subTest(early=early):
                self.assertLess(main, changed.index(early))
                self.assertLess(changed.index(early), fatal)
        self.assertLess(fatal, changed.index("runningServer, err := startPanelHTTP(server, certPath, keyPath)"))
        self.assertNotIn("upd3", (REPO / "cmd/panel/startup_readiness.go").read_text(encoding="utf-8"))
        with self.assertRaisesRegex(ValueError, "real-start"):
            t.apply_kind_patch("real-start", changed)
        with self.assertRaises(ValueError):
            t.apply_kind_patch("real-start", source.replace("runningServer, err :=", "server2, err :="))

    def test_fixture_source_edits_one_file_in_a_disposable_clone(self):
        with tempfile.TemporaryDirectory() as directory:
            repo = Path(directory) / "cp-upd1-build" / "repo"
            (repo / ".git").mkdir(parents=True)
            (repo / "cmd/panel").mkdir(parents=True)
            for name in ("main.go", "server_lifecycle.go"):
                shutil.copy2(REPO / "cmd/panel" / name, repo / "cmd/panel" / name)
            self.assertEqual(t.fixture_source(repo, "start-check", None),
                             {"kind": "start-check", "changed": ["cmd/panel/server_lifecycle.go"]})
            self.assertEqual(t.fixture_source(repo, "real-start", None),
                             {"kind": "real-start", "changed": ["cmd/panel/main.go"]})
            self.assertIn("upd3 fixture defect", (repo / "cmd/panel/main.go").read_text(encoding="utf-8"))

    def test_build_script_builds_both_kinds_over_the_good_candidate(self):
        text = (HERE / "build-upd1-artifacts.sh").read_text()
        self.assertIn('startcheck=$(commit_fixture start-check', text)
        self.assertIn('realstart=$(commit_fixture real-start', text)
        self.assertEqual(text.count('git -C "$clone" checkout --quiet --detach "$good"'), 2)
        self.assertIn('update-ref "refs/upd1/$kind"', text)
        self.assertIn('s_json=$(build "$startcheck" v0.1.0-alpha.82)', text)
        self.assertIn('r_json=$(build "$realstart" v0.1.0-alpha.82)', text)
        self.assertIn('"startcheck": item(s, 82, good), "realstart": item(r, 82, good)', text)
        wrapper = (HERE / "run-upd1.sh").read_text().splitlines()
        last = int(re.search(r"sed -n '2,(\d+)p'", "\n".join(wrapper)).group(1))
        self.assertTrue(all(line.startswith("#") for line in wrapper[1:last]))
        self.assertEqual(wrapper[last], "set -euo pipefail")
        self.assertIn("upd1-arch-realstart", "\n".join(wrapper[:last]))


class Upd3SidecarAndTextTests(unittest.TestCase):
    COMMIT = "4" * 40

    def sidecar(self, code=t.START_CHECK_CODE, request=RID, commit=COMMIT):
        return (f"schema={t.FAILURE_SIDECAR_SCHEMA}\nrequest_id={request}\ntarget_commit={commit}\n"
                f"failure_code={code}\n")

    def test_sidecar_schema_matches_the_product(self):
        record = (REPO / "internal/recoveryobs/record.go").read_text(encoding="utf-8")
        self.assertIn(f'FailureSchema = "{t.FAILURE_SIDECAR_SCHEMA}"', record)
        for code in t.FAILURE_CODES:
            self.assertIn(f'value == "{code}"', record)
        writer = (REPO / "deploy/release-recovery-observation.sh").read_text(encoding="utf-8")
        self.assertIn("schema=celikpanel-recovery-failure/v1", writer)

    def test_sidecar_parsing(self):
        self.assertEqual(t.parse_failure_sidecar(self.sidecar(), RID, self.COMMIT),
                         {"present": True, "valid": True, "code": t.START_CHECK_CODE})
        self.assertEqual(t.parse_failure_sidecar(self.sidecar(t.REAL_START_CODE), RID, self.COMMIT)["code"],
                         t.REAL_START_CODE)
        for raw in (self.sidecar(commit="5" * 40), self.sidecar(request="b" * 32), self.sidecar("update_failed"),
                    self.sidecar() + "extra=1\n", self.sidecar().rstrip("\n"), "withheld"):
            with self.subTest(raw=raw):
                parsed = t.parse_failure_sidecar(raw, RID, self.COMMIT)
                self.assertEqual((parsed["present"], parsed["valid"], parsed["code"]), (True, False, None))
        self.assertEqual(t.parse_failure_sidecar(None, RID, self.COMMIT)["present"], False)
        records = {"directory_present": True, "records": {RID: "x", RID + ".failure": self.sidecar()}}
        self.assertTrue(t.sidecar_from_records(records, RID, self.COMMIT)["valid"])
        self.assertIsNone(t.sidecar_from_records(None, RID, self.COMMIT)["present"])
        self.assertIn(RID + "*", t.observation_records_script(RID))   # the collect listing includes <id>.failure

    def test_update_failure_line_and_check_reason_parsing(self):
        journal = ("2026-10-01T12:00:01.1+00:00 h bash[9]: !! new panel start check failed before completion: "
                   "panel startup check failed: tls_pair_invalid: the panel TLS certificate and private key cannot be "
                   "loaded as a matching pair\n"
                   "2026-10-01T12:00:02.1+00:00 h bash[9]: !! CELIKPANEL_UPDATE_FAILURE code="
                   "candidate_panel_startup_check_failed state=rolled_back reason=new panel start check failed "
                   "before completion: panel startup check failed: tls_pair_invalid: the panel TLS x detail=\n")
        lines = t.parse_update_failure_lines(journal)
        self.assertEqual([(l["code"], l["state"]) for l in lines], [(t.START_CHECK_CODE, "rolled_back")])
        self.assertEqual({r["code"] for r in t.parse_start_check_reasons(journal)}, {"tls_pair_invalid"})
        self.assertEqual(t.parse_update_failure_lines("nothing\n"), [])
        # The product prints exactly these shapes (update.sh).
        update = (REPO / "update.sh").read_text(encoding="utf-8")
        self.assertIn("!! CELIKPANEL_UPDATE_FAILURE code=%s state=%s reason=%.300s detail=%.450s", update)
        self.assertIn("new panel start check failed before completion: ${reason:-", update)
        self.assertIn('"panel startup check failed: "', (REPO / "cmd/panel/startup_readiness.go").read_text())

    def test_cli_texts_come_from_the_product_source(self):
        texts = cli_texts()
        for key in (f"{t.START_CHECK_CODE}.recovered", f"{t.START_CHECK_CODE}.pending",
                    f"{t.REAL_START_CODE}.pending", "paused"):
            with self.subTest(key=key):
                self.assertTrue(texts[key]["en"] and texts[key]["tr"])
                self.assertNotEqual(texts[key]["en"], texts[key]["tr"])
        self.assertTrue(texts["cause_markers"]["en"] and texts["cause_markers"]["tr"])
        command = t.panel_log_command(texts)
        self.assertTrue(command.startswith("sudo journalctl -u celikpanel-panel"))
        self.assertIn(command, texts[f"{t.REAL_START_CODE}.pending"]["tr"])
        with self.assertRaises(ValueError):
            t.parse_cli_guidance("package main\n")

    def test_cli_parser_accepts_the_committed_and_the_support_line_shapes(self):
        committed = ('\nfunc failureCodeGuidance(status recoveryobs.Status) (string, string, bool) {\n'
                     '\tswitch status.FailureCode {\n'
                     '\tcase "candidate_panel_startup_check_failed":\n'
                     '\t\tif status.Phase == "recovered" && status.TerminalProof == "rollback_verified" {\n'
                     '\t\t\treturn "R en",\n\t\t\t\t"R tr", true\n\t\t}\n'
                     '\t\tif status.TerminalProof == "none" {\n\t\t\treturn "P en",\n\t\t\t\t"P tr", true\n\t\t}\n'
                     '\tcase "panel_start_unverified":\n'
                     '\t\tif status.TerminalProof == "none" {\n'
                     '\t\t\treturn "U en sudo journalctl -u celikpanel-panel -n 50. x",\n\t\t\t\t"U tr", true\n\t\t}\n'
                     '\t}\n\treturn "", "", false\n}\n'
                     'if status.AutomaticRecovery == "paused_retry_limit" {\n\t\ten, tr = "Pa en", "Pa tr"\n}\n')
        old = committed + 'fmt.Fprintf(w, "%s: %s\\n", translated(lang, "Recorded cause", "Kaydedilen neden"), status.FailureCode)\n'
        new = committed + '\t\tline += " failure_code=" + status.FailureCode\n'
        self.assertEqual(t.parse_cli_guidance(old)["cause_markers"]["tr"], ["Kaydedilen neden: {code}"])
        self.assertEqual(t.parse_cli_guidance(new)["cause_markers"]["en"], ["failure_code={code}"])
        self.assertEqual(t.parse_cli_guidance(new)[f"{t.START_CHECK_CODE}.recovered"], {"en": "R en", "tr": "R tr"})
        self.assertEqual(t.panel_log_command(t.parse_cli_guidance(new)), "sudo journalctl -u celikpanel-panel -n 50")
        with self.assertRaisesRegex(ValueError, "no known form"):
            t.parse_cli_guidance(committed)

    def test_cli_guidance_key_follows_write_status_precedence(self):
        base = {"observation": "known", "previous_failure": "update_failed", "terminal_proof": "none"}
        self.assertEqual(t.cli_guidance_key(dict(base, phase="recovered", terminal_proof="rollback_verified",
                                                 failure_code=t.START_CHECK_CODE)), f"{t.START_CHECK_CODE}.recovered")
        self.assertEqual(t.cli_guidance_key(dict(base, phase="recovering", failure_code=t.START_CHECK_CODE)),
                         f"{t.START_CHECK_CODE}.pending")
        self.assertEqual(t.cli_guidance_key(dict(base, phase="failed", failure_code=t.REAL_START_CODE)),
                         f"{t.REAL_START_CODE}.pending")
        self.assertEqual(t.cli_guidance_key(dict(base, phase="recovery_required", failure_code=t.REAL_START_CODE,
                                                 automatic_recovery="paused_retry_limit")), "paused")
        self.assertIsNone(t.cli_guidance_key(dict(base, phase="recovering", failure_code=t.REAL_START_CODE,
                                                  previous_failure="recovery_failed")))
        self.assertIsNone(t.cli_guidance_key(dict(base, phase="recovering", failure_code=t.REAL_START_CODE,
                                                  waiting_for="starting")))
        self.assertIsNone(t.cli_guidance_key(None))

    def test_cli_text_observations_read_the_output_verbatim(self):
        texts = cli_texts()
        pending = {"request_id": RID, "observation": "known", "phase": "failed", "terminal_proof": "none",
                   "previous_failure": "update_failed", "failure_code": t.REAL_START_CODE}
        paused = {"request_id": RID, "observation": "known", "phase": "recovery_required", "terminal_proof": "none",
                  "previous_failure": "recovery_failed", "automatic_recovery": "paused_retry_limit"}
        samples = [cli_sample(pending, texts), cli_sample(paused, texts)]
        seen = t.cli_text_observations(samples, texts)
        self.assertEqual(seen["by_key"][f"{t.REAL_START_CODE}.pending"]["en"], 1)
        self.assertEqual(seen["by_key"]["paused"]["tr"], 1)
        self.assertEqual(seen["panel_log_command_seen"], {"en": True, "tr": True})
        self.assertEqual(seen["recorded_cause_lines"], {"en": 1, "tr": 1})
        self.assertEqual(seen["mismatches"], [])
        wrong = cli_sample(pending, texts)
        wrong["cli"]["tr"]["stdout"] = texts[f"{t.REAL_START_CODE}.pending"]["en"] + "\n"
        seen = t.cli_text_observations([wrong], texts)
        self.assertEqual([(m["key"], m["language"]) for m in seen["mismatches"]],
                         [(f"{t.REAL_START_CODE}.pending", "tr")])

    def test_web_guidance_uses_the_product_catalogue_and_failure_code(self):
        guidance = load("tested_upd3_guidance", "../dns-pair-acceptance/guidance.py")
        translator = guidance.Translator(guidance.load_catalog(REPO / "web/src/i18n"))
        recovered = {"request_id": RID, "observation": "known", "phase": "recovered",
                     "terminal_proof": "rollback_verified", "previous_failure": "update_failed",
                     "failure_code": t.START_CHECK_CODE}
        shown = t.recovery_guidance(translator, recovered)
        self.assertEqual(shown["keys"], ["recovery.phase.recovered",
                                         f"recovery.failure.{t.START_CHECK_CODE}.recovered",
                                         f"recovery.reason.{t.START_CHECK_CODE}"])
        self.assertEqual(shown["missing_keys"], [])
        self.assertNotEqual(shown["texts"]["en"], shown["texts"]["tr"])
        pending = dict(recovered, phase="recovering", terminal_proof="none", failure_code=t.REAL_START_CODE)
        self.assertIn(f"recovery.failure.{t.REAL_START_CODE}.pending", t.recovery_guidance(translator, pending)["keys"])
        hidden = dict(pending, previous_failure="recovery_failed")
        self.assertEqual(t.recovery_guidance(translator, hidden)["keys"][1:], ["recovery.next.recovering",
                                                                            "recovery.reason.recovery_failed"])
        # Every key the mirror can return is a key of the product's own TypeScript.
        ts = (REPO / "web/src/lib/recoveryObservation.ts").read_text(encoding="utf-8")
        for key in (f"recovery.failure.{t.START_CHECK_CODE}.recovered", f"recovery.failure.{t.START_CHECK_CODE}.returning",
                    f"recovery.failure.{t.REAL_START_CODE}.pending"):
            self.assertIn(f"'{key}'", ts)
        self.assertIn("recovery.reason.${last.failure_code ?? last.previous_failure}",
                      (REPO / "web/src/components/RecoveryAccess.tsx").read_text(encoding="utf-8"))


def ideal_start_check(texts):
    final = product_status({"request_id": RID, "observation": "known", "phase": "recovered",
                            "terminal_proof": "rollback_verified", "previous_failure": "update_failed",
                            "failure_code": t.START_CHECK_CODE})
    return {"final": final, "update_failure_codes": [t.START_CHECK_CODE], "check_reasons": ["tls_pair_invalid"],
            "sidecar": {"present": True, "valid": True, "code": t.START_CHECK_CODE}, "completion_marker_seen": False,
            "receipt_phases": ["active", "active"],
            "receipt_dispatches": [{"operation": "update", "phase": "active"}, {"operation": "rollback",
                                                                                 "phase": "active"}],
            "installed": "baseline", "database": "equal-except-volatile",
            "cli": t.cli_text_observations([cli_sample(final, texts)], texts), "web_keys_missing": [],
            "owner_retry_run": False, "update_card": {"verdict": "as-expected", "findings": []}}


def ideal_real_start(texts):
    pending = product_status({"request_id": RID, "observation": "known", "phase": "failed", "terminal_proof": "none",
                              "previous_failure": "update_failed", "failure_code": t.REAL_START_CODE})
    final = product_status({"request_id": RID, "observation": "known", "phase": "recovery_required",
                            "terminal_proof": "none", "previous_failure": "recovery_failed",
                            "automatic_recovery": "paused_retry_limit"})
    return {"final": final, "update_failure_codes": [t.REAL_START_CODE], "check_reasons": [],
            "sidecar": {"present": True, "valid": True, "code": t.REAL_START_CODE}, "completion_marker_seen": True,
            "receipt_phases": ["completion"] * 3,
            "receipt_dispatches": [{"operation": "update", "phase": "completion"}] * 3,
            "installed": "candidate", "database": "different",
            "cli": t.cli_text_observations([cli_sample(pending, texts), cli_sample(final, texts)], texts),
            "web_keys_missing": [], "owner_retry_run": False,
            "printed_retry_command": "/usr/libexec/celikpanel/recovery recover --retry --snapshot " + SNAPSHOT,
            "workloads": {"web": "never-interrupted", "dns": t.DNS_NOT_PROVIDED, "smtp": "never-interrupted",
                          "cron": "never-interrupted"}, "panel_verdict": t.PANEL_UNTIL_END}


class Upd3ExpectationTests(unittest.TestCase):
    def test_start_check_expects_rollback_and_no_completion_marker(self):
        texts = cli_texts()
        judged = t.judge_start_check(ideal_start_check(texts))
        self.assertEqual((judged["verdict"], judged["findings"], judged["unknown"]), ("as-expected", [], []))
        self.assertFalse(judged["native_evidence"])
        self.assertEqual(t.expectation_step_verdict(judged), "passed")
        for change, rule in (({"completion_marker_seen": True}, "completion.pending was created"),
                             ({"receipt_dispatches": [{"operation": "update", "phase": "active"},
                                                      {"operation": "update", "phase": "completion"}]},
                              "a forward attempt"),
                             ({"update_card": {"verdict": "finding", "findings": ["x"]}}, "update card"),
                             ({"check_reasons": ["database_unverified"]}, "a good candidate could fail"),
                             ({"installed": "candidate"}, "installed release"),
                             ({"database": "different"}, "database differs"),
                             ({"update_failure_codes": [t.REAL_START_CODE]}, "failure line"),
                             ({"sidecar": {"present": False, "valid": False, "code": None}}, "sidecar"),
                             ({"final": dict(ideal_start_check(texts)["final"], phase="recovery_required",
                                             terminal_proof="none", automatic_recovery="paused_retry_limit")},
                              "not returned")):
            with self.subTest(rule=rule):
                judged = t.judge_start_check(dict(ideal_start_check(texts), **change))
                self.assertEqual(judged["verdict"], "finding")
                self.assertTrue(any(rule in f for f in judged["findings"]), judged["findings"])
                self.assertEqual(t.expectation_step_verdict(judged), "failed")
        unknown = t.judge_start_check(dict(ideal_start_check(texts), completion_marker_seen=None, cli=None))
        self.assertEqual((unknown["verdict"], sorted(unknown["unknown"])),
                         ("inconclusive", ["cli-returned-text", "no-completion-marker"]))

    def test_real_start_expects_the_pause_never_a_rollback_and_no_owner_retry(self):
        texts = cli_texts()
        judged = t.judge_real_start(ideal_real_start(texts))
        self.assertEqual((judged["verdict"], judged["findings"], judged["unknown"]), ("as-expected", [], []))
        self.assertIn("no-rollback", judged["expected"])
        self.assertIn("owner-retry-not-run", judged["expected"])
        recovered = dict(ideal_real_start(texts)["final"], phase="recovered", terminal_proof="rollback_verified",
                         automatic_recovery=None)
        for change, rule in (({"final": recovered, "installed": "baseline"}, "returned to the previous version"),
                             ({"owner_retry_run": True}, "owner retry was run"),
                             ({"check_reasons": ["tls_pair_invalid"],
                               "update_failure_codes": [t.START_CHECK_CODE]}, "start check refused"),
                             ({"completion_marker_seen": False}, "never observed"),
                             ({"receipt_dispatches": [{"operation": "rollback", "phase": "completion"}]},
                              "expected only completion"),
                             ({"workloads": {"web": "interrupted", "smtp": "never-interrupted"}}, "web"),
                             ({"panel_verdict": "came-back"}, "came-back"),
                             ({"printed_retry_command": ""}, "no one-time retry command")):
            with self.subTest(rule=rule):
                judged = t.judge_real_start(dict(ideal_real_start(texts), **change))
                self.assertEqual(judged["verdict"], "finding")
                self.assertTrue(any(rule in f for f in judged["findings"]), judged["findings"])
        # The product may hide failure_code once a forward attempt failed; then the typed text is never shown.
        paused_only = dict(ideal_real_start(texts))
        paused_only["cli"] = t.cli_text_observations([cli_sample(paused_only["final"], texts)], texts)
        judged = t.judge_real_start(paused_only)
        self.assertEqual(judged["verdict"], "finding")
        self.assertTrue(any("panel_start_unverified text" in f for f in judged["findings"]))
        self.assertTrue(any("panel log command" in f for f in judged["findings"]))

    def test_supporting_rules(self):
        timeline = [{"event": "timeline", "transaction_phase": "active"}]
        self.assertFalse(t.completion_marker_seen(timeline + [{"event": "released"}]))
        self.assertTrue(t.completion_marker_seen(timeline + [{"event": "timeline",
                                                              "transaction_phase": "completion.pending"}]))
        self.assertIsNone(t.completion_marker_seen([{"event": "armed"}]))
        self.assertIsNone(t.completion_marker_seen(None))
        receipt = {"snapshot": SNAPSHOT, "name": "1", "mtime_utc": "T1",
                   "text": f"schema=celikpanel-recovery-dispatch/v1\nsnapshot={SNAPSHOT}\nattempt=1\n"
                           f"token_sha256={'4' * 64}\noperation=update\nphase=completion\n"}
        counted = t.attempts_from_receipts([receipt, dict(receipt, name="2", text=None)], SNAPSHOT)
        self.assertEqual([a["phase"] for a in counted["automatic"]], ["completion", None])
        self.assertTrue(w.budget_receipt_safe(receipt["text"].encode()))
        down = [{"from": 105.0, "to": None, "first_bad": 110.0}]
        self.assertEqual(t.panel_verdict_until_end(down, 100.0)["verdict"], t.PANEL_UNTIL_END)
        self.assertEqual(t.panel_verdict_until_end([], 100.0)["verdict"], "never-down")
        self.assertEqual(t.panel_verdict_until_end([{"from": 105.0, "to": 300.0}], 100.0)["verdict"], "came-back")
        self.assertEqual(t.panel_verdict_until_end([{"from": 10.0, "to": None}], 100.0)["verdict"],
                         "down-outside-transaction")
        builds = {n: {"identity": f"version={t.CANDIDATE_VERSION}\ncommit={'5' * 40}\n"} for n in ("agent", "panel")}
        self.assertEqual(t.installed_role(builds, artifacts(upd3=True), "realstart"), "candidate")
        self.assertEqual(t.installed_role(builds, artifacts(upd3=True), "startcheck"), "other")
        self.assertIsNone(t.installed_role({}, artifacts(upd3=True), "realstart"))
        views = t.view_reachability([{"update_status": {"http": 200}, "recovery_api": {"http": 200}, "cli": {}},
                                     {"panel_error": "ConnectionRefusedError", "shell_fetch": {"error": "X"},
                                      "cli": {"en": {}}}])
        self.assertEqual(views["summary"]["panel_recovery_status"], {"reachable": 1, "samples": 2})
        self.assertEqual(views["summary"]["root_cli"], {"reachable": 1, "samples": 2})
        self.assertFalse(views["last"]["offline_page_served"])


class Upd3TrialFlowTests(unittest.TestCase):
    """The real-start owner continuation reads the printed retry and never runs it; kind_expectation judges
    from this run's own records."""

    def trial(self, variant):
        trial = object.__new__(t.Trial)
        cell = next(c for c in t.CELLS.values() if c.variant == variant)
        pair = t.pair_modules()
        guidance = load("tested_upd3_flow_guidance", "../dns-pair-acceptance/guidance.py")
        trial.cell, trial.artifacts = cell, artifacts(upd3=True)
        trial.role = t.candidate_role(cell)
        trial.candidate = trial.artifacts[trial.role]
        trial.p = dict(pair, guidance=guidance)
        trial.redactor = pair["redaction"].Redactor()
        trial.translator = guidance.Translator(guidance.load_catalog(REPO / "web/src/i18n"))
        trial.state = {"findings": [], "resets": [], "request_id": RID}
        trial.calls = []
        return trial

    def test_real_start_owner_continuation_prints_and_never_runs_the_retry(self):
        trial = self.trial("real-start")
        texts = cli_texts()
        paused = ideal_real_start(texts)["final"]
        sample = cli_sample(paused, texts)
        sample.update(panel_error="ConnectionRefusedError")
        sample["cli"]["en"]["stdout"] += "sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50\n"
        sample["cli"]["tr"]["stdout"] += "sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50\n"
        trial.state.update(paused=paused, status_samples=[sample])
        trial.fetch_shell = lambda label: {"status_command": {}, "texts": {}}
        trial.pending_snapshot = lambda: SNAPSHOT
        trial.trial = SimpleNamespace(save=lambda *a: self.fail("no owner-continuation attempt is saved"))

        def workload(mode, *args, timeout=120):
            trial.calls.append((mode, args))
            return {"action": "validated-not-executed", "snapshot": SNAPSHOT,
                    "argv": ["/usr/libexec/celikpanel/recovery", "recover", "--retry", "--snapshot", SNAPSHOT]}
        trial.workload = workload
        checks = {}
        self.assertEqual(trial.owner_continuation(checks), "observed")
        self.assertEqual([c[0] for c in trial.calls], ["owner-retry"])
        self.assertNotIn("--execute", trial.calls[0][1])
        self.assertFalse(trial.state.get("owner_continued"))
        self.assertIn("paused", trial.state)
        self.assertEqual(trial.state["printed_retry_command"],
                         "/usr/libexec/celikpanel/recovery recover --retry --snapshot " + SNAPSHOT)
        self.assertIn("not run by design", checks["owner_retry"])
        self.assertIn("recover --retry", w.RETRY_LINE_RE.pattern)

    def test_migrate_only_owner_continuation_still_runs_the_retry_once(self):
        source = inspect.getsource(t.Trial.owner_continuation)
        self.assertIn('"--snapshot-name", snapshot, "--execute"', source)
        self.assertIn('if self.cell.variant == "real-start":', source)

    def kind_state(self, trial, product_journal, observer, receipts, samples, terminal, workloads):
        trial.state.update(journals={"product": product_journal},
                           observation_records={"records": {RID + ".failure": (
                               f"schema={t.FAILURE_SIDECAR_SCHEMA}\nrequest_id={RID}\n"
                               f"target_commit={trial.candidate['commit']}\nfailure_code="
                               f"{t.START_CHECK_CODE if trial.cell.variant == 'start-check' else t.REAL_START_CODE}\n")}},
                           observer_events=observer, attempts=t.attempts_from_receipts(receipts, SNAPSHOT),
                           status_samples=samples, final_status=samples[-1]["observed"], terminal=terminal,
                           verdict_checks={"workloads": workloads},
                           card_judged={"verdict": "as-expected", "findings": []})
        trial._candidate_translator = trial.translator
        trial.load_cli_texts = lambda: dict(cli_texts(), source={"role": "test", "commit": "x"})

    def receipt(self, name, phase):
        return {"snapshot": SNAPSHOT, "name": name, "mtime_utc": "T" + name,
                "text": f"schema=celikpanel-recovery-dispatch/v1\nsnapshot={SNAPSHOT}\nattempt={name}\n"
                        f"token_sha256={'4' * 64}\noperation=update\nphase={phase}\n"}

    def test_kind_expectation_for_start_check_from_run_records(self):
        trial = self.trial("start-check")
        texts = cli_texts()
        final = ideal_start_check(texts)["final"]
        journal = ("x bash[1]: !! new panel start check failed before completion: panel startup check failed: "
                   "tls_pair_invalid: the panel TLS certificate and private key cannot be loaded as a matching pair\n"
                   f"x bash[1]: !! CELIKPANEL_UPDATE_FAILURE code={t.START_CHECK_CODE} state=x reason=y detail=\n")
        self.kind_state(trial, journal, [{"event": "timeline", "transaction_phase": "active"}],
                        [self.receipt("1", "active")], [cli_sample(final, texts)],
                        {"installed": "baseline", "database": "equal", "recovery_body": final},
                        {"web": {"verdict": "never-interrupted"}, "panel": {"verdict": "down-only-during-transaction"}})
        checks = {}
        self.assertEqual(trial.kind_expectation(checks), "passed", checks["judged"])
        self.assertEqual(checks["judged"]["verdict"], "as-expected")
        self.assertIn(f"recovery.failure.{t.START_CHECK_CODE}.recovered", checks["observations"]["web_keys"])

    def test_kind_expectation_for_real_start_from_run_records(self):
        trial = self.trial("real-start")
        texts = cli_texts()
        ideal = ideal_real_start(texts)
        pending = dict(ideal["final"], phase="failed", previous_failure="update_failed",
                       failure_code=t.REAL_START_CODE, automatic_recovery=None)
        journal = f"x bash[1]: !! CELIKPANEL_UPDATE_FAILURE code={t.REAL_START_CODE} state=x reason=y detail=\n"
        self.kind_state(trial, journal, [{"event": "timeline", "transaction_phase": "completion.pending"}],
                        [self.receipt(n, "completion") for n in ("1", "2", "3")],
                        [cli_sample(pending, texts), cli_sample(ideal["final"], texts)],
                        {"installed": "candidate", "database": "different"},
                        {"web": {"verdict": "never-interrupted"}, "smtp": {"verdict": "never-interrupted"},
                         "cron": {"verdict": "never-interrupted"}, "dns": {"verdict": t.DNS_NOT_PROVIDED},
                         "panel": {"verdict": t.PANEL_UNTIL_END}})
        trial.state["printed_retry_command"] = "/usr/libexec/celikpanel/recovery recover --retry --snapshot " + SNAPSHOT
        checks = {}
        self.assertEqual(trial.kind_expectation(checks), "passed", checks["judged"])
        self.assertEqual(checks["observations"]["attempts"]["automatic_count"], 3)
        self.assertEqual(checks["observations"]["receipt_phases"], ["completion"] * 3)


# ---------------------------------------------------------------------------
# upd4: the upd3 run-copy corrections made permanent (H8, H9, H10, sidecar v2)
# ---------------------------------------------------------------------------

h = load("tested_owner_port_hold", "guest_owner_port_hold.py")
GUIDANCE = load("tested_upd4_guidance", "../dns-pair-acceptance/guidance.py")


def product_translator():
    return GUIDANCE.Translator(GUIDANCE.load_catalog(REPO / "web/src/i18n"))


def failed_status(**extra):
    return product_status(dict({"request_id": RID, "observation": "known", "phase": "failed", "terminal_proof": "none",
                                "reason": "update_failed", "previous_failure": "update_failed"}, **extra))


class H8SettledFailureTests(unittest.TestCase):
    def test_rule_needs_600_s_over_three_unchanged_reads(self):
        rule = t.SettledFailure()
        self.assertEqual((t.SETTLED_FAILED_SECONDS, t.SETTLED_FAILED_SAMPLES), (600.0, 3))
        self.assertEqual([rule.observe(failed_status(), now) for now in (0, 300, 599)], [False, False, False])
        self.assertTrue(rule.observe(failed_status(), 600))
        record = rule.record(600)
        self.assertEqual((record["rule"], record["seconds"], record["samples"]), (t.SETTLED_FAILED_RULE, 600.0, 4))
        self.assertEqual(record["status"]["phase"], "failed")
        two = t.SettledFailure()
        two.observe(failed_status(), 0)
        self.assertFalse(two.observe(failed_status(), 900))    # two reads are not three

    def test_any_change_or_recovery_activity_restarts_the_clock(self):
        rule = t.SettledFailure()
        rule.observe(failed_status(), 0)
        rule.observe(failed_status(), 300)
        changed = failed_status(failure_code=t.REAL_START_CODE)
        self.assertFalse(rule.observe(changed, 400))
        self.assertFalse(rule.observe(changed, 900))
        self.assertTrue(rule.observe(changed, 1000))
        for other in (failed_status(automatic_recovery="paused_retry_limit"), failed_status(waiting_for="starting"),
                      failed_status(phase="recovering"), failed_status(terminal_proof="rollback_verified"),
                      {"observation": "unavailable"}, None):
            with self.subTest(other=other):
                again = t.SettledFailure()
                again.observe(failed_status(), 0)
                self.assertFalse(again.observe(other, 700))
                self.assertFalse(again.observe(failed_status(), 800))
                self.assertFalse(again.observe(failed_status(), 1300))
                self.assertTrue(again.observe(failed_status(), 1400))

    def track_trial(self, observed_sequence):
        trial = object.__new__(t.Trial)
        trial.cell = t.CELLS["upd1-debian13-good"]
        trial.state = {"findings": [], "resets": [], "request_id": RID, "check_at": 1.0, "awaiting_terminal": True}
        trial.step_dir = "steps/11-track"
        trial.ev = SimpleNamespace(write_json=lambda *a: None)

        class View:
            def __enter__(self):
                return self

            def __exit__(self, *a):
                return False
        trial.panel_client = lambda: SimpleNamespace(polling=View)
        values = iter(observed_sequence)
        trial.status_sample = lambda view, index: {"observed": next(values, observed_sequence[-1]),
                                                   "update_status": {"body": {"status": "failed"}},
                                                   "agreement": {"verdict": "single-source"}}
        trial.inspections = []
        trial.inspect = lambda label, light=False: trial.inspections.append(label)
        return trial

    def test_track_stops_with_the_named_rule_and_an_inconclusive_verdict(self):
        clock = [1000.0]
        trial = self.track_trial([failed_status()])
        checks = {}
        with mock.patch.object(t.time, "monotonic", lambda: clock[0]), \
                mock.patch.object(t.time, "sleep", lambda s: clock.__setitem__(0, clock[0] + s)):
            with self.assertRaisesRegex(t.StepInconclusive, t.SETTLED_FAILED_RULE):
                trial.track(checks)
        self.assertLess(clock[0] - 1000.0, 700)           # stopped after 600 s, not after the 90-min deadline
        self.assertEqual(checks["settled_failure"]["rule"], t.SETTLED_FAILED_RULE)
        self.assertGreaterEqual(checks["settled_failure"]["seconds"], 600)
        self.assertEqual(checks["settled_failure"]["update_status"], {"status": "failed"})
        self.assertIsNone(trial.state["final_status"])
        self.assertFalse(trial.state["awaiting_terminal"])
        self.assertEqual(trial.inspections, ["after-track"])
        self.assertEqual(t.classify_outcome("good", None, False), "not-terminal")

    def test_track_keeps_waiting_while_recovery_is_active(self):
        clock = [0.0]
        recovering = failed_status(phase="recovering")
        done = dict(failed_status(), phase="recovered", terminal_proof="rollback_verified")
        trial = self.track_trial([recovering] * 80 + [done])
        with mock.patch.object(t.time, "monotonic", lambda: clock[0]), \
                mock.patch.object(t.time, "sleep", lambda s: clock.__setitem__(0, clock[0] + s)):
            self.assertIn(trial.track({}), ("passed", "failed"))
        self.assertGreater(clock[0], 600)
        self.assertEqual(trial.state["final_status"], done)


class H9DispatchDirectionTests(unittest.TestCase):
    def receipt(self, attempt, operation, phase):
        return {"snapshot": SNAPSHOT, "name": attempt, "mtime_utc": "T" + attempt,
                "text": f"schema=celikpanel-recovery-dispatch/v1\nsnapshot={SNAPSHOT}\nattempt={attempt}\n"
                        f"token_sha256={'4' * 64}\noperation={operation}\nphase={phase}\n"}

    def test_the_planner_rule(self):
        d = t.dispatch_direction
        self.assertEqual([d("rollback", p) for p in ("active", "completion", "quiesce", None)], ["rollback"] * 4)
        self.assertEqual(d("update", "active"), "rollback")
        self.assertEqual((d("update", "completion"), d("update", "completion-scheduler")), ("forward", "forward"))
        self.assertEqual((d("update", "quiesce"), d(None, "active"), d("update", None)), ("unknown",) * 3)

    def test_the_arch_startcheck_receipt_pair_is_a_rollback(self):
        # upd3 upd1-arch-startcheck steps/14-collect/budget.json: a resumed rollback's own completion.
        counted = t.attempts_from_receipts([self.receipt("1", "update", "active"),
                                            self.receipt("2", "rollback", "completion")], SNAPSHOT)
        self.assertEqual([a["direction"] for a in counted["automatic"]], ["rollback", "rollback"])
        self.assertTrue(w.budget_receipt_safe(self.receipt("2", "rollback", "completion")["text"].encode()))
        texts = cli_texts()
        judged = t.judge_start_check(dict(ideal_start_check(texts), receipt_dispatches=counted["automatic"]))
        self.assertEqual(judged["verdict"], "as-expected", judged)
        self.assertIn("rollback-dispatch", judged["expected"])

    def test_a_forward_attempt_is_a_finding_and_an_unknown_phase_stays_unknown(self):
        texts = cli_texts()
        forward = t.judge_start_check(dict(ideal_start_check(texts), receipt_dispatches=[
            {"operation": "update", "phase": "active"}, {"operation": "update", "phase": "completion"}]))
        self.assertEqual(forward["verdict"], "finding")
        self.assertTrue(any("update/completion" in f and "forward attempt" in f for f in forward["findings"]))
        odd = t.judge_start_check(dict(ideal_start_check(texts), receipt_dispatches=[
            {"operation": "update", "phase": "active"}, {"operation": "update", "phase": "quiesce"}]))
        self.assertEqual((odd["verdict"], odd["findings"], odd["unknown"]), ("inconclusive", [], ["rollback-dispatch"]))
        self.assertIs(t.directions_rule(None, "forward"), None)
        self.assertIs(t.directions_rule([{"operation": "rollback", "phase": "active"}], "forward"), False)


# The upd3 run's own product-rendered cards (evidence/upd3-20261001/<cell>/run-a/side/extract.txt, rendered there
# from the build's systemUpdateOutcome.ts rules and catalogues by hand); the driver must now render the same.
UPD3_SC_STATUS = {"found": True, "request_id": "8200886620af7fbfd08be2c39042f970", "status": "failed"}
UPD3_SC_RECOVERY = {"failure_code": "candidate_panel_startup_check_failed", "observation": "known",
                    "observed_at": "2026-09-30T19:48:09Z", "panel_state": "ready", "phase": "recovered",
                    "previous_failure": "update_failed", "reason": "rollback_verified",
                    "request_id": "8200886620af7fbfd08be2c39042f970", "schema": "celikpanel-recovery-status/v1",
                    "terminal_proof": "rollback_verified"}
UPD3_SC_CARD = {
    "en": "Update not completed; previous version restored || The update to v0.1.0-alpha.82 did not complete. The "
          "server was returned to v0.1.0-alpha.81 automatically and is running it now. || Cause: the new version's "
          "panel failed its start check before anything was switched on. || Nothing needs to be done on the server. "
          "Do not start the update to v0.1.0-alpha.82 again until a corrected version is published. If the server's "
          "message names a problem on this server, fix it first. || Nothing resumes by itself: the server keeps "
          "running v0.1.0-alpha.81. When a newer version is published, “Check for updates” offers it.",
    "tr": "Güncelleme tamamlanmadı; önceki sürüm geri yüklendi || v0.1.0-alpha.82 sürümüne güncelleme tamamlanmadı. "
          "Sunucu otomatik olarak v0.1.0-alpha.81 sürümüne döndürüldü ve şu an onu çalıştırıyor. || Neden: yeni "
          "sürümün paneli, hiçbir şey devreye alınmadan önce başlangıç denetiminden geçemedi. || Sunucuda yapmanız "
          "gereken bir şey yok. Düzeltilmiş bir sürüm yayımlanana kadar v0.1.0-alpha.82 güncellemesini yeniden "
          "başlatmayın. Sunucunun iletisi bu sunucudaki bir sorunu belirtiyorsa önce onu giderin. || Hiçbir işlem "
          "kendiliğinden sürmez: sunucu v0.1.0-alpha.81 sürümünü çalıştırmaya devam eder. Daha yeni bir sürüm "
          "yayımlandığında “Güncellemeyi kontrol et” onu gösterir."}
UPD3_DEF_SUMMARY = ("reviewed updater failed: exit status 1: !! CELIKPANEL_UPDATE_FAILURE code=update_failed "
                    "state=recovery_required reason=offline panel database migration failed; its original database "
                    "and work evidence are preserved detail=")
UPD3_DEF_STATUS = {"found": True, "request_id": "3ceee6e456b916aa52a50916fe78fce2", "status": "failed",
                   "summary": UPD3_DEF_SUMMARY}
UPD3_DEF_RECOVERY = {"observation": "known", "observed_at": "2026-09-30T19:36:02Z", "panel_state": "ready",
                     "phase": "recovered", "previous_failure": "update_failed", "reason": "rollback_verified",
                     "request_id": "3ceee6e456b916aa52a50916fe78fce2", "schema": "celikpanel-recovery-status/v1",
                     "terminal_proof": "rollback_verified"}
UPD3_RS_PAUSED = {"automatic_recovery": "paused_retry_limit", "first_failure_code": "panel_start_unverified",
                  "observation": "known", "observed_at": "2026-09-30T20:04:02Z", "phase": "recovery_required",
                  "previous_failure": "recovery_failed", "reason": "recovery_incomplete",
                  "request_id": "ded53ec59a2d64864e45c34d5a675bbd", "schema": "celikpanel-recovery-status/v1",
                  "terminal_proof": "none"}


class H10CardModelTests(unittest.TestCase):
    """The card is what the build's own functions return (evaluated from its web/src), not a frozen copy."""

    def setUp(self):
        self.translator = product_translator()
        self.rules = t.load_card_rules(REPO / "web/src")
        self.sources = {name: (REPO / "web/src" / path).read_text(encoding="utf-8")
                        for name, path in t.CARD_SOURCES.items()}

    def card(self, status, recovery, rules=None):
        return t.update_card_guidance(self.translator, status, recovery, rules or self.rules, status.get("request_id"))

    def test_rules_are_the_build_functions(self):
        self.assertTrue(set(t.FAILURE_CODES) <= set(self.rules["failure_codes"]))
        self.assertEqual(self.rules["command"], t.RECOVERY_LOG_COMMAND)
        self.assertTrue(self.rules["screen"]["automatic_cause_line"])
        self.assertEqual(set(self.rules["source"]), set(t.CARD_SOURCES))
        module = self.rules["module"]
        parsed = module.call("parseRecoveryObservation", UPD3_SC_RECOVERY, UPD3_SC_RECOVERY["request_id"])
        self.assertEqual((parsed["phase"], parsed["failure_code"]), ("recovered", t.START_CHECK_CODE))
        self.assertEqual(module.call("recoveryFailureGuidanceKey", parsed),
                         f"recovery.failure.{t.START_CHECK_CODE}.recovered")
        e = t.web_eval()
        with self.assertRaises(e.JSThrow):          # the product refuses a foreign request id
            module.call("parseRecoveryObservation", UPD3_SC_RECOVERY, RID)

    def test_the_upd3_rolled_back_cards_are_reproduced(self):
        card = self.card(UPD3_SC_STATUS, UPD3_SC_RECOVERY)
        self.assertEqual(card["state"], "rolled_back")
        # The run agent's "CARD keys" line for d13-sc (same order).
        self.assertEqual(card["keys"], ["panelUpdate.outcome.rolledBackTitle", "panelUpdate.outcome.rolledBack",
                                        "panelUpdate.outcome.cause.candidate_panel_startup_check_failed",
                                        "panelUpdate.outcome.rolledBackNext", "panelUpdate.outcome.rolledBackResume"])
        if all(self.translator.text(key, language=language) == UPD3_SC_CARD[language].split(" || ")[index]
               for language in ("en", "tr") for index, key in ((0, "panelUpdate.outcome.rolledBackTitle"),
                                                              (2, "panelUpdate.outcome.cause."
                                                                  "candidate_panel_startup_check_failed"))):
            # Verbatim while the catalogue still has the 94be6b6e wording (another change may reword it).
            for language in ("en", "tr"):
                self.assertEqual(" || ".join(card["texts"][language]), UPD3_SC_CARD[language])
        self.assertEqual(card["missing_keys"], [])
        self.assertIsNone(card["server_message"])
        defective = self.card(UPD3_DEF_STATUS, UPD3_DEF_RECOVERY)
        self.assertIn("panelUpdate.outcome.cause.generic", defective["keys"])
        self.assertEqual(defective["keys"][-1], "panelUpdate.outcome.serverMessage")
        self.assertTrue(defective["texts"]["en"][-1].startswith("The server reported: "))
        self.assertTrue(defective["texts"]["tr"][-1].startswith("Sunucunun bildirdiği: "))   # O6 shape, recorded
        self.assertIn("offline panel database migration failed", defective["server_message"])
        judged = t.judge_update_card(defective, ("rolled_back",))
        self.assertEqual((judged["verdict"], judged["findings"]), ("as-expected", []))
        self.assertEqual(judged["server_message_line"], defective["server_message"])

    def test_the_paused_card_and_screen_carry_the_automatic_cause_line(self):
        status = dict(UPD3_DEF_STATUS, request_id=UPD3_RS_PAUSED["request_id"], summary="x")
        card = self.card(status, dict(UPD3_RS_PAUSED, panel_state="ready"))
        self.assertEqual(card["state"], "recovery")
        self.assertEqual(card["keys"][:3], ["recovery.automatic.pausedTitle",
                                            f"recovery.automatic.cause.{t.REAL_START_CODE}",
                                            "recovery.automatic.pausedHelp"])
        self.assertIn("panelUpdate.outcome.followsRecovery", card["keys"])
        self.assertLess(card["keys"].index("panelUpdate.outcome.followsRecovery"),
                        card["texts"]["en"].index(t.RECOVERY_LOG_COMMAND))
        self.assertIn("sudo journalctl -u celikpanel-panel -n 50", card["texts"]["tr"][1])
        screen = t.recovery_guidance(self.translator, UPD3_RS_PAUSED, self.rules)      # a CLI body: no panel_state
        self.assertEqual(screen["keys"][1], f"recovery.automatic.cause.{t.REAL_START_CODE}")
        self.assertEqual(screen["texts"]["en"][screen["keys"].index("recovery.automatic.inspect") + 1],
                         t.RECOVERY_LOG_COMMAND)
        without = t.recovery_guidance(self.translator, UPD3_RS_PAUSED, dict(self.rules, screen={}))
        self.assertNotIn(f"recovery.automatic.cause.{t.REAL_START_CODE}", without["keys"])
        mirror = t.recovery_guidance(self.translator, UPD3_RS_PAUSED)                   # no rules: 94be6b6e mirror
        self.assertEqual(mirror["keys"][:2], screen["keys"][:2])

    def test_succeeded_unknown_and_typed_cards(self):
        after_retry = self.card({"found": True, "request_id": RID, "status": "failed", "summary": "s"},
                                dict(verified(), panel_state="ready"))
        self.assertEqual((after_retry["state"], after_retry["keys"][0]), ("succeeded", "recovery.phase.succeeded"))
        self.assertNotIn("panelUpdate.outcome.followsRecovery", after_retry["keys"])
        self.assertEqual(t.judge_update_card(after_retry, ("succeeded",))["verdict"], "as-expected")
        self.assertEqual(self.card({"found": True, "request_id": RID, "status": "succeeded"}, None)["state"],
                         "succeeded")
        busy = self.card({"found": True, "request_id": RID, "status": "failed",
                          "summary": "x: !! CELIKPANEL_UPDATE_FAILURE code=package_manager_busy state=unchanged "
                                     "reason=apt lock"}, None)
        self.assertEqual(busy["keys"][0], "panelUpdate.failed")
        self.assertEqual(busy["texts"]["tr"][1], self.translator.text("panelUpdate.packageManagerBusy", language="tr"))
        unread = self.card({"found": True, "request_id": RID, "status": "failed"}, {"schema": "other"})
        self.assertEqual(unread["state"], "unknown")
        self.assertTrue(unread["observation_error"])

    def test_text_order_and_keys_follow_the_source_not_a_frozen_copy(self):
        swapped = (self.sources["outcome"].replace("'recovery.automatic.pausedHelp'", "'@HELP@'")
                   .replace("'recovery.automatic.inspect'", "'recovery.automatic.pausedHelp'")
                   .replace("'@HELP@'", "'recovery.automatic.inspect'"))
        rules = t.parse_card_rules(dict(self.sources, outcome=swapped))
        status = {"found": True, "request_id": RID, "status": "failed"}
        paused = dict(paused_on_port(), panel_state="ready")
        original, changed = self.card(status, paused)["keys"], self.card(status, paused, rules)["keys"]
        self.assertLess(original.index("recovery.automatic.pausedHelp"), original.index("recovery.automatic.inspect"))
        self.assertLess(changed.index("recovery.automatic.inspect"), changed.index("recovery.automatic.pausedHelp"))

    def test_a_construct_outside_the_subset_is_unknown_never_a_mismatch(self):
        broken = self.sources["outcome"].replace("if (!succeeded) lines.push(",
                                                 "lines.forEach((line) => line);\n        if (!succeeded) lines.push(")
        self.assertNotEqual(broken, self.sources["outcome"])
        with self.assertRaisesRegex(t.CardModelUnavailable, "arrow function"):
            t.parse_card_rules(dict(self.sources, outcome=broken))
        card = t.update_card_guidance(self.translator, UPD3_SC_STATUS, UPD3_SC_RECOVERY, None)
        judged = t.judge_update_card(card, ("rolled_back",))
        self.assertEqual((judged["verdict"], judged["findings"]), ("unknown", []))
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(t.CardModelUnavailable):
                t.load_card_rules(Path(directory))
        runtime = self.sources["outcome"].replace("const versions =", "const later = somethingUnknown;\n    const versions =")
        late = self.card(UPD3_SC_STATUS, UPD3_SC_RECOVERY, t.parse_card_rules(dict(self.sources, outcome=runtime)))
        self.assertIn("unknown name", late["unavailable"])
        self.assertEqual(t.judge_update_card(late, ("rolled_back",))["verdict"], "unknown")

    def test_the_judge_fails_only_on_a_real_mismatch(self):
        card = self.card(UPD3_SC_STATUS, UPD3_SC_RECOVERY)
        self.assertEqual(t.judge_update_card(card, ("rolled_back",))["verdict"], "as-expected")
        wrong_state = t.judge_update_card(card, ("succeeded",))
        self.assertTrue(any("state 'rolled_back'" in f for f in wrong_state["findings"]))
        missing = t.judge_update_card(dict(card, missing_keys=["panelUpdate.outcome.gone"]), ("rolled_back",))
        self.assertTrue(any("missing" in f for f in missing["findings"]))
        unfilled = dict(card, texts={"en": ["The update to {target}"], "tr": card["texts"]["tr"]})
        self.assertTrue(any("{target}" in f for f in t.judge_update_card(unfilled, ("rolled_back",))["findings"]))


class WebSourceEvalTests(unittest.TestCase):
    """The evaluator's subset, on small sources (no product file)."""

    def module(self, source):
        return t.web_eval().Module({"x": source})

    def test_expressions_statements_and_builtins(self):
        m = self.module(
            "export const codes = ['a', 'b'] as const;\n"
            "export function pick(o: X | null, n: string): string | undefined {\n"
            "    const lines: string[] = [];\n"
            "    let title: string;\n"
            "    if (o?.kind === 'x' && (codes as readonly string[]).includes(n)) {\n"
            "        title = `t.${n as Y}`;\n"
            "        lines.push(title, 'z');\n"
            "    } else if (!o) title = 'none';\n"
            "    else title = o.kind ?? 'k';\n"
            "    return { title, lines, ...(o ? { kind: o.kind } : {}), n: lines.length > 0 ? 'yes' : 'no' };\n"
            "}\n"
            "export function clean(m: string): string {\n"
            "    const s = m.replace(/\\b(?:code|state)=\\S*/g, ' ').replace(/\\s+/g, ' ').trim();\n"
            "    return /[a-z]/.test(s) ? s : '';\n"
            "}\n"
            "export function when(v: unknown): string {\n"
            "    if (typeof v !== 'string' || !Number.isFinite(Date.parse(v))) throw new Error('bad');\n"
            "    return new Date(v).toISOString().replace('.000Z', 'Z');\n"
            "}\n")
        self.assertEqual(m.call("pick", {"kind": "x"}, "a"),
                         {"title": "t.a", "lines": ["t.a", "z"], "kind": "x", "n": "yes"})
        self.assertEqual(m.call("pick", None, "a")["title"], "none")
        self.assertEqual(m.call("pick", {"kind": "q"}, "a")["title"], "q")
        self.assertEqual(m.call("clean", "!! code=x state=y reason ok"), "!! reason ok")
        self.assertEqual(m.call("clean", "code=1 state=2"), "")
        self.assertEqual(m.call("when", "2026-10-01T12:00:00Z"), "2026-10-01T12:00:00Z")
        e = t.web_eval()
        with self.assertRaises(e.JSThrow):
            m.call("when", "yesterday")
        with self.assertRaises(e.Unsupported):
            self.module("export function f() { return items.map((i) => i); }").call("f")
        with self.assertRaises(e.Unsupported):
            self.module("export function f() { return missing; }").call("f")
        with self.assertRaises(e.Unsupported):
            self.module("export function f() { return 1 * 2; }").call("f")


class SidecarV2Tests(unittest.TestCase):
    def test_getent_exit_code_2_is_not_found_in_both_copies(self):
        for outcome in (w.getent_outcome, t.getent_outcome):
            self.assertEqual([outcome({"status": "ok", "returncode": rc}) for rc in (0, 2, 1, 3)],
                             ["found", "not-found", "probe-failed", "probe-failed"])
            self.assertEqual((outcome({"status": "timeout"}), outcome(None)), ("probe-failed", "probe-failed"))
        missing = t.origin_verdict({"getent": {"status": "ok", "returncode": 2, "stdout": ""}, "https": {"stdout": "200"}})
        self.assertEqual((missing["getent_outcome"], missing["ok"]), ("not-found", False))

    def test_a_light_inspection_survives_getent_not_found(self):
        calls = []

        def run(argv, **kwargs):
            calls.append(argv[0])
            return {"argv": argv, "status": "ok", "returncode": 2 if argv[0] == "getent" else 0, "stdout": ""}
        with mock.patch.object(w, "run", run), mock.patch.object(w, "boot_id", lambda: "b"), \
                mock.patch.object(w, "HOSTING_RECEIPT", Path("/nonexistent/hosting-root-v1.json")):
            light = w.inspect(True)
        self.assertEqual({v["outcome"] for v in light["web_accounts"].values()}, {"not-found"})
        self.assertEqual(len(light["hosting_parents"]), 4)
        self.assertNotIn("listeners", light)
        self.assertIn("getent", calls)

    def test_inspections_never_overlap_the_update_preflight(self):
        allowed = t.inspection_allowed
        self.assertTrue(allowed({}, "after-seed")[0])
        self.assertTrue(allowed({}, "before-check")[0])
        self.assertFalse(allowed({}, "pre-update")[0])            # the v1 point is gone
        in_flight = {"check_at": 1.0, "awaiting_terminal": True}
        self.assertEqual([allowed(in_flight, label)[0] for label in t.INSPECTION_POINTS],
                         [False] * len(t.INSPECTION_POINTS))
        self.assertTrue(allowed(dict(in_flight, awaiting_terminal=False), "at-pause")[0])
        arm = inspect.getsource(t.Trial.arm)
        self.assertLess(arm.index('self.inspect("before-check")'), arm.index('self.state["check_at"] = time.time()'))
        self.assertLess(arm.index('self.state["check_at"] = time.time()'), arm.index("/api/v1/panel/update/check"))
        self.assertNotIn("inspect(", inspect.getsource(t.Trial.pre_state))
        self.assertNotIn("inspect(", inspect.getsource(t.Trial.owner_start))
        track = inspect.getsource(t.Trial.track)
        self.assertLess(track.index('self.state["awaiting_terminal"] = False'), track.index("self.inspect("))
        continuation = inspect.getsource(t.Trial.owner_continuation_port)
        self.assertLess(continuation.index('self.inspect("after-continuation")'),
                        continuation.index('self.state["awaiting_terminal"] = True'))

    def test_a_refused_inspection_is_recorded_without_touching_the_guest(self):
        trial = object.__new__(t.Trial)
        trial.state = {"check_at": 1.0, "awaiting_terminal": True}
        recorded = []
        trial.record_json = lambda name, value: recorded.append((name, value))
        trial.workload = lambda *a, **k: self.fail("no guest call inside the preflight window")
        record = trial.inspect("after-track")
        self.assertFalse(record["allowed"])
        self.assertEqual(recorded[0][0], "inspect-after-track-01.json")
        trial.state["awaiting_terminal"] = False
        trial.workload = lambda mode, *a, **k: {"mode": mode, "args": a}
        trial.redactor = SimpleNamespace(text=str)
        self.assertEqual(trial.inspect("after-track")["result"], {"mode": "inspect", "args": ()})
        self.assertEqual(recorded[-1][0], "inspect-after-track-02.json")


# ---------------------------------------------------------------------------
# upd4: owner-continuation (port hold) and management-off reboot cells
# ---------------------------------------------------------------------------

class OwnerPortHoldHelperTests(unittest.TestCase):
    ARGS = SimpleNamespace(operation_id=RID)

    def state(self, update="active", pid="0", recovery="inactive", operation="update", phase="active",
              snapshot=SNAPSHOT, marker=True):
        return {"update": {"ActiveState": update}, "panel": {"MainPID": pid}, "recovery": {"ActiveState": recovery},
                "transaction": {"operation": operation, "snapshot": snapshot, "phase": phase} if marker else None}

    def run_hold(self, sequence, *, interrupt_after=None, hold=600, binder_error=None):
        clock, index, events, listeners = [0.0], [0], [], []

        def observer(args):
            value = sequence[min(index[0], len(sequence) - 1)]
            index[0] += 1
            if isinstance(value, Exception):
                raise value
            return value

        class Listener:
            closed = False

            def close(self):
                self.closed = True

        def binder():
            if binder_error:
                raise binder_error
            listeners.append(Listener())
            return listeners[-1]
        code = h.run_hold(self.ARGS, lambda e, **f: events.append(dict(f, event=e)), hold_seconds=hold,
                          observer=observer, binder=binder, clock=lambda: clock[0],
                          pause=lambda s: clock.__setitem__(0, clock[0] + s),
                          interrupted=lambda: interrupt_after is not None and index[0] >= interrupt_after)
        return code, events, listeners

    def test_the_hold_outlasts_the_updater_and_every_forward_attempt_until_the_owner_releases_it(self):
        sequence = ([self.state(pid="42")] * 3 + [self.state()] * 5 + [self.state(phase="completion.pending")] * 5
                    + [self.state(update="inactive", recovery="activating", phase="completion.pending")] * 300
                    + [self.state(update="inactive", phase="completion.pending")] * 150
                    + [self.state(update="inactive", recovery="active", phase="completion.pending")] * 300)
        code, events, listeners = self.run_hold(sequence, interrupt_after=700)
        self.assertEqual(code, 0)
        kinds = [e["event"] for e in events]
        self.assertEqual(kinds[0], "armed")
        self.assertEqual(kinds.count("port_held"), 1)
        self.assertEqual(events[-1]["reason"], h.OWNER_RELEASED)
        self.assertTrue(events[-1]["port_was_held"])
        self.assertEqual(events[-1]["phases_seen"], ["active", "completion.pending"])
        self.assertTrue(listeners[0].closed)
        state = t.port_hold_state([dict(e, at="x") for e in events])
        self.assertEqual((state["held"], state["release_reason"]), (True, h.OWNER_RELEASED))

    def test_leaving_the_forward_path_releases_at_once(self):
        for last, reason in ((self.state(operation="rollback"), "transaction-entered-rollback"),
                             (self.state(snapshot=SNAPSHOT.replace("1" * 32, "2" * 32)), "transaction-snapshot-changed"),
                             (self.state(marker=False), "transaction-finished")):
            with self.subTest(reason=reason):
                code, events, listeners = self.run_hold([self.state()] * 3 + [last])
                self.assertEqual((code, events[-1]["reason"]), (2, reason))
                self.assertTrue(listeners[0].closed)

    def test_one_ambiguous_read_is_tolerated_but_not_a_lasting_one(self):
        error = h.probe.ProbeError("ambiguous release transaction markers")
        code, events, _ = self.run_hold([self.state(), error, error, self.state(phase="completion.pending")] + [
            self.state(phase="completion.pending")] * 20, interrupt_after=20)
        self.assertEqual((code, events[-1]["reason"]), (0, h.OWNER_RELEASED))
        code, events, listeners = self.run_hold([self.state()] + [error] * 200)
        self.assertEqual((code, events[-1]["reason"]), (2, "observation-error:ProbeError"))
        self.assertTrue(listeners[0].closed)

    def test_no_hold_before_the_old_panel_stops_and_the_bound_ends_it(self):
        code, events, listeners = self.run_hold([self.state(pid="42")], hold=60)
        self.assertEqual((code, events[-1]["reason"], events[-1]["port_was_held"], listeners), (2, "timeout", False, []))
        code, events, _ = self.run_hold([self.state(pid="42"), self.state(update="inactive", pid="42")])
        self.assertEqual(events[-1]["reason"], "update-unit-exited-before-hold")
        code, events, _ = self.run_hold([self.state()], hold=60)
        self.assertEqual((code, events[-1]["reason"], events[-1]["port_was_held"]), (2, "timeout", True))
        with self.assertRaises(ValueError):
            self.run_hold([self.state()], hold=30)
        self.assertEqual((h.MIN_HOLD_SECONDS, h.MAX_HOLD_SECONDS), (60, 3600))
        self.assertIn("guest_owner_port_hold.py", t.GUEST_HELPERS)
        self.assertEqual(h.SCHEMA, t.PORT_HOLD_EVENT_SCHEMA)


class PortHoldBoundTests(unittest.TestCase):
    def sources(self):
        return {name: (REPO / path).read_text(encoding="utf-8") for name, path in t.PORT_HOLD_SOURCES.items()}

    def test_the_bound_is_derived_from_the_product_timer_wait_and_budget(self):
        texts = self.sources()
        constants = t.parse_hold_sources(texts["update"], texts["timer"], texts["runner"])
        self.assertEqual(set(constants), {"panel_start_wait_s", "timer_inactive_s", "timer_accuracy_s", "retry_budget"})
        bound = t.port_hold_bound(constants)
        a = t.PORT_HOLD_ALLOWANCES
        per = constants["panel_start_wait_s"] + a["attempt_work_s"] + constants["timer_inactive_s"] \
            + constants["timer_accuracy_s"]
        worst = a["start_work_s"] + constants["panel_start_wait_s"] + constants["retry_budget"] * per
        self.assertEqual((bound["per_attempt_s"], bound["pause_worst_s"]), (per, worst))
        self.assertGreaterEqual(bound["hold_seconds"], worst + a["owner_detection_s"] + a["owner_step_s"])
        self.assertEqual(bound["hold_seconds"] % 60, 0)
        self.assertEqual(bound["runtime_max_seconds"], bound["hold_seconds"] + 60)
        self.assertGreaterEqual(bound["hold_seconds"], 2 * max(bound["measured_pause_s"].values()))
        self.assertLessEqual(bound["runtime_max_seconds"], h.MAX_HOLD_SECONDS)
        if constants == {"panel_start_wait_s": 60, "timer_inactive_s": 30.0, "timer_accuracy_s": 1.0, "retry_budget": 3}:
            self.assertEqual((bound["hold_seconds"], bound["runtime_max_seconds"]), (1080, 1140))
        plan = t.plan_port_hold()
        self.assertEqual(plan["hold_seconds"], bound["hold_seconds"])
        self.assertIn("plan only", plan["source"])

    def test_time_spans_and_refusals(self):
        self.assertEqual([t.systemd_seconds(v) for v in ("30s", "1min 30s", "500ms", "5", "2min")],
                         [30.0, 90.0, 0.5, 5.0, 120.0])
        for value in ("30 parsecs", "", "soon"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                t.systemd_seconds(value)
        with self.assertRaisesRegex(ValueError, "reviewed product"):
            t.parse_hold_sources("", "", "")
        self.assertIn("unavailable", t.plan_port_hold(Path("/nonexistent")))


def verified(**extra):
    return product_status(dict({"request_id": RID, "observation": "known", "phase": "succeeded",
                                "terminal_proof": "update_verified", "reason": "update_verified",
                                "previous_failure": "update_failed", "failure_code": t.REAL_START_CODE}, **extra))


def paused_on_port():
    return product_status({"request_id": RID, "observation": "known", "phase": "recovery_required",
                           "terminal_proof": "none", "reason": "recovery_incomplete",
                           "previous_failure": "recovery_failed", "automatic_recovery": "paused_retry_limit",
                           "first_failure_code": t.REAL_START_CODE})


def ideal_owner_continuation(texts):
    return {"final": verified(), "paused": paused_on_port(), "update_failure_codes": [t.REAL_START_CODE],
            "hold": {"held_at_pause": True, "release_reason": "owner-released", "released_before_retry": True},
            "receipt_dispatches": [{"operation": "update", "phase": "completion"}] * 3,
            "panel_log": {"names_cause": True},
            "printed_retry_command": "/usr/libexec/celikpanel/recovery recover --retry --snapshot " + SNAPSHOT,
            "owner_retry": {"action": "executed-once", "returncode": 0}, "owner_receipts": 1,
            "outcome": "recovered-after-owner-continuation", "installed": "candidate",
            "timers": {"equal": True, "changed": {}},
            "workloads": {"web": "never-interrupted", "dns": t.DNS_NOT_PROVIDED, "smtp": "never-interrupted",
                          "cron": "never-interrupted"},
            "panel_verdict": "down-only-during-transaction", "login_ok": True,
            "cli": t.cli_text_observations([cli_sample(paused_on_port(), texts)], texts), "web_keys_missing": [],
            "update_card": {"verdict": "as-expected", "findings": []}}


def ideal_management_off():
    return {"final": verified(failure_code=None, previous_failure=None),
            "management_after_reboot": {"disabled_and_stopped": True, "units": {}}, "new_boot": True,
            "window_seconds": 200.0,
            "served": {"web": {"verdict": "served"}, "smtp": {"verdict": "served"}, "cron": {"verdict": "served"},
                       "db": {"verdict": "served"}},
            "renewal_timer": {"verdict": "as-before"}, "firewall": {"present": True, "equal_to_before": True},
            "management_return": {"login_ok": True, "differences": []}}


class Upd4CellTests(unittest.TestCase):
    def plan(self, cell, document=None):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "a.json"
            path.write_text(json.dumps(document or artifacts()))
            out = io.StringIO()
            with redirect_stdout(out):
                self.assertEqual(t.main(["plan", "--cell", cell, "--artifacts", str(path),
                                         "--work-root", "/var/tmp/cp-release-drill-upd4-x", "--dry-run"]), 0)
            return json.loads(out.getvalue())

    def test_plan_validation_of_the_owner_continuation_cells(self):
        for name, node, mail in (("upd1-debian13-owner-continuation", "debian13", True),
                                 ("upd1-arch-owner-continuation", "arch", False)):
            with self.subTest(cell=name):
                plan = self.plan(name)
                self.assertEqual((plan["cell"]["node"], plan["cell"]["variant"], plan["cell"]["mail_required"],
                                  plan["cell"]["recovery_fault"]), (node, "owner-continuation", mail, None))
                self.assertEqual(plan["candidate"]["commit"], "2" * 40)       # the good candidate G
                self.assertFalse(plan["native_evidence"])
                self.assertIn("recovered-after-owner-continuation", plan["expected_outcome"])
                self.assertEqual(plan["port_hold"]["hold_seconds"], t.plan_port_hold()["hold_seconds"])
                self.assertEqual(plan["port_hold"]["address"], "127.0.0.1:2083")
                steps = {s["name"]: s["does"] for s in plan["steps"]}
                self.assertIn("owner port hold", steps["arm"])
                self.assertIn("release the port", steps["owner-continuation (required)"])
                self.assertIn("exactly the printed retry command once", steps["owner-continuation (required)"])
                self.assertIn("kind-expectation", steps)
                self.assertNotIn("defect", plan)
                self.assertIn("guest_owner_port_hold.py", plan["provenance"]["defect"])

    def test_plan_validation_of_the_management_off_cells(self):
        for name, node in (("upd1-debian13-mgmt-off-reboot", "debian13"), ("upd1-arch-mgmt-off-reboot", "arch")):
            with self.subTest(cell=name):
                plan = self.plan(name)
                self.assertEqual((plan["cell"]["node"], plan["cell"]["variant"]), (node, "mgmt-off-reboot"))
                names = [s["name"] for s in plan["steps"]]
                self.assertEqual(names[names.index("terminal"):names.index("collect")],
                                 ["terminal", "management-off", "owner-reboot", "management-off-measure",
                                  "management-return"])
                steps = {s["name"]: s["does"] for s in plan["steps"]}
                self.assertIn("disable --now celikpanel-panel.service celikpanel-agent.service", steps["management-off"])
                self.assertIn("databases", steps["seed"])
                self.assertIn("up to management-off", steps["verdicts"])
                self.assertEqual(plan["management_off"]["measure_seconds"], 180.0)
                self.assertIn("native client", plan["expected_outcome"])
                self.assertNotIn("port_hold", plan)

    def test_classification_of_the_new_variants(self):
        recovered = {"observation": "known", "phase": "recovered", "terminal_proof": "rollback_verified"}
        self.assertEqual(t.classify_outcome("owner-continuation", verified(), True), "recovered-after-owner-continuation")
        self.assertEqual(t.classify_outcome("owner-continuation", verified(), False),
                         "update-verified-without-owner-continuation")
        self.assertEqual(t.classify_outcome("owner-continuation", recovered, False), "rolled-back-instead-of-forward")
        self.assertEqual(t.classify_outcome("owner-continuation", paused_on_port(), True),
                         "paused-owner-action-required")
        self.assertEqual(t.classify_outcome("mgmt-off-reboot", verified(), False), "update-verified")
        self.assertEqual(t.candidate_role(t.CELLS["upd1-arch-mgmt-off-reboot"]), "good")
        self.assertEqual(t.cell_roles(t.CELLS["upd1-arch-owner-continuation"]), ("baseline", "good", "defective"))

    def test_owner_continuation_judge(self):
        texts = cli_texts()
        judged = t.judge_owner_continuation(ideal_owner_continuation(texts))
        self.assertEqual((judged["verdict"], judged["findings"], judged["unknown"]), ("as-expected", [], []))
        for change, rule in (
                ({"hold": {"held_at_pause": False, "release_reason": "timeout", "released_before_retry": True}},
                 "did not last until the pause"),
                ({"outcome": "update-verified-without-owner-continuation"}, "expected recovered-after"),
                ({"timers": {"equal": False, "changed": {"certbot.timer": {}}}}, "certbot.timer"),
                ({"owner_receipts": 2}, "not run exactly once"),
                ({"panel_log": {"names_cause": False}}, "does not show the port conflict"),
                ({"receipt_dispatches": [{"operation": "update", "phase": "completion"}] * 2}, "three forward"),
                ({"final": paused_on_port()}, "did not complete forward"),
                ({"paused": None}, "never paused"),
                ({"workloads": {"web": "interrupted"}}, "web"),
                ({"login_ok": False}, "did not come back")):
            with self.subTest(rule=rule):
                judged = t.judge_owner_continuation(dict(ideal_owner_continuation(texts), **change))
                self.assertEqual(judged["verdict"], "finding")
                self.assertTrue(any(rule in f for f in judged["findings"]), judged["findings"])

    def test_management_off_judge(self):
        judged = t.judge_management_off(ideal_management_off())
        self.assertEqual((judged["verdict"], judged["findings"], judged["unknown"]), ("as-expected", [], []))
        arch = dict(ideal_management_off(), served=dict(ideal_management_off()["served"], smtp={"verdict": "not-seeded"}))
        self.assertEqual(t.judge_management_off(arch)["verdict"], "as-expected")
        for change, rule in (({"window_seconds": 120.0}, "fewer than 180"),
                             ({"served": dict(ideal_management_off()["served"], db={"verdict": "never-served"})},
                              "db with management off"),
                             ({"renewal_timer": {"verdict": "changed"}}, "renewal timer"),
                             ({"firewall": {"present": False, "equal_to_before": False}}, "firewall"),
                             ({"management_after_reboot": {"disabled_and_stopped": False, "units": {}}},
                              "not disabled and stopped"),
                             ({"management_return": {"login_ok": True, "differences": ["cron_listed"]}},
                              "after management returned")):
            with self.subTest(rule=rule):
                judged = t.judge_management_off(dict(ideal_management_off(), **change))
                self.assertEqual(judged["verdict"], "finding")
                self.assertTrue(any(rule in f for f in judged["findings"]), judged["findings"])


class ManagementOffRuleTests(unittest.TestCase):
    def window(self, boot="B2", count=45, **overrides):
        samples = []
        for i in range(count):
            moment = 1000.0 + 5 * i
            sample = {"t": moment, "boot_id": boot, "monotonic": 20.0 + 5 * i, "web": {"ok": True},
                      "smtp": {"ok": True}, "db": {"ok": True},
                      "cron": {"ok": True, "mtime": 1000.0 + 60 * ((5 * i) // 60)}}
            for key, rule in overrides.items():
                sample[key] = {"ok": rule(i)}
            samples.append(sample)
        return samples

    def test_served_after_boot(self):
        window = self.window()
        self.assertEqual(t.window_seconds(window), 220.0)
        self.assertEqual(t.served_after_boot(window, "web")["verdict"], "served")
        late = self.window(web=lambda i: i >= 3)
        self.assertEqual((t.served_after_boot(late, "web")["verdict"],
                          t.served_after_boot(late, "web")["failing_before_first_ok"]), ("served", 3))
        self.assertEqual(t.served_after_boot(self.window(db=lambda i: i != 20), "db")["verdict"], "interrupted")
        self.assertEqual(t.served_after_boot(self.window(db=lambda i: False), "db")["verdict"], "never-served")
        self.assertEqual(t.served_after_boot([{"t": 1, "web": {"ok": None}}], "web")["verdict"], "not-measured")
        self.assertEqual(t.cron_after_boot(window)["verdict"], "served")
        stale = [dict(s, cron={"ok": True, "mtime": 900.0}) for s in window]
        self.assertEqual(t.cron_after_boot(stale)["verdict"], "not-advancing")      # a stamp from before the boot
        self.assertEqual(t.boot_window(window + self.window(boot="B1", count=3), "B2"), window)

    def test_management_state_renewal_timer_and_truth(self):
        off = {u: {"ActiveState": "inactive", "UnitFileState": "disabled"} for u in t.MANAGEMENT_UNITS}
        on = {u: {"ActiveState": "active", "UnitFileState": "enabled"} for u in t.MANAGEMENT_UNITS}
        self.assertTrue(t.management_state(off)["disabled_and_stopped"])
        self.assertTrue(t.management_state(on)["enabled_and_active"])
        self.assertFalse(t.management_state(dict(off, **{"celikpanel-agent.service": on["celikpanel-agent.service"]}))
                         ["disabled_and_stopped"])
        debian = {"certbot.timer": {"UnitFileState": "enabled", "ActiveState": "active"}}
        self.assertEqual(t.renewal_timer_verdict(debian, debian)["verdict"], "as-before")
        stopped = {"certbot.timer": {"UnitFileState": "enabled", "ActiveState": "inactive"}}
        self.assertEqual(t.renewal_timer_verdict(debian, stopped)["verdict"], "changed")     # F2's shape
        arch = {"certbot-renew.timer": {"UnitFileState": "disabled", "ActiveState": "inactive"}}
        self.assertEqual(t.renewal_timer_verdict(arch, arch)["verdict"], "as-before")
        self.assertEqual(t.renewal_timer_verdict({}, {})["verdict"], "not-present")
        self.assertEqual(t.truth_differences({"a": 1, "b": 2}, {"a": 1, "b": 3}), ["b: 2 -> 3"])

    def flow_trial(self):
        trial = object.__new__(t.Trial)
        trial.cell = t.CELLS["upd1-debian13-mgmt-off-reboot"]
        trial.state = {"findings": [], "resets": [], "request_id": RID,
                       "seed": {"domain_id": 7, "mail": {"listed": True}, "cron": {"seeded": True},
                                "database": {"seeded": True, "name": "upd1_owner_test_upd1db"}},
                       "pre_workload": {"timers": {"certbot.timer": {"UnitFileState": "enabled",
                                                                     "ActiveState": "active"}}}}
        trial.calls = []
        trial.guest = lambda body, timeout=120: trial.calls.append(body) or SimpleNamespace(returncode=0, stdout="")
        return trial

    def test_management_off_issues_the_owner_command_once(self):
        trial = self.flow_trial()
        off = {u: {"ActiveState": "inactive", "UnitFileState": "disabled"} for u in t.MANAGEMENT_UNITS}
        on = {u: {"ActiveState": "active", "UnitFileState": "enabled"} for u in t.MANAGEMENT_UNITS}
        snapshots = iter([{"services": on, "firewall": {"sha256": "f"}}, {"services": off}])
        trial.workload_snapshot = lambda label: next(snapshots)
        trial.panel_truth = lambda label: {"label": label}
        checks = {}
        self.assertEqual(trial.management_off(checks), "passed")
        self.assertEqual(trial.calls, ["systemctl disable --now celikpanel-panel.service celikpanel-agent.service"])
        self.assertIn("management_off_at", trial.state)
        self.assertEqual(trial.state["truth_before_off"], {"label": "before-management-off"})

    def test_the_measure_names_what_needed_the_panel(self):
        trial = self.flow_trial()
        trial.state.update(owner_reboot={"after_boot_id": "B2", "new_boot": True},
                           before_off_workload={"firewall": {"sha256": "f"}})
        off = {u: {"ActiveState": "inactive", "UnitFileState": "disabled"} for u in t.MANAGEMENT_UNITS}
        snapshot = {"services": off, "timers": trial.state["pre_workload"]["timers"],
                    "firewall": {"sha256": "f", "tables": ["inet filter"]}}
        good = trial.management_off_observations(self.window(), snapshot)
        self.assertEqual(good["needed_panel"], [])
        self.assertEqual({k: v["verdict"] for k, v in good["served"].items()},
                         {"web": "served", "smtp": "served", "cron": "served", "db": "served"})
        bad = trial.management_off_observations(self.window(db=lambda i: False),
                                                dict(snapshot, firewall={"sha256": "g", "tables": []}))
        self.assertIn("db: never-served", bad["needed_panel"])
        self.assertIn("firewall ruleset absent or changed", bad["needed_panel"])

    def test_verdicts_judge_the_update_part_only(self):
        trial = object.__new__(t.Trial)
        trial.cell = t.CELLS["upd1-debian13-mgmt-off-reboot"]
        samples = [{"t": float(i * 5), "web": {"ok": True}, "panel": {"ok": i < 20}, "smtp": {"ok": True},
                    "cron": {"ok": True, "mtime": float(60 * (i * 5 // 60))}} for i in range(60)]
        trial.state = {"findings": [], "resets": [], "samples": samples, "clock_skew": 0.0, "started_at": 1.0,
                       "terminal_at": 50.0, "management_off_at": 98.0, "seed": {"mail": {"listed": True}},
                       "cron_availability": {"available": True}, "cron_precondition": True, "status_samples": []}
        trial.host_samples = []
        trial.dns_mode = "external"
        checks = {}
        self.assertEqual(trial.verdicts(checks), "passed")
        self.assertEqual(checks["samples_until_management_off"], 20)
        trial.state.pop("management_off_at")
        with self.assertRaises(t.StepFailed):
            t.Trial.verdicts(trial, {})


class OwnerContinuationFlowTests(unittest.TestCase):
    def trial(self, *, printed=True, held=True):
        trial = object.__new__(t.Trial)
        trial.cell = t.CELLS["upd1-debian13-owner-continuation"]
        trial.artifacts = artifacts()
        trial.role = "good"
        trial.candidate = trial.artifacts["good"]
        trial.redactor = SimpleNamespace(text=str)
        trial.node_name, trial.root = "debian13", Path("/nonexistent")
        trial.state = {"findings": [], "resets": [], "request_id": RID, "port_hold_unit": "hold.service",
                       "awaiting_terminal": False}
        trial.step_dir = "steps/12-owner-continuation-required"
        trial.ev = SimpleNamespace(write_text=lambda *a: None)
        trial.calls = []
        released = [False]
        armed = {"event": "armed", "at": "a"}
        held_event = {"event": "port_held", "at": "b", "phase": "active"}

        def events():
            base = [armed, held_event] if held else [armed, held_event,
                                                     {"event": "released", "reason": "timeout", "at": "c"}]
            if released[0] and held:
                base = base + [{"event": "released", "reason": "owner-released", "at": "d", "held_seconds": 500}]
            return base
        trial.port_hold_events = events

        def guest(body, timeout=120):
            trial.calls.append(("guest", body))
            if body.startswith("systemctl stop"):
                released[0] = True
            return SimpleNamespace(returncode=0, stdout="")
        trial.guest = guest

        def workload(mode, *args, timeout=120):
            trial.calls.append((mode, args))
            if mode == "journal":
                return {"stdout": "panel[9]: listen tcp :2083: bind: address already in use\n"}
            if mode == "owner-retry" and "--execute" not in args:
                return {"action": "validated-not-executed", "argv": ["/usr/libexec/celikpanel/recovery", "recover",
                                                                       "--retry", "--snapshot", SNAPSHOT]
                        if printed else [], "journal_text": "j"}
            if mode == "owner-retry":
                return {"action": "executed-once", "result": {"returncode": 0}}
            raise AssertionError(mode)
        trial.workload = workload
        trial.workload_snapshot = lambda label: trial.calls.append(("snapshot", label)) or {"timers": {}}
        trial.inspect = lambda label, light=False: trial.calls.append(("inspect", label))
        trial.trial = SimpleNamespace(save=lambda *a: trial.calls.append(("save", a[2])))
        return trial

    def test_the_owner_reads_the_log_releases_the_port_then_runs_the_printed_retry_once(self):
        trial = self.trial()
        checks = {}
        self.assertEqual(trial.owner_continuation_port(checks, RID, SNAPSHOT, paused_on_port()), "observed")
        order = [c[0] if c[0] != "guest" else "stop" for c in trial.calls]
        self.assertEqual(order, ["journal", "snapshot", "owner-retry", "stop", "inspect", "save", "owner-retry"])
        retries = [c for c in trial.calls if c[0] == "owner-retry"]
        self.assertNotIn("--execute", retries[0][1])
        self.assertIn("--execute", retries[1][1])
        self.assertEqual(sum("--execute" in c[1] for c in retries), 1)
        self.assertEqual(trial.calls[3][1], "systemctl stop hold.service")
        self.assertTrue(trial.state["owner_continued"])
        self.assertTrue(trial.state["awaiting_terminal"])
        self.assertEqual((trial.state["hold"]["release_reason"], trial.state["hold"]["released_before_retry"],
                          trial.state["hold"]["held_at_pause"]), ("owner-released", True, True))
        self.assertTrue(trial.state["panel_log"]["names_cause"])
        self.assertEqual(trial.state["owner_retry"], {"action": "executed-once", "returncode": 0})
        self.assertEqual(trial.state["printed_retry_command"],
                         "/usr/libexec/celikpanel/recovery recover --retry --snapshot " + SNAPSHOT)

    def test_no_printed_command_runs_nothing(self):
        trial = self.trial(printed=False)
        with self.assertRaisesRegex(t.StepInconclusive, "no one-time retry"):
            trial.owner_continuation_port({}, RID, SNAPSHOT, paused_on_port())
        self.assertFalse(any(c[0] == "guest" for c in trial.calls))
        self.assertFalse(trial.state.get("owner_continued"))

    def test_a_hold_that_ended_before_the_pause_is_a_finding(self):
        trial = self.trial(held=False)
        checks = {}
        trial.owner_continuation_port(checks, RID, SNAPSHOT, paused_on_port())
        self.assertFalse(trial.state["hold"]["held_at_pause"])
        self.assertTrue(any("not held at the pause" in f for f in trial.state["findings"]))
        self.assertFalse(any(c[0] == "guest" for c in trial.calls))        # nothing left to stop
        self.assertIn("already ended", checks["release"]["note"])
        self.assertEqual(sum("--execute" in c[1] for c in trial.calls if c[0] == "owner-retry"), 1)

    def test_owner_continuation_dispatches_to_the_port_path(self):
        source = inspect.getsource(t.Trial.owner_continuation)
        self.assertIn('if self.cell.variant == "owner-continuation":', source)
        self.assertIn("return self.owner_continuation_port(checks, rid, snapshot, paused)", source)


class GuestDatabaseTests(unittest.TestCase):
    MARKER = "upd1-marker-" + "a" * 32

    def test_owner_table_and_read_only_query(self):
        statements = w.db_statements(self.MARKER)
        self.assertIn("CREATE TABLE IF NOT EXISTS upd1_owner", statements)
        self.assertIn(self.MARKER, statements)
        with self.assertRaises(ValueError):
            w.db_statements("x'; DROP TABLE y; --")
        self.assertTrue(w.db_query_statement().startswith("SELECT marker FROM upd1_owner"))
        for name, ok in (("upd1_owner_test_upd1db", True), ("1abc", False), ("a;b", False), ("a" * 65, False)):
            self.assertEqual(bool(w.DB_NAME_RE.fullmatch(name)), ok)

    def test_probe_db(self):
        answer = {"status": "ok", "returncode": 0, "stdout": self.MARKER + "\n", "stderr": ""}
        with mock.patch.object(w, "db_client", lambda: "/usr/bin/mariadb"), \
                mock.patch.object(w, "run", lambda argv, **k: answer):
            self.assertTrue(w.probe_db("upd1db", self.MARKER)["ok"])
            self.assertFalse(w.probe_db("upd1db", "upd1-marker-" + "b" * 32)["ok"])
        with mock.patch.object(w, "db_client", lambda: None):
            self.assertEqual(w.probe_db("upd1db", self.MARKER)["error"], "no-native-client")
        self.assertIsNone(w.probe_db(None, self.MARKER)["ok"])
        self.assertIn('["--db-name", args.db_name]', inspect.getsource(w.install_sampler))


def _git(repository, *args):
    environment = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
    return subprocess.run(["git", "-C", str(repository), "-c", "core.autocrlf=false", "-c", "commit.gpgsign=false",
                           "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", *args],
                          check=True, capture_output=True, text=True, env=environment).stdout.strip()


@unittest.skipUnless(shutil.which("git") and hasattr(os, "O_NOFOLLOW"), "real Git and POSIX archive reads are required")
class ArtifactProofTests(unittest.TestCase):
    """H2: the host-side proof of all three archives with dns-owner-tools/ (synthetic, offline)."""

    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.modules = t.lab_modules()
        self.archive = self.modules["archive"]
        repo = self.root / "repo"
        repo.mkdir()
        _git(repo, "init", "-q")
        statics = [n for n in self.archive.REQUIRED if not n.startswith(("bin/", "web/dist/"))]
        self.sources = {"download-portal/get.sh" if n == "libexec/get.sh" else n: n for n in statics}
        for source in list(self.sources) + ["cmd/dns-peer-enroll/README.md", "cmd/panel/main.go"]:
            (repo / source).parent.mkdir(parents=True, exist_ok=True)
            (repo / source).write_bytes(f"committed {source}\n".encode())
        policy = repo / "deploy" / "release-sequence-policy"
        policy.write_text(t.policy_text(t.BASELINE_VERSION, 81, 80, "v0.1.0-alpha.80", t.ALPHA80_COMMIT))
        _git(repo, "add", "-A")
        _git(repo, "commit", "-q", "-m", "B")
        baseline = _git(repo, "rev-parse", "HEAD")
        policy.write_text(t.policy_text(t.CANDIDATE_VERSION, 82, 81, t.BASELINE_VERSION, baseline))
        _git(repo, "commit", "-q", "-am", "G")
        good = _git(repo, "rev-parse", "HEAD")
        (repo / "cmd/panel/main.go").write_text("defective fixture\n")
        _git(repo, "commit", "-q", "-am", "D")
        defective = _git(repo, "rev-parse", "HEAD")
        web = self.root / "product-web-src"
        (web / "i18n").mkdir(parents=True)
        self.repo = repo
        self.document = {"schema": t.ARTIFACTS_SCHEMA, "source_head": baseline, "clone": str(repo),
                         "baseline": self.item(baseline, t.BASELINE_VERSION, 81, web),
                         "good": dict(self.item(good, t.CANDIDATE_VERSION, 82, web), parent=baseline),
                         "defective": dict(self.item(defective, t.CANDIDATE_VERSION, 82, web), parent=good)}

    def item(self, commit, version, sequence, web, readme=None):
        tree = _git(self.repo, "rev-parse", commit + "^{tree}")
        files = {}
        for source, name in self.sources.items():
            files[name] = subprocess.run(["git", "-C", str(self.repo), "show", f"{commit}:{source}"], check=True,
                                         capture_output=True).stdout
        files["deploy/release-sequence-policy"] = subprocess.run(
            ["git", "-C", str(self.repo), "show", f"{commit}:deploy/release-sequence-policy"],
            check=True, capture_output=True).stdout
        for name in self.archive.REQUIRED:
            if name.startswith(("bin/", "web/dist/")):
                files[name] = f"build output {name} {commit}\n".encode()
        committed_readme = subprocess.run(["git", "-C", str(self.repo), "show", f"{commit}:cmd/dns-peer-enroll/README.md"],
                                          check=True, capture_output=True).stdout
        files["dns-owner-tools/README.md"] = committed_readme if readme is None else readme
        for tool in ("dns-peer-enroll", "bind-peer-inspect", "pdns-peer-inspect"):
            files["dns-owner-tools/" + tool] = f"tool {tool}\n".encode()
        files[t.ACCEPTANCE_NOTICE] = t.ACCEPTANCE_NOTICE_PREFIX + b"\nfixture\n"
        files.update({"release.version": b"1\n", "release.commit": (commit + "\n").encode(),
                      "release.tree": (tree + "\n").encode()})
        files["SHA256SUMS"] = "".join(hashlib.sha256(files[n]).hexdigest() + "  ./" + n + "\n"
                                      for n in sorted(files)).encode()
        path = self.root / f"{commit}.tar.gz"
        root = "celikpanel-" + version
        with tarfile.open(path, "w:gz") as bundle:
            entry = tarfile.TarInfo(root)
            entry.type = tarfile.DIRTYPE
            bundle.addfile(entry)
            for name, data in files.items():
                entry = tarfile.TarInfo(root + "/" + name)
                entry.size, entry.mode = len(data), 0o755
                bundle.addfile(entry, io.BytesIO(data))
        return {"version": version, "sequence": sequence, "commit": commit, "tree": tree, "archive": str(path),
                "sha256": hashlib.sha256(path.read_bytes()).hexdigest(), "license_mode": "acceptance-fixture",
                "product_web_src": str(web)}

    def test_three_archives_are_proved_with_the_owner_tools_readme(self):
        t.validate_artifacts(self.document)
        proofs = t.prove_artifacts(self.document, ("baseline", "good", "defective"), self.modules)
        for role, proof in proofs.items():
            with self.subTest(role=role):
                self.assertEqual(proof["commit"], self.document[role]["commit"])
                self.assertEqual(len(proof["dns_owner_tools"]), 4)
                self.assertEqual(proof["source_proof"]["exempted_from_git_proof"], [t.ACCEPTANCE_NOTICE])
                self.assertEqual(proof["source_proof"]["verified_static_files"], len(self.sources) + 2)  # + policy + README
        path = self.root / "upd1-artifacts.json"
        path.write_text(json.dumps(self.document))
        out = io.StringIO()
        with redirect_stdout(out):
            self.assertEqual(t.main(["prove", "--artifacts", str(path)]), 0)
        self.assertEqual(sorted(json.loads(out.getvalue())["proofs"]), ["baseline", "defective", "good"])

    def test_five_archives_with_the_upd3_start_kinds_are_proved(self):
        good = self.document["good"]["commit"]
        web = Path(self.document["good"]["product_web_src"])
        for role, name in (("startcheck", "cmd/panel/server_lifecycle.go"), ("realstart", "cmd/panel/main.go")):
            _git(self.repo, "checkout", "-q", "--detach", good)
            (self.repo / name).write_text(f"{role} fixture\n")
            _git(self.repo, "add", "-A")
            _git(self.repo, "commit", "-q", "-m", role)
            commit = _git(self.repo, "rev-parse", "HEAD")
            self.document[role] = dict(self.item(commit, t.CANDIDATE_VERSION, 82, web), parent=good)
        t.validate_artifacts(self.document)
        t.validate_cell_artifacts(self.document, t.CELLS["upd1-debian13-realstart"])
        path = self.root / "upd1-artifacts.json"
        path.write_text(json.dumps(self.document))
        out = io.StringIO()
        with redirect_stdout(out):
            self.assertEqual(t.main(["prove", "--artifacts", str(path)]), 0)
        self.assertEqual(sorted(json.loads(out.getvalue())["proofs"]),
                         ["baseline", "defective", "good", "realstart", "startcheck"])

    def test_an_owner_tools_readme_not_from_the_commit_is_refused(self):
        web = Path(self.document["good"]["product_web_src"])
        good = self.document["good"]
        self.document["good"] = dict(self.item(good["commit"], t.CANDIDATE_VERSION, 82, web, readme=b"edited\n"),
                                     parent=good["parent"])
        with self.assertRaisesRegex(ValueError, "dns-owner-tools/README.md"):
            t.prove_artifacts(self.document, ("good",), self.modules)


if __name__ == "__main__":
    unittest.main()
