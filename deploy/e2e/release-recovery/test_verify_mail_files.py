import copy
import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_files import verify

class MailFilesEvidenceTest(unittest.TestCase):
    def setUp(self): self.r = json.loads(Path(__file__).with_name('MAIL-FILES-BE.json').read_text())
    def test_record(self): self.assertEqual(verify(self.r)['native_inverse'], 'verified')
    def test_missing_cut_recovery_or_preservation(self):
        for needle in ['returncode=-9', 'original_inodes=restored', 'configuration_ledger=unchanged', 'management=absent', 'prior_guard_refusal=preserved']:
            with self.subTest(needle=needle):
                r=copy.deepcopy(self.r); item=r['logs']['mail-files-native-resume.log'];item['text']=item['text'].replace(needle,'unknown',1);item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
                with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_broader_scope_rejected(self):
        for key in ['native_schedule_activation','production_dispatch','whole_update_rollback','power_loss']:
            r=copy.deepcopy(self.r);r['scope'][key]=True
            with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_changed_operation_rejected(self):
        item=self.r['logs']['mail-files-native-resume.log'];item['text']=item['text'].replace('operation=a4c0f42e45db4cc7af843fd2ab338907','operation='+'b'*32,1);item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
        with self.assertRaises((ValueError,AssertionError)):verify(self.r)
