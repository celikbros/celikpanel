#!/usr/bin/env python3
"""Bounded contention fixture; runs only in a registered disposable systemd guest.

Both candidate CLIs must refuse while the same native lock is held. The only
candidate restore calls are made under that lock; unlocked calls are preflight.
"""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
from datetime import datetime, timezone

ROOT = Path('/root/celikpanel-release-recovery-lab')


def require(ok, message):
    if not ok: raise ValueError(message)


def output(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=30).stdout


def run_fixture(agent_sha, boot_sha):
    require(os.geteuid() == 0 and Path('/proc/1/comm').read_text().strip() == 'systemd', 'native root guest required')
    identity = json.loads(Path('/etc/celikpanel-release-recovery-lab').read_text())
    require(identity['schema'] == 'celikpanel-release-recovery-lab/v1' and identity['vm_uuid'] == Path('/sys/class/dmi/id/product_uuid').read_text().strip().lower(), 'foreign guest')
    candidates = {'agent': ROOT / 'firewall-lock-agent', 'boot': ROOT / 'firewall-lock-boot'}
    for name, digest in (('agent', agent_sha), ('boot', boot_sha)):
        info = candidates[name].lstat()
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1, 'unsafe candidate')
        require(hashlib.sha256(candidates[name].read_bytes()).hexdigest() == digest, 'wrong candidate')
    require(all(not os.path.lexists('/opt/celikpanel/bin/' + name) for name in ('agent', 'panel')), 'ordinary management still installed')
    def state():
        return {'rules': output('/usr/sbin/nft', 'list', 'ruleset'),
                'policy_sha256': hashlib.sha256(Path('/etc/celikpanel/firewall.nft').read_bytes()).hexdigest(),
                'services': output('systemctl', 'show', 'celikpanel-agent.service', 'celikpanel-panel.service', '-p', 'ActiveState', '-p', 'MainPID')}
    before = state()
    require('ActiveState=active' not in before['services'] and 'MainPID=0' in before['services'], 'management running')
    fd = os.open('/run/celikpanel-firewall-boot/restore.lock', os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK)
    info = os.fstat(fd)
    require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1, 'unsafe lock')
    observations = []
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        for name, flag in (('agent', '--check-firewall-restore'), ('agent', '--restore-firewall'), ('boot', '--check'), ('boot', '--restore')):
            result = subprocess.run([str(candidates[name]), flag], capture_output=True, text=True, timeout=30)
            require(result.returncode == 1 and 'another firewall operation is running; observe it before retrying' in result.stderr, 'candidate did not refuse held lock')
            require(state() == before, 'held-lock invocation changed host state')
            observations.append({'candidate': name, 'flag': flag, 'exit_code': result.returncode, 'stderr': result.stderr, 'state_unchanged': True})
    finally:
        os.close(fd)
    ready = {}
    for name, flag in (('agent', '--check-firewall-restore'), ('boot', '--check')):
        ready[name] = output(str(candidates[name]), flag)
    after = state()
    require(after == before, 'preflight changed host state')
    return {'schema': 'celikpanel/native-firewall-exclusion/v1', 'identity': identity,
            'at': datetime.now(timezone.utc).isoformat(),
            'boot_id': Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
            'agent_sha256': agent_sha, 'boot_sha256': boot_sha,
            'lock_identity': {'device': info.st_dev, 'inode': info.st_ino},
            'before': before, 'after': after, 'contended': observations,
            'unlocked_preflight': ready, 'ordinary_management_absent': True}


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--agent-sha256', required=True)
    p.add_argument('--boot-sha256', required=True)
    p.add_argument('--execute', action='store_true', required=True)
    a = p.parse_args()
    print(json.dumps(run_fixture(a.agent_sha256, a.boot_sha256), sort_keys=True))
