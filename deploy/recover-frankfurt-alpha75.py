#!/usr/bin/env python3
"""Restore existing Alpha75 services after the exact September 13 TLS failure.
Yalnız 13 Eylül TLS hatasının mevcut Alpha75 servislerini yeniden açar.
No installation, database restore, TLS rewrite or release retry is performed.
Kurulum, veritabanı geri yükleme, TLS değişikliği veya güncelleme tekrarı yapmaz.
"""
import argparse
from contextlib import closing
import fcntl
import hashlib
import os
from pathlib import Path
import re
import socket
import sqlite3
import stat
import subprocess
import sys
import time

TX = Path('/var/lib/celikpanel-release-transaction')
SNAPS = Path('/var/backups/celikpanel/update-snapshots')
NAME = '20260913T190800Z-from-unknown-to-9c55f235d569a91264304a74cf09b26c83123e68-cfc80563db4889a798d1254959a81ae6'
STAGE = SNAPS / '.release-snapshot.incomplete.1071613.cfc80563db4889a798d1254959a81ae6'
CHILD = STAGE / NAME
RELEASES = Path('/var/backups/celikpanel/releases')
EVIDENCE = Path('/var/backups/celikpanel/frankfurt-recovery-20260913T190800Z')
BIN = Path('/opt/celikpanel/bin')
WEB = Path('/opt/celikpanel/web')
DB = Path('/var/lib/celikpanel/celikpanel.db')
AGENT_STATE = Path('/var/lib/celikpanel-agent-private')
RUNTIME = Path('/run/celikpanel')
LOCK = RUNTIME / 'service-mutation.lock'
SOCKET = RUNTIME / 'agent.sock'
PROGRAMS = {
    'agent': 'e8e69c520cf4112a5c60380378b8347021aaa035cdaacd820c9927436beff078',
    'panel': '568a7b15f20e5666fced0dcec77f52e83e596efd6e0969d3bf05508faa485d05',
}
WEB_SHA = 'c529af99219b698bfd34fb20fb373a942fc2b24ed98edbc8470a2f0006794430'
GUARD_SHA = '666373620d35ef5f5826c38eab4732fdba5954aa6fa2a503b00e8a49ce752595'
SERVICES = (b'celikpanel-agent.service\tenabled\tactive\n'
            b'celikpanel-panel.service\tenabled\tactive\n'
            b'celikpanel-firewall-restore.service\tenabled\tinactive\n')
COORDINATORS = (b'celikpanel-agent.service\tactive\t856154\t40957114\n'
                b'celikpanel-panel.service\tactive\t858903\t40985057\n')
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL': 'C'}
UNITS = ['celikpanel-agent.service', 'celikpanel-panel.service']


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def trusted(path, directory=False, mode=None, uid=0):
    info = path.lstat()
    require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode),
            'Unsafe object type: ' + str(path))
    require(info.st_uid == uid and not info.st_mode & 0o022, 'Unsafe owner/mode: ' + str(path))
    require(directory or info.st_nlink == 1, 'Hard-linked file: ' + str(path))
    require(mode is None or stat.S_IMODE(info.st_mode) == mode, 'Unexpected permissions: ' + str(path))
    require(path.resolve() == path, 'Aliased path: ' + str(path))
    return info


def trusted_chain(path):
    for item in [path, *path.parents]:
        trusted(item, directory=True)


def command(args, **kwargs):
    return subprocess.run(args, check=True, timeout=60, env=kwargs.pop('env', ENV),
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE, **kwargs).stdout


def unit_info(unit):
    text = command(['/usr/bin/systemctl', 'show', unit, '-p', 'ActiveState',
                    '-p', 'MainPID', '-p', 'ControlGroup', '-p', 'Job',
                    '-p', 'RuntimeDirectoryPreserve', '-p', 'LoadState']).decode()
    return dict(line.split('=', 1) for line in text.splitlines())


