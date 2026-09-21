#!/usr/bin/env python3
"""Bounded real systemd boot job for a registered disposable recovery drill.

Only the fixture unit/receipt/log are written. Product readiness, observations,
locks, bindings and recovery are never replaced or started by this observer.
"""
from __future__ import annotations
import argparse
import datetime as dt
import fcntl
import grp
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time
from types import SimpleNamespace

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('boot_wait_probe', HERE / 'guest_probe.py')
probe = importlib.util.module_from_spec(spec); sys.modules[spec.name] = probe; spec.loader.exec_module(probe)
ROOT = Path('/root/celikpanel-release-recovery-lab')
UNIT = 'celikpanel-lab-boot-wait.service'
UNIT_PATH = Path('/etc/systemd/system') / UNIT
LINK = Path('/etc/systemd/system/multi-user.target.wants') / UNIT
TRANSACTION = Path('/var/lib/celikpanel-release-transaction')
OBSERVATIONS = Path('/var/lib/celikpanel-recovery-observations')
BINDINGS = Path('/var/lib/celikpanel-release-state/recovery-observation-bindings')
SCHEMA = 'celikpanel/disposable-boot-wait/v1'
MAX_SECONDS = 120
WAIT_MESSAGE = 'Recovery waiting for the operating system transition; no owner action is needed.'
ENV = {'PATH':'/usr/sbin:/usr/bin:/sbin:/bin', 'LANG':'C', 'LC_ALL':'C', 'TZ':'UTC0'}


def encoded(value): return (json.dumps(value, sort_keys=True) + '\n').encode()
def sha(raw): return hashlib.sha256(raw).hexdigest()
def boot_id(): return Path('/proc/sys/kernel/random/boot_id').read_text().strip()
def run(argv): return subprocess.run(argv, capture_output=True, timeout=8, env=ENV)


def read(path, limit=8192, mode=0o600, gid=0):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        before = os.fstat(stream.fileno())
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_gid != gid
                or stat.S_IMODE(before.st_mode) != mode or before.st_nlink != 1 or before.st_size > limit):
            raise ValueError('unsafe fixture or evidence file')
        raw = stream.read(limit + 1)
        if len(raw) > limit or probe.metadata(before) != probe.metadata(os.fstat(stream.fileno())):
            raise ValueError('evidence changed during read')
        return raw


def once(path, raw, mode=0o600):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    with os.fdopen(fd, 'wb') as out:
        out.write(raw); out.flush(); os.fsync(out.fileno())
    fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try: os.fsync(fd)
    finally: os.close(fd)


def record(raw, keys):
    lines = raw.decode('ascii').splitlines()
    if len(lines) != len(keys) or not raw.endswith(b'\n'): raise ValueError('noncanonical record')
    value = {}
    for line, key in zip(lines, keys):
        if not line.startswith(key + '='): raise ValueError('record field differs')
        value[key] = line[len(key) + 1:]
    if raw != ''.join(k + '=' + value[k] + '\n' for k in keys).encode(): raise ValueError('record bytes differ')
    return value


def guarded_intent(operation):
    if not re.fullmatch('[0-9a-f]{32}', operation): raise ValueError('invalid operation')
    info = ROOT.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or stat.S_IMODE(info.st_mode) != 0o700:
        raise ValueError('unsafe fixture root')
    raw = read(ROOT / ('bound-worker-' + operation + '.json'))
    intent = probe.strict_object(raw)
    who = intent['identity']
    actual = probe.guard_guest(SimpleNamespace(lab_nonce=who['nonce'], vm_uuid=who['vm_uuid'], cell_id=who['cell_id'], node=who['node']))
    if (intent.get('schema') != 'celikpanel/bound-worker-fault-intent/v1' or who != actual
            or intent.get('operation_id') != operation
            or intent.get('recovery_fault') != {'action':'reboot','checkpoint':'payload_restored'}
            or not re.fullmatch('[0-9a-f]{40}', intent.get('target', {}).get('commit', ''))):
        raise ValueError('unrecognized bound reboot intent')
    return intent, raw


def unit_bytes(operation):
    if not re.fullmatch('[0-9a-f]{32}', operation): raise ValueError('invalid operation')
    return ('[Unit]\nDescription=Disposable bounded real boot wait probe\nAfter=local-fs.target\nBefore=multi-user.target\n'
            '[Service]\nType=oneshot\nExecStart=/usr/bin/python3 -I ' + str(HERE / 'guest_boot_wait.py') +
            ' observe --operation-id ' + operation + '\nTimeoutStartSec=150s\nUMask=0077\n'
            '[Install]\nWantedBy=multi-user.target\n').encode()


