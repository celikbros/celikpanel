"""Offline tests of set5's pure rules: the token-digest redaction, the name-pinning verdict, and that the added
steps borrow from the earlier drivers without replacing a method of the update driver."""
import json
from pathlib import Path
import sys
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import set5_redact  # noqa: E402
import set5_trial  # noqa: E402

A, B, C = "ab" * 32, "cd" * 32, "0123456789abcdef" * 4


class Plain:
    def text(self, value):
        return value

    def value(self, value):
        return value


class RedactionTest(unittest.TestCase):
    def test_a_digest_under_a_token_named_key_goes_and_other_digests_stay(self):
        rules = set5_redact.TokenDigests()
        got = rules.value({"bound_worker": {"transaction_token_sha256": A, "worker_state_sha256": B},
                           "handoff": {"intent": {"transaction_token_sha256": "sha256:" + A, "nonce": C}}})
        self.assertEqual(got["bound_worker"]["transaction_token_sha256"], set5_redact.MARK)
        self.assertEqual(got["handoff"]["intent"]["transaction_token_sha256"], set5_redact.MARK)
        self.assertEqual(got["bound_worker"]["worker_state_sha256"], B)
        self.assertEqual(got["handoff"]["intent"]["nonce"], C)
        self.assertEqual(rules.counts["named_field"], 2)

    def test_the_directory_name_and_every_later_occurrence_go(self):
        rules = set5_redact.TokenDigests()
        line = f"sudo[1]: COMMAND=/usr/bin/env CELIKPANEL_DATA_DIR=/var/lib/celikpanel/.release-db-migrations/{A}/work panel"
        self.assertNotIn(A, rules.text(line))
        self.assertIn(".release-db-migrations/" + set5_redact.MARK + "/work", rules.text(line))
        self.assertEqual(rules.text(f"elsewhere {A} again"), f"elsewhere {set5_redact.MARK} again")
        self.assertEqual(rules.text(f"another digest {B}"), f"another digest {B}")

    def test_pairs_in_text_and_in_escaped_json(self):
        rules = set5_redact.TokenDigests()
        for text in (f"token_sha256={A}", f'"transaction_token_sha256": "{A}"', f'\\"transaction_token_sha256\\": \\"{A}\\"',
                     f"transaction_token_sha256: sha256:{A}"):
            self.assertNotIn(A, rules.text(text), text)
        self.assertEqual(rules.text('"c_version_tokens": ["v1"] tokenizer.ini'), '"c_version_tokens": ["v1"] tokenizer.ini')
        self.assertEqual(rules.text(f"request id {C[:32]}"), f"request id {C[:32]}")

    def test_wrap_puts_the_rules_in_front_of_the_driver_redactor(self):
        redactor, rules = set5_redact.wrap(Plain())
        self.assertEqual(redactor.value({"transaction_token_sha256": A})["transaction_token_sha256"], set5_redact.MARK)
        self.assertEqual(redactor.text(f"seen again {A}"), f"seen again {set5_redact.MARK}")
        self.assertEqual(rules.learned, 1)

    def test_sweep_removes_a_value_from_a_file_written_before_it_was_known_and_keeps_json_valid(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "journal.txt").write_text(f"early line {A}\n")
            (root / "events.json").write_text(json.dumps([{"transaction_token_sha256": A, "manifest_sha256": B}]))
            report = set5_redact.sweep([root])
            self.assertEqual(sorted(report["files_changed"]), ["events.json", "journal.txt"])
            self.assertNotIn(A, (root / "journal.txt").read_text())
            self.assertEqual(json.loads((root / "events.json").read_text())[0]["manifest_sha256"], B)
            self.assertNotIn(A, json.dumps(report))
            again = set5_redact.sweep([root], write=False)
            self.assertEqual(again["files_that_would_change"], {})


class PinningTest(unittest.TestCase):
    def reading(self, ahosts, hosts):
        return {"ahosts": {"returncode": 0, "stdout": ahosts}, "hosts": {"returncode": 0, "stdout": hosts}}

    def test_loopback_only(self):
        good = self.reading("127.0.0.1       STREAM acme.zerossl.com\n127.0.0.1       DGRAM\n::1 STREAM", "::1 acme.zerossl.com")
        self.assertTrue(set5_trial.loopback_only(good))
        self.assertFalse(set5_trial.loopback_only(self.reading("", "")))
        self.assertFalse(set5_trial.loopback_only(self.reading("127.0.0.1 STREAM x\n203.0.113.9 STREAM x", "127.0.0.1 x")))
        self.assertFalse(set5_trial.loopback_only(self.reading("127.0.0.1 STREAM x", "")))
        self.assertFalse(set5_trial.loopback_only(None))

    def test_the_pinned_names_are_the_directories_the_product_names(self):
        source = (HERE.parents[2] / "internal" / "core" / "acme_providers.go").read_text(encoding="utf-8")
        for name in ("acme.zerossl.com", "dv.acme-v02.api.pki.goog"):
            self.assertIn(name, source)
            self.assertIn(name, set5_trial.CA_NAMES)
        self.assertIn("acme-v02.api.letsencrypt.org", set5_trial.CA_NAMES)
        self.assertIn("acme-staging-v02.api.letsencrypt.org", set5_trial.CA_NAMES)

    def test_the_guest_script_is_valid_python(self):
        compile(set5_trial.PIN_SCRIPT, "pin", "exec")


class CompositionTest(unittest.TestCase):
    def test_the_ten_cells_are_set3_part_two(self):
        self.assertEqual(len(set5_trial.UPDATE_CELLS), 10)
        for name in set5_trial.UPDATE_CELLS:
            set5_trial.base.validate_cell(name)
        self.assertEqual(set5_trial.cell_additions("upd1-debian13-good"), {"item9": True, "m10": True})
        self.assertEqual(set5_trial.cell_additions("upd1-arch-good"), {"item9": False, "m10": False})
        self.assertEqual(set5_trial.cell_additions("upd1-ubuntu-defective"), {"item9": False, "m10": False})

    def test_no_method_of_the_update_driver_is_replaced_by_a_borrowed_one(self):
        borrowed = ("native", "snap", "owner", "keep_text", "check", "note", "catalogue", "call", "refused", "section",
                    "postfix", "smtp", "journal", "guest_clock", "service_units", "service_state", "daemon",
                    "service_action", "snap4", "m10_postfix_stop")
        for name in borrowed:
            self.assertFalse(hasattr(set5_trial.s4.Set4UpdateTrial, name), name)
            self.assertTrue(callable(getattr(set5_trial.Set5UpdateTrial, name)), name)
        self.assertIs(set5_trial.Set5UpdateTrial.m10_postfix_stop, set5_trial.s4.Set4Trial.m10_postfix_stop)
        self.assertIs(set5_trial.Set5UpdateTrial.service_action, set5_trial.sw.SettingsTrial.service_action)
        for name in ("execute", "collect", "verdicts", "terminal", "post_update_facts", "seed", "setup", "preflight", "origin"):
            self.assertIs(getattr(set5_trial.Set5UpdateTrial, name), getattr(set5_trial.base.Trial, name), name)
        for name in ("php_before", "php_after"):
            self.assertIs(getattr(set5_trial.Set5UpdateTrial, name), getattr(set5_trial.s4.Set4UpdateTrial, name), name)


if __name__ == "__main__":
    unittest.main()
