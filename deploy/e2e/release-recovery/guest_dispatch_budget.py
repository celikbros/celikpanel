#!/usr/bin/env python3
"""Two bounded recovery cuts after one registered disposable QEMU reboot.

Uses the existing exact-process/checkpoint/snapshot proofs. Never edits product
evidence, starts recovery, resets budgets, or touches an unregistered host.
"""
from __future__ import annotations
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import sys

HERE = Path(__file__).resolve().parent
def module(name):
    spec = importlib.util.spec_from_file_location('budget_' + name, HERE / (name + '.py'))
    value = importlib.util.module_from_spec(spec); sys.modules[spec.name] = value; spec.loader.exec_module(value)
    return value

boot = module('guest_boot_wait')
fault = module('guest_recovery_fault')
ROOT = boot.ROOT
UNIT = 'celikpanel-lab-budget-cuts.service'
UNIT_PATH = Path('/etc/systemd/system') / UNIT
BUDGET = Path('/var/lib/celikpanel-release-state/recovery-dispatch/v1')
SCHEMA = 'celikpanel/disposable-budget-cuts/v1'
DEPENDENCIES = ('guest_dispatch_budget.py', 'guest_boot_wait.py', 'guest_recovery_fault.py',
                'guest_update_kill.py', 'guest_port_fault.py', 'guest_probe.py')


def unit_bytes(operation):
    if not re.fullmatch('[0-9a-f]{32}', operation): raise ValueError('invalid operation')
    return ('[Unit]\nDescription=Disposable bounded recovery budget cuts\nAfter=local-fs.target\n'
            'Before=celikpanel-release-recovery.service\n[Service]\nType=exec\n'
            'ExecStart=/usr/bin/python3 -I ' + str(ROOT / 'guest_dispatch_budget.py') +
            ' run --operation-id ' + operation + '\nRuntimeMaxSec=650\nTimeoutStopSec=5\nUMask=0077\n'
            '[Install]\nWantedBy=multi-user.target\n').encode()


def arm(operation):
    intent, raw = boot.guarded_intent(operation)
    if HERE != ROOT: raise ValueError('fixture must run from guarded private directory')
    unit = unit_bytes(operation)
    config = {'schema':SCHEMA, 'operation_id':operation, 'identity':intent['identity'],
              'armed_boot_id':boot.boot_id(), 'intent_sha256':boot.sha(raw),
              'attempts':[2,3], 'checkpoint':'payload_restored', 'unit_sha256':boot.sha(unit),
              'files':{name:boot.sha(boot.read(HERE / name, 1048576)) for name in DEPENDENCIES}}
    boot.once(ROOT / ('budget-cuts-' + operation + '.json'), boot.encoded(config))
    boot.once(UNIT_PATH, unit, 0o644)
    for argv in (['systemctl','daemon-reload'], ['systemctl','enable',UNIT]):
        if boot.run(argv).returncode: raise ValueError('could not arm fixture boot unit')
    return {'action':'armed-for-next-boot', 'attempts':[2,3], 'operation_id':operation}


def validate_receipt(raw, snapshot, attempt):
    value = boot.record(raw, ('schema','snapshot','attempt','token_sha256','operation','phase'))
    if (value['schema'] != 'celikpanel-recovery-dispatch/v1' or value['snapshot'] != snapshot
            or value['attempt'] != str(attempt) or not re.fullmatch('[0-9a-f]{64}',value['token_sha256'])
            or value['operation'] not in ('update','rollback')
            or value['phase'] not in ('quiesce','active','completion','completion-scheduler','scheduler')):
        raise ValueError('dispatch receipt identity differs')
    return value


