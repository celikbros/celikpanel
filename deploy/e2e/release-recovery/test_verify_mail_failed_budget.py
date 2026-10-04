import copy
import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_failed_budget import verify

class MailFailedBudgetEvidenceTest(unittest.TestCase):
    def setUp(self):self.r=json.loads(Path(__file__).with_name('MAIL-FAILED-BUDGET-BE.json').read_text())
    def test_record(self):self.assertEqual(verify(self.r)['failed_retry_budget'],'verified')
    def test_missing_attempt_owner_or_preservation(self):
        for needle in ['failed_attempt=2','wrong_owner=refused','ledger=unchanged','original_native_inodes=restored','attempt=4','management=absent']:
            with self.subTest(needle=needle):
                r=copy.deepcopy(self.r);item=r['log'];item['text']=item['text'].replace(needle,'unknown',1);item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
                with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_unproven_scope_rejected(self):
        for key in ['automatic_timer_dispatch','preselection_interruption','production_enrollment','power_loss']:
            r=copy.deepcopy(self.r);r['scope'][key]=True
            with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_changed_request_rejected(self):
        item=self.r['log'];item['text']=item['text'].replace('failed_attempt=2 request=79066f5075f0b92fe8ce964819030c60','failed_attempt=2 request='+'a'*32);item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
        with self.assertRaises((ValueError,AssertionError)):verify(self.r)
