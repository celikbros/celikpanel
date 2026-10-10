"""Offline tests of set6's pure rules: the pinning verdict by both lookup paths, the preflight that knows the lab's
own origin line, the header judgement, the base64 pass of the digest rules, and that the cells borrow the sections
of the earlier drivers without replacing them."""
import base64
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import set6_redact  # noqa: E402
import set6_trial  # noqa: E402

A, B = "ab" * 32, "cd" * 32


def answer(text):
    return {"returncode": 0, "stdout": text}


def reading(ahosts, hosts):
    return {"ahosts": answer(ahosts), "hosts": answer(hosts)}


def request(name, status, hsts, *, scheme="https", path="/", method="GET"):
    return {"name": name, "scheme": scheme, "host": "127.0.0.1", "port": 2083, "method": method, "path": path,
            "status": status, "strict_transport_security": hsts, "headers": [], "as_logged_in_owner": False}


def full_reading(value, plain=None):
    requests = [request("spa-root", 200, [value]), request("api-public", 200, [value], path="/api/v1/panel/access-address"),
                request("api-domains-no-login", 401, [value], path="/api/v1/domains"),
                request("api-unknown-as-owner", 404, [value], path="/api/v1/set6-no-such-route")]
    requests += plain if plain is not None else [
        {"name": "plain-http-to-the-panel-port-2083", "scheme": "http", "host": "127.0.0.1", "port": 2083, "answered": True,
         "status_line": "HTTP/1.0 400 Bad Request", "status": 400, "headers": [], "strict_transport_security": [],
         "header_name_anywhere_in_the_answer": False, "body_start": "Client sent an HTTP request to an HTTPS server."}]
    return {"requests": requests}


def verdicts(checks):
    return [ok for _name, ok, _detail in checks]


class PinningTest(unittest.TestCase):
    def test_both_paths_must_answer_loopback_for_every_name(self):
        loop = reading("127.0.0.1 STREAM x\n127.0.0.1 DGRAM", "127.0.0.1 localhost")
        good = {"resolves": {n: loop for n in set6_trial.PINNED_NAMES}, "default_path": {n: loop for n in set6_trial.PINNED_NAMES}}
        self.assertTrue(set6_trial.pin_holds(set6_trial.pin_verdict(good)))
        outside = dict(good, default_path=dict(good["default_path"], **{"celikpanel.net": reading("203.0.113.9 STREAM x", "203.0.113.9 x")}))
        verdict = set6_trial.pin_verdict(outside)
        self.assertTrue(verdict["celikpanel.net"]["hosts_file"])
        self.assertFalse(verdict["celikpanel.net"]["default_path"])
        self.assertFalse(set6_trial.pin_holds(verdict))

    def test_a_default_path_that_was_not_asked_is_not_a_pass(self):
        loop = reading("127.0.0.1 STREAM x", "127.0.0.1 x")
        only_files = {"resolves": {n: loop for n in set6_trial.PINNED_NAMES}, "default_path": None}
        verdict = set6_trial.pin_verdict(only_files)
        self.assertIsNone(verdict["celikpanel.net"]["default_path"])
        self.assertFalse(set6_trial.pin_holds(verdict))
        self.assertFalse(set6_trial.pin_holds({}))

    def test_the_pinned_names_are_the_origin_and_the_four_of_set5(self):
        self.assertEqual(set6_trial.PINNED_NAMES[0], "celikpanel.net")
        self.assertEqual(set(set6_trial.PINNED_NAMES[1:]), {"acme-v02.api.letsencrypt.org", "acme-staging-v02.api.letsencrypt.org",
                                                           "acme.zerossl.com", "dv.acme-v02.api.pki.goog"})

    def test_the_origin_line_is_the_one_the_fixture_origin_accepts(self):
        spec = importlib.util.spec_from_file_location("tested_set6_origin", HERE / "worker_fixture_origin.py")
        origin = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(origin)
        self.assertEqual(set6_trial.ORIGIN_PIN_LINE, origin.LAB_PRE_PIN_LINE)
        fields = set6_trial.ORIGIN_PIN_LINE.split("#", 1)[0].split()
        self.assertEqual(fields, ["127.0.0.1", "celikpanel.net"])

    def test_the_preflight_takes_the_labs_own_line_and_nothing_else(self):
        original = set6_trial.base.hosts_mappings
        mappings = set6_trial.tolerant_mappings(original)
        own = "127.0.0.1 localhost\n" + set6_trial.ORIGIN_PIN_LINE + "\n"
        self.assertEqual(mappings(own), [])
        self.assertEqual(mappings.seen, [set6_trial.ORIGIN_PIN_LINE])
        other = own + "203.0.113.9 celikpanel.net\n"
        self.assertEqual(len(mappings(other)), 2)
        fixture = "127.0.0.1 celikpanel.net # disposable CelikPanel worker fixture\n"
        self.assertEqual(mappings(fixture), original(fixture))
        self.assertEqual(mappings("127.0.0.1 localhost\n"), [])

    def test_resolved_transactions(self):
        self.assertEqual(set6_trial.resolved_transactions(answer("Transactions\nCurrent Transactions: 0\n  Total Transactions: 17\n")), 17)
        self.assertIsNone(set6_trial.resolved_transactions(None))
        self.assertIsNone(set6_trial.resolved_transactions(answer("")))

    def test_the_guest_scripts_are_valid_python(self):
        compile(set6_trial.PIN_SCRIPT, "pin", "exec")
        compile(set6_trial.HEADER_SCRIPT.replace("@@COOKIE@@", json.dumps("a-value")), "headers", "exec")
        self.assertEqual(set6_trial.HEADER_SCRIPT.count("@@COOKIE@@"), 1)


