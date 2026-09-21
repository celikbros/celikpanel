import copy
import hashlib
import importlib.util
import json
import os
import tempfile
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
import unittest

SPEC = importlib.util.spec_from_file_location('tested_bound_worker', Path(__file__).with_name('guest_bound_worker.py'))
s = importlib.util.module_from_spec(SPEC); SPEC.loader.exec_module(s)


class BoundWorkerTests(unittest.TestCase):
    def setUp(self):
        self.operation = 'a' * 32
        self.identity = {'nonce': 'b' * 64, 'vm_uuid': 'd61f4a8b-31dd-4bb0-b280-1741c539f4c3', 'cell_id': 'bound-worker-fixture', 'node': 'debian13'}
        self.intent = {'schema': s.SCHEMA, 'identity': self.identity, 'operation_id': self.operation,
                       'baseline': {'version': 'v0.1.0-alpha.81', 'commit': 'c' * 40, 'agent_sha256': 'd' * 64, 'panel_sha256': 'e' * 64},
                       'target': {'version': 'v0.1.0-alpha.82', 'commit': 'c' * 40, 'agent_sha256': 'f' * 64, 'panel_sha256': '0' * 64}, 'recovery_fault': None}
        self.snapshot = '20260916T180000Z-from-unknown-to-' + 'c' * 40 + '-' + '1' * 32
        self.transaction = {'version': '1', 'token': '2' * 64, 'operation': 'update', 'snapshot': self.snapshot}
        self.observation = {'schema': 'celikpanel-recovery-observation/v1', 'request_id': self.operation, 'target_commit': 'c' * 40, 'phase': 'running', 'terminal_proof': 'none', 'reason': 'update_running', 'observed_at': '2026-09-16T18:00:00Z', 'previous_failure': 'none'}
        self.binding = {'schema': 'celikpanel-recovery-binding/v1', 'request_id': self.operation, 'target_commit': 'c' * 40, 'snapshot': self.snapshot, 'update_token': '2' * 64}
        self.state = {'version': 1, 'request_id': self.operation, 'status': 'running', 'target_version': 'v0.1.0-alpha.82', 'target_commit': 'c' * 40, 'expected_current_version': 'v0.1.0-alpha.81', 'expected_current_commit': 'c' * 40, 'target_os': 'linux', 'target_arch': 'amd64'}

    @staticmethod
    def record(value): return ''.join(key + '=' + val + '\n' for key, val in value.items()).encode()

    def proof(self):
        return s.bound_proof(self.intent, self.snapshot, self.record(self.transaction), self.record(self.observation), self.record(self.binding), json.dumps(self.state).encode())

    def test_exact_production_records_bind_same_operation_without_exposing_token(self):
        result = self.proof()
        self.assertEqual(result['request_id'], self.operation)
        self.assertEqual(result['transaction_token_sha256'], hashlib.sha256(('2' * 64).encode()).hexdigest())
        self.assertNotIn('2' * 64, json.dumps(result))
        self.assertEqual(result['terminal_proof'], 'none')

    def test_identity_bound_intent_rejects_retargeting_and_unsupported_profile(self):
        for path, value in [(('identity', 'nonce'), 'f' * 64), (('baseline', 'version'), 'v0.1.0-alpha.75'), (('target', 'commit'), 'unknown'), (('target', 'agent_sha256'), 'd' * 64)]:
            modified = copy.deepcopy(self.intent); modified[path[0]][path[1]] = value
            with self.subTest(path=path), self.assertRaises(s.probe.ProbeError):
                s.validate_intent(modified, self.identity, self.operation)
        self.assertEqual(s.validate_intent(self.intent, self.identity, self.operation), self.intent)

    def test_foreign_binding_token_snapshot_commit_or_request_never_admits_cut(self):
        for key, value in [('request_id', 'f' * 32), ('target_commit', 'f' * 40), ('snapshot', self.snapshot + 'x'), ('update_token', 'f' * 64), ('schema', 'future')]:
            original = self.binding[key]; self.binding[key] = value
            with self.subTest(key=key), self.assertRaises(s.probe.ProbeError): self.proof()
            self.binding[key] = original

    def test_nonrunning_or_foreign_observation_never_admits_cut(self):
        for key, value in [('phase', 'accepted'), ('phase', 'recovered'), ('terminal_proof', 'rollback_verified'), ('reason', 'update_failed'), ('previous_failure', 'update_failed'), ('request_id', 'f' * 32), ('observed_at', '2026-99-16T18:00:00Z')]:
            original = self.observation[key]; self.observation[key] = value
            with self.subTest(key=key, value=value), self.assertRaises(s.probe.ProbeError): self.proof()
            self.observation[key] = original

    def test_accepted_worker_tuple_and_active_transaction_must_match(self):
        for obj, key, value in [(self.state, 'expected_current_version', 'v0.1.0-alpha.75'), (self.state, 'expected_current_commit', 'f' * 40), (self.state, 'target_version', 'v0.1.0-alpha.83'), (self.state, 'status', 'succeeded'), (self.state, 'request_id', 'f' * 32), (self.transaction, 'operation', 'rollback')]:
            original = obj[key]; obj[key] = value
            with self.subTest(key=key, value=value), self.assertRaises(s.probe.ProbeError): self.proof()
            obj[key] = original

    def test_reordered_duplicate_missing_newline_or_extra_fields_are_rejected(self):
        raw = self.record(self.binding)
        for malformed in [raw[:-1], raw + b'other=x\n', raw.replace(b'snapshot=', b'update_token='), b'\r\n'.join(raw.split(b'\n'))]:
            with self.subTest(raw=malformed), self.assertRaises(s.probe.ProbeError):
                s.bound_proof(self.intent, self.snapshot, self.record(self.transaction), self.record(self.observation), malformed, json.dumps(self.state).encode())

    def test_protected_record_rejects_unsafe_mode_symlink_and_fifo(self):
        if os.geteuid() != 0: self.skipTest('root-only private metadata contract')
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'record'
            path.write_bytes(b'exact')
            path.chmod(0o600)
            self.assertEqual(s.protected_read(path, 32), b'exact')
            path.chmod(0o644)
            with self.assertRaises(s.probe.ProbeError): s.protected_read(path, 32)
            path.unlink(); path.symlink_to(Path(directory) / 'missing')
            with self.assertRaises(s.probe.ProbeError): s.protected_read(path, 32)
            path.unlink(); os.mkfifo(path, 0o600)
            with self.assertRaises(s.probe.ProbeError): s.protected_read(path, 32)

    def test_binding_must_be_revalidated_immediately_before_kill(self):
        native = s.Native(SimpleNamespace(operation_id=self.operation), self.intent)
        with patch.object(s.base.Native, 'kill') as kill:
            with self.assertRaises(s.base.MissedCheckpoint): native.kill()
            kill.assert_not_called()
            native.bound = self.proof()
            with patch.object(native, 'read_binding', return_value=dict(native.bound, binding_sha256='f' * 64)):
                with self.assertRaises(s.base.MissedCheckpoint): native.kill()
            kill.assert_not_called()
            with patch.object(native, 'read_binding', return_value=native.bound): native.kill()
            kill.assert_called_once_with()


if __name__ == '__main__': unittest.main()
