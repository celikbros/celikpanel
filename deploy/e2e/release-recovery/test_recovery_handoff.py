import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
from types import SimpleNamespace
import sys
import tempfile
import unittest
from unittest import mock
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('tested_recovery_handoff', HERE / 'guest_recovery_handoff.py')
h = importlib.util.module_from_spec(spec); spec.loader.exec_module(h)
spec = importlib.util.spec_from_file_location('tested_recovery_host', HERE / 'recovery_fault_trial.py')
c = importlib.util.module_from_spec(spec); spec.loader.exec_module(c)
spec = importlib.util.spec_from_file_location('handoff_update_tests', HERE / 'test_guest_update_kill.py')
k = importlib.util.module_from_spec(spec); spec.loader.exec_module(k)
OP = 'a' * 32
ID = {'nonce':'b'*64,'vm_uuid':'67139a2c-7b23-4387-95ad-45f9e2b831ea','cell_id':'release-recovery__fixture','node':'arch'}
SNAP = '20260914T120000Z-from-unknown-to-' + 'c'*40 + '-' + 'd'*32
TX = {'snapshot': SNAP, 'transaction_phase':'active','transaction_operation':'update','transaction_token_sha256':'e'*64}
ARGS = SimpleNamespace(operation_id=OP,recovery_action='kill',recovery_checkpoint='payload_restored')
PROOF = {'snapshot':SNAP,'manifest_sha256':'f'*64,'verified_files':129}


