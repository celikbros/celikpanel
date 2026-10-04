import copy
import os
import tempfile
from unittest.mock import patch
import importlib.util
from pathlib import Path
import sys
from types import SimpleNamespace
import unittest

SPEC=importlib.util.spec_from_file_location('tested_update_kill',Path(__file__).with_name('guest_update_kill.py'))
s=importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name]=s
SPEC.loader.exec_module(s)

class FakeNative:
    def __init__(self):
        self.state={'worker':{'ActiveState':'active'},'transaction':{'phase':'active','operation':'update','snapshot':'fixture'},'recovery':{'ActiveState':'inactive'}}
        self.identity={'unit':'exact','pid':123,'start_ticks':'44','invocation_id':'a'*32}
        self.actions=[]
        self.error=None
    def observe(self): return copy.deepcopy(self.state)
    def installed(self): return {'agent':'a'*64,'panel':'b'*64}
    def worker_identity(self): return self.identity.copy()
    def freeze(self):
        self.actions.append('freeze')
        if self.error=='freeze': raise OSError()
        if self.error=='phase': self.state['transaction']['phase']='completion.pending'
    def revalidate(self,identity):
        if identity != self.identity or self.error=='identity': raise s.MissedCheckpoint('identity-changed')
    def full_proof(self,snapshot,tick):
        tick()
        if self.error=='proof': raise OSError()
        return {'snapshot':snapshot,'manifest_sha256':'c'*64,'verified_files':129,'installed_artifacts':self.installed()}
    def kill(self):
        self.actions.append('kill')
        if self.error=='kill': raise OSError()
    def thaw(self): self.actions.append('thaw');return 0

