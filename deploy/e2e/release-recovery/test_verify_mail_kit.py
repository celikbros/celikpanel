import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_kit import verify


class KitEvidenceTests(unittest.TestCase):
    def fixture(self): return json.loads(Path(__file__).with_name('MAIL-KIT-BE.json').read_text())
    def test_accepts_bounded_native_evidence(self): self.assertEqual(verify(self.fixture())['automatic_boot_new_renewal'], 'verified')
    def test_rejects_changed_native_files(self):
        for name in ('service', 'timer', 'hook'):
            with self.subTest(name=name):
                r = self.fixture(); r[name] += '\n'; r['manifest'][name + '_sha256'] = hashlib.sha256(r[name].encode()).hexdigest()
                with self.assertRaises(ValueError): verify(r)
    def test_rejects_rehashed_loss_of_proof(self):
        for name, old, new in [('mail-kit-boot-result.log', 'Result=success', 'Result=exit-code'), ('mail-kit-boot-result.log', 'runtime=root:celikpanel:0750', 'runtime=unknown'), ('mail-kit-boot-prepare.log', 'before_reboot=pending', 'before_reboot=absent'), ('mail-kit-timer3.log', 'config=unchanged', 'config=changed')]:
            with self.subTest(name=name, field=old):
                r = self.fixture(); item = r['logs'][name]; self.assertIn(old, item['text']); item['text'] = item['text'].replace(old, new); item['sha256'] = hashlib.sha256(item['text'].encode()).hexdigest()
                with self.assertRaises(ValueError): verify(r)
    def test_rejects_overclaim_and_wrong_build(self):
        r = self.fixture(); r['scope']['production_enrollment'] = True
        with self.assertRaises(ValueError): verify(r)
        r = self.fixture(); r['source_commit'] = '0' * 40
        with self.assertRaises(ValueError): verify(r)
