"""Offline guard tests; no guest service, updater or recovery is started."""
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location('tested_forward_completion', Path(__file__).with_name('guest_forward_completion_fault.py'))
s = importlib.util.module_from_spec(SPEC); sys.modules[SPEC.name] = s; SPEC.loader.exec_module(s)


class FakeNative:
    def __init__(self):
        self.state = {'worker': {'ActiveState': 'active'}, 'transaction': {'phase': 'completion.pending',
                      'operation': 'update', 'snapshot': 'fixture'}, 'recovery': {'ActiveState': 'inactive'},
                      'update_lock_exclusive': True}
        self.identity = {'unit': 'exact', 'pid': 123, 'start_ticks': '44', 'invocation_id': 'a' * 32}
        self.actions = []; self.error = None
    def observe(self): return copy.deepcopy(self.state)
    def worker_identity(self): return self.identity.copy()
    def maybe_hold_port(self, state):
        if 'port' in self.actions: return False
        self.actions.append('port'); return True
    def release_port(self): self.actions.append('release-port')
    def hint(self, identity):
        if self.error == 'hint': self.state['worker']['ActiveState'] = 'inactive'; return None
        return {'message': s.CHECKPOINT, 'authority': 'informational-journal-hint-only'}
    def freeze(self):
        self.actions.append('freeze')
        if self.error == 'freeze': raise OSError()
        if self.error == 'phase': self.state['transaction']['phase'] = 'scheduler-restore.pending'
        if self.error == 'lock': self.state['update_lock_exclusive'] = False
    def revalidate(self, identity):
        if self.error == 'identity': raise s.kill.MissedCheckpoint('identity-changed')
    def completion_proof(self, snapshot, identity, tick):
        self.actions.append('proof'); tick()
        if self.error in ('db', 'snapshot', 'material', 'runtime', 'coordinator'):
            raise s.kill.MissedCheckpoint(self.error + '-proof-failed')
        return {'snapshot': snapshot, 'phase': 'completion.pending', 'verified_files': 129,
                'manifest_sha256': 'c' * 64, 'database_readonly_checker': {'exit_code': 0}}
    def candidate_data_fault(self, identity, proof, tick):
        self.actions.append('data-fault')
        if self.error == 'data': raise OSError()
        return {'status': 'applied'}
    def verify_after_fault(self, proof, identity, tick):
        self.actions.append('after-proof')
        if self.error == 'after': raise s.kill.MissedCheckpoint('after-data-proof-failed')
        return {'agent': {'main_pid': 42, 'running_sha256': 'd' * 64}}
    def kill(self):
        self.actions.append('kill')
        if self.error == 'kill': raise OSError()
    def thaw(self): self.actions.append('thaw'); return 0


