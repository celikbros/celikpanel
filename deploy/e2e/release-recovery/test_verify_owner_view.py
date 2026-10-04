import copy
import importlib.util
from pathlib import Path
import unittest
spec=importlib.util.spec_from_file_location('tested_owner_view',Path(__file__).with_name('verify_owner_view.py'))
f=importlib.util.module_from_spec(spec);spec.loader.exec_module(f)

class OwnerViewEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.op='a'*32;self.digest='b'*64
        receipts={str(n):{'sha256':str(n)*64} for n in (1,2,3)}
        before={'operation_id':self.op,'boot_id':'boot','identity':{'node':'sealed'},'receipts':receipts}
        stopped={name:{'MainPID':'0','ActiveState':'inactive'} for name in ('panel','agent')}
        self.samples={'admitted':{'before':before,'entry_sha256':self.digest},'ready':{
            'operation_id':self.op,'boot_id':'boot','identity':before['identity'],'services':stopped,
            'runtime_sha256':'c'*64,'entry_sha256':self.digest,'running_sha256':self.digest,
            'anonymous_http':401,'wrong_code_http':401,'foreign_origin_http':403,'authorized_http':200,
            'response':{'status':{'request_id':self.op,'observation':'known','phase':'recovery_required','automatic_recovery':'paused_retry_limit','terminal_proof':'none'}}},
            'closed':{'operation_id':self.op,'boot_id':'boot','services':stopped,'exit_code':0,'stdout_bytes':0,'stderr_bytes':0,'automatic_receipts':receipts}}
        self.result={'request_id':self.op,'selected_runtime_sha256':'c'*64,'automatic_receipts_preserved':receipts}
    def check(self,samples=None):f.check(samples or self.samples,self.result,self.digest)
    def test_complete_scoped_proof(self):self.check()
    def test_active_normal_panel_cannot_count_as_independent_view(self):
        self.samples['ready']['services']['panel']={'MainPID':'42','ActiveState':'active'}
        self.assertRaises(ValueError,self.check)
    def test_other_executable_or_runtime_or_vm_rejected(self):
        for key,value in [('runtime_sha256','d'*64),('running_sha256','d'*64),('identity',{'node':'other'}),('boot_id','other'),('operation_id','e'*32)]:
            sample=copy.deepcopy(self.samples);sample['ready'][key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):self.check(sample)
    def test_anonymous_or_wrong_code_or_foreign_origin_success_rejected(self):
        for key in ('anonymous_http','wrong_code_http','foreign_origin_http'):
            sample=copy.deepcopy(self.samples);sample['ready'][key]=200
            with self.subTest(key=key),self.assertRaises(ValueError):self.check(sample)
    def test_unavailable_or_terminal_data_is_not_exhausted_access_proof(self):
        for key,value in [('observation','unavailable'),('phase','recovered'),('terminal_proof','rollback_verified')]:
            sample=copy.deepcopy(self.samples);sample['ready']['response']['status'][key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):self.check(sample)
    def test_leaked_output_failed_exit_or_changed_budget_rejected(self):
        for key,value in [('stdout_bytes',64),('stderr_bytes',64),('exit_code',1),('automatic_receipts',{})]:
            sample=copy.deepcopy(self.samples);sample['closed'][key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):self.check(sample)

if __name__=='__main__':unittest.main()