def receipts(snapshot, attempt):
    if attempt not in (2,3) or not fault.files.SNAPSHOT.fullmatch(snapshot):
        raise ValueError('unsupported exact budget cut')
    root = BUDGET / snapshot
    fault.probe.protected_parents(root / '1', {0})
    result = {}
    for number in range(1,attempt+1):
        raw = fault.private_read(root / str(number),1024)
        validate_receipt(raw,snapshot,number)
        result[str(number)] = boot.sha(raw)
    for number in range(attempt+1,4):
        if (root / str(number)).exists() or (root / str(number)).is_symlink():
            raise fault.Unavailable('later-attempt-already-published')
    if any(path.name.startswith('owner.') for path in root.iterdir()):
        raise fault.Unavailable('owner-retry-already-admitted')
    return result


class BudgetNative(fault.Native):
    def __init__(self, intent, attempt):
        super().__init__(intent); self.attempt = attempt

    def observe(self):
        # A short-lived native boot deferral can exit during the read-only
        # process probe. Unknown evidence never authorizes a cut; the bounded
        # observer may resample until the complete identity/checkpoint agrees.
        try: value = super().observe()
        except fault.probe.ProbeError as exc:
            raise fault.Unavailable('native-process-probe-unavailable') from exc
        try: value['budget_receipts'] = receipts(self.intent['snapshot'],self.attempt)
        except (FileNotFoundError,ValueError) as exc:
            raise fault.Unavailable('exact-budget-reservation-not-observed') from exc
        return value


def run(operation):
    intent, raw = boot.guarded_intent(operation)
    config = fault.probe.strict_object(boot.read(ROOT / ('budget-cuts-' + operation + '.json')))
    if (set(config) != {'schema','operation_id','identity','armed_boot_id','intent_sha256','attempts','checkpoint','unit_sha256','files'}
            or config['schema'] != SCHEMA or config['operation_id'] != operation or config['identity'] != intent['identity']
            or config['armed_boot_id'] == boot.boot_id() or config['intent_sha256'] != boot.sha(raw)
            or config['attempts'] != [2,3] or config['checkpoint'] != 'payload_restored'
            or config['unit_sha256'] != boot.sha(boot.read(UNIT_PATH,4096,mode=0o644))
            or config['files'] != {name:boot.sha(boot.read(HERE/name,1048576)) for name in DEPENDENCIES}):
        raise ValueError('sealed next-boot fixture differs')
    recovery = fault.probe.strict_object(fault.private_read(ROOT / ('recovery-fault-' + operation + '.json')))
    fault.validate_intent(recovery,intent['identity'],operation)
    if recovery['action'] != 'reboot' or recovery['checkpoint'] != config['checkpoint']:
        raise ValueError('wrong initial recovery handoff')
    # A separate sealed lab intent authorizes two SIGKILLs after the first reboot.
    # It is not a product marker or authority to start any mutation.
    recovery = dict(recovery,action='kill')
    path = ROOT / ('budget-cuts-' + operation + '.jsonl')
    fd = os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
    stopped = False
    def stop(signum, frame):
        nonlocal stopped
        stopped = True
    for sig in (signal.SIGINT,signal.SIGTERM): signal.signal(sig,stop)
    with os.fdopen(fd,'w') as out:
        for attempt in config['attempts']:
            def emit(event, **fields):
                out.write(json.dumps({'schema':SCHEMA,'event':event,'at':fault.probe.utc_now(),
                    'operation_id':operation,'identity':intent['identity'],'attempt':attempt,**fields},sort_keys=True)+'\n')
                out.flush(); os.fsync(out.fileno())
            if fault.run_fault(recovery,emit,BudgetNative(recovery,attempt),interrupted=lambda:stopped) != 0:
                return 2
    return 0


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command',choices=('arm','run'));parser.add_argument('--operation-id',required=True)
    args=parser.parse_args()
    if args.command=='arm': print(json.dumps(arm(args.operation_id)));return 0
    return run(args.operation_id)

if __name__=='__main__':
    try: raise SystemExit(main())
    except (OSError,ValueError,fault.Unavailable,fault.probe.ProbeError) as exc:
        print('budget fault refused: '+type(exc).__name__,file=sys.stderr);raise SystemExit(2)
