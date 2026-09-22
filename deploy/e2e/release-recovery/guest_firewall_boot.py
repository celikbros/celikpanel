#!/usr/bin/env python3
"""Read-only observation for the independent firewall consumer's disposable fixture.

Invocation is additionally protected by lab.guarded_script's registered QEMU,
SSH host key, nonce and DMI identity checks. Never starts a service or update.
"""
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
from datetime import datetime, timezone


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=30).stdout


def file_info(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as source:
        before = os.fstat(source.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1 or before.st_size > 16 * 1024 * 1024:
            raise ValueError("unsafe fixture file")
        raw = source.read(16 * 1024 * 1024 + 1)
        after = os.fstat(source.fileno())
        if (before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns):
            raise ValueError("fixture file changed")
    return {"sha256": hashlib.sha256(raw).hexdigest(), "uid": before.st_uid,
            "gid": before.st_gid, "mode": stat.S_IMODE(before.st_mode), "size": len(raw)}


def observe():
    if os.geteuid() != 0 or Path('/proc/1/comm').read_text().strip() != 'systemd':
        raise ValueError('native root fixture required')
    identity = json.loads(Path('/etc/celikpanel-release-recovery-lab').read_text())
    if identity['schema'] != 'celikpanel-release-recovery-lab/v1' or identity['vm_uuid'] != Path('/sys/class/dmi/id/product_uuid').read_text().strip().lower():
        raise ValueError('not the registered disposable guest')
    helper = '/usr/local/libexec/celikpanel-firewall-boot-fixture'
    units = {}
    for name in ('celikpanel-agent.service', 'celikpanel-panel.service', 'celikpanel-lab-native-firewall.service'):
        output = run('systemctl', 'show', name, '-p', 'ActiveState', '-p', 'MainPID', '-p', 'Result', '-p', 'UnitFileState')
        units[name] = dict(line.split('=', 1) for line in output.splitlines())
    return {"schema": "celikpanel/native-firewall-observation/v1", "identity": identity,
            "at": datetime.now(timezone.utc).isoformat(),
            "boot_id": Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
            "helper": file_info(helper), "policy": file_info('/etc/celikpanel/firewall.nft'),
            "management_binaries_absent": all(not os.path.lexists('/opt/celikpanel/bin/' + name) for name in ('panel', 'agent')),
            "units": units,
            "tables": {name: run('/usr/sbin/nft', 'list', 'table', 'inet', name) for name in ('celikpanel_fw', 'celikpanel_lab_other')},
            "preflight": run(helper, '--check'),
            "systemd": run('systemctl', '--version').splitlines()[0],
            "nft": run('/usr/sbin/nft', '--version').strip(),
            "unit_sha256": file_info('/etc/systemd/system/celikpanel-lab-native-firewall.service')['sha256']}


if __name__ == '__main__':
    print(json.dumps(observe(), sort_keys=True))
