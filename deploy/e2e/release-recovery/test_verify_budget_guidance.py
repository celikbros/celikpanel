"""Reject mismatched or stale native recovery guidance acceptance."""
import copy
import importlib.util
from pathlib import Path
import unittest
spec=importlib.util.spec_from_file_location('tested_guidance',Path(__file__).with_name('verify_budget_guidance.py'))
f=importlib.util.module_from_spec(spec);spec.loader.exec_module(f)

class GuidanceEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.op='a'*32;self.target='b'*40
        status={'schema':'celikpanel-recovery-observation/v1','request_id':self.op,'target_commit':self.target,
                'phase':'recovery_required','terminal_proof':'none','reason':'recovery_incomplete','observed_at':'2026-09-21T22:00:00Z','previous_failure':'recovery_incomplete'}
        raw=''.join(k+'='+v+'\n' for k,v in status.items())
        cli={k:status[k] for k in ('request_id','phase','terminal_proof','reason','observed_at','previous_failure')}
        cli.update(observation='known',automatic_recovery='paused_retry_limit')
        hint={'schema':'celikpanel-recovery-automatic/v1','request_id':self.op,'observation_identity':'native-identity',
              'observation_sha256':f.v.w.wait.sha(raw.encode()),'automatic_recovery':'paused_retry_limit'}
        command='sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50'
        self.value={'schema':'celikpanel/native-budget-guidance/v1','operation_id':self.op,'phase':'exhausted',
            'status_raw':raw,'status_identity':'native-identity','automatic_raw':''.join(k+'='+v+'\n' for k,v in hint.items()),
            'cli':cli,'receipts':{'1':{},'2':{},'3':{}},'lock_free':True,'cli_text':{
            'en':self.op+' all three attempts one-time same-operation retry '+command,
            'tr':self.op+' Sunucu sahibi tek seferlik '+command}}
    def check(self,value=None,phase='exhausted'):
        f.check(value or self.value,self.op,self.target,phase)
    def test_bound_native_guidance_passes(self):self.check()
    def test_republished_identical_bytes_reject_old_identity(self):
        self.value['status_identity']='republished';self.assertRaises(ValueError,self.check)
    def test_foreign_or_altered_status_and_hint_rejected(self):
        for key,old,new in [('status_raw',self.target,'c'*40),('automatic_raw',self.op,'d'*32),
                             ('automatic_raw','paused_retry_limit','different'),('status_raw','recovery_incomplete','update_failed')]:
            value=copy.deepcopy(self.value);value[key]=value[key].replace(old,new)
            with self.subTest(key=key,old=old),self.assertRaises(ValueError):self.check(value)
    def test_unknown_cli_lost_failure_or_missing_pause_rejected(self):
        for key,value in [('observation','unknown'),('previous_failure','none'),('automatic_recovery','')]:
            wrong=copy.deepcopy(self.value);wrong['cli'][key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):self.check(wrong)
    def test_missing_action_or_unreleased_lock_rejected(self):
        self.value['lock_free']=False;self.assertRaises(ValueError,self.check)
        self.value['lock_free']=True;self.value['cli_text']['tr']='generic error';self.assertRaises(ValueError,self.check)
    def test_terminal_ignores_retained_pause_and_rejects_visible_stale_pause(self):
        value=copy.deepcopy(self.value);value['phase']='terminal';value['status_identity']='terminal'
        value['status_raw']=value['status_raw'].replace('phase=recovery_required','phase=recovered').replace('terminal_proof=none','terminal_proof=rollback_verified').replace('reason=recovery_incomplete','reason=rollback_verified')
        value['cli'].update(phase='recovered',terminal_proof='rollback_verified',reason='rollback_verified')
        value['cli'].pop('automatic_recovery');value['cli_text']={'en':'verified restoration','tr':'restored'}
        self.check(value,'terminal')
        value['cli']['automatic_recovery']='paused_retry_limit'
        with self.assertRaises(ValueError):self.check(value,'terminal')

if __name__=='__main__':unittest.main()