class HeaderTest(unittest.TestCase):
    def test_the_candidate_value_on_every_route(self):
        checks = set6_trial.header_judgement(full_reading(set6_trial.HSTS_CANDIDATE), set6_trial.HSTS_CANDIDATE)
        self.assertEqual(verdicts(checks), [True, True, True, True])

    def test_the_old_value_fails_the_candidate_and_passes_as_the_baseline(self):
        old = full_reading(set6_trial.HSTS_ALPHA81)
        checks = set6_trial.header_judgement(old, set6_trial.HSTS_CANDIDATE)
        self.assertEqual(verdicts(checks), [False, True, False, True])
        self.assertEqual(verdicts(set6_trial.header_judgement(old, set6_trial.HSTS_ALPHA81)), [True, True, True])

    def test_one_route_without_the_header_or_with_two_lines_fails(self):
        one = full_reading(set6_trial.HSTS_CANDIDATE)
        one["requests"][2]["strict_transport_security"] = []
        self.assertIs(set6_trial.header_judgement(one, set6_trial.HSTS_CANDIDATE)[0][1], False)
        two = full_reading(set6_trial.HSTS_CANDIDATE)
        two["requests"][0]["strict_transport_security"] = [set6_trial.HSTS_CANDIDATE, set6_trial.HSTS_CANDIDATE]
        self.assertIs(set6_trial.header_judgement(two, set6_trial.HSTS_CANDIDATE)[0][1], False)

    def test_a_route_class_that_was_not_reached_is_not_a_pass(self):
        short = full_reading(set6_trial.HSTS_CANDIDATE)
        short["requests"] = [r for r in short["requests"] if r.get("status") != 404]
        checks = set6_trial.header_judgement(short, set6_trial.HSTS_CANDIDATE)
        self.assertIsNone(checks[1][1])
        self.assertFalse(checks[1][2]["a 404"])

    def test_an_https_request_that_got_no_answer_is_not_a_pass(self):
        lost = full_reading(set6_trial.HSTS_CANDIDATE)
        lost["requests"].append({"name": "spa-asset", "scheme": "https", "error": "TimeoutError"})
        self.assertIs(set6_trial.header_judgement(lost, set6_trial.HSTS_CANDIDATE)[0][1], False)
        nothing = {"requests": [{"name": "spa-root", "scheme": "https", "error": "ConnectionRefusedError"}]}
        self.assertIsNone(set6_trial.header_judgement(nothing, set6_trial.HSTS_CANDIDATE)[0][1])

    def test_plain_http(self):
        with_header = full_reading(set6_trial.HSTS_CANDIDATE, plain=[
            {"name": "plain-http-to-the-panel-port-2083", "scheme": "http", "answered": True, "status_line": "HTTP/1.1 200 OK",
             "headers": [["Strict-Transport-Security", "max-age=31536000"]], "strict_transport_security": ["max-age=31536000"]}])
        self.assertIs(set6_trial.header_judgement(with_header, set6_trial.HSTS_CANDIDATE)[-1][1], False)
        refused = full_reading(set6_trial.HSTS_CANDIDATE, plain=[
            {"name": "plain-http-to-the-panel-port-2083", "scheme": "http", "answered": False, "error": "ConnectionResetError"}])
        self.assertIs(set6_trial.header_judgement(refused, set6_trial.HSTS_CANDIDATE)[-1][1], True)
        only_the_web_server = full_reading(set6_trial.HSTS_CANDIDATE, plain=[
            {"name": "plain-http-port-80-address", "scheme": "http", "answered": True, "status_line": "HTTP/1.1 200 OK", "headers": []}])
        self.assertIsNone(set6_trial.header_judgement(only_the_web_server, set6_trial.HSTS_CANDIDATE)[-1][1])
        self.assertIsNone(set6_trial.header_judgement(full_reading(set6_trial.HSTS_CANDIDATE, plain=[]), set6_trial.HSTS_CANDIDATE)[-1][1])

    def test_the_values_are_the_ones_of_the_two_sources(self):
        source = (HERE.parents[2] / "cmd" / "panel" / "security.go").read_text(encoding="utf-8")
        self.assertIn('h.Set("Strict-Transport-Security", "' + set6_trial.HSTS_CANDIDATE + '")', source)
        self.assertEqual(set6_trial.HSTS_ALPHA81, set6_trial.HSTS_CANDIDATE + "; includeSubDomains")

    def test_no_recorded_key_is_one_the_redactor_blanks(self):
        redaction = set6_trial.base.pair_modules()["redaction"]
        checks = set6_trial.header_judgement(full_reading(set6_trial.HSTS_CANDIDATE), set6_trial.HSTS_CANDIDATE)

        def keys(value):
            if isinstance(value, dict):
                for key, item in value.items():
                    yield str(key)
                    yield from keys(item)
            elif isinstance(value, list):
                for item in value:
                    yield from keys(item)
        names = set(keys([detail for _n, _ok, detail in checks]))
        names |= {"as_logged_in_owner", "owner_login_held", "strict_transport_security", "request_headers", "tls_leaf_sha256",
                  "api-version-as-owner", "api-domains-as-owner", "api-domains-no-login", "api-unknown-no-login",
                  "api-unknown-as-owner", "api-post-without-origin", "resolved_total_transactions", "default_path_answers"}
        self.assertEqual(sorted(name for name in names if redaction.secret_key(name)), [])

    def test_the_text_form_names_every_answer(self):
        text = set6_trial.headers_text({"requests": [dict(request("spa-root", 200, ["max-age=31536000"]), reason="OK", http_version=11,
                                                         headers=[["Strict-Transport-Security", "max-age=31536000"]],
                                                         request_headers=[["Host", "127.0.0.1:2083"]], body_bytes=3, body_sha256="x")]})
        self.assertIn("### spa-root: GET https://127.0.0.1:2083/", text)
        self.assertIn("< HTTP/1.1 200 OK", text)
        self.assertIn("< Strict-Transport-Security: max-age=31536000", text)