class FaultTests(unittest.TestCase):
    def run_case(self, native, **kwargs):
        events = []
        result = s.run_fault(SimpleNamespace(operation_id='a' * 32),
                             lambda event, **fields: events.append(dict(event=event, **fields)),
                             native, **kwargs)
        return result, events

    def test_proof_then_fixed_data_loss_then_only_updater_kill(self):
        native = FakeNative(); result, events = self.run_case(native)
        self.assertEqual(result, 0)
        self.assertEqual(native.actions, ['port', 'freeze', 'proof', 'data-fault', 'after-proof',
                                         'kill', 'release-port', 'thaw'])
        self.assertEqual([e['event'] for e in events], ['armed', 'completion_port_held', 'worker_frozen',
                         'completion_database_verified_checkpoint', 'candidate_data_fault_applied',
                         'kill_requested', 'kill_sent', 'released'])
        self.assertEqual(events[-2]['worker'], native.identity)
        self.assertTrue(events[-1]['port_released'])

    def test_old_hint_or_absent_hint_never_freezes_or_changes_data(self):
        native = FakeNative(); native.error = 'hint'
        result, events = self.run_case(native, pause=lambda _: None)
        self.assertEqual(result, 2)
        self.assertEqual(native.actions, ['port', 'release-port'])
        self.assertFalse(events[-1]['kill_sent'])

    def test_every_proof_refusal_releases_port_and_freeze_without_kill(self):
        for reason in ('freeze', 'phase', 'lock', 'identity', 'db', 'snapshot', 'material', 'runtime', 'coordinator'):
            with self.subTest(reason=reason):
                native = FakeNative(); native.error = reason
                result, events = self.run_case(native)
                self.assertEqual(result, 2); self.assertNotIn('kill', native.actions)
                self.assertNotIn('data-fault', native.actions)
                self.assertEqual(native.actions[-2:], ['release-port', 'thaw'])

    def test_partial_data_fault_and_changed_final_proof_never_send_kill(self):
        for reason in ('data', 'after'):
            with self.subTest(reason=reason):
                native = FakeNative(); native.error = reason
                result, events = self.run_case(native)
                self.assertEqual(result, 2); self.assertNotIn('kill', native.actions)
                self.assertTrue(events[-1]['port_released'])
                self.assertFalse(events[-1]['kill_sent'])

    def test_ambiguous_kill_never_becomes_confirmed(self):
        native = FakeNative(); native.error = 'kill'
        result, events = self.run_case(native)
        self.assertEqual(result, 2)
        self.assertNotIn('kill_sent', [e['event'] for e in events])
        self.assertEqual(native.actions[-2:], ['release-port', 'thaw'])

    def test_signal_before_and_after_freeze_always_releases(self):
        for late in (False, True):
            native = FakeNative()
            result, events = self.run_case(native, interrupted=lambda: not late or 'freeze' in native.actions)
            self.assertEqual(result, 2); self.assertNotIn('kill', native.actions)
            self.assertIn('release-port', native.actions)
            if late: self.assertEqual(native.actions[-1], 'thaw')

    def test_port_cleanup_failure_still_thaws_and_is_not_confirmed(self):
        native = FakeNative()
        def fail_close():
            native.actions.append('release-port')
            raise OSError()
        native.release_port = fail_close
        result, events = self.run_case(native)
        self.assertEqual(result, 0)
        self.assertEqual(native.actions[-1], 'thaw')
        self.assertFalse(events[-1]['port_released'])

    def test_strict_completion_phase_and_exclusive_holder(self):
        state = FakeNative().state
        self.assertEqual(s.completion_snapshot(state), 'fixture')
        for key, value in (('phase', 'active'), ('phase', 'scheduler-restore.pending'), ('operation', 'rollback')):
            changed = copy.deepcopy(state); changed['transaction'][key] = value
            with self.subTest(key=key, value=value), self.assertRaises(s.kill.MissedCheckpoint): s.completion_snapshot(changed)
        for proof in (None, False, 'true'):
            changed = copy.deepcopy(state); changed['update_lock_exclusive'] = proof
            with self.assertRaises(s.kill.MissedCheckpoint): s.completion_snapshot(changed)
        with self.assertRaises(s.kill.MissedCheckpoint): s.completion_snapshot(state, 'different')


