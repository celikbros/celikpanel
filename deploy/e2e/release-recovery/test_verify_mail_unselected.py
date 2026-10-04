import copy
import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_unselected import verify

class MailUnselectedEvidenceTest(unittest.TestCase):
    def setUp(self):self.r=json.loads(Path(__file__).with_name('MAIL-UNSELECTED-BE.json').read_text())
    def test_record(self):self.assertEqual(verify(self.r)['actual_sigkills'],4)
    def test_scope_cannot_expand(self):
        for key in ['automatic_timer_dispatch','production_enrollment','cross_build_adoption','reboot','power_loss']:
            r=copy.deepcopy(self.r);r['scope'][key]=True
            with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_owner_and_operation_proofs_required(self):
        for key,value in [('attempt',1),('before_selection_unchanged',False),('config_unchanged',False),('prior_jobs_unchanged',False),('terminal_cut',False),('retained_stages',[]),('old_leaf','a'*64),('served',{'smtp587':'a'*64,'imap993':'a'*64})]:
            with self.subTest(key=key):
                r=copy.deepcopy(self.r);r['result']['trials'][2][key]=value
                with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_hashed_log_cannot_claim_missing_kill_or_success(self):
        for name in self.r['journals']:
            r=copy.deepcopy(self.r);item=r['journals'][name];item['text']=item['text'].replace('status=9/KILL','status=1/FAILURE').replace('status=0/SUCCESS','status=1/FAILURE');item['sha256']=hashlib.sha256(item['text'].encode()).hexdigest()
            with self.assertRaises((ValueError,AssertionError)):verify(r)
    def test_wrong_source_and_hidden_failures_rejected(self):
        for field in ['source','preparations']:
            r=copy.deepcopy(self.r)
            if field=='source':r['result']['commit']='a'*40
            else:r['retained_preparations']=[]
            with self.assertRaises((ValueError,AssertionError)):verify(r)
