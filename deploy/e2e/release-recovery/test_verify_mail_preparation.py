import copy
import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_preparation import verify

class PreparationEvidenceTest(unittest.TestCase):
    def setUp(self): self.r = json.loads(Path(__file__).with_name('MAIL-PREPARATION-BE.json').read_text())
    def test_record(self): self.assertEqual(verify(self.r)['preparation'], 'verified')
    def test_missing_or_changed_claim(self):
        for needle in ['repeat_inode=preserved', 'owner_edit=refused', 'without_lock=refused', 'ledger=unchanged', 'older_generations=retained']:
            with self.subTest(needle=needle):
                r = copy.deepcopy(self.r); item = r['logs']['mail-preparation-cli.log']; item['text'] = item['text'].replace(needle, 'unverified'); item['sha256'] = hashlib.sha256(item['text'].encode()).hexdigest()
                with self.assertRaises((ValueError, AssertionError)): verify(r)
    def test_no_scope_inflation(self):
        self.r['scope']['production_enrollment'] = True
        with self.assertRaises((ValueError, AssertionError)): verify(self.r)
