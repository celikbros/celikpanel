import copy
import datetime as dt
import hashlib
import importlib.util
import json
import os
import sys
import tempfile
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('tested_recovery_fault', Path(__file__).with_name('guest_recovery_fault.py'))
f = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(f)
OP = 'a' * 32
TOKEN = 'b' * 64
TOKEN_SHA = hashlib.sha256(TOKEN.encode()).hexdigest()
RUNTIME = 'c' * 64
SNAPSHOT = '20260914T120000Z-from-unknown-to-' + 'd' * 40 + '-' + 'e' * 32
BOOT = '67139a2c-7b23-4387-95ad-45f9e2b831ea'
IDENTITY = {'nonce': 'a' * 64, 'vm_uuid': BOOT, 'cell_id': 'fixture', 'node': 'arch'}


def intent(action='kill'):
    return {'schema': f.INTENT_SCHEMA, 'identity': IDENTITY, 'operation_id': OP, 'action': action,
            'snapshot': SNAPSHOT, 'snapshot_manifest_sha256': 'f' * 64, 'transaction_token_sha256': TOKEN_SHA,
            'runtime_manifest_sha256': RUNTIME, 'checkpoint': 'payload_restored', 'worker_executable_sha256': '0' * 64,
            'worker_command': ['/bin/bash', str(f.RUNTIME_ROOT / RUNTIME / 'deploy/release-recovery-runner.sh')]}


def worker():
    return {'unit': f.UNIT, 'pid': 987, 'start_ticks': '12', 'invocation_id': '1' * 32,
            'boot_id': BOOT, 'cgroup': '/system.slice/' + f.UNIT, 'running_executable_sha256': '0' * 64}


def transaction():
    return {'snapshot': SNAPSHOT, 'transaction_token_sha256': TOKEN_SHA,
            'transaction_operation': 'rollback', 'transaction_phase': 'active'}


def checkpoint():
    return dict(transaction(), schema=f.CHECKPOINT_SCHEMA, checkpoint='payload_restored', sequence=2,
                observed_at=dt.datetime.now(dt.timezone.utc).isoformat(), recovery_unit=f.UNIT,
                invocation_id='1' * 32, main_pid=987, main_start_ticks='12', boot_id=BOOT, runtime_manifest_sha256=RUNTIME)


class Clock:
    def __init__(self): self.value = 0
    def __call__(self): return self.value
    def sleep(self, value): self.value += max(value, 1)


class FakeNative:
    def __init__(self):
        self.value = {'worker': worker(), 'transaction': transaction(), 'checkpoint': checkpoint(), 'checkpoint_sha256': '2' * 64}
        self.actions = []
        self.error = None
    def observe(self):
        if self.error == 'missing': raise f.Unavailable('missing')
        return copy.deepcopy(self.value)
    def freeze(self):
        self.actions.append('freeze')
        if self.error == 'freeze': raise OSError()
    def proof(self, before, tick):
        tick()
        if self.error == 'proof': raise f.Unavailable('missing-proof')
        if self.error == 'change': self.value['worker']['invocation_id'] = '3' * 32
        return {'snapshot': SNAPSHOT, 'manifest_sha256': 'f' * 64, 'verified_files': 129}
    def kill(self):
        self.actions.append('kill')
        if self.error == 'kill': raise OSError()
    def thaw(self, expected): self.actions.append('thaw'); return 'thawed'


