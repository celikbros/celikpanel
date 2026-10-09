"""Offline tests of the set2 ``request-identity`` cell: the pure rules of the driver and of its guest helper."""
import gzip
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tarfile
import unittest

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[2]


def load(name):
    spec = importlib.util.spec_from_file_location(name, HERE / (name + ".py"))
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


trial = load("request_identity_trial")
native = load("guest_request_identity_native")

ARTIFACT = {"version": "v0.1.0-alpha.81", "commit": "a" * 40, "sha256": "b" * 64}


def answer(status, code=None, reason=None, replayed=None):
    parsed = {"code": code, "reason": reason} if code else {"success": True}
    return {"status": status, "_parsed": parsed, "replayed": replayed}


class ProductContractTests(unittest.TestCase):
    def test_header_codes_and_routes_are_the_products(self):
        guard = (REPO / "cmd" / "panel" / "request_identity.go").read_text(encoding="utf-8")
        self.assertIn('"' + trial.HEADER + '"', guard)
        self.assertIn('"' + trial.REPLAYED + '"', guard)
        for code in trial.GUARD_CODES:
            self.assertIn('"' + code + '"', guard)
        patterns = {route.split(" ")[1] for _, route in trial.ROUTES}
        self.assertEqual(len(patterns), 8)
        for pattern in patterns:
            self.assertIn('"' + pattern + '"', guard, pattern)
        self.assertRegex(trial.new_identity(), r"\A[0-9a-f]{32}\Z")
        self.assertNotEqual(trial.new_identity(), trial.new_identity())

    def test_the_agents_refusal_and_the_origins_are_the_products(self):
        spec = (REPO / "internal" / "backupspec" / "spec.go").read_text(encoding="utf-8")
        self.assertIn('OriginPreRestore = "pre_restore"', spec)
        self.assertIn('OriginManual     = "manual"', spec)
        driver = (HERE / "request_identity_trial.py").read_text(encoding="utf-8")
        self.assertIn("BACKUP_RESTORE_IN_PROGRESS", driver)
        self.assertIn('"request:" + identity', driver)
        self.assertIn('manualBackupJobKeyPrefix', (REPO / "cmd" / "panel" / "domain_backup_handlers.go").read_text(encoding="utf-8"))

    def test_paths_follow_the_product(self):
        self.assertIn('"' + native.SITES_ROOT + '"', (REPO / "internal" / "hostingpath" / "path.go").read_text(encoding="utf-8"))
        self.assertIn('"' + str(native.BACKUP_BASE) + '"', (REPO / "cmd" / "agent" / "backup_rpc.go").read_text(encoding="utf-8"))
        self.assertIn('"' + str(native.IMPORT_ROOT) + '"', (REPO / "cmd" / "agent" / "cpmove_archive_linux.go").read_text(encoding="utf-8"))
        self.assertEqual(native.document_root(3, 7), "/var/www/celikpanel/subscriptions/3/sites/7/public_html")
        self.assertEqual(str(native.backup_directory(3, 7)), "/var/backups/celikpanel/subscriptions/3/domains/7")
        for bad in ((0, 1), (1, 0), ("1", 2), (1, None)):
            with self.assertRaises(native.Refused):
                native.backup_directory(*bad)
        self.assertEqual(native.site_username("set2-import-seq.test"), "set2_import_seq_test")


class CellTests(unittest.TestCase):
    def test_cells_and_plan(self):
        self.assertEqual(sorted(trial.CELLS), ["rid-arch", "rid-debian13", "rid-ubuntu"])
        self.assertFalse(set(trial.CELLS) & set(trial.base.CELLS))
        self.assertFalse(set(trial.CELLS) & set(trial.sw.CELLS))
        plan = trial.build_plan(trial.CELLS["rid-ubuntu"], {"baseline": ARTIFACT}, "/var/tmp/cp-release-drill-x", 18443)
        self.assertIs(plan["native_evidence"], False)
        self.assertEqual(plan["cell_kind"], "request-identity")
        self.assertEqual(plan["steps"][-2:], ["C9-identities", "collect"])
        self.assertEqual(sorted(plan["arrivals"]), ["i", "ii", "iii", "iv", "v", "vi", "vii"])
        self.assertEqual(len(plan["routes"]), len(trial.ROUTES))
        json.dumps(plan)

    def test_the_driver_reaches_the_guest_only_through_the_lab_and_the_panel_api(self):
        source = (HERE / "request_identity_trial.py").read_text(encoding="utf-8")
        self.assertNotIn("celikpanel.net", source)
        self.assertNotIn("subprocess", source)
        self.assertIn('("127.0.0.1", self.local_port)', source)   # the only socket it opens: the lab's loopback tunnel
        self.assertEqual(source.count("socket.create_connection"), 1)