class JournalTests(unittest.TestCase):
    def setUp(self):
        self.identity = {'unit': 'celikpanel-lab-local-update-' + 'a' * 32 + '.service',
                         'invocation_id': 'b' * 32, 'boot_id': 'aabbccdd-1234-1234-1234-aabbccddeeff'}
        self.record = {'MESSAGE': s.CHECKPOINT, '_SYSTEMD_UNIT': self.identity['unit'],
                       '_SYSTEMD_INVOCATION_ID': self.identity['invocation_id'],
                       '_BOOT_ID': self.identity['boot_id'].replace('-', ''), '_PID': '44',
                       '__REALTIME_TIMESTAMP': '1789422248208973'}
    def raw(self, value): return (json.dumps(value) + '\n').encode()

    def test_exact_native_journal_string_and_bytes_are_only_hints(self):
        for message in (s.CHECKPOINT, list(s.CHECKPOINT.encode())):
            proof = s.checkpoint_hint(self.raw({**self.record, 'MESSAGE': message}), self.identity)
            self.assertEqual(proof['authority'], 'informational-journal-hint-only')
            self.assertEqual(proof['pid'], 44)

    def test_wrong_invocation_boot_unit_message_and_untrusted_fields_ignored(self):
        for key, value in (('_SYSTEMD_INVOCATION_ID', 'c' * 32), ('_BOOT_ID', 'c' * 32),
                           ('_SYSTEMD_UNIT', 'celikpanel-agent.service'), ('MESSAGE', 'error: ' + s.CHECKPOINT),
                           ('_PID', ''), ('__REALTIME_TIMESTAMP', '')):
            with self.subTest(key=key):
                self.assertIsNone(s.checkpoint_hint(self.raw({**self.record, key: value}), self.identity))

    def test_duplicate_and_oversized_hint_refused(self):
        with self.assertRaises(s.kill.MissedCheckpoint):
            s.checkpoint_hint(self.raw(self.record) * 2, self.identity)
        with self.assertRaises(s.kill.MissedCheckpoint):
            s.checkpoint_hint(b' ' * 262145, self.identity)

    def test_event_parser_requires_distinct_boundary_and_full_order(self):
        _, events = FaultTests().run_case(FakeNative())
        for event in events: event.update(schema=s.kill.SCHEMA, identity=self.identity, at='2026-09-15T00:00:00Z')
        encode = lambda values: b''.join(self.raw(v) for v in values)
        self.assertEqual(len(s.validate_events(encode(events), self.identity, 'a' * 32)), 8)
        for values in (events[:3] + events[4:], events[:4] + [events[3]] + events[4:], events[1:]):
            with self.assertRaises(ValueError): s.validate_events(encode(values), self.identity, 'a' * 32)
        events[0]['boundary'] = 'candidate-installed'
        with self.assertRaises(ValueError): s.validate_events(encode(events), self.identity, 'a' * 32)


class PortTests(unittest.TestCase):
    def setUp(self):
        self.native = object.__new__(s.CompletionNative); self.native.socket = None
        self.native.native = SimpleNamespace(worker_identity=lambda: {'pid': 42}, properties=lambda: {},
                                            owns_transaction_lock=lambda _: True)
        self.state = FakeNative().state
        self.panel = mock.patch.object(s.shared, 'unit_state', return_value={'MainPID': '0', 'ActiveState': 'inactive'})
        self.panel.start(); self.addCleanup(self.panel.stop)
        self.patch = mock.patch.object(s.socket, 'socket')
        self.binder = self.patch.start(); self.addCleanup(self.patch.stop)

    def test_port_admission_uses_only_free_loopback_after_exact_worker_lock(self):
        self.assertTrue(self.native.maybe_hold_port(self.state))
        listener = self.binder.return_value
        listener.bind.assert_called_once_with(('127.0.0.1', 2083))
        listener.setsockopt.assert_called_once_with(s.socket.SOL_SOCKET, s.socket.SO_REUSEADDR, 1)
        self.native.release_port(); listener.close.assert_called_once()

    def test_foreign_or_unproved_worker_never_binds_port(self):
        self.state['update_lock_exclusive'] = False
        self.assertFalse(self.native.maybe_hold_port(self.state)); self.binder.assert_not_called()
        self.state['update_lock_exclusive'] = True
        self.native.native.worker_identity = mock.Mock(side_effect=s.kill.MissedCheckpoint('wrong-worker'))
        with self.assertRaises(s.kill.MissedCheckpoint): self.native.maybe_hold_port(self.state)
        self.binder.assert_not_called()

    def test_identity_change_closes_just_opened_listener(self):
        self.native.native.worker_identity = mock.Mock(side_effect=[{'pid': 42}, {'pid': 43}])
        with self.assertRaises(s.kill.MissedCheckpoint): self.native.maybe_hold_port(self.state)
        self.binder.return_value.close.assert_called_once()
        self.assertIsNone(self.native.socket)

    def test_post_bind_observation_error_also_closes_listener(self):
        self.native.native.worker_identity = mock.Mock(side_effect=[{'pid': 42}, OSError()])
        with self.assertRaises(OSError): self.native.maybe_hold_port(self.state)
        self.binder.return_value.close.assert_called_once()
        self.assertIsNone(self.native.socket)

    def test_occupied_port_waits_without_reuseport_or_foreign_signal(self):
        self.binder.return_value.bind.side_effect = OSError(s.errno.EADDRINUSE, 'already held')
        self.assertFalse(self.native.maybe_hold_port(self.state))
        self.binder.return_value.close.assert_called_once()
        self.assertIsNone(self.native.socket)