class Base64Test(unittest.TestCase):
    def encoded(self, text):
        return base64.b64encode(text.encode()).decode()

    def test_a_digest_inside_base64_text_goes_and_the_object_says_so(self):
        events = "\n".join(json.dumps({"event": "kill sent", "transaction_token_sha256": A, "manifest_sha256": B}) for _ in range(2))
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            record = {"events_base64": self.encoded(events), "events_sha256": "ee" * 32, "node": "debian13"}
            (root / "recovery-fault-collection-1.json").write_text(json.dumps(record))
            (root / "journal.txt").write_text(f"a later line {A}\n")
            report = set6_redact.sweep([root])
            stored = json.loads((root / "recovery-fault-collection-1.json").read_text())
            inner = base64.b64decode(stored["events_base64"]).decode()
            self.assertNotIn(A, inner)
            self.assertIn(B, inner)
            self.assertEqual([json.loads(line)["transaction_token_sha256"] for line in inner.splitlines()], [set6_redact.MARK] * 2)
            self.assertEqual(stored["events_sha256"], "ee" * 32)
            self.assertEqual(stored[set6_redact.NOTE_KEY]["members_changed"], ["events_base64"])
            self.assertEqual(stored[set6_redact.NOTE_KEY]["digests_of_this_object_that_no_longer_match_the_stored_text"], ["events_sha256"])
            self.assertNotIn(A, (root / "journal.txt").read_text())
            self.assertEqual(report["base64_runs_replaced"], 1)
            self.assertEqual(report["json_objects_marked"], 1)
            self.assertNotIn(A, json.dumps(report))
            for alignment in ("", "x", "xx"):
                self.assertNotIn(self.encoded(alignment + A)[4:-4], (root / "recovery-fault-collection-1.json").read_text())
            again = set6_redact.sweep([root], write=False)
            self.assertEqual(again["files_that_would_change"], {})
            self.assertEqual(again["files_that_would_change_inside_base64_text"], {})

    def test_a_value_known_only_from_base64_text_is_removed_from_plain_text_too(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "a-journal.txt").write_text(f"sudo: COMMAND=/usr/bin/panel --work /var/lib/x/{A}/work\n")
            (root / "z-record.json").write_text(json.dumps({"events_base64": self.encoded(json.dumps({"transaction_token_sha256": A}) * 2)}))
            set6_redact.sweep([root])
            self.assertNotIn(A, (root / "a-journal.txt").read_text())

    def test_two_levels_and_text_that_is_not_base64(self):
        rules = set6_redact.TokenDigests()
        inner = self.encoded(json.dumps({"transaction_token_sha256": A, "padding": "p" * 20}))
        outer = self.encoded(json.dumps({"wrapped": inner}))
        counter = {"places": 0, "runs": 0}
        result = set6_redact.redact_base64(rules, json.dumps({"blob": outer}), counter)
        unwrapped = json.loads(base64.b64decode(json.loads(base64.b64decode(json.loads(result)["blob"]))["wrapped"]))
        self.assertEqual(unwrapped["transaction_token_sha256"], set6_redact.MARK)
        plain = "a public key AAAAC3NzaC1lZDI1NTE5AAAAIBbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb and a digest " + B
        self.assertEqual(set6_redact.redact_base64(rules, plain, counter), plain)
        self.assertIsNone(set6_redact.decoded_text("A" * 41))

    def test_set5s_rules_are_the_same_objects(self):
        self.assertIs(set6_redact.TokenDigests, set6_redact.set5_redact.TokenDigests)
        self.assertIs(set6_redact.wrap, set6_redact.set5_redact.wrap)
        self.assertEqual(set6_redact.MARK, "[REDACTED-SHA256]")