def verify_stopped():
    for unit in UNITS:
        info = unit_info(unit)
        require(info['LoadState'] == 'loaded' and info['MainPID'] == '0'
                and info['ActiveState'] in ('inactive', 'failed') and not info['Job'],
                'Coordinator is not stopped without a queued job: ' + unit)
        if unit == UNITS[0]:
            require(info['RuntimeDirectoryPreserve'] == 'yes', 'Agent runtime directory preservation is required')
        group = info['ControlGroup']
        if group:
            require(group == '/system.slice/' + unit, 'Unexpected cgroup: ' + unit)
            root = Path('/sys/fs/cgroup' + group)
            if root.exists():
                for procs in root.rglob('cgroup.procs'):
                    require(not procs.read_text().strip(), 'Residual coordinator processes: ' + unit)
    for pid, started in ((856154, '40957114'), (858903, '40985057')):
        proc = Path('/proc') / str(pid) / 'stat'
        if proc.exists():
            require(proc.read_text().rsplit(') ', 1)[1].split()[19] != started,
                    'Original coordinator still exists')


def verify_programs():
    trusted_chain(BIN)
    for name, expected in PROGRAMS.items():
        trusted(BIN / name)
        require(sha(BIN / name) == expected, 'Installed executable differs from Alpha75: ' + name)
    trusted_chain(WEB)
    rows = []
    for item in WEB.rglob('*'):
        directory = item.is_dir()
        trusted(item, directory=directory)
        rel = item.relative_to(WEB).as_posix()
        rows.append('D ' + rel + '\n' if directory else 'F ' + sha(item) + ' ' + rel + '\n')
    require(hashlib.sha256(''.join(sorted(rows)).encode()).hexdigest() == WEB_SHA,
            'Installed web files differ from the published Alpha75 archive')


def verify_transaction():
    trusted_chain(TX)
    trusted(TX, directory=True, mode=0o700)
    require({p.name for p in TX.iterdir()} == {'active', 'transaction.lock'},
            'Expected only the exact active marker and transaction lock')
    trusted(TX / 'active', mode=0o600)
    marker = (TX / 'active').read_bytes()
    match = re.fullmatch(rb'version=1\ntoken=([0-9a-f]{64})\noperation=update\nsnapshot=' + NAME.encode() + rb'\n', marker)
    require(match is not None, 'Active marker is not the reviewed incident')
    return marker, match[1].decode()


def verify_stage():
    trusted_chain(SNAPS)
    require(not os.path.lexists(SNAPS / NAME), 'Final snapshot exists; pre-mutation abort is forbidden')
    require(list(SNAPS.glob('.release-snapshot.incomplete*')) == [STAGE], 'Unexpected incomplete snapshot stage')
    trusted(STAGE, directory=True, mode=0o700)
    require(list(STAGE.iterdir()) == [CHILD], 'Unexpected stage child')
    trusted(CHILD, directory=True, mode=0o700)
    expected = {'service-states.tsv', 'quiesce-coordinators.tsv', 'snapshot-transition.state',
                'agent-state', 'agent-state/service-mutations.json', 'agent-state-root',
                'celikpanel.db', 'agent-ledger.state'}
    actual = {p.relative_to(CHILD).as_posix() for p in CHILD.rglob('*')}
    require(actual == expected, 'Snapshot contains data beyond the reviewed pre-TLS stage')
    for rel in expected:
        trusted(CHILD / rel, directory=rel == 'agent-state', mode=0o700 if rel == 'agent-state' else 0o600)
    require((CHILD / 'service-states.tsv').read_bytes() == SERVICES, 'Saved service states changed')
    require((CHILD / 'quiesce-coordinators.tsv').read_bytes() == COORDINATORS, 'Saved coordinator identities changed')
    require((CHILD / 'snapshot-transition.state').read_bytes() == b'normal\n', 'Not a normal-ledger update')
    require((CHILD / 'agent-state-root').read_bytes() == str(AGENT_STATE).encode() + b'\n', 'Agent state root changed')
    require((CHILD / 'agent-ledger.state').read_bytes() == b'present\n', 'Agent ledger presence changed')
    ledger = AGENT_STATE / 'service-mutations.json'
    trusted_chain(AGENT_STATE)
    trusted(ledger, mode=0o600)
    require(sha(ledger) == sha(CHILD / 'agent-state/service-mutations.json'), 'Agent ledger differs from its stopped snapshot')


def database_digest(path):
    require(path.is_file() and not path.is_symlink(), 'Unsafe database path')
    # Read the live WAL-aware database; never restore or migrate either database.
    # Canlı WAL dahil okunur; hiçbir veritabanı geri yüklenmez veya taşınmaz.
    with closing(sqlite3.connect(path.as_uri() + '?mode=ro', uri=True, timeout=3)) as connection:
        connection.execute('PRAGMA query_only=ON')
        require(connection.execute('PRAGMA quick_check').fetchall() == [('ok',)], 'Database integrity check failed')
        digest = hashlib.sha256()
        for statement in connection.iterdump():
            digest.update(statement.encode())
            digest.update(b'\n')
        return digest.hexdigest()


