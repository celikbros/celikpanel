"""Reject incomplete or contradictory budget acceptance evidence."""
import copy
import importlib.util
from pathlib import Path
import unittest
spec=importlib.util.spec_from_file_location('tested_budget_verify',Path(__file__).with_name('verify_dispatch_budget.py'))
f=importlib.util.module_from_spec(spec);spec.loader.exec_module(f)

class BudgetEvidenceTests(unittest.TestCase):
    def test_platform_comes_from_closed_sealed_node(self):
        self.assertEqual(f.platform_name('arch'),'Arch Linux')
        self.assertEqual(f.platform_name('debian13'),'Debian 13')
        with self.assertRaises(ValueError):f.platform_name('unknown')

    def test_republished_wait_requires_exact_stale_bytes_and_multiple_invocations(self):
        op='a'*32
        raw='schema=celikpanel-recovery-wait/v1\nrequest_id='+op+'\nobservation_identity=earlier\nobservation_sha256='+'b'*64+'\nwaiting_for=starting\n'
        saved={'operation_id':op,'boot_id':'boot','wait_raw':raw,'status_sha256':'c'*64,'status_identity':'terminal'}
        terminal={'boot_id':'boot','stale_wait_sha256':f.w.wait.sha(raw.encode())}
        waits=[{'_SYSTEMD_INVOCATION_ID':'d'*32},{'_SYSTEMD_INVOCATION_ID':'e'*32}]
        f.check_republished_wait(saved,terminal,op,waits)
        for key,value in [('operation_id','f'*32),('boot_id','other'),('wait_raw',raw+'x'),('status_sha256','b'*64),('status_identity','earlier')]:
            wrong=dict(saved);wrong[key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):f.check_republished_wait(wrong,terminal,op,waits)
        with self.assertRaises(ValueError):f.check_republished_wait(saved,terminal,op,waits[:1])

    def setUp(self):
        self.op='a'*32;self.snapshot='snapshot'
        self.before={'schema':'celikpanel/native-budget-result/v1','operation_id':self.op,'snapshot':self.snapshot,
            'lock_free':True,'boot_id':'boot','identity':{'node':'lab'},'phase':'exhausted','at':'2026-09-21T20:00:00Z',
            'cli':{'request_id':self.op,'observation':'known','phase':'recovery_required','terminal_proof':'none','reason':'recovery_incomplete'},
            'marker':{'snapshot':self.snapshot,'transaction_phase':'active'},'receipts':{str(n):{'sha256':str(n)*64,'bytes':275} for n in (1,2,3)},
            'paused_messages':['Automatic recovery paused after three admitted attempts.']*2}
        self.after=copy.deepcopy(self.before);self.after.update(phase='terminal',marker=None,at='2026-09-21T20:01:00Z')
        self.after['cli'].update(phase='recovered',reason='rollback_verified',terminal_proof='rollback_verified')
        self.after['receipts']['owner.Abc123']={'sha256':'4'*64,'bytes':279}
    def check(self):f.check_budget(self.before,self.after,self.op,self.snapshot)
    def test_consistent_budget_and_one_owner_retry_pass(self):self.check()
    def test_unknown_or_other_operation_cannot_be_accepted(self):
        for key,value in [('request_id','b'*32),('observation','unknown'),('phase','recovering'),('terminal_proof','none')]:
            original=copy.deepcopy(self.after);self.after['cli'][key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):self.check()
            self.after=original
    def test_receipt_reset_or_fourth_automatic_attempt_is_not_owner_retry(self):
        self.after['receipts']['1']['sha256']='f'*64
        with self.assertRaises(ValueError):self.check()
        self.after['receipts']['1']=copy.deepcopy(self.before['receipts']['1'])
        self.after['receipts']['4']=self.after['receipts'].pop('owner.Abc123')
        with self.assertRaises(ValueError):self.check()
    def test_duplicate_owner_admission_rejected(self):
        self.after['receipts']['owner.Def456']={'sha256':'5'*64,'bytes':279}
        with self.assertRaises(ValueError):self.check()
    def test_marker_lock_boot_or_snapshot_mismatch_rejected(self):
        for key,value in [('marker',self.before['marker']),('lock_free',False),('boot_id','other'),('snapshot','foreign'),('at','2026-09-21T19:59:00Z')]:
            original=copy.deepcopy(self.after);self.after[key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):self.check()
            self.after=original
    def test_single_pause_is_not_repeated_native_deferral(self):
        self.before['paused_messages']=self.before['paused_messages'][:1]
        with self.assertRaises(ValueError):self.check()

if __name__=='__main__':unittest.main()