class NativeOracleTests(unittest.TestCase):
    def setUp(self):
        self.native = object.__new__(s.CompletionNative)
        self.native.plan = {'candidate': {'manifest_sha256': 'b' * 64}}
        self.native.native = SimpleNamespace(full_proof=lambda snapshot, tick: {'snapshot': snapshot, 'manifest_sha256': 'c' * 64},
                                             revalidate=lambda identity: None)
        self.native.coordinators = lambda tick: {'agent': {'state': 'inactive', 'main_pid': 0}}
        self.transaction = {'transaction_phase': 'completion.pending', 'transaction_operation': 'update',
                            'snapshot': 'fixture', 'transaction_token_sha256': 'a' * 64}
        self.material = {'schema': 'celikpanel/recovery-material/v2', 'snapshot_manifest_sha256': 'c' * 64}
        for target, name, value in ((s.data.fault, 'read_transaction', self.transaction),
                                    (s.data.hand, 'selection', ('d' * 64, b'selection')),
                                    (s.data.fault, 'verify_runtime', {'runtime_manifest_sha256': 'd' * 64}),
                                    (s.shared, 'digest_file', 'e' * 64), (s, 'material_proof', self.material)):
            patch = mock.patch.object(target, name, return_value=value); patch.start(); self.addCleanup(patch.stop)
        self.run = mock.patch.object(s.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout=b'',
                                      stderr=b'')); self.command = self.run.start(); self.addCleanup(self.run.stop)

    def test_exact_readonly_schema_checker_without_rpc_or_inherited_authority(self):
        result = self.native.completion_proof('fixture', {'pid': 42}, lambda: None)
        args, kwargs = self.command.call_args
        self.assertEqual(args[0], [str(s.data.fault.RUNTIME_ROOT / ('d' * 64) / 'bin/panel-checker'),
                                  '--check-completed-update-database-wal-aware'])
        self.assertEqual(kwargs['env'], s.ENV)
        self.assertNotIn('pass_fds', kwargs)
        self.assertNotIn('CELIKPANEL_RELEASE_TRANSACTION_TOKEN', kwargs['env'])
        self.assertEqual(result['database_readonly_checker']['exit_code'], 0)

    def test_v1_material_cannot_admit_new_native_completion(self):
        self.material['schema'] = 'celikpanel/recovery-material/v1'
        with self.assertRaises(s.kill.MissedCheckpoint):
            self.native.completion_proof('fixture', {}, lambda: None)
        self.command.assert_not_called()

    def test_checker_failure_preserves_unknown_without_recovery_or_migration_command(self):
        self.command.return_value = SimpleNamespace(returncode=1, stdout=b'', stderr=b'private failure')
        with self.assertRaisesRegex(s.kill.MissedCheckpoint, 'readonly-database-check-failed'):
            self.native.completion_proof('fixture', {}, lambda: None)
        self.assertEqual(self.command.call_count, 1)


@unittest.skipUnless(sys.platform == 'linux' and getattr(os, 'geteuid', lambda: -1)() == 0,
                     'root-owned Linux filesystem required')
class MaterialTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='completion-material-', dir='/root')
        self.addCleanup(self.temp.cleanup); self.base = Path(self.temp.name)
        self.snapshot = 'fixture'; self.token = 'a' * 64; self.candidate = 'b' * 64
        self.root = self.base / hashlib.sha256(self.snapshot.encode()).hexdigest()
        self.root.mkdir(mode=0o700); (self.root / 'data').mkdir(mode=0o700)
        (self.root / 'data/libexec').mkdir(mode=0o700)
        payload = self.root / 'data/libexec/get.sh'; payload.write_bytes(b'known updater'); payload.chmod(0o600)
        self.hash = hashlib.sha256(payload.read_bytes()).hexdigest()
        sums = (self.hash + '  ./libexec/get.sh\n').encode()
        (self.root / 'data/SHA256SUMS').write_bytes(sums); (self.root / 'data/SHA256SUMS').chmod(0o600)
        self.record = {'schema': 'celikpanel/recovery-material/v2', 'snapshot': self.snapshot,
                       'snapshot_manifest_sha256': 'c' * 64, 'transaction_token_sha256': self.token,
                       'candidate_manifest_sha256': self.candidate,
                       'data_manifest_sha256': hashlib.sha256(sums).hexdigest()}
        self.write()
        patch = mock.patch.object(s, 'MATERIAL', self.base); patch.start(); self.addCleanup(patch.stop)
    def write(self):
        path = self.root / 'material.json'; path.write_text(json.dumps(self.record) + '\n'); path.chmod(0o600)
    def proof(self):
        return s.material_proof(self.snapshot, self.token, self.candidate, lambda: None)

    def test_v2_exact_material_inventory_and_tuple_observed_without_mutation(self):
        before = {str(p): (p.stat().st_ino, p.stat().st_mode, p.read_bytes()) for p in self.root.rglob('*') if p.is_file()}
        result = self.proof()
        self.assertEqual(result['data_inventory']['libexec/get.sh'], self.hash)
        self.assertEqual(result['schema'], 'celikpanel/recovery-material/v2')
        self.assertEqual(before, {str(p): (p.stat().st_ino, p.stat().st_mode, p.read_bytes()) for p in self.root.rglob('*') if p.is_file()})

    def test_corrupt_or_missing_or_extra_data_refused(self):
        payload = self.root / 'data/libexec/get.sh'
        payload.write_bytes(b'changed')
        with self.assertRaises(s.kill.MissedCheckpoint): self.proof()
        payload.unlink()
        with self.assertRaises(s.kill.MissedCheckpoint): self.proof()

    def test_wrong_material_identity_schema_token_and_hash_refused(self):
        for key in ('schema', 'snapshot', 'transaction_token_sha256', 'candidate_manifest_sha256', 'data_manifest_sha256'):
            before = self.record[key]; self.record[key] = 'wrong'; self.write()
            with self.subTest(key=key), self.assertRaises(s.kill.MissedCheckpoint): self.proof()
            self.record[key] = before
        self.write()

    def test_symlink_fifo_owner_mode_and_extra_file_refused(self):
        path = self.root / 'data/libexec/get.sh'
        for kind in ('symlink', 'fifo', 'mode', 'owner', 'extra'):
            path.unlink()
            if kind == 'symlink': path.symlink_to(self.root / 'material.json')
            elif kind == 'fifo': os.mkfifo(path, 0o600)
            else: path.write_bytes(b'known updater'); path.chmod(0o600)
            if kind == 'mode': path.chmod(0o666)
            if kind == 'owner': os.chown(path, 65534, 65534)
            if kind == 'extra': (self.root / 'data/extra').write_bytes(b'extra')
            with self.subTest(kind=kind), self.assertRaises((ValueError, OSError, s.kill.MissedCheckpoint)): self.proof()


if __name__ == '__main__': unittest.main()
