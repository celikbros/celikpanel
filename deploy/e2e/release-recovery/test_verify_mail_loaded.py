import copy
import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_loaded import verify

class MailLoadedEvidenceTest(unittest.TestCase):
    def setUp(self):self.r=json.loads(Path(__file__).with_name('MAIL-LOADED-BE.json').read_text())
    def test_record(self):self.assertEqual(verify(self.r)['existing_loaded_schedule'],'verified')
    def test_missing_cut_command_or_preservation(self):
        for needle in ['returncode=-9','loaded_exec phase=forward-cut','loaded_exec phase=rollback-cut','timer_preference=preserved','configuration_ledger=unchanged','management=absent']:
            with self.subTest(needle=needle):
                r=copy.deepcopy(self.r);item=r['log'];item['text']=item['text'].replace(needle,'unknown',1);item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
                with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_unproven_scope_rejected(self):
        for key in ['bootstrap_enrollment','production_dispatch','whole_update_rollback','power_loss']:
            r=copy.deepcopy(self.r);r['scope'][key]=True
            with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_changed_request_rejected(self):
        item=self.r['log'];item['text']=item['text'].replace('operation=2dbbb7485fe846de98e972cf2fa654c1','operation='+'a'*32,1);item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
        with self.assertRaises((ValueError,AssertionError)):verify(self.r)