class HandoffTests(unittest.TestCase):
    def test_exact_tuple_construction_never_contains_raw_token(self):
        value = h.make_intent(ARGS, ID, PROOF, TX, '1'*64, '2'*64)
        self.assertEqual(value['transaction_token_sha256'], TX['transaction_token_sha256'])
        self.assertEqual(value['snapshot_manifest_sha256'], PROOF['manifest_sha256'])
        self.assertNotIn('token', value)
        self.assertEqual(value['worker_command'], ['/bin/bash',str(h.fault.RUNTIME_ROOT/('1'*64)/'deploy/recovery/runtime-entry.sh')])

    def test_wrong_phase_snapshot_operation_never_constructs_intent(self):
        for key, value in [('snapshot','other'),('transaction_phase','completion.pending'),('transaction_operation','rollback')]:
            changed = dict(TX); changed[key] = value
            with self.subTest(key=key), self.assertRaises(h.fault.Unavailable): h.make_intent(ARGS, ID, PROOF, changed, '1'*64,'2'*64)

    def test_launch_has_exact_unit_and_identity_bound_execstoppost(self):
        argv = h.launch_argv(ID, OP)
        self.assertIn('--unit=celikpanel-lab-recovery-fault-'+OP+'.service', argv)
        self.assertEqual(h.helper_argv(ID,OP)[0],'/usr/bin/python3')
        self.assertIn('/usr/bin/python3',argv)
        self.assertNotIn('python3',argv)
        cleanup = next(value for value in argv if value.startswith('--property=ExecStopPost='))
        self.assertIn('--cleanup',cleanup);self.assertIn('--operation-id '+OP,cleanup);self.assertIn(ID['nonce'],cleanup)
        self.assertNotIn('/usr/bin/systemctl thaw celikpanel-release-recovery.service',cleanup)
        self.assertFalse(any('reboot' in value or 'system_reset' in value for value in argv))

    def test_existing_or_invalid_operation_never_changes_unit(self):
        for value in ('',OP+';reboot','../x'):
            with self.assertRaises(h.fault.Unavailable):h.names(value)

    @unittest.skipUnless(sys.platform=='linux' and getattr(os,'geteuid',lambda:-1)()==0,'native private metadata')
    def test_durable_intent_is_private_and_cannot_be_replaced(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'intent.json'
            digest=h.save_once(path,{'operation_id':OP})
            self.assertEqual(digest,hashlib.sha256(path.read_bytes()).hexdigest())
            self.assertEqual(path.stat().st_mode & 0o777,0o600)
            with self.assertRaises(FileExistsError):h.save_once(path,{'operation_id':'other'})
            self.assertEqual(json.loads(path.read_text()),{'operation_id':OP})

    def test_changed_invocation_cleanup_does_not_stop_new_unit(self):
        actual={'MainPID':'5','InvocationID':'1'*32,'ControlGroup':'/system.slice/exact'}
        with patch.object(h,'properties',return_value=actual),patch.object(h.subprocess,'run') as run:
            self.assertEqual(h.cancel(OP,dict(actual,InvocationID='2'*32)),'identity-changed')
        run.assert_not_called()

    @unittest.skipUnless(sys.platform=='linux' and getattr(os,'geteuid',lambda:-1)()==0,'native private metadata')
    def test_real_sealed_intent_precedes_launch_and_armed_proof(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            unit={'Id':h.names(OP)['unit'],'MainPID':'12','InvocationID':'9'*32,'ControlGroup':'/system.slice/'+h.names(OP)['unit'],'LoadState':'loaded','ActiveState':'active'}
            event={'schema':h.fault.SCHEMA,'identity':ID,'operation_id':OP,'event':'armed','action':'kill','checkpoint':'payload_restored'}
            order=[]
            def launch(argv,**kwargs):
                saved=json.loads((root/('recovery-fault-'+OP+'.json')).read_bytes())
                self.assertEqual(saved['transaction_token_sha256'],TX['transaction_token_sha256'])
                order.append('launch-after-seal');return SimpleNamespace(returncode=0)
            with patch.object(h.fault,'PRIVATE_ROOT',root),patch.object(h.fault.probe,'guard_guest',return_value=ID),patch.object(h,'properties',side_effect=[dict(unit,LoadState='not-found'),unit]),patch.object(h,'helper_identity',return_value=unit),patch.object(h.fault,'read_transaction',return_value=TX),patch.object(h,'selection',return_value=('1'*64,b'selection')),patch.object(h.fault,'verify_runtime',return_value={'verified_files':12}),patch.object(h.fault.files,'digest_file',return_value='2'*64),patch.object(h.subprocess,'run',side_effect=launch),patch.object(h,'read_events',return_value=(h.encoded(event),[event])):
                result=h.arm(ARGS,{'pid':987},PROOF,lambda:None,lambda identity:order.append('revalidate'))
            self.assertEqual(result['unit'],unit)
            self.assertEqual(result['intent']['snapshot'],SNAP)
            self.assertEqual(order.count('launch-after-seal'),1)
            self.assertEqual(result['intent_sha256'],hashlib.sha256((root/('recovery-fault-'+OP+'.json')).read_bytes()).hexdigest())

    def test_changed_marker_after_arm_stops_helper_and_refuses_handoff(self):
        unit={'MainPID':'12','InvocationID':'9'*32,'ControlGroup':'/system.slice/exact','LoadState':'loaded','ActiveState':'active'}
        changed=dict(TX,transaction_token_sha256='0'*64)
        with patch.object(h.fault.probe,'guard_guest',return_value=ID),patch.object(h,'properties',return_value=dict(unit,LoadState='not-found')),patch.object(Path,'exists',return_value=False),patch.object(Path,'is_symlink',return_value=False),patch.object(h.fault,'read_transaction',side_effect=[TX,TX,changed]),patch.object(h,'selection',return_value=('1'*64,b'selection')),patch.object(h.fault,'verify_runtime',return_value={}),patch.object(h.fault.files,'digest_file',return_value='2'*64),patch.object(h,'save_once',return_value='3'*64),patch.object(h.subprocess,'run',return_value=SimpleNamespace(returncode=0)),patch.object(h,'cancel_admission_unknown') as cancel:
            with self.assertRaises(h.fault.Unavailable):h.arm(ARGS,{'pid':987},PROOF,lambda:None,lambda identity:None)
        cancel.assert_called_once_with(ID,OP)

    def test_launch_timeout_or_nonzero_enters_exact_cleanup_scope_without_retry(self):
        import subprocess
        for outcome in (subprocess.TimeoutExpired('systemd-run',8),SimpleNamespace(returncode=1)):
            with self.subTest(outcome=type(outcome).__name__):
                with patch.object(h.fault.probe,'guard_guest',return_value=ID),patch.object(h,'properties',return_value={'LoadState':'not-found'}),patch.object(Path,'exists',return_value=False),patch.object(Path,'is_symlink',return_value=False),patch.object(h.fault,'read_transaction',return_value=TX),patch.object(h,'selection',return_value=('1'*64,b'selection')),patch.object(h.fault,'verify_runtime',return_value={}),patch.object(h.fault.files,'digest_file',return_value='2'*64),patch.object(h,'save_once',return_value='3'*64),patch.object(h.subprocess,'run',side_effect=outcome if isinstance(outcome,Exception) else None,return_value=outcome) as launch,patch.object(h,'cancel_admission_unknown') as cancel:
                    with self.assertRaises((h.fault.Unavailable,subprocess.TimeoutExpired)):
                        h.arm(ARGS,{'pid':987},PROOF,lambda:None,lambda identity:None)
                self.assertEqual(launch.call_count,1)
                cancel.assert_called_once_with(ID,OP)

    def test_unknown_admission_never_stops_without_exact_helper_process_proof(self):
        with patch.object(h,'helper_identity',side_effect=h.fault.Unavailable('wrong command')),patch.object(h,'cancel') as cancel:
            self.assertEqual(h.cancel_admission_unknown(ID,OP),'identity-unavailable')
        cancel.assert_not_called()
        exact={'MainPID':'12','InvocationID':'1'*32,'helper_proof':{'operation_id':OP,'identity':ID}}
        with patch.object(h,'helper_identity',return_value=exact),patch.object(h,'cancel',return_value='stopped') as cancel:
            self.assertEqual(h.cancel_admission_unknown(ID,OP),'stopped')
        cancel.assert_called_once_with(OP,exact)

    def test_opt_in_handoff_happens_before_updater_kill(self):
        native=k.FakeNative(); events=[]
        def arm(identity,proof,tick):native.actions.append('recovery-arm');tick();return {'intent_sha256':'e'*64}
        native.recovery_handoff=arm;native.cancel_recovery_handoff=lambda:native.actions.append('cancel')
        result=k.s.run_kill(ARGS,lambda event,**fields:events.append(event),native)
        self.assertEqual(result,0);self.assertEqual(native.actions,['freeze','recovery-arm','kill','thaw'])
        self.assertLess(events.index('recovery_fault_armed'),events.index('kill_requested'))

    def test_failed_handoff_never_kills_and_cleans_up(self):
        native=k.FakeNative()
        def arm(*args):native.actions.append('recovery-arm');raise RuntimeError('unavailable')
        native.recovery_handoff=arm;native.cancel_recovery_handoff=lambda:native.actions.append('cancel')
        result=k.s.run_kill(ARGS,lambda *args,**kwargs:None,native)
        self.assertEqual(result,2);self.assertEqual(native.actions,['freeze','recovery-arm','cancel','thaw'])

    def test_default_updater_fault_does_not_arm_recovery(self):
        native=k.FakeNative()
        native.recovery_handoff=lambda *args:self.fail('default changed')
        result=k.s.run_kill(SimpleNamespace(operation_id=OP),lambda *args,**kwargs:None,native)
        self.assertEqual(result,0);self.assertEqual(native.actions,['freeze','kill','thaw'])

    def test_host_mutation_without_execute_never_loads_lab(self):
        for mode in ('prepare','arm','start','reboot'):
            with self.subTest(mode=mode),patch.object(c.lab,'checked_root') as checked,self.assertRaises(SystemExit):
                c.main(['--work-root','/var/tmp/cp-release-drill-test','--node','arch','--mode',mode])
            checked.assert_not_called()

    def test_qmp_reset_cannot_be_repeated_and_checks_same_process(self):
        q=c.QMP.__new__(c.QMP);q.node={};q.expected={'pid':123};q.sent=False
        calls=[];q.command=lambda command:calls.append(command)
        with patch.object(c,'qemu_identity',return_value=q.expected):
            q.reset()
            with self.assertRaises(ValueError):q.reset()
        self.assertEqual(calls,['system_reset'])
        q.sent=False
        with patch.object(c,'qemu_identity',return_value={'pid':456}),self.assertRaises(ValueError):q.reset()
        self.assertEqual(calls,['system_reset'])

    def test_reboot_request_durably_saved_before_one_qmp_reset(self):
        intent={'identity':ID,'operation_id':OP,'recovery_fault':{'action':'reboot','checkpoint':'payload_restored'}}
        worker={'boot_id':'old'}
        events=[{'event':'reboot_ready','worker':worker}]
        value={'reboot_proof':{'worker':worker,'checkpoint_sha256':'a'*64}}
        order=[]
        class Q:
            def reset(self):order.append('reset')
            def close(self):order.append('close')
        with patch.object(c.local,'assert_absent'),patch.object(c.trial,'read_private',return_value=json.dumps(intent).encode()),patch.object(c,'read_guest',return_value=(value,b'events',events)),patch.object(c,'qemu_identity',return_value={'pid':123}),patch.object(c,'QMP',return_value=Q()),patch.object(c,'save_collection',return_value={}),patch.object(c.trial,'save',side_effect=lambda *a:order.append(a[2])):
            c.reboot(Path('/unused'),{}, {'nodes':{'arch':{}}},'arch',intent)
        self.assertEqual(order,[c.REBOOT_ATTEMPT,'reset','recovery-fault-reboot-result.json','close'])

    def test_kill_intent_never_allows_reboot(self):
        with patch.object(c,'read_guest') as read,self.assertRaises(ValueError):
            c.reboot(Path('/unused'),{}, {},'arch',{'recovery_fault':{'action':'kill'}})
        read.assert_not_called()


class CandidateDataForwardingTests(unittest.TestCase):
    def test_recovery_prepare_forwards_sealed_data_fault(self):
        with mock.patch.object(c.lab,'checked_root',return_value=Path('/unused')),mock.patch.object(c.lab,'load',return_value=({}, {'nodes':{'arch':{}}})),mock.patch.object(c.lab,'process_guard'),mock.patch.object(c.local,'prepare') as prepare:
            c.main(['--work-root','/unused','--node','arch','--mode','prepare','--execute','--archive','/tmp/payload','--archive-sha256','a'*64,'--action','kill','--checkpoint','payload_restored','--candidate-data-fault','quarantine-fixed-three'])
        self.assertEqual(prepare.call_args.kwargs['candidate_data_fault'],'quarantine-fixed-three')
        self.assertEqual(prepare.call_args.kwargs['recovery_fault'],{'action':'kill','checkpoint':'payload_restored'})

    def test_data_fault_cannot_be_reselected_at_start(self):
        with mock.patch.object(c.lab,'checked_root') as checked,self.assertRaises(SystemExit):
            c.main(['--work-root','/unused','--node','arch','--mode','start','--execute','--candidate-data-fault','quarantine-fixed-three'])
        checked.assert_not_called()


if __name__=='__main__':unittest.main()