class ArrivalRuleTests(unittest.TestCase):
    def test_sequential_retained(self):
        three = [answer(200), answer(200, replayed="1"), answer(200, replayed="1")]
        self.assertIs(trial.judge_sequential(three, ["a", "a", "a"], True)[0], True)
        self.assertIs(trial.judge_sequential(three, ["a", "a", "b"], True)[0], False)                    # other bytes
        self.assertIs(trial.judge_sequential([answer(200)] * 3, ["a"] * 3, True)[0], False)             # replays unmarked
        self.assertIs(trial.judge_sequential([answer(200), answer(500, replayed="1"), answer(200, replayed="1")],
                                             ["a"] * 3, True)[0], False)
        self.assertIsNone(trial.judge_sequential([answer(200), answer(None), answer(200)], ["a"] * 3, True)[0])
        # a failed first answer that is kept is replayed as it was
        failed = [answer(500, "INTERNAL"), answer(500, "INTERNAL", replayed="1"), answer(500, "INTERNAL", replayed="1")]
        self.assertIs(trial.judge_sequential(failed, ["x"] * 3, True)[0], True)

    def test_sequential_status_only(self):
        kept = "REQUEST_COMPLETED_RESULT_NOT_RETAINED"
        self.assertIs(trial.judge_sequential([answer(200), answer(409, kept), answer(409, kept)], ["a", "b", "b"], False)[0], True)
        self.assertIs(trial.judge_sequential([answer(200), answer(200), answer(409, kept)], ["a", "a", "b"], False)[0], False)
        self.assertIs(trial.judge_sequential([answer(409, "X"), answer(409, kept, "failed"), answer(409, kept, "failed")],
                                             ["a", "b", "b"], False)[0], True)
        self.assertIs(trial.judge_sequential([answer(409, "X"), answer(409, kept), answer(409, kept)], ["a", "b", "b"], False)[0], False)
        self.assertIs(trial.judge_sequential([answer(409, kept), answer(409, kept), answer(409, kept)], ["b"] * 3, False)[0], False)

    def test_concurrent(self):
        kept, waiting = "REQUEST_COMPLETED_RESULT_NOT_RETAINED", "REQUEST_IN_PROGRESS"
        self.assertIs(trial.judge_concurrent([answer(200)] * 3, ["a"] * 3, True)[0], True)
        self.assertIs(trial.judge_concurrent([answer(200), answer(409, waiting), answer(200)], ["a", "w", "a"], True)[0], True)
        self.assertIs(trial.judge_concurrent([answer(200), answer(200), answer(200)], ["a", "b", "a"], True)[0], False)
        self.assertIsNone(trial.judge_concurrent([answer(409, waiting)] * 3, ["w"] * 3, True)[0])
        self.assertIs(trial.judge_concurrent([answer(200), answer(409, kept), answer(409, kept)], ["a", "k", "k"], False)[0], True)
        self.assertIs(trial.judge_concurrent([answer(200), answer(409, waiting), answer(409, kept)], ["a", "w", "k"], False)[0], True)
        self.assertIs(trial.judge_concurrent([answer(200), answer(200), answer(409, kept)], ["a", "b", "k"], False)[0], False)
        self.assertIs(trial.judge_concurrent([answer(409, kept)] * 3, ["k"] * 3, False)[0], False)
        self.assertIsNone(trial.judge_concurrent([answer(200), answer(None), answer(200)], ["a", None, "a"], True)[0])

    def test_matrix_and_classification(self):
        self.assertEqual(trial.worse(None, "pass"), "pass")
        self.assertEqual(trial.worse("pass", "FAIL"), "FAIL")
        self.assertEqual(trial.worse("FAIL", "pass"), "FAIL")
        self.assertEqual(trial.worse("pass", "not established"), "not established")
        self.assertEqual([trial.matrix_value(v) for v in (True, False, None)], ["pass", "FAIL", "not established"])
        reference, drifted = {"docroot": "a", "rows": ["10", "55"]}, {"docroot": "b", "rows": ["6", "30"]}
        self.assertTrue(trial.restore_classification(reference, drifted, dict(reference)).startswith("finished"))
        self.assertTrue(trial.restore_classification(reference, drifted, dict(drifted)).startswith("not started"))
        half = trial.restore_classification(reference, drifted, {"docroot": "a", "rows": ["6", "30"]})
        self.assertEqual(half, "half: files restored, rows as before")
        self.assertIn("neither state", trial.restore_classification(reference, drifted, {"docroot": "c", "rows": ["6", "30"]}))
        self.assertEqual(trial.one_new(["a"], ["a", "b"]), ["b"])
        self.assertTrue(trial.is_guard_refusal(answer(409, "REQUEST_ID_REUSED")))
        self.assertFalse(trial.is_guard_refusal(answer(409, "BACKUP_RESTORE_IN_PROGRESS")))


