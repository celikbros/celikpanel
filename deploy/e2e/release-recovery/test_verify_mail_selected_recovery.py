import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_selected_recovery import verify


class SelectedRecoveryEvidenceTests(unittest.TestCase):
    def fixture(self): return json.loads(Path(__file__).with_name('MAIL-SELECTED-BE.json').read_text())
    def test_accepts_bounded_native_evidence(self): self.assertEqual(verify(self.fixture())['native_boot_timer'], 'verified')
    def test_rejects_overclaim(self):
        for name in ['power_loss', 'production_enrollment', 'preselection_recovery', 'bounded_retry']:
            with self.subTest(name=name):
                r = self.fixture(); r['scope'][name] = True
                with self.assertRaises(ValueError): verify(r)
    def test_rejects_rehashed_loss_of_native_proof(self):
        for name, old, new in [('mail-selected-kill-observe.log', 'ExecMainStatus=9', 'ExecMainStatus=0'), ('mail-selected-refuse.log', 'ledger=unchanged', 'ledger=changed'), ('mail-selected-recover.log', 'foreign_history=preserved', 'foreign_history=unknown'), ('mail-selected-boot-kill.log', 'timer=enabled_but_stopped', 'timer=disabled'), ('mail-selected-boot-proof2.log', 'config_bytes_and_mtime=preserved', 'config_bytes_and_mtime=unknown'), ('mail-selected-boot-proof2.log', 'queue=acknowledged', 'queue=pending')]:
            with self.subTest(name=name, field=old):
                r = self.fixture(); item = r['logs'][name]; self.assertIn(old, item['text']); item['text'] = item['text'].replace(old, new); item['sha256'] = hashlib.sha256(item['text'].encode()).hexdigest()
                with self.assertRaises(ValueError): verify(r)
    def test_rejects_changed_build(self):
        r = self.fixture(); r['source_commit'] = '0' * 40
        with self.assertRaises(ValueError): verify(r)
        r = self.fixture(); r['manifest']['binary_sha256'] = '0' * 64
        with self.assertRaises(ValueError): verify(r)
