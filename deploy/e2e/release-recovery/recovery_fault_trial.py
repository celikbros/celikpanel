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


def reboot(root, record, plan, node, intent):
    if intent['recovery_fault']['action'] != 'reboot': raise ValueError('sealed intent does not request reboot')
    local.assert_absent(root, node, REBOOT_ATTEMPT)
    # The updater was submitted exactly once by this same local fixture controller.
    started = json.loads(trial.read_private(local.evidence_path(root, node, local.START)))
    if started.get('identity') != intent['identity'] or started.get('operation_id') != intent['operation_id']:
        raise ValueError('exact local updater start not established')
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
    native_node = plan['nodes'][node]
    expected = qemu_identity(native_node)
    connection = QMP(native_node, expected)
    try:
        value, raw, events = read_guest(root, record, plan, node, intent, reboot_proof=True)
        proof_at = time.monotonic()
        proof = value.get('reboot_proof')
        if not proof or proof['worker']['boot_id'] != events[-1]['worker']['boot_id']:
            raise ValueError('exact frozen recovery reboot proof unavailable')
        refs = save_collection(root, node, value, raw)
        attempt = {'schema': 'celikpanel/recovery-fault-reboot-attempt/v1', 'identity': intent['identity'],
                   'operation_id': intent['operation_id'], 'created_at': trial.now(), 'qemu': expected,
                   'checkpoint_sha256': proof['checkpoint_sha256'], 'before_boot_id': proof['worker']['boot_id'],
                   'evidence': refs, 'command': 'system_reset', 'scope': 'registered-disposable-QEMU-only'}
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
    args = parser.parse_args(argv)
    if args.mode != 'collect' and not args.execute: parser.error('mutation requires --execute and a registered disposable VM')
    if args.mode == 'prepare' and (args.archive is None or not args.archive_sha256 or not args.action or not args.checkpoint):
        parser.error('prepare requires exact committed archive, action and durable checkpoint')
    root = lab.checked_root(args.work_root); record, plan = lab.load(root); lab.process_guard(plan['nodes'][args.node])
    if args.mode == 'prepare':
        return local.prepare(root, record, plan, args.node, args.archive, args.archive_sha256, 'require-unit-reload',
                             recovery_fault={'action': args.action, 'checkpoint': args.checkpoint})
    intent = local.load_intent(root, record, plan, args.node)
    if 'recovery_fault' not in intent: raise ValueError('existing intent did not opt in to a recovery fault')
    return {'arm': local.arm, 'start': local.start, 'collect': collect, 'reboot': reboot}[args.mode](root, record, plan, args.node, intent)


if __name__ == '__main__':
    try: main()
    except (ValueError, OSError, TimeoutError, subprocess.SubprocessError) as exc:
        print('recovery fault controller refused: ' + type(exc).__name__ + ': ' + str(exc), file=sys.stderr)
        raise SystemExit(2)