class CompositionTest(unittest.TestCase):
    def test_the_cells(self):
        self.assertEqual(sorted(set6_trial.CELLS), ["set6-arch", "set6-debian13", "set6-ubuntu"])
        self.assertEqual(sorted(set6_trial.UPDATE_CELLS), ["upd1-arch-good", "upd1-debian13-good", "upd1-ubuntu-good"])
        for name in set6_trial.UPDATE_CELLS:
            self.assertEqual(set6_trial.base.validate_cell(name).variant, "good")
        for name, settings in set6_trial.CELLS.items():
            earlier = set6_trial.s4.CELLS[name.replace("set6", "set4")]
            self.assertEqual((settings.node, settings.purpose, settings.components, settings.mail),
                             (earlier.node, earlier.purpose, earlier.components, earlier.mail))

    def test_the_sections_are_set4s_in_set4s_order_with_the_header_reading_added(self):
        own = [key for key, _title, _only in set6_trial.SECTIONS if not key.startswith("B-headers")]
        self.assertEqual(own, [key for key, _title, _only in set6_trial.s4.SECTIONS])
        self.assertEqual([only for key, _t, only in set6_trial.SECTIONS if key == "M10-postfix-stop"], ["mail"])

    def test_no_section_of_the_earlier_drivers_is_replaced(self):
        for name in ("m0_prepare", "m1_php_site", "m5_import_absolute", "m10_postfix_stop", "m4_reload_stopped", "m6_site_refused",
                     "php_facts", "delete_site", "refused_reading"):
            self.assertIs(getattr(set6_trial.Set6Trial, name), getattr(set6_trial.s4.Set4Trial, name), name)
        self.assertIs(set6_trial.Set6Trial.m2_import_lists, set6_trial.s4b.Set4bTrial.m2_import_lists)
        for name in ("preflight", "origin", "baseline_install", "owner_login", "license", "collect"):
            self.assertIs(getattr(set6_trial.Set6Trial, name), getattr(set6_trial.s4.Set4Trial, name), name)
        for name in ("execute", "collect", "verdicts", "terminal", "post_update_facts", "seed", "setup", "preflight", "origin"):
            self.assertIs(getattr(set6_trial.Set6UpdateTrial, name), getattr(set6_trial.base.Trial, name), name)
        for name in ("php_before", "php_after"):
            self.assertIs(getattr(set6_trial.Set6UpdateTrial, name), getattr(set6_trial.s4.Set4UpdateTrial, name), name)
        self.assertIs(set6_trial.Set6UpdateTrial.m10_postfix_stop, set6_trial.s4.Set4Trial.m10_postfix_stop)
        self.assertIs(set6_trial.Set6UpdateTrial.m10_after_the_update, set6_trial.s5.Set5UpdateTrial.m10_after_the_update)

    def test_the_list_rule_of_the_candidate(self):
        steps = [{"step": "domain", "ok": True}, {"step": "files", "ok": True}, {"step": "dns", "ok": True, "state": "left_to_owner"},
                 {"step": "database:x", "ok": True}, {"step": "member:/etc/x", "ok": False}, {"step": "finalize", "ok": False}]
        self.assertEqual(set6_trial.partial_lists(steps), (["domain", "files", "database:x"], ["member:/etc/x"]))
        self.assertEqual(set6_trial.left_out_list(steps), ["dns"])
        self.assertEqual(set6_trial.s4.rid.partial_lists(steps)[0], ["domain", "files", "dns", "database:x"])
        source = (HERE.parents[2] / "cmd" / "panel" / "import_handlers.go").read_text(encoding="utf-8")
        self.assertIn("case step.State != \"\":", source)
        self.assertIn("answer.LeftOut = append(answer.LeftOut, step.Step)", source)

    def test_the_plans(self):
        artifacts = {"baseline": {"version": "v0.1.0-alpha.81", "commit": "a" * 40, "sha256": "b" * 64}}
        fresh = set6_trial.build_plan("set6-arch", artifacts, "/var/tmp/cp-release-drill-x", 18443)
        self.assertEqual(fresh["steps"][0], "set6-name-pinning")
        self.assertEqual(fresh["steps"][1], "preflight")
        self.assertNotIn("M10-postfix-stop", fresh["steps"])
        self.assertEqual(fresh["steps"][-2:], ["set6-name-pinning-at-the-end", "collect"])
        mail = set6_trial.build_plan("set6-ubuntu", artifacts, "/var/tmp/cp-release-drill-x", 18443)
        self.assertIn("M10-postfix-stop", mail["steps"])
        self.assertEqual(mail["steps"].count("B-headers-after-setup") + mail["steps"].count("B-headers-at-the-end"), 2)


if __name__ == "__main__":
    unittest.main()