def verify_data():
    require(database_digest(DB) == database_digest(CHILD / 'celikpanel.db'),
            'Live database differs from the pre-mutation snapshot')


def held_lock(path, uid, gid, mode):
    info = trusted(path, mode=mode, uid=uid)
    require(info.st_gid == gid and info.st_size == 0, 'Unexpected lock metadata: ' + str(path))
    fd = os.open(path, os.O_RDWR | os.O_NOFOLLOW)
    current = os.fstat(fd)
    require((info.st_dev, info.st_ino) == (current.st_dev, current.st_ino), 'Lock identity changed')
    fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    return fd


def verify_idle(mutation_fd):
    env = dict(ENV, CELIKPANEL_AGENT_STATE_DIR=str(AGENT_STATE),
               CELIKPANEL_MUTATION_LOCK=str(LOCK),
               CELIKPANEL_MUTATION_LOCK_FD=str(mutation_fd))
    command([str(BIN / 'agent'), '--check-service-mutation-idle-under-external-lock'],
            pass_fds=(mutation_fd,), env=env)
    command([str(BIN / 'panel'), '--check-service-operations-idle-wal-aware'],
            env=dict(ENV, CELIKPANEL_DATA_DIR=str(DB.parent)))


def verify_lock_identity(path, fd):
    now, held = path.lstat(), os.fstat(fd)
    require(stat.S_ISREG(now.st_mode) and (now.st_dev, now.st_ino) == (held.st_dev, held.st_ino),
            'Held lock path changed: ' + str(path))
    fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)


def find_guard():
    trusted_chain(RELEASES)
    candidates = list(RELEASES.glob('9c55f235d569-*'))
    require(len(candidates) == 1, 'Need exactly one retained Alpha77 release')
    root = candidates[0]
    trusted(root, directory=True, mode=0o700)
    guard = root / 'deploy/release-transaction-guard.sh'
    trusted(guard)
    require(sha(guard) == GUARD_SHA, 'Retained transaction guard does not match the published source')
    for item in root.rglob('*'):
        trusted(item, directory=item.is_dir())
    # Reproduce the retained release's complete canonical manifest, then source
    # only the independently pinned transaction guard.
    # Saklanan sürümün tam manifestini doğrula, yalnız sabit hash'li korumayı yükle.
    manifest = command(['/bin/bash', '-c',
        "set -euo pipefail; cd -- \"$1\"; find . -type f ! -path './SHA256SUMS' -print0 | sort -z | xargs -0 sha256sum", 'verify', str(root)])
    require(manifest == (root / 'SHA256SUMS').read_bytes(), 'Retained release manifest mismatch')
    require((root / 'release.commit').read_bytes() == b'9c55f235d569a91264304a74cf09b26c83123e68\n', 'Wrong retained release commit')
    return root


