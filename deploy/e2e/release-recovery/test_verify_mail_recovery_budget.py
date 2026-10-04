import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_recovery_budget import verify


class MailBudgetEvidenceTests(unittest.TestCase):
    def fixture(self): return json.loads(Path(__file__).with_name('MAIL-BUDGET-BE.json').read_text())
    def test_accepts_native_budget(self): self.assertEqual(verify(self.fixture())['explicit_owner_attempt'], 4)
    def test_rejects_rehashed_counter_or_operation_loss(self):
        for name, old, new in [('mail-budget-attempt2.log', 'attempt=2', 'attempt=1'), ('mail-budget-boot.log', 'attempt=3', 'attempt=2'), ('mail-budget-exhausted.log', 'native_reload=not_repeated', 'native_reload=repeated'), ('mail-budget-exhausted.log', 'ledger=unchanged', 'ledger=changed'), ('mail-budget-owner.log', 'attempt=4', 'attempt=1'), ('mail-budget-owner.log', 'queue=acknowledged', 'queue=unknown')]:
            with self.subTest(name=name, field=old):
                r = self.fixture(); item = r['logs'][name]; self.assertIn(old, item['text']); item['text'] = item['text'].replace(old, new); item['sha256'] = hashlib.sha256(item['text'].encode()).hexdigest()
                with self.assertRaises(ValueError): verify(r)
    def test_rejects_unsupported_scope_or_source(self):
        for name in ['power_loss', 'production_enrollment', 'preselection_retry']:
            r = self.fixture(); r['scope'][name] = True
            with self.assertRaises(ValueError): verify(r)
        r = self.fixture(); r['source_commit'] = '0' * 40
        with self.assertRaises(ValueError): verify(r)