def arm(operation):
    intent, raw = guarded_intent(operation)
    if HERE != ROOT: raise ValueError('fixture must be staged in the registered private root')
    for path in (UNIT_PATH, LINK):
        if path.exists() or path.is_symlink(): raise ValueError('fixture unit already exists')
    unit = unit_bytes(operation)
    config = {'schema':SCHEMA, 'identity':intent['identity'], 'operation_id':operation,
              'armed_boot_id':boot_id(), 'intent_sha256':sha(raw), 'script_sha256':sha(read(Path(__file__), 1048576)),
              'probe_sha256':sha(read(HERE / 'guest_probe.py', 1048576)), 'unit_sha256':sha(unit), 'max_seconds':MAX_SECONDS}
    once(ROOT / ('boot-wait-' + operation + '.json'), encoded(config))
    once(UNIT_PATH, unit, 0o644)
    for argv in (['/usr/bin/systemctl','daemon-reload'], ['/usr/bin/systemctl','enable',UNIT]):
        result = run(argv)
        if result.returncode: raise ValueError('fixture unit could not be enabled')
    # enable is deliberately not --now; the genuine reset starts the fixture.
    return {'action':'armed-for-next-boot-only', 'operation_id':operation, 'max_seconds':MAX_SECONDS}


def verify_boot(config, intent, raw, current_boot):
    expected = {'schema','identity','operation_id','armed_boot_id','intent_sha256','script_sha256','probe_sha256','unit_sha256','max_seconds'}
    if (set(config) != expected or config['schema'] != SCHEMA or config['identity'] != intent['identity']
            or config['operation_id'] != intent['operation_id'] or config['intent_sha256'] != sha(raw)
            or not re.fullmatch('[0-9a-f-]{36}', config['armed_boot_id']) or current_boot == config['armed_boot_id']
            or type(config['max_seconds']) is not int or config['max_seconds'] != MAX_SECONDS):
        raise ValueError('not the sealed next-boot observation')


def material(intent):
    transaction = read(TRANSACTION / 'active', 512)
    value = record(transaction, ('version','token','operation','snapshot'))
    snapshot = value['snapshot']
    if (value['version'] != '1' or value['operation'] not in ('update','rollback')
            or not re.fullmatch('[0-9a-f]{64}', value['token'])
            or not re.fullmatch('[A-Za-z0-9][A-Za-z0-9._-]{0,127}', snapshot)
            or '-to-' + intent['target']['commit'] + '-' not in snapshot):
        raise ValueError('unexpected native transaction')
    raw = read(BINDINGS / (snapshot + '.binding'), 1024)
    bound = record(raw, ('schema','request_id','target_commit','snapshot','update_token'))
    if (bound['schema'] != 'celikpanel-recovery-binding/v1' or bound['request_id'] != intent['operation_id']
            or bound['target_commit'] != intent['target']['commit'] or bound['snapshot'] != snapshot
            or not re.fullmatch('[0-9a-f]{64}', bound['update_token'])):
        raise ValueError('snapshot is not bound to the genuine request')
    manifest = read(Path('/var/backups/celikpanel/update-snapshots') / snapshot / 'SHA256SUMS', 8388608)
    return {'active_sha256':sha(transaction),'snapshot':snapshot,'binding_sha256':sha(raw),'manifest_sha256':sha(manifest)}


def lock_free():
    fd = os.open(TRANSACTION / 'transaction.lock', os.O_RDONLY | os.O_NOFOLLOW)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1:
            raise ValueError('unsafe native lock')
        try: fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError: return False
        fcntl.flock(fd, fcntl.LOCK_UN)
        return True
    finally: os.close(fd)


def sample(intent):
    operation = intent['operation_id']; gid = grp.getgrnam('celikpanel').gr_gid
    raw = read(OBSERVATIONS / (operation + '.status'), 2048, 0o640, gid)
    status = record(raw, ('schema','request_id','target_commit','phase','terminal_proof','reason','observed_at','previous_failure'))
    wait_raw = read(OBSERVATIONS / (operation + '.wait'), 2048, 0o640, gid)
    hint = record(wait_raw, ('schema','request_id','observation_identity','observation_sha256','waiting_for'))
    identity = run(['/usr/bin/stat','-Lc','%d:%i:%s:%y:%z','--',str(OBSERVATIONS / (operation + '.status'))])
    cli_result = run(['/usr/libexec/celikpanel/recovery','status','--request-id',operation,'--json'])
    cli = probe.strict_object(cli_result.stdout)
    readiness = run(['/usr/bin/systemctl','is-system-running'])
    properties = run(['/usr/bin/systemctl','show','celikpanel-release-recovery.service','-p','ActiveState','-p','SubState','-p','MainPID','-p','ExecMainStatus','-p','InvocationID'])
    journal = run(['/usr/bin/journalctl','-b','-u','celikpanel-release-recovery.service','--no-pager','-o','json','-n','100'])
    if any(cp.returncode for cp in (identity,cli_result,properties,journal)): raise ValueError('observer command unavailable')
    if len(journal.stdout) > 262144: raise ValueError('journal evidence too large')
    rows = [probe.strict_object(line) for line in journal.stdout.splitlines()]
    service = dict(line.split('=',1) for line in properties.stdout.decode().splitlines())
    result = {'status':status,'status_sha256':sha(raw),'hint':hint,'hint_sha256':sha(wait_raw),
              'status_identity':identity.stdout.decode().strip(),'cli':cli,'readiness':readiness.stdout.decode().strip(),
              'readiness_exit':readiness.returncode,'service':service,'journal':rows,'lock_free':lock_free()}
    if read(OBSERVATIONS / (operation + '.status'), 2048, 0o640, gid) != raw: raise ValueError('observation replaced during sample')
    validate_sample(result, intent, boot_id())
    return result


