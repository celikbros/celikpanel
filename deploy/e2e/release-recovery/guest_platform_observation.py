#!/usr/bin/env python3
"""Read-only same-process platform observation inside a guarded disposable VM."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess


def process_identity():
    raw = subprocess.check_output(['systemctl', 'show', 'celikpanel-agent.service',
                                  '-p', 'MainPID', '-p', 'InvocationID', '-p', 'ActiveState'], text=True)
    fields = dict(line.split('=', 1) for line in raw.splitlines())
    pid = int(fields['MainPID'])
    if fields['ActiveState'] != 'active' or pid <= 1:
        raise ValueError('Agent is not running')
    process = Path('/proc') / str(pid)
    ticks = process.joinpath('stat').read_text().rsplit(') ', 1)[1].split()[19]
    return {'pid': pid, 'start_ticks': ticks, 'invocation_id': fields['InvocationID'],
            'executable_sha256': hashlib.sha256(process.joinpath('exe').read_bytes()).hexdigest()}


def observe(nonce):
    if os.geteuid() != 0 or Path('/proc/1/comm').read_text().strip() != 'systemd':
        raise ValueError('native root fixture required')
    before = process_identity()
    boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
    state = subprocess.run(['systemctl', 'is-system-running'], capture_output=True, text=True, timeout=10).stdout.strip()
    command = ['/root/celikpanel-release-recovery-lab/bound-update-driver', '--nonce', nonce, '--mode', 'check']
    result = subprocess.run(command, check=True, capture_output=True, text=True, timeout=100)
    check = json.loads(result.stdout)
    if check.get('event') != 'support_observed' or check.get('schema') != 'celikpanel-release-recovery-update/v1':
        raise ValueError('unexpected authenticated observation')
    if process_identity() != before or Path('/proc/sys/kernel/random/boot_id').read_text().strip() != boot:
        raise ValueError('Agent or boot changed during observation')
    return {'schema': 'celikpanel/native-platform-observation/v1', 'boot_id': boot,
            'systemd_state': state, 'agent': before, 'check': check}


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--nonce', required=True)
    print(json.dumps(observe(parser.parse_args().nonce), sort_keys=True))