class GuestHelperRuleTests(unittest.TestCase):
    def test_names(self):
        self.assertEqual(native.user_name("s2impseq"), "s2impseq")
        self.assertEqual(native.sql_name("s2impseq_app"), "s2impseq_app")
        self.assertEqual(native.domain_name("set2-import-seq.test"), "set2-import-seq.test")
        for bad in ("root", "a;b", "", 7):
            with self.assertRaises(native.Refused):
                native.user_name(bad)
        for bad in ("a`b", "a b", "a;drop", "", None, "x" * 80):
            with self.assertRaises(native.Refused):
                native.sql_name(bad)
        for bad in ("localhost", "a..b", "celikpanel", "UPPER.test", "a b.test"):
            with self.assertRaises(native.Refused):
                native.domain_name(bad)

    def test_stored_bodies_are_described_and_never_returned(self):
        body = json.dumps({"success": True, "backup": {"name": "full-x.cpbak"}}).encode()
        shape = native.body_shape(body)
        self.assertEqual(shape, {"stored": True, "bytes": len(body), "sha256": hashlib.sha256(body).hexdigest(),
                                 "json_keys": ["backup", "success"], "code": None})
        self.assertEqual(native.body_shape(None), {"stored": False})
        self.assertEqual(native.secret_occurrences(b'{"client_config":"PrivateKey = abc123XYZ"}', [b"abc123XYZ", b"other", b""]), 1)
        self.assertEqual(native.secret_occurrences(b"", [b"abc123XYZ"]), 0)
        source = (HERE / "guest_request_identity_native.py").read_text(encoding="utf-8")
        self.assertIn("mode=ro", source)
        for forbidden in ("celikpanel.net", "curl", "urllib", "http.client"):
            self.assertNotIn(forbidden, source)
        # the reader of request_identities returns no column of the stored answer itself
        reader = source[source.index("def read_identities"):source.index("# -- backups, document root")]
        self.assertNotIn('"response_body": row', reader)
        self.assertNotIn("body.decode", reader)

    def test_only_loopback_answers_confirm_the_acme_isolation(self):
        self.assertTrue(native.loopback_only("::1             acme-v02.api.letsencrypt.org"))
        self.assertTrue(native.loopback_only("127.0.0.1 a" + chr(10) + "::1 a"))
        self.assertFalse(native.loopback_only(""))
        self.assertFalse(native.loopback_only("2606:4700:60:0:f53d:5624:85c7:3a2c x.pacloudflare.com"))
        self.assertFalse(native.loopback_only("127.0.0.1 a" + chr(10) + "203.0.113.9 a"))
        driver = (HERE / "request_identity_trial.py").read_text(encoding="utf-8")
        self.assertLess(driver.index('isolation.get("acme_isolated") is not True'), driver.index("what one entry leaves"))

    def test_modes(self):
        self.assertTrue(all(mode.startswith(("read-", "owner-", "lab-")) for mode in native.MODES))
        self.assertEqual(sorted(m for m in native.MODES if not m.startswith("read-")),
                         ["lab-isolate-acme", "lab-kill-panel", "owner-change-site", "owner-cpmove-fixture",
                          "owner-restart-panel", "owner-seed-rows", "owner-seed-site"])
        unit = (REPO / "deploy" / "systemd" / "celikpanel-panel.service").read_text(encoding="utf-8")
        self.assertIn("Restart=on-failure", unit)     # what brings the Panel back after lab-kill-panel
        self.assertNotIn("restore", native.lab_isolate_acme.__doc__.lower().split("isolation")[0])

    def test_the_fixture_archive_is_what_the_import_parser_reads(self):
        members = native.cpmove_members("set2-import-seq.test", "s2impseq", "s2impseq_app", 1, 7, 1200,
                                        random_bytes=lambda n: b"\0" * n)
        names = [name for name, _ in members]
        top = "cpmove-s2impseq/"
        self.assertTrue(all(name.startswith(top) for name in names))
        relative = {name[len(top):]: data for name, data in members}
        self.assertIn(b"DNS=set2-import-seq.test\n", relative["cp/s2impseq"])                      # cp/<user>: the domain
        self.assertTrue(relative["homedir/etc/set2-import-seq.test/shadow"].startswith(b"info:$"))   # user:$hash
        self.assertEqual(relative["homedir/etc/set2-import-seq.test/quota"], b"info:268435456\n")
        self.assertIn(b"@", relative["va/set2-import-seq.test"])
        self.assertIn("mysql/s2impseq_app.create", relative)
        dump = relative["mysql/s2impseq_app.sql"].decode()
        self.assertEqual(dump.count("imported row"), 1200)
        self.assertTrue(dump.rstrip().endswith("DO SLEEP(7);"))
        self.assertNotIn("SLEEP", native.cpmove_members("a.test", "u", "d", 0, 0, 3)[-1][1].decode())
        self.assertEqual(len(relative["homedir/public_html/assets/blob.bin"]), 1024 * 1024)
        parser = (REPO / "cmd" / "agent" / "cpmove_rpc.go").read_text(encoding="utf-8")
        for rule in ('"cp/"', '"homedir/public_html"', '"/shadow"', '"/quota"', '"va/"', '"mysql/"', '".create"', '".sql"',
                     '"cpmove-"', 'k == "DNS"'):
            self.assertIn(rule, parser)
        data = native.cpmove_archive(members)
        with tarfile.open(fileobj=io.BytesIO(gzip.decompress(data)), mode="r") as archive:
            listed = archive.getnames()
            regular = [m for m in archive.getmembers() if m.isreg()]
        self.assertEqual(sorted(m.name for m in regular), sorted(names))
        self.assertIn("cpmove-s2impseq/homedir/public_html", listed)
        self.assertTrue(all(not m.issym() and not m.islnk() for m in regular))
        # H34: as tar writes it, the archive holds the directory member `homedir/public_html/`; the other form leaves
        # exactly that one member out.
        without = native.cpmove_archive(members, public_html_directory_member=False)
        with tarfile.open(fileobj=io.BytesIO(gzip.decompress(without)), mode="r") as archive:
            other = archive.getnames()
        self.assertEqual(sorted(set(listed) - set(other)), ["cpmove-s2impseq/homedir/public_html"])
        self.assertIn("cpmove-s2impseq/homedir/public_html/assets", other)
        raw = gzip.decompress(data)
        self.assertIn(b"cpmove-s2impseq/homedir/public_html/\0", raw)      # the stored name ends with a slash
        self.assertNotIn(b"cpmove-s2impseq/homedir/public_html/\0", gzip.decompress(without))


if __name__ == "__main__":
    unittest.main()
