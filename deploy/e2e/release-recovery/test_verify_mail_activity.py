import copy
import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_activity import verify

class MailActivityEvidenceTest(unittest.TestCase):
    def setUp(self):self.r=json.loads(Path(__file__).with_name('MAIL-ACTIVITY-BE.json').read_text())
    def test_record(self):self.assertEqual(verify(self.r)['native_timer_activity_and_inverse'],'verified')
    def test_unproven_scope_rejected(self):
        for key in ['automatic_no_work_success','wants_parent_creation','mail_workload','production_dispatch','whole_update_rollback','power_loss']:
            r=copy.deepcopy(self.r);r['scope'][key]=True
            with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_wrong_native_states(self):
        for phase in ['forward-cut','rollback-cut','recover']:
            r=copy.deepcopy(self.r);r['result']['phases'][phase]['celikpanel-mail-renewal.timer']['properties']['ActiveState']='unknown'
            with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_missing_cut_or_preservation(self):
        for needle in ['returncode=-9','native_files=absent','manual_no_work_invocation=verified','management=absent','wants_parent=preserved']:
            r=copy.deepcopy(self.r);item=r['log'];item['text']=item['text'].replace(needle,'unknown',1);item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
            with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_changed_operation(self):
        self.r['result']['operation']='a'*32
        with self.assertRaises((ValueError,AssertionError)):verify(self.r)

    def test_no_failure_erasure(self):
        self.r['preparation_failure']['log']=''
        with self.assertRaises((ValueError,AssertionError)):verify(self.r)
    def test_failed_manual_invocation(self):
        self.r['result']['manual_no_work_invocation']['Result']='exit-code'
        with self.assertRaises((ValueError,AssertionError)):verify(self.r)
