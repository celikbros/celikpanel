import copy
import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_hook import verify

class HookEvidenceTest(unittest.TestCase):
    def setUp(self): self.r = json.loads(Path(__file__).with_name('MAIL-HOOK-BE.json').read_text())
    def test_record(self): self.assertEqual(verify(self.r)['native_hook'], 'preserved')
    def test_missing_preservation_or_scope(self):
        for needle in ['bytes_inode_mtime=preserved', 'native_config_units_ledger=unchanged', 'management=absent']:
            with self.subTest(needle=needle):
                r = copy.deepcopy(self.r); item = r['logs']['mail-hook-owner.log']; item['text'] = item['text'].replace(needle, 'unknown'); item['sha256'] = hashlib.sha256(item['text'].encode()).hexdigest()
                with self.assertRaises((ValueError, AssertionError)): verify(r)
        self.r['scope']['production_enrollment'] = True
        with self.assertRaises((ValueError, AssertionError)): verify(self.r)
