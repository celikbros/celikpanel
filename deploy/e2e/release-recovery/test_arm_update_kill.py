import importlib.util
import json
from pathlib import Path
import sys
import unittest
from unittest import mock

SPEC=importlib.util.spec_from_file_location('tested_kill_controller',Path(__file__).with_name('arm_update_kill.py'))
s=importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name]=s
SPEC.loader.exec_module(s)

class ControllerTests(unittest.TestCase):
    def setUp(self):
        self.identity={'nonce':'a'*64,'vm_uuid':'fixture','cell_id':'fixture','node':'arch'}
        self.operation='b'*32
    def event(self,kind):
        return {'schema':s.EVENT_SCHEMA,'event':kind,'identity':self.identity,'operation_id':self.operation,'at':'2026-09-14T08:00:00Z'}
    def raw(self,events):return b''.join((json.dumps(e)+'\n').encode() for e in events)
    def test_complete_stream(self):
        kinds=['armed','worker_frozen','candidate_installed_checkpoint','kill_requested','kill_sent','released']
        events=s.validate_events(self.raw([self.event(k) for k in kinds]),self.identity,self.operation)
        self.assertEqual([e['event'] for e in events],kinds)
    def test_opted_in_recovery_handoff_stream_preserves_required_order(self):
        kinds=['armed','worker_frozen','candidate_installed_checkpoint','recovery_fault_armed','kill_requested','kill_sent','released']
        self.assertEqual([e['event'] for e in s.validate_events(self.raw([self.event(k) for k in kinds]),self.identity,self.operation)],kinds)
        for invalid in (['armed','recovery_fault_armed'],['armed','worker_frozen','candidate_installed_checkpoint','kill_requested','recovery_fault_armed']):
            with self.subTest(invalid=invalid),self.assertRaises(ValueError):s.validate_events(self.raw([self.event(k) for k in invalid]),self.identity,self.operation)

    def test_missed_checkpoint_stream_allowed_without_success_inference(self):
        events=s.validate_events(self.raw([self.event('armed'),self.event('released')]),self.identity,self.operation)
        self.assertEqual(len(events),2)
    def test_wrong_identity_duplicate_and_partial_refused(self):
        good=self.raw([self.event('armed')])
        cases=[(good,self.identity,'c'*32),(good+good,self.identity,self.operation),(good[:-1],self.identity,self.operation)]
        for raw,identity,op in cases:
            with self.subTest(raw=raw),self.assertRaises(ValueError):s.validate_events(raw,identity,op)
    def test_kill_sent_without_checkpoint_is_refused(self):
        with self.assertRaises(ValueError):
            s.validate_events(self.raw([self.event("armed"),self.event("kill_sent")]),self.identity,self.operation)
    def test_native_launch_only_fault_and_exact_thaw_cleanup(self):
        intent={'identity':self.identity,'request_id':self.operation}
        assets={'guest_update_kill.py':{'guest_path':'/root/celikpanel-release-recovery-lab/guest_update_kill.py'}}
        argv=s.launch_argv(intent,assets,{'agent':'c'*64,'panel':'d'*64},'arch')
        self.assertIn('--unit=celikpanel-lab-update-kill-'+self.operation+'.service',argv)
        self.assertIn('--property=ExecStopPost=-/usr/bin/systemctl thaw celikpanel-self-update-'+self.operation+'.service',argv)
        self.assertIn('--property=RuntimeMaxSec=650',argv)
        self.assertNotIn('--self-update-worker',argv)
        self.assertNotIn('start',argv)
        self.assertNotIn('celikpanel-agent.service',' '.join(argv))
    def test_execute_required_before_access(self):
        with mock.patch.object(s.lab,'checked_root') as checked:
            with self.assertRaises(SystemExit):s.main(['--work-root','/var/tmp/cp-release-drill-test','--mode','arm'])
            checked.assert_not_called()
    def test_collector_fixed_kind_and_generated_code(self):
        script=s.shared.guest_collection_script(self.operation,'update-kill')
        program=script.split('\n',1)[1].rsplit('CELIKPANEL_FAULT_COLLECT',1)[0]
        compile(program,'collector','exec')
        self.assertIn('/root/celikpanel-release-recovery-lab/update-kill-'+self.operation+'.jsonl',script)
        self.assertIn('celikpanel-lab-update-kill-'+self.operation+'.service',script)
        with self.assertRaises(ValueError):s.shared.guest_collection_script(self.operation,'../../bad')

if __name__=='__main__':unittest.main()
