import copy
import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_parent import verify

class MailParentEvidenceTest(unittest.TestCase):
    def setUp(self):self.r=json.loads(Path(__file__).with_name('MAIL-PARENT-BE.json').read_text())
    def test_record(self):self.assertEqual(verify(self.r)['native_parent_and_timer_inverse'],'verified')
    def test_unproven_scope_rejected(self):
        for key in ['mail_workload','production_dispatch','whole_update_rollback','power_loss']:
            r=copy.deepcopy(self.r);r['scope'][key]=True
            with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_wrong_native_states(self):
        for phase in ['forward-cut','rollback-cut','recover']:
            r=copy.deepcopy(self.r);r['result']['phases'][phase]['celikpanel-mail-renewal.timer']['properties']['ActiveState']='unknown'
            with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_missing_cut_or_preservation(self):
        for needle in ['returncode=-9','native_files=absent','native_no_work_invocation=verified','management=absent','wants_parent=created_and_retained']:
            r=copy.deepcopy(self.r);item=r['log'];item['text']=item['text'].replace(needle,'unknown',1);item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
            with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_changed_operation(self):
        self.r['result']['operation']='a'*32
        with self.assertRaises((ValueError,AssertionError)):verify(self.r)

    def test_changed_parent(self):
        self.r['result']['parent_publication']['directory']['uid']=1000
        with self.assertRaises((ValueError,AssertionError)):verify(self.r)
    def test_stale_invocation(self):
        self.r['result']['native_no_work_invocation']['ExecMainStartTimestampMonotonic']='1'
        with self.assertRaises((ValueError,AssertionError)):verify(self.r)
