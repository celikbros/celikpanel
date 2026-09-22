#!/usr/bin/env python3
"""Read-only independent firewall generation proof in a guarded disposable VM."""
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
from datetime import datetime, timezone


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=30).stdout


def digest(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as source:
        before = os.fstat(source.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1 or not 0 < before.st_size <= 32*1024*1024:
            raise ValueError('unsafe generation file')
        raw = source.read(32*1024*1024+1)
        after = os.fstat(source.fileno())
        if (before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns):
            raise ValueError('generation file changed')
    return {'sha256': hashlib.sha256(raw).hexdigest(), 'mode': stat.S_IMODE(before.st_mode), 'uid': before.st_uid, 'gid': before.st_gid}


def observe():
    if os.geteuid() != 0 or Path('/proc/1/comm').read_text().strip() != 'systemd':
        raise ValueError('native root fixture required')
    identity = json.loads(Path('/etc/celikpanel-release-recovery-lab').read_text())
    if identity['schema'] != 'celikpanel-release-recovery-lab/v1' or identity['vm_uuid'] != Path('/sys/class/dmi/id/product_uuid').read_text().strip().lower():
        raise ValueError('not a registered disposable VM')
    proof = Path('/root/celikpanel-release-recovery-lab/firewall-generation-proof')
    generations = {}
    for label in ('a', 'b'):
        prepared = json.loads((proof/('prepared-'+label+'.json')).read_text())
        generation = prepared['generation']
        if not re.fullmatch('[a-f0-9]{64}', generation):
            raise ValueError('invalid recorded generation')
        root = Path('/usr/libexec/celikpanel/firewall')/generation
        generations[label] = {'generation': generation, 'files': {name: digest(root/name) for name in ('restore', 'runtime.manifest', 'celikpanel-firewall-restore.service')}}
    units = {}
    for name in ('celikpanel-agent.service', 'celikpanel-panel.service', 'celikpanel-firewall-restore.service'):
        raw = run('systemctl', 'show', name, '-p', 'ActiveState', '-p', 'SubState', '-p', 'Result', '-p', 'UnitFileState', '-p', 'FragmentPath')
        units[name] = dict(line.split('=', 1) for line in raw.splitlines())
    return {'schema': 'celikpanel/native-firewall-generations/v1', 'identity': identity,
            'at': datetime.now(timezone.utc).isoformat(),
            'boot_id': Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
            'management_binaries_absent': all(not os.path.lexists('/opt/celikpanel/bin/'+n) for n in ('panel','agent')),
            'generations': generations, 'units': units,
            'installed_unit': digest('/etc/systemd/system/celikpanel-firewall-restore.service'),
            'policy': digest('/etc/celikpanel/firewall.nft'),
            'tables': {n: run('/usr/sbin/nft','list','table','inet',n) for n in ('celikpanel_fw','celikpanel_lab_other')},
            'systemd': run('systemctl','--version').splitlines()[0]}


if __name__ == '__main__':
    print(json.dumps(observe(), sort_keys=True))