class RecoveryFaultTests(unittest.TestCase):
    def test_exact_intent_and_checkpoint_accepted(self):
        self.assertEqual(f.validate_intent(intent(), IDENTITY, OP), intent())
        self.assertEqual(f.validate_checkpoint(checkpoint(), intent(), worker(), transaction())['sequence'], 2)

    def test_arbitrary_target_secret_or_wrong_identity_intents_refused(self):
        changes = {'snapshot': '../outside', 'operation_id': 'other', 'action': 'reboot-now', 'checkpoint': 'guessed',
                   'transaction_token_sha256': TOKEN, 'worker_command': ['/bin/bash', '/root/arbitrary.sh']}
        # A different valid hash cannot be detected before the actual record; test
        # its mismatch at validate_checkpoint below, rather than pretend otherwise.
        changes.pop('transaction_token_sha256')
        for key, value in changes.items():
            changed = intent(); changed[key] = value
            with self.subTest(key=key), self.assertRaises(f.Unavailable): f.validate_intent(changed, IDENTITY, OP)
        changed = intent(); changed['raw_token'] = TOKEN
        with self.assertRaises(f.Unavailable): f.validate_intent(changed, IDENTITY, OP)

    def test_checkpoint_every_identity_and_boundary_mismatch_refused(self):
        changes = {'snapshot': 'other', 'transaction_token_sha256': '0' * 64, 'transaction_operation': 'update',
                   'transaction_phase': 'completion.pending', 'checkpoint': 'runtime_verified', 'runtime_manifest_sha256': '0' * 64,
                   'recovery_unit': 'celikpanel-agent.service', 'invocation_id': '0' * 32, 'main_pid': 2,
                   'main_start_ticks': '999', 'boot_id': '9' * 36, 'sequence': 0,
                   'observed_at': 'unknown', 'schema': 'future'}
        for key, value in changes.items():
            changed = checkpoint(); changed[key] = value
            with self.subTest(key=key), self.assertRaises(f.Unavailable): f.validate_checkpoint(changed, intent(), worker(), transaction())
        changed = checkpoint(); changed['raw_token'] = TOKEN
        with self.assertRaises(f.Unavailable): f.validate_checkpoint(changed, intent(), worker(), transaction())

    def test_marker_canonical_and_token_is_only_emitted_as_digest(self):
        raw = f'version=1\ntoken={TOKEN}\noperation=rollback\nsnapshot={SNAPSHOT}\n'.encode()
        parsed = f.parse_marker(raw)
        self.assertEqual(parsed['transaction_token_sha256'], TOKEN_SHA)
        self.assertNotIn(TOKEN, json.dumps(parsed))
        for bad in (raw + b'extra=x\n', raw.replace(b'version=1', b'version=2'), raw.replace(b'rollback', b'arbitrary')):
            with self.assertRaises(f.Unavailable): f.parse_marker(bad)

    def run_case(self, native, action='kill', **kwargs):
        clock = Clock(); events = []
        result = f.run_fault(intent(action), lambda event, **fields: events.append(dict(event=event, **fields)), native,
                             clock=clock, pause=clock.sleep, **kwargs)
        return result, events

    def test_proven_checkpoint_kills_only_exact_recovery_then_thaws(self):
        native = FakeNative()
        result, events = self.run_case(native)
        self.assertEqual(result, 0)
        self.assertEqual(native.actions, ['freeze', 'kill', 'thaw'])
        self.assertEqual([event['event'] for event in events], ['armed', 'freeze_requested', 'freeze_observed', 'checkpoint_verified', 'kill_requested', 'kill_sent', 'released'])
        self.assertTrue(events[-1]['checkpoint_verified'])
        self.assertNotIn(TOKEN, json.dumps(events))

    def test_restart_during_freeze_is_only_thawed_never_killed(self):
        native = FakeNative()
        def freeze():
            native.actions.append('freeze')
            native.value['worker']['invocation_id'] = '4' * 32
            return copy.deepcopy(native.value['worker'])
        thawed=[]
        native.freeze=freeze
        native.thaw=lambda actual:thawed.append(actual) or 'thawed'
        result,events=self.run_case(native)
        self.assertEqual(result,2)
        self.assertNotIn('kill',native.actions)
        self.assertEqual(thawed[0]['invocation_id'],'4'*32)
        observed=next(event for event in events if event['event']=='freeze_observed')
        self.assertEqual(observed['scope'],'cleanup-only')
        self.assertFalse(events[-1]['kill_sent'])

    def test_execstoppost_proves_freeze_result_if_helper_died_before_result_record(self):
        original=worker();actual=dict(original,invocation_id='4'*32,pid=999)
        event={'schema':f.SCHEMA,'identity':IDENTITY,'operation_id':OP,'event':'freeze_requested','worker':original}
        native=unittest.mock.Mock()
        native.properties.return_value={'FreezerState':'frozen'}
        native.worker_identity.return_value=actual
        native.thaw.return_value='thawed'
        with patch.object(f,'private_read',return_value=(json.dumps(event)+'\n').encode()),patch.object(f,'Native',return_value=native):
            self.assertEqual(f.cleanup(intent(),IDENTITY,OP),'thawed')
        native.worker_identity.assert_called_once_with(cleanup=True)
        native.thaw.assert_called_once_with(actual)

    def test_missing_checkpoint_never_freezes_or_kills(self):
        native = FakeNative(); native.error = 'missing'
        result, events = self.run_case(native)
        self.assertEqual(result, 2); self.assertEqual(native.actions, [])
        self.assertFalse(events[-1]['checkpoint_verified'])

    def test_proof_freeze_and_postproof_identity_failure_never_kill(self):
        for error in ('proof', 'freeze', 'change'):
            native = FakeNative(); native.error = error
            result, events = self.run_case(native)
            self.assertEqual(result, 2, error)
            self.assertEqual(native.actions, ['freeze', 'thaw'])
            self.assertFalse(events[-1]['kill_sent'])

    def test_ambiguous_kill_is_never_success(self):
        native = FakeNative(); native.error = 'kill'
        result, events = self.run_case(native)
        self.assertEqual(result, 2); self.assertFalse(events[-1]['kill_sent'])

    def test_reboot_only_publishes_verified_handoff_and_times_out_thawed(self):
        native = FakeNative()
        result, events = self.run_case(native, 'reboot')
        self.assertEqual(result, 2)
        self.assertEqual(native.actions, ['freeze', 'thaw'])
        self.assertIn('reboot_ready', [event['event'] for event in events])
        self.assertEqual(events[-1]['reboot_performed'], 'not-observed-by-guest')
        self.assertEqual(events[-1]['reason'], 'timeout')

    def test_signal_before_or_after_freeze_leaves_no_kill(self):
        native = FakeNative()
        result, _ = self.run_case(native, interrupted=lambda: True)
        self.assertEqual(result, 2); self.assertEqual(native.actions, [])
        native = FakeNative()
        result, _ = self.run_case(native, interrupted=lambda: 'freeze' in native.actions)
        self.assertEqual(result, 2); self.assertEqual(native.actions, ['freeze', 'thaw'])