def validate_sample(value, intent, current_boot):
    operation = intent['operation_id']; status,hint,cli = [value[k] for k in ('status','hint','cli')]
    if (status['schema'] != 'celikpanel-recovery-observation/v1' or status['request_id'] != operation
            or status['target_commit'] != intent['target']['commit'] or status['phase'] != 'recovering'
            or status['terminal_proof'] != 'none' or status['reason'] != 'recovery_running'
            or hint != {'schema':'celikpanel-recovery-wait/v1','request_id':operation,
                        'observation_identity':value['status_identity'],'observation_sha256':value['status_sha256'],'waiting_for':'starting'}
            or any(cli.get(k) != expected for k,expected in {'schema':'celikpanel-recovery-status/v1','request_id':operation,
                     'observation':'known','phase':'recovering','terminal_proof':'none','waiting_for':'starting',
                     'observed_at':status['observed_at'],'reason':'recovery_running'}.items())
            or cli.get('previous_failure','none') != status['previous_failure']
            or value['readiness'] != 'starting' or value['readiness_exit'] != 1 or value['lock_free'] is not True
            or value['service'].get('ActiveState') != 'inactive' or value['service'].get('MainPID') != '0'
            or value['service'].get('ExecMainStatus') != '0'):
        raise ValueError('native deferral not yet verified')
    if not any(row.get('_BOOT_ID') == current_boot.replace('-','') and row.get('MESSAGE','').startswith(WAIT_MESSAGE) for row in value['journal']):
        raise ValueError('no genuine current-boot deferral journal')


def observe(operation):
    intent, raw = guarded_intent(operation)
    config = probe.strict_object(read(ROOT / ('boot-wait-' + operation + '.json')))
    current_boot = boot_id(); verify_boot(config, intent, raw, current_boot)
    for path,key,mode in ((Path(__file__),'script_sha256',0o600),(HERE/'guest_probe.py','probe_sha256',0o600),(UNIT_PATH,'unit_sha256',0o644)):
        if sha(read(path,1048576,mode)) != config[key]: raise ValueError('sealed observer bytes changed')
    path = ROOT / ('boot-wait-' + operation + '.jsonl')
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    def emit(event, **fields):
        os.write(fd, encoded({'schema':SCHEMA,'at':probe.utc_now(),'event':event,'operation_id':operation,'boot_id':current_boot,**fields})); os.fsync(fd)
    try:
        before = material(intent); emit('boot-job-started',material=before)
        deadline = time.monotonic() + MAX_SECONDS; last_error = None
        while time.monotonic() < deadline:
            try:
                observed = sample(intent)
                if material(intent) != before: raise ValueError('native material changed while deferring')
                emit('native-wait-verified',sample=observed,material=before)
                emit('boot-job-released',reason='real-native-deferral-observed')
                return 0
            except (OSError,ValueError,subprocess.SubprocessError,probe.ProbeError) as exc:
                message = str(exc)
                if message != last_error:
                    emit('sample-unavailable',error_type=type(exc).__name__,reason=message[:160]); last_error = message
                time.sleep(.25)
        emit('boot-job-released',reason='inconclusive-timeout')
        return 3
    finally: os.close(fd)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command',choices=('arm','observe'));parser.add_argument('--operation-id',required=True)
    args=parser.parse_args()
    if args.command=='arm': print(json.dumps(arm(args.operation_id),sort_keys=True)); return 0
    return observe(args.operation_id)


if __name__=='__main__':
    try: raise SystemExit(main())
    except (ValueError,OSError,KeyError,probe.ProbeError,subprocess.SubprocessError) as exc:
        print('boot wait fixture refused: '+type(exc).__name__,file=sys.stderr); raise SystemExit(2)
