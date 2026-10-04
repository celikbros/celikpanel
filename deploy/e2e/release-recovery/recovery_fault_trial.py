#!/usr/bin/env python3
"""Opt-in recovery interruption through one unpublished update in a registered VM.

No production target, panel API, guest reboot command, manual rollback or retry.
"""
from __future__ import annotations
import argparse
import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shlex
import socket
import stat
import struct
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location('recovery_fault_local_trial', HERE / 'local_candidate_trial.py')
local = importlib.util.module_from_spec(SPEC); sys.modules[SPEC.name] = local; SPEC.loader.exec_module(local)
hand = local.module('host_recovery_handoff', 'guest_recovery_handoff.py')
lab, trial = local.lab, local.trial
REBOOT_ATTEMPT = 'recovery-fault-reboot-attempt.json'


def read_guest(root, record, plan, node, intent, reboot_proof=False):
    args = hand.helper_argv(intent['identity'], intent['operation_id'])
    args[2] = str(hand.fault.PRIVATE_ROOT / 'guest_recovery_handoff.py')
    args.remove('--execute')
    args.extend(('--mode', 'reboot-proof' if reboot_proof else 'collect'))
    result = lab.guarded_script(root, record, plan, node, shlex.join(args), timeout=12)
    if len(result.stdout) > 2 * 1024 * 1024: raise ValueError('recovery evidence exceeds bound')
    value = json.loads(result.stdout)
    if (value.get('schema') != hand.SCHEMA or value.get('identity') != intent['identity']
            or value.get('operation_id') != intent['operation_id']): raise ValueError('recovery collection identity differs')
    exact = hand.fault.validate_intent(value.get('intent'), intent['identity'], intent['operation_id'])
    if {key: exact[key] for key in ('action', 'checkpoint')} != intent['recovery_fault']:
        raise ValueError('recovery action differs from sealed local intent')
    raw = base64.b64decode(value['events_base64'], validate=True)
    if len(raw) > 1048576 or hashlib.sha256(raw).hexdigest() != value['events_sha256']:
        raise ValueError('recovery events checksum differs')
    events = []
    for line in raw.splitlines():
        event = hand.fault.probe.strict_object(line)
        if (event.get('schema') != hand.fault.SCHEMA or event.get('identity') != intent['identity']
                or event.get('operation_id') != intent['operation_id']): raise ValueError('recovery event identity differs')
        events.append(event)
    if raw and not raw.endswith(b'\n'): raise ValueError('recovery events truncated')
    return value, raw, events


def save_collection(root, node, value, raw):
    label = 'recovery-fault-collection-' + str(time.time_ns())
    return {'record': trial.save(root, node, label + '.json', local.encoded(value)),
            'events': trial.save(root, node, label + '.jsonl', raw)}


def collect(root, record, plan, node, intent):
    value, raw, events = read_guest(root, record, plan, node, intent)
    refs = save_collection(root, node, value, raw)
    print(json.dumps({'action': 'recovery-fault-collected', 'node': node, 'operation_id': intent['operation_id'],
                      'events': [event['event'] for event in events], 'unit': value['unit'],
                      'evidence': refs, 'native_recovery_success': 'not-inferred'}), flush=True)
    return value


def qemu_identity(node):
    lab.process_guard(node)
    pid = int(Path(node['paths']['pid']).read_text())
    proc = Path('/proc') / str(pid)
    start = hand.fault.shared.process_start((proc / 'stat').read_text())
    path = Path(node['paths']['qmp']); info = path.lstat()
    if not stat.S_ISSOCK(info.st_mode) or info.st_uid != os.getuid(): raise ValueError('QMP socket ownership differs')
    expected = node['qemu_command'][node['qemu_command'].index('-uuid') + 1]
    return {'pid': pid, 'start_ticks': start, 'socket_device': info.st_dev, 'socket_inode': info.st_ino, 'vm_uuid': expected}


