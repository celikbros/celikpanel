"""Exchange controller boundaries only: no tracing, guest or systemd mutation."""
import base64
import copy
import hashlib
import importlib.util
import json
import shutil
import subprocess
import tempfile
from pathlib import Path
import sys
from types import SimpleNamespace
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('database_exchange_controller_tests', HERE / 'native_database_exchange_trial.py')
wrapper = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = wrapper
spec.loader.exec_module(wrapper)
h = wrapper.controller
g = h.guest
BOUNDARY = 'database-exchange'
OPERATION = 'a' * 32
IDENTITY = {'nonce': 'b' * 64, 'vm_uuid': 'fixture-uuid', 'cell_id': 'fixture-cell', 'node': 'arch'}


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


class ExchangeControllerTests(unittest.TestCase):
    def test_fixed_wrapper_requires_execute_before_guest_guard(self):
        for mode in ('prepare', 'arm', 'start'):
            with self.subTest(mode=mode), mock.patch.object(h.lab, 'checked_root') as guard:
                with self.assertRaises(SystemExit) as stopped:
                    wrapper.main(['--work-root', '/unused', '--node', 'arch', '--mode', mode])
                self.assertEqual(stopped.exception.code, 2)
                guard.assert_not_called()

    def test_flat_guest_imports_under_isolated_python_without_path_overrides(self):
        with tempfile.TemporaryDirectory(prefix='cp-exchange-flat-import-') as directory:
            target = Path(directory)
            for source in h.assets_for(BOUNDARY):
                shutil.copyfile(HERE / source, target / Path(source).name)
            script = """import importlib.util,sys
from pathlib import Path
path=Path(sys.argv[1])
spec=importlib.util.spec_from_file_location('flat_guest',path/'guest_native_wal_trial.py')
guest=importlib.util.module_from_spec(spec);sys.modules[spec.name]=guest;spec.loader.exec_module(guest)
tracer=guest.module('flat_exchange_trace','exchange_trace.py')
checkpoint=guest.module('flat_exchange_checkpoint','guest_exchange_checkpoint.py')
assert callable(tracer.trace_database_publication)
assert callable(checkpoint.inspect_entry) and callable(checkpoint.inspect)
assert len(guest.profile('database-exchange')['helpers'])==19
print('flat isolated imports verified')
"""
            result = subprocess.run([sys.executable, '-I', '-c', script, str(target)], capture_output=True, text=True, timeout=15)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, 'flat isolated imports verified\n')

    def test_wrapper_always_selects_exchange(self):
        with mock.patch.object(h, 'main') as main:
            wrapper.main(['--mode', 'collect'])
        main.assert_called_once_with(['--mode', 'collect'], boundary=BOUNDARY)

    def test_profiles_share_only_the_exact_registered_worker(self):
        old, new = g.names(OPERATION), g.names(OPERATION, BOUNDARY)
        self.assertEqual(old['worker'], new['worker'])
        for key in old.keys() - {'worker'}:
            self.assertNotEqual(old[key], new[key])
        self.assertFalse(set(h.host_names()) & set(h.host_names(BOUNDARY)))
        for key in ('schema', 'gate_schema', 'release_schema', 'fault'):
            self.assertNotEqual(g.profile()[key], g.profile(BOUNDARY)[key])
        for invalid in ('exchange', '../wal', '', None):
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                g.names(OPERATION, invalid)

    def test_exact_exchange_helper_inventory_cannot_use_wal_subset(self):
        assets = {Path(name).name: 'a' * 64 for name in h.assets_for(BOUNDARY)}
        self.assertEqual(len(assets), 19)
        self.assertEqual(g.validate_helpers(assets, BOUNDARY), assets)
        for missing in assets:
            changed = dict(assets)
            del changed[missing]
            with self.subTest(missing=missing), self.assertRaises(ValueError):
                g.validate_helpers(changed, BOUNDARY)
        with self.assertRaises(ValueError):
            g.validate_helpers(assets)
        with self.assertRaises(ValueError):
            g.validate_helpers({Path(name).name: 'a' * 64 for name in h.ASSETS}, BOUNDARY)
        for invalid in ({}, {**assets, 'other.py': 'a' * 64}, {**assets, 'exchange_trace.py': 'X' * 64}):
            with self.assertRaises(ValueError):
                g.validate_helpers(invalid, BOUNDARY)

    def test_guest_argv_has_only_fixed_boundary_and_exact_identity(self):
        value = {'operation_id': OPERATION, 'identity': IDENTITY}
        command = h.argv(value, 'gate', boundary=BOUNDARY)
        self.assertEqual(command[-2:], ['--boundary', BOUNDARY])
        self.assertNotIn('--boundary', h.argv(value, 'gate'))
        for invalid in ('--pid', '--command', '--database', '--hostname'):
            self.assertNotIn(invalid, command)

    def test_exchange_events_require_both_entry_and_exit_proofs(self):
        sequence = ['armed', 'gate_released', 'database_exchange_entry_verified',
                    'database_exchange_checkpoint_verified', 'kill_requested', 'kill_sent']
        for n in range(len(sequence) + 1):
            self.assertEqual(h.validate_event_order(sequence[:n], boundary=BOUNDARY), sequence[:n])
            if n < len(sequence):
                with self.assertRaises(ValueError):
                    h.validate_event_order(sequence[:n], {'status': 'cut-sent'}, boundary=BOUNDARY)
        self.assertEqual(h.validate_event_order(sequence + ['trace_finished'], {'status': 'cut-sent'}, boundary=BOUNDARY), sequence + ['trace_finished'])
        for n in range(len(sequence)):
            with self.subTest(missing=n), self.assertRaises(ValueError):
                h.validate_event_order(sequence[:n] + sequence[n + 1:], {'status': 'cut-sent'}, boundary=BOUNDARY)
        with self.assertRaises(ValueError):
            h.validate_event_order(['armed', 'gate_released', 'wal_checkpoint_verified'], boundary=BOUNDARY)
        with self.assertRaises(ValueError):
            h.validate_event_order(sequence)

    def test_read_refuses_foreign_profile_or_partial_result(self):
        value = {'operation_id': OPERATION, 'identity': IDENTITY}
        event = {'schema': g.SCHEMA, 'identity': IDENTITY, 'operation_id': OPERATION, 'event': 'armed'}
        data = {'schema': g.profile(BOUNDARY)['schema'], 'identity': IDENTITY, 'operation_id': OPERATION,
                'events_base64': base64.b64encode((json.dumps(event) + '\n').encode()).decode(), 'result': None}
        with mock.patch.object(h.lab, 'guarded_script', return_value=SimpleNamespace(stdout=json.dumps(data))):
            with self.assertRaises(ValueError):
                h.read(None, None, None, 'arch', value, boundary=BOUNDARY)
        event['schema'] = data['schema']
        data['events_base64'] = base64.b64encode((json.dumps(event) + '\n').encode()).decode()
        data['result'] = {'status': 'cut-sent'}
        with mock.patch.object(h.lab, 'guarded_script', return_value=SimpleNamespace(stdout=json.dumps(data))):
            with self.assertRaises(ValueError):
                h.read(None, None, None, 'arch', value, boundary=BOUNDARY)

    def test_start_persists_once_before_uncertain_transport(self):
        value = {'operation_id': OPERATION, 'identity': IDENTITY}
        paths = g.names(OPERATION, BOUNDARY)
        data = {'transaction': None, 'states': {paths['tracer']: {'ActiveState': 'active'}, paths['worker']: {'LoadState': 'not-found'}}}
        order = []
        with mock.patch.object(h.local, 'assert_absent'), mock.patch.object(h.local.trial, 'read_private', return_value=json.dumps(value).encode()), mock.patch.object(h, 'read', return_value=(data, b'armed', [{'event': 'armed'}])), mock.patch.object(h.local.trial, 'save', side_effect=lambda *a: order.append(('save', a[2]))), mock.patch.object(h, 'launch', side_effect=lambda *a, **kw: (_ for _ in ()).throw(TimeoutError('unknown'))):
            with self.assertRaises(TimeoutError):
                h.start(Path('/unused'), {}, {}, 'arch', value, boundary=BOUNDARY)
        self.assertEqual(order, [('save', h.host_names(BOUNDARY)[1])])


