import copy
import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_bootstrap_loaded import verify

class MailBootstrapLoadedEvidenceTest(unittest.TestCase):
    def setUp(self): self.r=json.loads(Path(__file__).with_name('MAIL-BOOTSTRAP-LOADED-BE.json').read_text())
    def test_record(self): self.assertEqual(verify(self.r)['initial_idle_loading_and_compensation'],'verified')
    def test_unknown_native_result_is_not_absence(self):
        for phase in ['rollback-cut','recover']:
            for key,value in [('code',1),('properties',{})]:
                r=copy.deepcopy(self.r);r['result']['phases'][phase]['celikpanel-mail-renewal.service'][key]=value
                with self.assertRaises((ValueError,AssertionError)): verify(r)
    def test_unproven_scope_rejected(self):
        for key in ['timer_activation','mail_workload','production_dispatch','whole_update_rollback','power_loss']:
            r=copy.deepcopy(self.r);r['scope'][key]=True
            with self.assertRaises((ValueError,AssertionError)): verify(r)
    def test_changed_operation_and_missing_restoration(self):
        for needle in ['returncode=-9','native_files=absent','timer=never_enabled','management=absent','operation='+self.r['result']['operation']]:
            r=copy.deepcopy(self.r);item=r['log'];item['text']=item['text'].replace(needle,'unknown',1);item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
            with self.assertRaises((ValueError,AssertionError)): verify(r)
    def test_preparation_history_retained(self):
        self.r['preparation_refusals']=[]
        with self.assertRaises((ValueError,AssertionError)): verify(self.r)