class QMP:
    """Same peer connection proves UUID and accepts at most one reset request."""
    def __init__(self, node, expected):
        self.node, self.expected, self.sent = node, expected, False
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        try:
            self.sock.settimeout(3)
            self.sock.connect(node['paths']['qmp'])
            pid, uid, _ = struct.unpack('3i', self.sock.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
            if pid != expected['pid'] or uid != os.getuid() or qemu_identity(node) != expected:
                raise ValueError('QMP peer identity differs')
            self.stream = self.sock.makefile('rwb', buffering=0)
            if 'QMP' not in self.read(): raise ValueError('QMP greeting unavailable')
            self.command('qmp_capabilities')
            if self.command('query-uuid') != {'UUID': expected['vm_uuid']}:
                raise ValueError('QMP VM UUID differs')
            if self.command('query-status').get('status') != 'running': raise ValueError('QEMU is not running')
        except BaseException:
            self.close(); raise
    def read(self):
        raw = self.stream.readline(65537)
        if not raw or len(raw) > 65536 or not raw.endswith(b'\n'): raise ValueError('QMP response truncated')
        value = json.loads(raw)
        if not isinstance(value, dict): raise ValueError('invalid QMP response')
        return value
    def command(self, name):
        request_id = 'celikpanel-lab-' + name
        self.stream.write(local.encoded({'execute': name, 'id': request_id}))
        for _ in range(64):
            result = self.read()
            if result.get('id') != request_id: continue
            if 'error' in result or 'return' not in result: raise ValueError('QMP command failed')
            return result['return']
        raise ValueError('QMP bounded response unavailable')
    def reset(self):
        if self.sent or qemu_identity(self.node) != self.expected: raise ValueError('QMP reset identity or once-only guard failed')
        self.sent = True
        return self.command('system_reset')
    def close(self):
        if hasattr(self, 'stream'): self.stream.close()
        self.sock.close()


def native_start(root, record, plan, node, intent, boundary):
    """A separate closed profile must prove its own immutable once-only start."""
    native = local.module('reboot_native_exchange', 'native_wal_trial.py')
    if boundary != native.guest.RECOVERY_BOUNDARY:
        raise ValueError('unsupported native recovery reboot profile')
    value, inner = native.load(root, record, plan, node, boundary=boundary)
    if inner != intent:
        raise ValueError('native recovery local intent changed')
    started = json.loads(trial.read_private(local.evidence_path(root, node, native.host_names(boundary)[1]), 4 * 1024 * 1024))
    if (started.get('intent') != value
            or started.get('entrypoint') != native.argv(value, 'gate', boundary=boundary)):
        raise ValueError('exact native updater start not established')
    return native, value


def bind_native_cut(data, events, recovery, intent):
    """The second fault is subordinate to the confirmed first cut and handoff."""
    expected = ['armed', 'gate_released', 'database_exchange_entry_verified',
                'database_exchange_checkpoint_verified', 'recovery_fault_armed',
                'kill_requested', 'kill_sent', 'trace_finished']
    result = data.get('result') or {}
    if ([e.get('event') for e in events] != expected or result.get('status') != 'cut-sent'
            or events[-1].get('result') != result or 'recovery_handoff_cleanup' in result):
        raise ValueError('native cut not conclusively finished; no reboot')
    if any(e.get('identity') != intent['identity'] or e.get('operation_id') != intent['operation_id'] for e in events):
        raise ValueError('native cut operation differs')
    entry, checkpoint = events[2]['checkpoint'], events[3]['checkpoint']
    digest = lambda v: hashlib.sha256(json.dumps(v, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
    receipt = events[6]['receipt']
    handoff = events[4]['handoff']
    exact = recovery['intent']
    if (checkpoint.get('status') != 'verified'
            or checkpoint.get('classification') != 'database-exchanged-before-receipt-held'
            or checkpoint.get('entry_proof_sha256') != digest(entry)
            or checkpoint.get('trace_sha256') != digest(events[3]['trace'])
            or result.get('cut_receipt') != receipt
            or result.get('independent_proof') != checkpoint
            or receipt.get('status') != 'cut-sent' or receipt.get('operation_id') != intent['operation_id']
            or receipt.get('trace_sha256') != checkpoint['trace_sha256']
            or receipt.get('entry_proof_sha256') != checkpoint['entry_proof_sha256']
            or receipt.get('scope') != 'exact-update-unit-cgroup' or receipt.get('signal') != 'SIGKILL'
            or handoff.get('identity') != intent['identity'] or handoff.get('operation_id') != intent['operation_id']
            or handoff.get('intent') != exact
            or handoff.get('intent_sha256') != hashlib.sha256(hand.encoded(exact)).hexdigest()
            or exact.get('snapshot') != checkpoint['transaction']['snapshot']
            or exact.get('snapshot_manifest_sha256') != checkpoint['database']['snapshot']['manifest_sha256']
            or exact.get('transaction_token_sha256') != checkpoint['transaction']['transaction_token_sha256']
            or exact.get('runtime_manifest_sha256') != checkpoint['kit']['manifest_sha256']
            or {k: exact.get(k) for k in ('action', 'checkpoint')} != intent['recovery_fault']):
        raise ValueError('recovery reboot is not bound to exact native exchange')
    return {'checkpoint_sha256': digest(checkpoint), 'handoff_intent_sha256': handoff['intent_sha256'],
            'snapshot': exact['snapshot'], 'transaction_token_sha256': exact['transaction_token_sha256']}


def reboot(root, record, plan, node, intent, *, native_boundary=None):
    if intent['recovery_fault']['action'] != 'reboot': raise ValueError('sealed intent does not request reboot')
    local.assert_absent(root, node, REBOOT_ATTEMPT)
    # The updater was submitted exactly once by this same local fixture controller.
    native = None
    if native_boundary is None:
        started = json.loads(trial.read_private(local.evidence_path(root, node, local.START)))
        if started.get('identity') != intent['identity'] or started.get('operation_id') != intent['operation_id']:
            raise ValueError('exact local updater start not established')
    else:
        native, native_intent = native_start(root, record, plan, node, intent, native_boundary)
    deadline = time.monotonic() + 600
    while time.monotonic() < deadline:
        try:
            value, raw, events = read_guest(root, record, plan, node, intent)
        except subprocess.CalledProcessError:
            time.sleep(.1); continue  # recovery intent does not exist before sealed updater checkpoint
        if events and events[-1]['event'] == 'released': raise ValueError('recovery reboot checkpoint missed; no reset')
        if not events or events[-1]['event'] != 'reboot_ready':
            time.sleep(.1); continue
        break
    else: raise TimeoutError('recovery reboot checkpoint unavailable; no reset')
    cut_binding = None
    if native is not None:
        cut, cut_raw, cut_events = native.read(root, record, plan, node, native_intent, boundary=native_boundary)
        cut_binding = bind_native_cut(cut, cut_events, value, intent)
        cut_binding['evidence'] = {
            'record': trial.save(root, node, 'native-recovery-reboot-cut.json', local.encoded(cut)),
            'events': trial.save(root, node, 'native-recovery-reboot-cut.jsonl', cut_raw)}
        journal = lab.guarded_script(root, record, plan, node, shlex.join([
            'journalctl', '--no-pager', '--output=json', '-u',
            native.guest.names(intent['operation_id'], native_boundary)['worker'],
            '-u', 'celikpanel-release-recovery.service', '--since', native_intent['created_at'], '-n', '600']), timeout=10).stdout.encode()
        if len(journal) > 4 * 1024 * 1024 or len(journal.splitlines()) >= 600:
            raise ValueError('pre-reset journal exceeds bound')
        cut_binding['evidence']['journal'] = trial.save(root, node, 'native-recovery-reboot-before.jsonl', journal)
    native_node = plan['nodes'][node]
    expected = qemu_identity(native_node)
    connection = QMP(native_node, expected)
    try:
        value, raw, events = read_guest(root, record, plan, node, intent, reboot_proof=True)
        proof_at = time.monotonic()
        if native is not None:
            bind_native_cut(cut, cut_events, value, intent)
        proof = value.get('reboot_proof')
        if not proof or proof['worker']['boot_id'] != events[-1]['worker']['boot_id']:
            raise ValueError('exact frozen recovery reboot proof unavailable')
        refs = save_collection(root, node, value, raw)
        attempt = {'schema': 'celikpanel/recovery-fault-reboot-attempt/v1', 'identity': intent['identity'],
                   'operation_id': intent['operation_id'], 'created_at': trial.now(), 'qemu': expected,
                   'checkpoint_sha256': proof['checkpoint_sha256'], 'before_boot_id': proof['worker']['boot_id'],
                   'evidence': refs, 'command': 'system_reset', 'scope': 'registered-disposable-QEMU-only'}
        if cut_binding is not None:
            attempt['native_cut'] = cut_binding
        trial.save(root, node, REBOOT_ATTEMPT, local.encoded(attempt))
        if time.monotonic() - proof_at > 3: raise ValueError('frozen checkpoint proof expired before reset')
        connection.reset()
        result = dict(attempt, action='registered-QEMU-reset-submitted-once', reboot_verified=False, recovery_success='unconfirmed')
        trial.save(root, node, 'recovery-fault-reboot-result.json', local.encoded(result))
        print(json.dumps({'action': result['action'], 'node': node, 'operation_id': intent['operation_id'],
                          'recovery_success': 'unconfirmed'}), flush=True)
        return result
    finally: connection.close()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work-root', required=True); parser.add_argument('--node', choices=('arch', 'debian13'), required=True)
    parser.add_argument('--mode', choices=('prepare', 'arm', 'start', 'collect', 'reboot'), required=True)
    parser.add_argument('--execute', action='store_true')
    parser.add_argument('--archive', type=Path); parser.add_argument('--archive-sha256')
    parser.add_argument('--action', choices=('kill', 'reboot')); parser.add_argument('--checkpoint', choices=hand.fault.CHECKPOINTS)
    parser.add_argument('--candidate-data-fault', choices=('quarantine-fixed-three',))
    args = parser.parse_args(argv)
    if args.mode != 'collect' and not args.execute: parser.error('mutation requires --execute and a registered disposable VM')
    if args.mode == 'prepare' and (args.archive is None or not args.archive_sha256 or not args.action or not args.checkpoint):
        parser.error('prepare requires exact committed archive, action and durable checkpoint')
    if args.mode != 'prepare' and args.candidate_data_fault is not None: parser.error('candidate data fault must be sealed during prepare')
    root = lab.checked_root(args.work_root); record, plan = lab.load(root); lab.process_guard(plan['nodes'][args.node])
    if args.mode == 'prepare':
        return local.prepare(root, record, plan, args.node, args.archive, args.archive_sha256, 'require-unit-reload',
                             recovery_fault={'action': args.action, 'checkpoint': args.checkpoint}, candidate_data_fault=args.candidate_data_fault)
    intent = local.load_intent(root, record, plan, args.node)
    if 'recovery_fault' not in intent: raise ValueError('existing intent did not opt in to a recovery fault')
    return {'arm': local.arm, 'start': local.start, 'collect': collect, 'reboot': reboot}[args.mode](root, record, plan, args.node, intent)


if __name__ == '__main__':
    try: main()
    except (ValueError, OSError, TimeoutError, subprocess.SubprocessError) as exc:
        print('recovery fault controller refused: ' + type(exc).__name__ + ': ' + str(exc), file=sys.stderr)
        raise SystemExit(2)