class KillTests(unittest.TestCase):
    def run_case(self,native,**kwargs):
        events=[]
        result=s.run_kill(SimpleNamespace(operation_id='a'*32),lambda event,**fields:events.append(dict(event=event,**fields)),native,**kwargs)
        return result,events
    def test_valid_checkpoint_kills_only_after_proof_then_thaws(self):
        native=FakeNative()
        result,events=self.run_case(native)
        self.assertEqual(result,0)
        self.assertEqual(native.actions,['freeze','kill','thaw'])
        self.assertEqual([e['event'] for e in events],['armed','worker_frozen','candidate_installed_checkpoint','kill_requested','kill_sent','released'])
        self.assertTrue(events[-1]['kill_sent'])
    def test_opted_in_data_fault_occurs_after_proof_before_kill(self):
        native=FakeNative(); events=[]
        def data_fault(identity,proof,tick):
            self.assertEqual(events[-1]['event'],'candidate_installed_checkpoint')
            self.assertEqual(identity,native.identity); self.assertEqual(proof['verified_files'],129)
            native.actions.append('data-fault');return {'status':'applied'}
        native.candidate_data_fault=data_fault
        result=s.run_kill(SimpleNamespace(operation_id='a'*32,candidate_data_fault='quarantine-fixed-three'),lambda event,**fields:events.append(dict(event=event,**fields)),native)
        self.assertEqual(result,0);self.assertEqual(native.actions,['freeze','data-fault','kill','thaw'])
        self.assertEqual([e['event'] for e in events],['armed','worker_frozen','candidate_installed_checkpoint','candidate_data_fault_applied','kill_requested','kill_sent','released'])

    def test_data_fault_failure_cannot_claim_application_or_kill(self):
        native=FakeNative();events=[]
        def fail(*args):raise ValueError('partial retained data fault')
        native.candidate_data_fault=fail
        result=s.run_kill(SimpleNamespace(operation_id='a'*32,candidate_data_fault='quarantine-fixed-three'),lambda event,**fields:events.append(dict(event=event,**fields)),native)
        self.assertEqual(result,2);self.assertEqual(native.actions,['freeze','thaw'])
        self.assertFalse(any(e['event'] in ('candidate_data_fault_applied','kill_requested','kill_sent') for e in events))

    def test_completion_pending_is_never_killed(self):
        native=FakeNative();native.state['transaction']['phase']='completion.pending'
        result,events=self.run_case(native)
        self.assertEqual(result,2);self.assertEqual(native.actions,[])
        self.assertFalse(events[-1]['kill_sent'])
    def test_phase_changed_after_freeze_is_thawed_without_kill(self):
        native=FakeNative();native.error='phase'
        result,events=self.run_case(native)
        self.assertEqual(result,2);self.assertEqual(native.actions,['freeze','thaw'])
    def test_proof_identity_or_freeze_failure_never_kills(self):
        for error in ('proof','identity','freeze'):
            with self.subTest(error=error):
                native=FakeNative();native.error=error
                result,events=self.run_case(native)
                self.assertEqual(result,2);self.assertNotIn('kill',native.actions);self.assertEqual(native.actions[-1],'thaw')
    def test_kill_ambiguous_never_becomes_confirmed(self):
        native=FakeNative();native.error='kill'
        result,events=self.run_case(native)
        self.assertEqual(result,2);self.assertEqual(native.actions,['freeze','kill','thaw'])
        self.assertNotIn('kill_sent',[e['event'] for e in events])
    def test_signal_before_checkpoint_makes_no_changes(self):
        native=FakeNative()
        result,events=self.run_case(native,interrupted=lambda:True)
        self.assertEqual(result,2);self.assertEqual(native.actions,[])
    def test_signal_during_proof_always_thaws_without_kill(self):
        native=FakeNative()
        result,events=self.run_case(native,interrupted=lambda:'freeze' in native.actions)
        self.assertEqual(result,2);self.assertEqual(native.actions,['freeze','thaw'])
    def test_process_start_allows_spaces_and_parentheses_in_comm(self):
        fields=['S']+['0']*18+['12345']+['0']*4
        self.assertEqual(s.process_start('2 (worker (test)) '+' '.join(fields)),'12345')
    def test_exact_unit_boundaries(self):
        op='a'*32
        props={'Id':s.unit_name(op),'LoadState':'loaded','ActiveState':'active','MainPID':'12','ControlGroup':'/system.slice/'+s.unit_name(op),'InvocationID':'b'*32}
        s.validate_worker_fields(props,op)
        for key,value in [('Id','celikpanel-agent.service'),('MainPID','0'),('ControlGroup','/system.slice/celikpanel-agent.service'),('InvocationID','')]:
            changed=dict(props);changed[key]=value
            with self.subTest(key=key),self.assertRaises(s.MissedCheckpoint):s.validate_worker_fields(changed,op)
        with self.assertRaises(s.probe.ProbeError):s.unit_name('../bad')
    def test_timer_noop_requires_exact_updater_exclusive_lock(self):
        native=FakeNative();native.state['recovery']['ActiveState']='active'
        for proof in (None,False,'true'):
            native.state['update_lock_exclusive']=proof
            with self.assertRaises(s.MissedCheckpoint):s.active_snapshot(native.state,'fixture')
        native.state['update_lock_exclusive']=True
        self.assertEqual(s.active_snapshot(native.state,'fixture'),'fixture')
        result,events=self.run_case(native)
        self.assertEqual(result,0);self.assertTrue(events[-1]['kill_sent'])

    @unittest.skipUnless(sys.platform=='linux' and getattr(os,'geteuid',lambda:-1)()==0,'real kernel flock evidence')
    def test_real_kernel_fd_exclusive_shared_open_and_foreign_holder(self):
        import fcntl, subprocess
        with tempfile.TemporaryDirectory() as directory:
            lock=Path(directory)/'transaction.lock';lock.touch(mode=0o600)
            with lock.open('rb') as held:
                self.assertFalse(s.process_owns_exclusive_lock(Path('/proc/self'),lock))
                fcntl.flock(held,fcntl.LOCK_SH)
                self.assertFalse(s.process_owns_exclusive_lock(Path('/proc/self'),lock))
                fcntl.flock(held,fcntl.LOCK_EX)
                self.assertTrue(s.process_owns_exclusive_lock(Path('/proc/self'),lock))
                fcntl.flock(held,fcntl.LOCK_UN)
                child=subprocess.Popen([sys.executable,'-c',"import fcntl,sys;f=open(sys.argv[1],'rb');fcntl.flock(f,fcntl.LOCK_EX);print('held',flush=True);sys.stdin.read()",str(lock)],stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True)
                try:
                    self.assertEqual(child.stdout.readline().strip(),'held')
                    self.assertFalse(s.process_owns_exclusive_lock(Path('/proc/self'),lock))
                    self.assertTrue(s.process_owns_exclusive_lock(Path('/proc')/str(child.pid),lock))
                finally:child.communicate('',timeout=5)
            lock.chmod(0o644)
            self.assertFalse(s.process_owns_exclusive_lock(Path('/proc/self'),lock))

    def test_snapshot_change_and_recovery_refused(self):
        native=FakeNative()
        with self.assertRaises(s.MissedCheckpoint):s.active_snapshot(native.state,'other')
        native.state['recovery']['ActiveState']='active'
        with self.assertRaises(s.MissedCheckpoint):s.active_snapshot(native.state,'fixture')

if __name__=='__main__':unittest.main()