@unittest.skipUnless(sys.platform == 'linux' and getattr(os,'geteuid',lambda:-1)()==0, 'native root metadata')
class RuntimeProofTests(unittest.TestCase):
    def test_exact_runtime_inventory_and_payload_mutation(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            payload = {'bin/recovery': b'fixture executable', 'rollback.sh': b'fixture rollback', 'update.sh': b'fixture update',
                       'deploy/recovery/runtime-entry.sh': b'fixture entry'}
            raw = b'format=celikpanel-recovery-runtime-v1\nprotocol=1\nsnapshot=6\n'
            raw += b''.join((hashlib.sha256(value).hexdigest()+'  '+name+'\n').encode() for name,value in sorted(payload.items()))
            digest = hashlib.sha256(raw).hexdigest()
            runtime = root / digest
            for name,value in payload.items():
                path=runtime/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(value)
            manifest=runtime/'runtime.manifest';manifest.write_bytes(raw);manifest.chmod(0o600)
            with patch.object(f,'RUNTIME_ROOT',root):
                self.assertEqual(f.verify_runtime(digest)['verified_files'],4)
                extra=runtime/'extra';extra.write_bytes(b'unexpected')
                with self.assertRaises(f.Unavailable): f.verify_runtime(digest)
                extra.unlink()
                (runtime/'bin/recovery').write_bytes(b'changed')
                with self.assertRaises(f.Unavailable): f.verify_runtime(digest)

    def test_recovery_observation_reader_refuses_symlink_and_public_mode(self):
        with tempfile.TemporaryDirectory() as temporary:
            path=Path(temporary)/'record';path.write_bytes(b'private');path.chmod(0o600)
            self.assertEqual(f.private_read(path),b'private')
            link=Path(temporary)/'link';link.symlink_to(path)
            with self.assertRaises((f.Unavailable,f.probe.ProbeError)): f.private_read(link)
            path.chmod(0o644)
            with self.assertRaises(f.Unavailable): f.private_read(path)


if __name__ == '__main__': unittest.main()