class ExchangeCallbackTests(unittest.TestCase):
    def setUp(self):
        self.args = SimpleNamespace(boundary=BOUNDARY, operation_id=OPERATION)
        self.native = mock.Mock()
        self.native.worker_identity.return_value = {'pid': 123, 'start_ticks': '456'}
        self.entry_trace = {'publisher': {'pid': 234, 'tid': 235}, 'exchange': {'stage': 'entry'}}
        self.exit_trace = {'publisher': {'pid': 234, 'tid': 235}, 'exchange': {'stage': 'exit', 'result': 0}}
        self.entry = {'status': 'verified', 'operation_id': OPERATION, 'trace_sha256': digest(self.entry_trace), 'database': {'unchanged': True}}
        self.exit = {'status': 'verified', 'operation_id': OPERATION, 'trace_sha256': digest(self.exit_trace),
                     'entry_proof_sha256': digest(self.entry), 'database': {'pair': 'after-before'}}
        self.checkpoint = mock.Mock()
        self.checkpoint.inspect_entry.side_effect = lambda *a: copy.deepcopy(self.entry)
        self.checkpoint.inspect.side_effect = lambda *a: copy.deepcopy(self.exit)
        self.events = []
        self.callbacks = g.make_callbacks(self.args, {'identity': IDENTITY}, {}, g.names(OPERATION, BOUNDARY), self.native,
                                         {'start_ticks': '456'}, SimpleNamespace(proof_digest=digest), self.checkpoint,
                                         lambda event, **fields: self.events.append((event, fields)), -1)
        self.callbacks.revalidate = mock.Mock()
        self.capture = mock.patch.object(g, 'capture_exchange_checkpoint', return_value={'files': {'canonical.db': 'copied', 'retained-before.db': 'copied'}}).start()
        self.addCleanup(mock.patch.stopall)

    def ready(self):
        entry = self.callbacks.exchange_entry(self.entry_trace)
        before = copy.deepcopy(entry)
        proof = self.callbacks.authorize_cut(self.exit_trace, entry)
        self.assertEqual(entry, before)
        return proof

    def test_entry_and_exit_bind_exact_same_proof_then_reinspect_before_kill(self):
        proof = self.ready()
        self.assertEqual(proof['entry_proof_sha256'], digest(self.entry))
        receipt = self.callbacks.perform_cut(proof)
        self.assertEqual(receipt['entry_proof_sha256'], digest(self.entry))
        self.assertEqual(self.checkpoint.inspect.call_count, 2)
        self.native.kill.assert_called_once_with()
        self.assertEqual([event for event, _ in self.events], ['database_exchange_entry_verified', 'database_exchange_checkpoint_verified', 'kill_requested', 'kill_sent'])
        self.assertNotIn('capture', self.callbacks.cut_base)

    def test_exit_without_entry_or_with_mutated_entry_never_captures(self):
        with self.assertRaises(ValueError):
            self.callbacks.authorize_cut(self.exit_trace, self.entry)
        entry = self.callbacks.exchange_entry(self.entry_trace)
        entry['database']['unchanged'] = False
        with self.assertRaises(ValueError):
            self.callbacks.authorize_cut(self.exit_trace, entry)
        self.capture.assert_not_called()
        self.native.kill.assert_not_called()

    def test_bad_exit_binding_is_refused_before_capture(self):
        entry = self.callbacks.exchange_entry(self.entry_trace)
        for key in ('operation_id', 'trace_sha256', 'entry_proof_sha256'):
            bad = {**self.exit, key: 'changed'}
            self.checkpoint.inspect.side_effect = lambda *a: copy.deepcopy(bad)
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.callbacks.authorize_cut(self.exit_trace, entry)
        self.capture.assert_not_called()
        self.native.kill.assert_not_called()

    def test_changed_final_pair_never_emits_kill_request(self):
        proof = self.ready()
        self.checkpoint.inspect.side_effect = lambda *a: {**self.exit, 'database': {'pair': 'owner-changed'}}
        with self.assertRaises(ValueError):
            self.callbacks.perform_cut(proof)
        self.native.kill.assert_not_called()
        self.assertNotIn('kill_requested', [event for event, _ in self.events])

    def test_capture_failure_does_not_become_cut_permission(self):
        entry = self.callbacks.exchange_entry(self.entry_trace)
        self.capture.side_effect = ValueError('source changed')
        with self.assertRaises(ValueError):
            self.callbacks.authorize_cut(self.exit_trace, entry)
        self.native.kill.assert_not_called()
        self.assertEqual([event for event, _ in self.events], ['database_exchange_entry_verified'])

    def test_unknown_kill_keeps_requested_without_sent(self):
        proof = self.ready()
        self.native.kill.side_effect = TimeoutError('transport unknown')
        with self.assertRaises(TimeoutError):
            self.callbacks.perform_cut(proof)
        self.assertEqual([event for event, _ in self.events][-1], 'kill_requested')
        self.assertNotIn('kill_sent', [event for event, _ in self.events])

    def test_wal_callback_still_uses_prior_bytes_and_wal_capture(self):
        self.args.boundary = 'wal'
        callbacks = g.make_callbacks(self.args, {'identity': IDENTITY}, {}, g.names(OPERATION), self.native,
                                     {'start_ticks': '456'}, SimpleNamespace(proof_digest=digest), self.checkpoint,
                                     lambda *a, **kw: None, -1)
        callbacks.revalidate = mock.Mock()
        self.checkpoint.inspect.side_effect = lambda *a, **kw: {'status': 'verified', 'database': {'wal': True}}
        with mock.patch.object(g, 'capture_checkpoint', return_value={'wal': 'copied'}) as capture:
            proof = callbacks.authorize_cut({'write': 'wal'}, b'prior-wal')
            callbacks.perform_cut(proof)
        self.assertEqual(proof['prior_wal_sha256'], hashlib.sha256(b'prior-wal').hexdigest())
        self.assertEqual(capture.call_count, 1)
        self.assertEqual(self.checkpoint.inspect.call_args.kwargs, {'prior_wal': b'prior-wal'})
        self.capture.assert_not_called()
        with self.assertRaises(ValueError):
            callbacks.exchange_entry(self.entry_trace)


if __name__ == '__main__':
    unittest.main()
