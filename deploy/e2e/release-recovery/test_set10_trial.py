"""Offline tests of set10's pure rules: the render header as the classifier reads it, the lab's stale text, the
journal times and lines, and the verdict of a cell. No guest is needed."""
from __future__ import annotations

import hashlib
import importlib.util
from pathlib import Path
import sys
import unittest

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import set10_trial as s10  # noqa: E402


def guest_module():
    spec = importlib.util.spec_from_file_location("set10_guest_under_test", HERE / "guest_set10_native.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


BODY = b"# Nginx vhost for set10-a.test\nserver {\n    listen 80;\n    server_name set10-a.test www.set10-a.test;\n}\n"


class Set10Rules(unittest.TestCase):
    def setUp(self):
        self.g = guest_module()

    def test_sealed_text_has_a_valid_header(self):
        sealed = self.g.seal(BODY)
        head = self.g.header_of(sealed)
        self.assertTrue(head["present"] and head["valid"])
        self.assertEqual(head["declared"], hashlib.sha256(BODY).hexdigest())
        self.assertEqual(sealed.split(b"\n", 1)[1], BODY)

    def test_an_edited_body_keeps_the_header_but_not_its_digest(self):
        edited = self.g.seal(BODY).replace(b"listen 80;", b"listen 8080;")
        head = self.g.header_of(edited)
        self.assertTrue(head["present"])
        self.assertFalse(head["valid"])

    def test_crlf_header_line_is_still_the_header(self):
        sealed = self.g.seal(BODY)
        first, rest = sealed.split(b"\n", 1)
        self.assertTrue(self.g.header_of(first + b"\r\n" + rest)["present"])

    def test_no_header_and_malformed_header(self):
        self.assertFalse(self.g.header_of(BODY)["present"])
        self.assertFalse(self.g.header_of(b"# celikpanel-render v2 sha256=XYZ\n" + BODY)["present"])
        self.assertFalse(self.g.header_of(None)["present"])

    def test_stale_text_is_header_valid_and_differs(self):
        sealed = self.g.seal(BODY)
        stale = self.g.seal(self.g.stale_body(sealed))
        head = self.g.header_of(stale)
        self.assertTrue(head["valid"])
        self.assertNotEqual(stale, sealed)
        with self.assertRaises(self.g.Refused):
            self.g.stale_body(BODY)

    def test_owner_include_texts(self):
        self.assertIn("location /owner-dir/", self.g.owner_include_text("location", "set10-a.test"))
        self.assertIn("root ", self.g.owner_include_text("duplicate-root", "set10-a.test"))
        self.assertTrue(self.g.OWNER_FILE_RE.fullmatch("set10-dup-root.conf"))
        self.assertIsNone(self.g.OWNER_FILE_RE.fullmatch("../x.conf"))
        self.assertIsNone(self.g.OWNER_FILE_RE.fullmatch("owner.conf"))

    def test_start_line_wait_includes_the_d031_line(self):
        self.assertIn("site configuration files at start:", self.g.g8.RECONCILE_LINES)


class Set10Driver(unittest.TestCase):
    def test_journal_lines(self):
        lines = ["2026-10-10T19:00:00.000000+00:00 host nginx[1]: Reloaded nginx.service",
                 "2026-10-10T19:00:10.000000+00:00 host systemd[1]: Reloaded nginx.service - x",
                 "2026-10-10T19:00:05.000000+00:00 host panel[2]: site configuration set10-a.test (startup): unchanged"]
        epoch = s10.journal_epoch(lines[0])
        self.assertIsNotNone(epoch)
        self.assertEqual(len(s10.lines_after(lines, epoch + 1, "Reloaded")), 1)
        self.assertEqual(s10.site_lines(lines, "set10-a.test"), [lines[2]])
        self.assertEqual(s10.site_lines(lines, "set10-a.tes"), [])
        self.assertIsNone(s10.journal_epoch("not a time"))

    def test_verdicts(self):
        self.assertEqual(s10.verdict_of([("x", True, None)]), "PASS")
        self.assertEqual(s10.verdict_of([("x", True, None), ("y", None, None)]), "NOT-MEASURED")
        self.assertEqual(s10.verdict_of([("x", False, None), ("y", None, None)]), "FAIL")

    def test_runs_and_states(self):
        self.assertEqual(len(s10.RUNS), 12)
        self.assertEqual({r[3] for r in s10.RUNS}, {"start", "restart", "save"})
        self.assertTrue(all(r[4] == (r[3] == "start") for r in s10.RUNS))
        self.assertEqual(s10.kept_word("owner_edited"), "kept (owner-edited)")
        self.assertEqual(s10.KEPT_STATE["replace"], "foreign")
        self.assertEqual(s10.s8.DOMAIN_A, "set10-a.test")

    def test_safe_name(self):
        self.assertEqual(s10.safe_name("a1 after"), "a1-after")
        self.assertEqual(s10.safe_name("after the update set10-r1.test"), "after-the-update-set10-r1.test")

    def test_body_of(self):
        self.assertEqual(s10.body_of("# h\nbody\n"), "body\n")
        self.assertIsNone(s10.body_of(None))


if __name__ == "__main__":
    unittest.main()