def sync_path(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def save_evidence(marker):
    trusted_chain(EVIDENCE.parent)
    require(not os.path.lexists(EVIDENCE), 'Recovery evidence already exists; inspect it before retrying')
    EVIDENCE.mkdir(mode=0o700)
    with (EVIDENCE / 'active.before').open('xb') as stream:
        stream.write(marker)
        stream.flush()
        os.fsync(stream.fileno())
    sync_path(EVIDENCE)
    sync_path(EVIDENCE.parent)


def start_existing(name):
    unit = 'celikpanel-' + name + '.service'
    command(['/usr/bin/systemctl', 'start', unit])
    for _ in range(50):
        info = unit_info(unit)
        if info['ActiveState'] == 'active' and int(info['MainPID']) > 0:
            exe = Path('/proc') / info['MainPID'] / 'exe'
            # Type=simple can report active before the child has executed the
            # program. Wait for the exact executable and stable PID, never
            # accept an intermediate launcher as proof of readiness.
            # Type=simple, alt süreç programı çalıştırmadan active bildirebilir.
            # Tam program ve kararlı PID beklenir; ara süreç hazır sayılmaz.
            try:
                matches = exe.resolve() == BIN / name and sha(exe) == PROGRAMS[name]
                current = unit_info(unit) if matches else {}
                stable = current.get('MainPID') == info['MainPID'] and current.get('ActiveState') == 'active'
                if matches and stable and (name == 'panel' or SOCKET.is_socket()):
                    return
            except FileNotFoundError:
                pass
        time.sleep(0.2)
    raise RuntimeError('Existing service executable/PID/socket did not become ready: ' + unit)


def run(restore=False):
    require(os.geteuid() == 0, 'Run as root')
    require(socket.gethostname().split('.')[0] == 'frankfurt', 'This recovery is only for Frankfurt')
    os.umask(0o077)
    marker, token = verify_transaction()
    txfd = held_lock(TX / 'transaction.lock', 0, 0, 0o600)
    mutation_fd = None
    try:
        trusted_chain(RUNTIME)
        import grp
        mutation_fd = held_lock(LOCK, 0, grp.getgrnam('celikpanel').gr_gid, 0o600)
        verify_stopped()
        verify_programs()
        verify_stage()
        verify_idle(mutation_fd)
        verify_data()
        root = find_guard()
        require(verify_transaction()[0] == marker, 'Transaction changed during proof')
        if not restore:
            print('PASS: exact pre-mutation incident; Alpha75 programs, web, database and ledger match. No service was started.')
            return
        # All proofs precede the durable marker removal. Never rerun update.sh.
        # Bütün kanıtlar kalıcı işaretçi kaldırılmadan alınır; update.sh çalışmaz.
        save_evidence(marker)
        verify_stopped()
        verify_stage()
        verify_programs()
        verify_data()
        verify_idle(mutation_fd)
        verify_lock_identity(TX / 'transaction.lock', txfd)
        verify_lock_identity(LOCK, mutation_fd)
        require(verify_transaction()[0] == marker, 'Transaction changed before abort')
        if os.path.lexists(SOCKET):
            require(stat.S_ISSOCK(SOCKET.lstat().st_mode), 'Unexpected agent socket object')
            SOCKET.unlink()
        script = ('set -euo pipefail; TRUSTED_RELEASE_ROOT="$1"; '
                  'source "$1/deploy/release-transaction-guard.sh"; '
                  'release_txn_remove_pre_mutation_active_marker "$2" "$3" "$4" update "$5" "$6" "$7"')
        command(['/bin/bash', '-c', script, 'abort', str(root), str(TX), str(txfd),
                 token, NAME, str(SNAPS), str(STAGE)], pass_fds=(txfd,))
        require({p.name for p in TX.iterdir()} == {'transaction.lock'}, 'Transaction marker remains after abort')
        # Keep the stopped snapshot as evidence outside active snapshot storage.
        # Durmuş snapshot etkin yedek deposunun dışında kanıt olarak saklanır.
        require(STAGE.stat().st_dev == EVIDENCE.stat().st_dev, 'Evidence must be on the same filesystem')
        STAGE.rename(EVIDENCE / 'snapshot-stage')
        sync_path(EVIDENCE)
        sync_path(SNAPS)
        verify_lock_identity(LOCK, mutation_fd)
        start_existing('agent')
        start_existing('panel')
        print('RECOVERED: existing Alpha75 agent and panel are active. No update was installed.')
        print('Evidence: ' + str(EVIDENCE))
    finally:
        if mutation_fd is not None:
            os.close(mutation_fd)
        os.close(txfd)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--restore', action='store_true', help='After all checks, restore the existing Alpha75 services')
    arguments = parser.parse_args()
    try:
        run(arguments.restore)
    except (RuntimeError, OSError, sqlite3.Error, subprocess.SubprocessError) as error:
        # Subprocess output may contain secrets. Print the bounded failure class.
        # Alt süreç çıktısı gizli bilgi içerebilir; sınırlı hata sınıfını göster.
        if isinstance(error, subprocess.CalledProcessError):
            tool = Path(error.cmd[0]).name
            message = tool + ' failed with exit status ' + str(error.returncode)
        elif isinstance(error, subprocess.TimeoutExpired):
            message = Path(error.cmd[0]).name + ' timed out'
        else:
            message = str(error)
        print('RECOVERY STOPPED: ' + message, file=sys.stderr)
        print('Restoration is not confirmed. Preserve the evidence directory and report this output; do not start another update.', file=sys.stderr)
        sys.exit(1)
