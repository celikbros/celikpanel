"""Negative admission checks only; no QEMU or product mutation occurs here."""
import copy
from contextlib import ExitStack, contextmanager
import importlib.util
import json
from pathlib import Path
from tempfile import TemporaryDirectory
from types import SimpleNamespace
from unittest.mock import Mock, patch
import unittest

SPEC = importlib.util.spec_from_file_location('tested_bound_reboot', Path(__file__).with_name('bound_worker_reboot.py'))
f = importlib.util.module_from_spec(SPEC); SPEC.loader.exec_module(f)


class BoundRebootTests(unittest.TestCase):
    def setUp(self):
        self.op = 'a' * 32
        self.identity = {'nonce': 'b' * 64, 'cell_id': 'bound-worker', 'node': 'debian13', 'vm_uuid': '11111111-1111-4111-8111-111111111111'}
        self.intent = {'schema': f.worker.SCHEMA, 'identity': self.identity, 'operation_id': self.op,
                       'baseline': {'version': 'v0.1.0-alpha.81', 'commit': 'c' * 40, 'agent_sha256': 'd' * 64, 'panel_sha256': 'e' * 64},
                       'target': {'version': 'v0.1.0-alpha.82', 'commit': '3' * 40, 'agent_sha256': 'f' * 64, 'panel_sha256': '0' * 64},
                       'recovery_fault': {'action': 'reboot', 'checkpoint': 'payload_restored'}}
        event = {'schema': 'celikpanel-release-recovery-update/v1', 'cell_id': self.identity['cell_id'], 'node': 'debian13',
                 'request_id': self.op, 'target_version': 'v0.1.0-alpha.82', 'target_commit': '3' * 40, 'target_archive_sha256': '1' * 64}
        request = {key: event[key] for key in ('request_id', 'target_version', 'target_commit', 'target_archive_sha256')}
        request.update(target_archive_size='1234', target_sequence='82', target_os='linux', target_arch='amd64', expected_current_version='v0.1.0-alpha.81', expected_current_commit='c' * 40)
        self.starts = [dict(event, event='reviewed', request=request, manifest_sha256='2' * 64), dict(event, event='accepted', status='queued')]
        self.intent_raw = f.encoded(self.intent)
        self.raw = b''.join(f.encoded(e) for e in self.starts)
        self.receipt = {'schema': f.START_SCHEMA, 'identity': self.identity, 'operation_id': self.op,
                        'intent_sha256': f.sha(self.intent_raw), 'mode': 'start', 'exit_code': 0, 'stdout_sha256': f.sha(self.raw)}
        snapshot = '20260921T180000Z-from-unknown-to-' + '3' * 40 + '-' + '3' * 32
        worker = {'running_executable_sha256': 'd' * 64, 'pid': 123, 'invocation_id': '4' * 32}
        exact = {'schema': f.native.hand.fault.INTENT_SCHEMA, 'identity': self.identity, 'operation_id': self.op,
                 'action': 'reboot', 'checkpoint': 'payload_restored', 'snapshot': snapshot,
                 'snapshot_manifest_sha256': '5' * 64, 'transaction_token_sha256': '6' * 64,
                 'runtime_manifest_sha256': '7' * 64, 'worker_executable_sha256': '8' * 64,
                 'worker_command': ['/bin/bash', '/usr/libexec/celikpanel/recovery-runtimes/v1/' + '7' * 64 + '/deploy/recovery/runtime-entry.sh']}
        self.recovery = {'intent': exact}
        bound = {'request_id': self.op, 'target_commit': '3' * 40, 'phase': 'running', 'terminal_proof': 'none', 'snapshot': snapshot,
                 'transaction_token_sha256': '6' * 64, 'binding_sha256': '9' * 64, 'observation_sha256': 'a' * 64, 'worker_state_sha256': 'b' * 64}
        handoff = {'schema': f.native.hand.SCHEMA, 'identity': self.identity, 'operation_id': self.op,
                   'updater': worker, 'intent': exact, 'intent_sha256': f.sha(f.native.hand.encoded(exact))}
        names = ['armed', 'worker_frozen', 'candidate_installed_checkpoint', 'recovery_fault_armed', 'kill_requested', 'kill_sent', 'released']
        self.events = [{'schema': f.kill.EVENT_SCHEMA, 'at': '2026-09-21T18:00:00Z',
                        'event': name, 'identity': self.identity, 'operation_id': self.op} for name in names]
        self.events[1]['worker'] = worker
        self.events[2].update(phase='active', worker=worker, snapshot=snapshot, manifest_sha256='5' * 64,
                              verified_files=150, installed_artifacts={'agent': 'f' * 64, 'panel': '0' * 64}, bound_worker=bound)
        self.events[3]['handoff'] = handoff
        for index in (4, 5): self.events[index]['worker'] = worker
        self.events[-1].update(kill_sent=True, checkpoint_verified=True, reason='exact-update-unit-killed')

    def test_exact_real_start_and_worker_cut_bound_to_recovery(self):
        self.assertEqual(f.validate_start(self.intent, self.intent_raw, self.receipt, self.raw)['archive_sha256'], '1' * 64)
        self.assertEqual(f.bind_cut(self.intent, self.events, self.recovery)['binding_sha256'], '9' * 64)

    def test_start_must_be_real_accepted_same_id_target_and_predecessor(self):
        for index, key, value in [(1, 'event', 'unconfirmed'), (1, 'request_id', 'f' * 32), (1, 'target_commit', 'f' * 40), (1, 'status', 'failed')]:
            events = copy.deepcopy(self.starts); events[index][key] = value
            raw = b''.join(f.encoded(e) for e in events)
            receipt = dict(self.receipt, stdout_sha256=f.sha(raw))
            with self.subTest(key=key), self.assertRaises(ValueError): f.validate_start(self.intent, self.intent_raw, receipt, raw)
        for key, value in [('expected_current_version', 'v0.1.0-alpha.75'), ('target_version', 'v0.1.0-alpha.83'), ('target_archive_size', '0'), ('target_archive_size', '2147483649')]:
            events = copy.deepcopy(self.starts); events[0]['request'][key] = value
            raw = b''.join(f.encoded(e) for e in events)
            with self.subTest(key=key), self.assertRaises(ValueError): f.validate_start(self.intent, self.intent_raw, dict(self.receipt, stdout_sha256=f.sha(raw)), raw)

    def test_resealed_or_unconfirmed_start_receipt_rejected(self):
        for key, value in [('intent_sha256', 'f' * 64), ('stdout_sha256', 'f' * 64), ('mode', 'preview'), ('exit_code', 1), ('exit_code', False)]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                f.validate_start(self.intent, self.intent_raw, dict(self.receipt, **{key: value}), self.raw)

    def test_signed_origin_archive_size_and_hash_must_match_actual_start(self):
        start = f.validate_start(self.intent, self.intent_raw, self.receipt, self.raw)
        origin = {'schema': 'celikpanel/worker-fixture-origin/v1', 'identity': {'schema': f.lab.SCHEMA, **self.identity},
                  'target': {'version': self.intent['target']['version'], 'commit': self.intent['target']['commit'],
                             'archive_sha256': start['archive_sha256'], 'archive_size': start['archive_size']},
                  'files': {'worker-origin-manifest': {'sha256': start['manifest_sha256']}}}
        f.validate_origin(self.intent, start, origin, self.identity)
        for key, bad in [('archive_size', '1235'), ('archive_sha256', 'f' * 64)]:
            changed = copy.deepcopy(origin); changed['target'][key] = bad
            with self.subTest(key=key), self.assertRaises(ValueError):
                f.validate_origin(self.intent, start, changed, self.identity)

    def test_cut_requires_full_confirmed_sequence_and_unchanged_binding(self):
        for changes in ('missing-kill', 'false-release', 'other-binding', 'other-snapshot', 'wrong-binary'):
            events = copy.deepcopy(self.events)
            if changes == 'missing-kill': del events[5]
            elif changes == 'false-release': events[-1]['kill_sent'] = False
            elif changes == 'other-binding': events[2]['bound_worker']['transaction_token_sha256'] = 'f' * 64
            elif changes == 'other-snapshot': events[2]['snapshot'] += 'x'
            else: events[2]['installed_artifacts']['agent'] = 'e' * 64
            with self.subTest(change=changes), self.assertRaises(ValueError): f.bind_cut(self.intent, events, self.recovery)

    @contextmanager
    def reboot_environment(self, streams):
        with TemporaryDirectory() as temporary, ExitStack() as stack:
            root = Path(temporary)
            record, plan = {'fixture': 'registered'}, {'nodes': {'debian13': {'fixture': 'registered'}}}
            stack.enter_context(patch.object(f.lab, 'checked_root', return_value=root))
            stack.enter_context(patch.object(f.lab, 'load', return_value=(record, plan)))
            stack.enter_context(patch.object(f.lab, 'process_guard'))
            stack.enter_context(patch.object(f, 'load', return_value=(self.intent, {})))
            reads = stack.enter_context(patch.object(f.kill.shared, 'read_guest', side_effect=[({}, raw) for raw in streams]))
            subordinate = stack.enter_context(patch.object(f.native, 'read_guest', side_effect=AssertionError('no subordinate handoff is expected')))
            sleep = stack.enter_context(patch.object(f.time, 'sleep'))
            qmp = stack.enter_context(patch.object(f.native, 'QMP'))
            save = stack.enter_context(patch.object(f.trial, 'save'))
            yield root, record, plan, reads, subordinate, sleep
            qmp.assert_not_called()
            save.assert_not_called()
            self.assertEqual(list(root.rglob('*')), [])

    def exited_worker_events(self):
        # Use the real guest cut loop's early-exit path rather than inventing a
        # released event which may differ from the on-guest fixture protocol.
        native = Mock()
        native.observe.side_effect = [
            {'worker': {'ActiveState': 'active'}, 'transaction': None},
            {'worker': {'ActiveState': 'failed'}, 'transaction': None}]
        events = []
        def emit(event, **fields):
            events.append({'schema': f.kill.EVENT_SCHEMA, 'identity': self.identity,
                           'at': '2026-09-21T18:00:00Z', 'event': event, **fields})
        args = SimpleNamespace(operation_id=self.op, recovery_action='reboot')
        result = f.worker.base.run_kill(args, emit, native, pause=lambda _: None)
        self.assertEqual(result, 2)
        self.assertEqual(events[-1]['reason'], 'update-unit-exited-before-checkpoint')
        native.kill.assert_not_called()
        return events

    def test_real_worker_exit_before_checkpoint_stops_without_handoff_or_reset(self):
        events = self.exited_worker_events()
        raw = b''.join(f.encoded(event) for event in events)
        with self.reboot_environment([raw]) as (root, record, plan, reads, subordinate, sleep):
            with self.assertRaisesRegex(ValueError, 'cut is not conclusively finished'):
                f.reboot(root, record, plan, 'debian13', self.op, execute=True)
            reads.assert_called_once_with(root, record, plan, 'debian13', self.op, 'update-kill')
            subordinate.assert_not_called()
            sleep.assert_not_called()

    def test_pending_cut_then_failed_release_stops_at_first_terminal_read(self):
        events = self.exited_worker_events()
        streams = [f.encoded(events[0]), b''.join(f.encoded(event) for event in events)]
        with self.reboot_environment(streams) as (root, record, plan, reads, subordinate, sleep):
            with self.assertRaisesRegex(ValueError, 'cut is not conclusively finished'):
                f.reboot(root, record, plan, 'debian13', self.op, execute=True)
            self.assertEqual(reads.call_count, 2)
            sleep.assert_called_once_with(.1)
            subordinate.assert_not_called()

    def test_unconfirmed_or_other_operation_release_never_polls_handoff(self):
        cases = []
        for key, value in [('kill_sent', False), ('checkpoint_verified', False), ('reason', 'observation-error:ProbeError')]:
            events = copy.deepcopy(self.events); events[-1][key] = value
            cases.append(b''.join(f.encoded(event) for event in events))
        events = copy.deepcopy(self.events); events[-1]['operation_id'] = 'f' * 32
        cases.append(b''.join(f.encoded(event) for event in events))
        events = copy.deepcopy(self.events); events[-1]['identity']['nonce'] = 'e' * 64
        cases.append(b''.join(f.encoded(event) for event in events))
        cases.append(b''.join(f.encoded(event) for event in self.events)[:-1])
        for raw in cases:
            with self.subTest(raw_sha256=f.sha(raw)), self.reboot_environment([raw]) as (root, record, plan, reads, subordinate, sleep):
                with self.assertRaises(ValueError): f.reboot(root, record, plan, 'debian13', self.op, execute=True)
                self.assertEqual(reads.call_count, 1)
                subordinate.assert_not_called()
                sleep.assert_not_called()

    def test_confirmed_cut_can_wait_for_handoff_but_never_reset_after_recovery_release(self):
        raw = b''.join(f.encoded(event) for event in self.events)
        with self.reboot_environment([raw, raw]) as (root, record, plan, reads, subordinate, sleep):
            subordinate.side_effect = [f.subprocess.CalledProcessError(1, 'fixture-read'),
                                       (self.recovery, b'', [{'event': 'released'}])]
            with self.assertRaisesRegex(ValueError, 'recovery reset checkpoint missed'):
                f.reboot(root, record, plan, 'debian13', self.op, execute=True)
            self.assertEqual(reads.call_count, 2)
            self.assertEqual(subordinate.call_count, 2)
            sleep.assert_called_once_with(.1)

    def test_no_guest_or_qmp_access_without_execute(self):
        with patch.object(f.lab, 'checked_root') as check, patch.object(f.native, 'QMP') as qmp:
            with self.assertRaises(ValueError): f.reboot(Path('/unused'), {}, {}, 'debian13', self.op)
            check.assert_not_called(); qmp.assert_not_called()

    def test_exact_operation_names_prevent_path_injection(self):
        with self.assertRaises(ValueError): f.names('../another')
        self.assertIn(self.op, f.names(self.op)['attempt'])


if __name__ == '__main__': unittest.main()
