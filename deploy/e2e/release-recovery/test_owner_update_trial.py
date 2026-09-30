"""Offline tests for the upd1 owner-started update trial (no guest, no network)."""
import base64
import hashlib
import importlib.util
import io
import json
import re
import sys
import tarfile
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from types import SimpleNamespace

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[2]


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


def artifacts():
    def item(version, sequence, commit, parent=None):
        value = {"version": version, "sequence": sequence, "commit": commit, "tree": "e" * 40, "sha256": "f" * 64,
                 "archive": "/nonexistent.tar.gz", "license_mode": "acceptance-fixture",
                 "product_web_src": str(REPO / "web" / "src")}
        if parent:
            value["parent"] = parent
        return value
    return {"schema": t.ARTIFACTS_SCHEMA, "source_head": "9" * 40, "clone": "/var/tmp/cp-upd1-build/x/repo",
            "baseline": item("v0.1.0-alpha.81", 81, "1" * 40),
            "good": item("v0.1.0-alpha.82", 82, "2" * 40, "1" * 40),
            "defective": item("v0.1.0-alpha.82", 82, "3" * 40, "2" * 40)}


class PlanTests(unittest.TestCase):
    def test_four_cells_with_the_selected_second_faults(self):
        self.assertEqual(sorted(t.CELLS), ["upd1-arch-defective", "upd1-arch-good", "upd1-debian13-defective",
                                           "upd1-debian13-good"])
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
        card = t.update_card_guidance(translator, {"found": True, "request_id": RID, "status": "failed"})
        self.assertEqual((card["keys"], card["missing_keys"]), (["panelUpdate.failed"], []))
        self.assertNotEqual(card["texts"]["en"], card["texts"]["tr"])
        summary = t.update_card_guidance(translator, {"found": True, "status": "failed", "summary": "update failed"})
        self.assertTrue(summary["untranslated_summary"])
        self.assertEqual(summary["texts"]["tr"], ["update failed"])
        self.assertEqual(t.update_card_guidance(translator, {"found": False})["texts"], {"en": [], "tr": []})

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


if __name__ == "__main__":
    unittest.main()
