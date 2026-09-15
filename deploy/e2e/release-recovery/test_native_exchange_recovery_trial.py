"""Closed two-fault fixture admission; never resets a VM in unit tests."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent

def module(name, file):
    spec = importlib.util.spec_from_file_location(name, HERE / file)
    value = importlib.util.module_from_spec(spec); sys.modules[name] = value; spec.loader.exec_module(value)
    return value

wrapper = module('composite_wrapper_tests', 'native_exchange_recovery_trial.py')
h = wrapper.controller; g = h.guest
c = module('composite_host_tests', 'recovery_fault_trial.py')
guest = module('composite_handoff_tests', 'guest_exchange_recovery_handoff.py')
old = module('composite_exchange_callbacks', 'test_native_database_exchange_trial.py')
B = g.RECOVERY_BOUNDARY
OP = 'a' * 32
ID = old.IDENTITY
FAULT = {'action': 'reboot', 'checkpoint': 'payload_restored'}

def fixtures():
    intent = {'identity': ID, 'operation_id': OP, 'recovery_fault': FAULT}
    exact = dict(FAULT, snapshot='snapshot', snapshot_manifest_sha256='b'*64,
                 transaction_token_sha256='c'*64, runtime_manifest_sha256='d'*64)
    entry = {'status': 'verified'}; trace = {'exit': 0}
    checkpoint = {'status': 'verified', 'classification': 'database-exchanged-before-receipt-held',
        'identity': ID, 'operation_id': OP, 'entry_proof_sha256': old.digest(entry), 'trace_sha256': old.digest(trace),
        'transaction': {'snapshot': exact['snapshot'], 'transaction_token_sha256': exact['transaction_token_sha256']},
        'database': {'snapshot': {'snapshot': exact['snapshot'], 'manifest_sha256': exact['snapshot_manifest_sha256']}},
        'kit': {'manifest_sha256': exact['runtime_manifest_sha256']}}
    handoff = {'identity': ID, 'operation_id': OP, 'intent': exact, 'unit': {'MainPID': '123'},
               'intent_sha256': hashlib.sha256(c.hand.encoded(exact)).hexdigest()}
    receipt = {'status': 'cut-sent', 'operation_id': OP, 'trace_sha256': checkpoint['trace_sha256'],
               'entry_proof_sha256': checkpoint['entry_proof_sha256'], 'scope': 'exact-update-unit-cgroup', 'signal': 'SIGKILL'}
    result = {'status': 'cut-sent', 'cut_receipt': receipt, 'independent_proof': checkpoint}
    kinds = ['armed', 'gate_released', 'database_exchange_entry_verified', 'database_exchange_checkpoint_verified',
             'recovery_fault_armed', 'kill_requested', 'kill_sent', 'trace_finished']
    events = [dict(identity=ID, operation_id=OP, event=kind) for kind in kinds]
    events[2]['checkpoint'] = entry; events[3].update(checkpoint=checkpoint, trace=trace)
    events[4]['handoff'] = handoff; events[6]['receipt'] = receipt; events[7]['result'] = result
    return intent, {'intent': exact}, {'result': result}, events


class CompositeProfileTests(unittest.TestCase):
    def test_wrapper_is_fixed_and_mutation_requires_execute(self):
        with mock.patch.object(h, 'main') as main:
            wrapper.main(['--mode', 'collect'])
        main.assert_called_once_with(['--mode', 'collect'], boundary=B)
        for mode in ('prepare', 'arm', 'start', 'reboot'):
            with self.subTest(mode=mode), mock.patch.object(h.lab, 'checked_root') as guard, self.assertRaises(SystemExit):
                wrapper.main(['--work-root', '/unused', '--node', 'arch', '--mode', mode])
            guard.assert_not_called()

    def test_inventory_and_names_isolate_both_old_profiles(self):
        assets = {Path(name).name: 'f'*64 for name in h.assets_for(B)}
        self.assertEqual(len(assets), 20); g.validate_helpers(assets, B)
        self.assertEqual(g.profile(B)['recovery_fault'], FAULT)
        for previous in ('wal', 'database-exchange'):
            self.assertNotIn('recovery_fault', g.profile(previous))
            self.assertFalse(set(h.host_names(previous)) & set(h.host_names(B)))
            with self.assertRaises(ValueError): g.validate_helpers(assets, previous)
        for name in assets:
            changed = dict(assets); del changed[name]
            with self.subTest(missing=name), self.assertRaises(ValueError): g.validate_helpers(changed, B)

    def test_flat_isolated_import_has_fixed_handoff(self):
        with tempfile.TemporaryDirectory() as directory:
            for name in h.assets_for(B): shutil.copyfile(HERE/name, Path(directory)/Path(name).name)
            script = "import importlib.util,sys; s=importlib.util.spec_from_file_location('h',sys.argv[1]); m=importlib.util.module_from_spec(s); s.loader.exec_module(m); assert m.FAULT=={'action':'reboot','checkpoint':'payload_restored'}"
            result = subprocess.run([sys.executable, '-I', '-c', script, str(Path(directory)/'guest_exchange_recovery_handoff.py')], capture_output=True, timeout=15)
            self.assertEqual(result.returncode, 0, result.stderr)

    def test_event_order_requires_handoff_before_cut(self):
        _, _, data, events = fixtures(); kinds = [e['event'] for e in events]
        h.validate_event_order(kinds, data['result'], boundary=B)
        for index in range(len(kinds)-1):
            with self.subTest(missing=index), self.assertRaises(ValueError):
                h.validate_event_order(kinds[:index]+kinds[index+1:], data['result'], boundary=B)
        with self.assertRaises(ValueError): h.validate_event_order(kinds, data['result'], boundary='database-exchange')


class CompositeHandoffTests(unittest.TestCase):
    def test_arm_requires_exact_tuple_and_revalidates_after_launch(self):
        intent, _, _, events = fixtures(); args = SimpleNamespace(boundary=B, operation_id=OP)
        checkpoint = events[3]['checkpoint']; worker = {'pid': 123}; revalidate = mock.Mock()
        with mock.patch.object(guest.hand, 'arm', return_value=events[4]['handoff']) as arm:
            result = guest.arm(args, intent, worker, checkpoint, revalidate)
        self.assertEqual(result, events[4]['handoff']); revalidate.assert_called_once_with()
        self.assertEqual(arm.call_args.args[0].recovery_checkpoint, 'payload_restored')
        self.assertEqual(arm.call_args.args[2], checkpoint['database']['snapshot'])

    def test_bad_plan_refused_without_launch(self):
        intent, _, _, events = fixtures(); args = SimpleNamespace(boundary=B, operation_id=OP)
        for fault in ({'action':'kill','checkpoint':'payload_restored'}, {'action':'reboot','checkpoint':'runtime_verified'}, None):
            with mock.patch.object(guest.hand, 'arm') as arm, self.assertRaises(ValueError):
                guest.arm(args, {**intent,'recovery_fault':fault}, {}, events[3]['checkpoint'], lambda:None)
            arm.assert_not_called()

    def test_changed_proof_cancels_only_admitted_helper_and_preserves_failure(self):
        intent, _, _, events = fixtures(); args = SimpleNamespace(boundary=B, operation_id=OP)
        with mock.patch.object(guest.hand, 'arm', return_value=events[4]['handoff']), mock.patch.object(guest.hand, 'cancel', side_effect=OSError('cleanup unknown')) as cancel:
            with self.assertRaisesRegex(ValueError, 'original'):
                guest.arm(args, intent, {}, events[3]['checkpoint'], lambda: (_ for _ in ()).throw(ValueError('original')))
        cancel.assert_called_once_with(OP, events[4]['handoff']['unit'])

    def test_deadline_expiry_does_not_authorize_kill(self):
        intent, _, _, events = fixtures(); args = SimpleNamespace(boundary=B, operation_id=OP)
        with mock.patch.object(guest.hand, 'arm', return_value=events[4]['handoff']), mock.patch.object(guest, 'cancel') as cancel, self.assertRaises(TimeoutError):
            guest.arm(args, intent, {}, events[3]['checkpoint'], lambda:None, clock=mock.Mock(side_effect=[0,46]))
        cancel.assert_called_once()


class CompositeHostTests(unittest.TestCase):
    def test_exact_cut_is_required_for_second_fault(self):
        intent, recovery, data, events = fixtures()
        proof = c.bind_native_cut(data, events, recovery, intent)
        self.assertEqual(proof['snapshot'], 'snapshot')
        cases = []
        d=copy.deepcopy(data);d['result']['status']='inconclusive';cases.append((d,events,recovery))
        e=copy.deepcopy(events);e[4]['handoff']['intent']['snapshot']='foreign';cases.append((data,e,recovery))
        e=copy.deepcopy(events);e[6]['receipt']['signal']='SIGTERM';cases.append((data,e,recovery))
        e=copy.deepcopy(events);e[3]['checkpoint']['entry_proof_sha256']='x';cases.append((data,e,recovery))
        r=copy.deepcopy(recovery);r['intent']['transaction_token_sha256']='foreign';cases.append((data,events,r))
        cases.append((data,events[:-1],recovery))
        for args in cases:
            with self.subTest(args=args), self.assertRaises(ValueError): c.bind_native_cut(*args,intent)

    def test_native_start_cannot_borrow_an_old_profile_or_other_entrypoint(self):
        intent, _, _, _ = fixtures(); value={'sealed':'native'}
        n=mock.Mock();n.guest.RECOVERY_BOUNDARY=B;n.load.return_value=(value,intent)
        n.host_names.return_value=('intent','start','arm');n.argv.return_value=['fixed','gate']
        with mock.patch.object(c.local, 'module', return_value=n), mock.patch.object(c.trial,'read_private',return_value=json.dumps({'intent':value,'entrypoint':['fixed','gate']}).encode()):
            self.assertEqual(c.native_start(Path('/unused'),{},{},'arch',intent,B),(n,value))
            with self.assertRaises(ValueError):c.native_start(Path('/unused'),{},{},'arch',intent,'database-exchange')
        with mock.patch.object(c.local,'module',return_value=n),mock.patch.object(c.trial,'read_private',return_value=json.dumps({'intent':value,'entrypoint':['other']}).encode()),self.assertRaises(ValueError):
            c.native_start(Path('/unused'),{},{},'arch',intent,B)

    def test_unconfirmed_cut_never_opens_qmp(self):
        intent, recovery, _, events = fixtures(); n=mock.Mock()
        n.read.return_value=({'result':{'status':'inconclusive'}},b'raw',events)
        ready=[{'event':'reboot_ready'}]
        with mock.patch.object(c.local,'assert_absent'),mock.patch.object(c,'native_start',return_value=(n,{'created_at':'2026-09-15T00:00:00Z'})),mock.patch.object(c,'read_guest',return_value=(recovery,b'raw',ready)),mock.patch.object(c,'QMP') as qmp,self.assertRaises(ValueError):
            c.reboot(Path('/unused'),{},{},'arch',intent,native_boundary=B)
        qmp.assert_not_called()

    def test_reset_saves_both_fault_proofs_and_rechecks_fresh_handoff(self):
        intent,recovery,data,events=fixtures();n=mock.Mock();n.read.return_value=(data,b'cut',events)
        n.guest.names.return_value={'worker':'update.service'}
        worker={'boot_id':'old'};recovery['reboot_proof']={'worker':worker,'checkpoint_sha256':'e'*64}
        ready=[{'event':'reboot_ready','worker':worker}];order=[];q=mock.Mock()
        q.reset.side_effect=lambda:order.append('reset')
        saved={}
        def save(root,node,name,raw):saved[name]=json.loads(raw) if name.endswith('.json') else raw;order.append(name);return {'path':name}
        with mock.patch.object(c.local,'assert_absent'),mock.patch.object(c,'native_start',return_value=(n,{'created_at':'2026-09-15T00:00:00Z'})),mock.patch.object(c,'read_guest',return_value=(recovery,b'raw',ready)),mock.patch.object(c,'qemu_identity',return_value={'pid':123}),mock.patch.object(c,'QMP',return_value=q),mock.patch.object(c,'save_collection',return_value={}),mock.patch.object(c.trial,'save',side_effect=save),mock.patch.object(c.lab,'guarded_script',return_value=SimpleNamespace(stdout='{}\n')):
            c.reboot(Path('/unused'),{},{'nodes':{'arch':{}}},'arch',intent,native_boundary=B)
        self.assertLess(order.index(c.REBOOT_ATTEMPT),order.index('reset'))
        self.assertIn('native_cut',saved[c.REBOOT_ATTEMPT]);q.reset.assert_called_once()
        q.close.assert_called_once()


class CompositeCallbackTests(unittest.TestCase):
    def setUp(self):
        old.ExchangeCallbackTests.setUp(self)
        self.args.boundary = B
        self.capture = mock.patch.object(g, 'capture_exchange_checkpoint', return_value={'files':{}}).start()
        self.callbacks = g.make_callbacks(self.args, {'identity': ID}, {'recovery_fault': FAULT},
            g.names(OP, B), self.native, {'start_ticks': '456'}, SimpleNamespace(proof_digest=old.digest),
            self.checkpoint, lambda event, **fields: self.events.append((event, fields)), -1)
        self.callbacks.revalidate = mock.Mock()
        self.helper = mock.Mock(); self.helper.arm.return_value = {'unit': {'MainPID':'123'}}
        self.modules = mock.patch.object(g, 'module', return_value=self.helper).start()

    def ready(self):
        entry = self.callbacks.exchange_entry(self.entry_trace)
        return self.callbacks.authorize_cut(self.exit_trace, entry)

    def test_handoff_finishes_and_revalidates_before_kill(self):
        proof=self.ready(); self.callbacks.perform_cut(proof)
        self.helper.arm.assert_called_once(); self.native.kill.assert_called_once()
        self.assertEqual([e for e,_ in self.events][-3:], ['recovery_fault_armed','kill_requested','kill_sent'])
        self.helper.cancel.assert_not_called()
        self.assertEqual(self.capture.call_args.kwargs, {'boundary':B})

    def test_unknown_handoff_never_kills(self):
        proof=self.ready();self.helper.arm.side_effect=TimeoutError('unknown')
        with self.assertRaises(TimeoutError):self.callbacks.perform_cut(proof)
        self.native.kill.assert_not_called()
        self.assertNotIn('kill_requested',[e for e,_ in self.events])

    def test_changed_checkpoint_after_handoff_cancels_without_kill(self):
        proof=self.ready()
        def arm(*a):
            self.checkpoint.inspect.side_effect=lambda *a:{**self.exit,'changed':True}
            return {'unit':{'MainPID':'123'}}
        self.helper.arm.side_effect=arm
        with self.assertRaises(ValueError):self.callbacks.perform_cut(proof)
        self.native.kill.assert_not_called();self.helper.cancel.assert_called_once()

    def test_unknown_kill_cancels_observer_and_keeps_unknown(self):
        proof=self.ready();self.native.kill.side_effect=TimeoutError('unknown')
        self.helper.cancel.return_value='unknown'
        with self.assertRaises(TimeoutError):self.callbacks.perform_cut(proof)
        self.assertEqual(self.callbacks.recovery_handoff_cleanup,'unknown')
        self.assertEqual(self.events[-1][0],'kill_requested')


if __name__ == '__main__': unittest.main()
