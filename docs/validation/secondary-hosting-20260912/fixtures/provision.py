"""Provision new isolated DNS guests; never targets an installed user panel."""
import concurrent.futures
import hashlib
import json
import os
import socket
import subprocess
import time
from pathlib import Path

SOURCE = Path('/mnt/c/CELIKBROS PROJECTS/celikpanel')
ROOT = Path(os.environ.get('CELIKPANEL_SECONDARY_HOSTING_VM_ROOT', '/var/tmp/cp-secondary-hosting-20260912'))
OLD = Path('/var/tmp/cp-install-vm')
PREVIOUS = SOURCE / 'docs/validation/server-setup-profiles-20260911/fixtures'
assert ROOT.parent == Path('/var/tmp') and ROOT.name in ('cp-secondary-hosting-20260912', 'cp-secondary-hosting-20260912-v2')
assert not ROOT.exists(), 'preserve previous evidence; inspect before retrying'
base_port = 2483 if ROOT.name.endswith('-v2') else 2383
peer_port = base_port - 3
for port in (peer_port, base_port + 1, base_port + 2):
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', port))
ROOT.mkdir(mode=0o700)
original = json.loads((OLD / 'cells/febe481be1059907aca89ccd/fixture-plan.json').read_text())['nodes']['debian13']
base = Path(original['base']['path'])
with base.open('rb') as stream:
    assert hashlib.file_digest(stream, original['base']['digest']['algorithm']).hexdigest() == original['base']['digest']['value']

def run(args, **kwargs):
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, **kwargs)
    if result.returncode:
        raise RuntimeError(f'{args[0]} exit {result.returncode}: {result.stdout.decode()[-6000:]}')
    return result.stdout

pebble = Path('/var/tmp/cp-setup-profiles-20260911/pebble')
assert pebble.is_file()
nodes = {}
for index, name in enumerate(('dnsprimary', 'dnssecondary'), 1):
    folder = ROOT / name
    folder.mkdir(mode=0o700)
    mac, peer_mac = f'52:54:00:71:00:{index:02d}', f'52:54:00:71:01:{index:02d}'
    ip = '192.0.2.10' if index == 1 else '192.0.2.20'
    for key, value in original['cloud_init'].items():
        value = value.replace(original['management']['mac'], mac).replace('dns-kill-', 'profile65-').replace('dns-debian13', 'profile65-' + name)
        if key == 'network-config':
            value = value.replace('52:54:00:53:00:10', peer_mac).replace('192.0.2.10/24', ip + '/24')
        (folder / key).write_text(value)
    run(['genisoimage', '-quiet', '-output', str(folder / 'seed.iso'), '-volid', 'cidata', '-joliet', '-rock', *[str(folder / item) for item in ('user-data', 'meta-data', 'network-config')]])
    run(['qemu-img', 'create', '-f', 'qcow2', '-F', 'qcow2', '-b', str(base), str(folder / 'overlay.qcow2'), '24G'])
    name_arg = 'cp-secondary-hosting-' + name
    command = ['qemu-system-x86_64', '-name', name_arg, '-accel', 'kvm', '-cpu', 'host', '-smp', '2', '-m', '2048', '-drive', f'file={folder}/overlay.qcow2,if=virtio,format=qcow2,cache=none', '-drive', f'file={folder}/seed.iso,if=virtio,format=raw,media=cdrom,readonly=on', '-netdev', f'user,id=mgmt,hostfwd=tcp:127.0.0.1:{base_port + index}-:22', '-device', f'virtio-net-pci,netdev=mgmt,mac={mac}', '-qmp', f'unix:{folder}/qmp.sock,server=on,wait=off', '-pidfile', str(folder / 'qemu.pid'), '-serial', f'file:{folder}/serial.log', '-display', 'none', '-monitor', 'none', '-daemonize', '-netdev', 'socket,id=peer,' + ('listen' if index == 1 else 'connect') + f'=127.0.0.1:{peer_port}', '-device', 'virtio-net-pci,netdev=peer,mac=' + peer_mac]
    run(command)
    nodes[name] = {'port': base_port + index, 'peer_ip': ip, 'qemu_name': name_arg, 'qemu_command': command, 'base_digest': original['base']['digest']}
(ROOT / 'plan.json').write_text(json.dumps(nodes, indent=2))

def ssh_options():
    return ['-i', str(OLD / 'key'), '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=5', '-o', 'StrictHostKeyChecking=accept-new', '-o', f'UserKnownHostsFile={ROOT}/ssh-known-hosts']

def provision(item):
    name, row = item
    port, folder = row['port'], ROOT / name
    pid = int((folder / 'qemu.pid').read_text())
    command = Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
    assert row['qemu_name'].encode() in command
    assert any(('file=' + str(folder / 'overlay.qcow2')).encode() in arg for arg in command)
    ssh = ['ssh', *ssh_options(), '-p', str(port), 'celik@127.0.0.1']
    for attempt in range(90):
        result = subprocess.run(ssh + ['cloud-init status --wait'], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
        if result.returncode in (0, 2):
            break
        time.sleep(3)
    else:
        raise RuntimeError(name + ' cloud-init timeout')
    (folder / 'cloud-init.log').write_bytes(result.stdout)
    run(['scp', *ssh_options(), '-P', str(port), str(pebble), 'celik@127.0.0.1:/tmp/pebble'])
    script = (PREVIOUS / 'setup-dns-profile-guest.sh').read_text().replace('__PROFILE__', name).replace('__PEER_IP__', row['peer_ip'])
    script += "\nprintf 'secondary-hosting-20260912\\n' > /var/lib/celikpanel-profile-vm/secondary-hosting-fixture\n"
    script += "printf '192.0.2.10 panel.dnsprimary.setup.test\\n192.0.2.20 panel.dnssecondary.setup.test\\n' >> /etc/hosts\n"
    with (folder / 'infra.log').open('wb') as log:
        result = subprocess.run(ssh + ['sudo bash -s'], input=script.encode(), stdout=log, stderr=subprocess.STDOUT, timeout=900)
    assert result.returncode == 0, (folder / 'infra.log').read_text()[-6000:]
    print(name + ' infrastructure ready', flush=True)

with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
    list(pool.map(provision, nodes.items()))

# Both guests trust the peer's isolated issuance root for real certificate
# verification. No private key or root is transferred to a production host.
for name, row in nodes.items():
    root = run(['ssh', *ssh_options(), '-p', str(row['port']), 'celik@127.0.0.1', 'sudo cat /var/lib/celikpanel-profile-vm/issued-root.crt'])
    (ROOT / (name + '-issued-root.crt')).write_bytes(root)
for name, row in nodes.items():
    peer = 'dnssecondary' if name == 'dnsprimary' else 'dnsprimary'
    run(['scp', *ssh_options(), '-P', str(row['port']), str(ROOT / (peer + '-issued-root.crt')), 'celik@127.0.0.1:/tmp/peer-issued-root.crt'])
    run(['ssh', *ssh_options(), '-p', str(row['port']), 'celik@127.0.0.1', "sudo bash -c 'test ! -e /opt/celikpanel/bin/panel && install -m 0644 /tmp/peer-issued-root.crt /usr/local/share/ca-certificates/celikpanel-peer-fixture.crt && update-ca-certificates'"])
print('SECONDARY_HOSTING_INFRA_READY', flush=True)
