#!/usr/bin/env python3
"""Explicit fixture trust for a fresh disposable native-worker trial.

This origin is guest-loopback only. It signs unpublished test artifacts with a
new lab key, never a production key, and does not manufacture operation state.
Production-signed release admission is outside this fixture's evidence scope.
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import http.server
import importlib.util
import json
import os
from pathlib import Path
import re
import ssl
import stat
import subprocess
import sys

HERE = Path(__file__).resolve().parent
GUEST_ROOT = Path('/root/celikpanel-release-recovery-lab')
INTENT = GUEST_ROOT / 'worker-origin-intent.json'
SCHEMA = 'celikpanel/worker-fixture-origin/v1'
PREFIX = 'worker-origin-'
MAX_ARCHIVE = 100 * 1024 * 1024


def module(name):
    spec = importlib.util.spec_from_file_location('worker_origin_' + name, HERE / (name + '.py'))
    value = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = value
    spec.loader.exec_module(value)
    return value


def run(argv):
    return subprocess.run(argv, check=True, capture_output=True, timeout=60)


def digest(path):
    value = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b''):
            value.update(chunk)
    return value.hexdigest()


def private_write(path, raw):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())


def read_file(path, maximum=16384, mode=0o600):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        before = os.fstat(stream.fileno())
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_gid != 0
                or stat.S_IMODE(before.st_mode) != mode or before.st_nlink != 1
                or not 0 < before.st_size <= maximum):
            raise ValueError('unsafe fixture file: ' + path.name)
        raw = stream.read(maximum + 1)
        after = os.fstat(stream.fileno())
        if ((before.st_dev, before.st_ino, before.st_mtime_ns, before.st_ctime_ns, before.st_size)
                != (after.st_dev, after.st_ino, after.st_mtime_ns, after.st_ctime_ns, after.st_size)
                or len(raw) != before.st_size):
            raise ValueError('fixture file changed')
        return raw


def identity(root, node_name):
    lab = module('lab')
    root = lab.checked_root(root)
    record, plan = lab.load(root)
    node = plan['nodes'][node_name]
    value = {'schema': lab.SCHEMA, 'nonce': record['nonce'], 'cell_id': record['cell_id'],
             'node': node_name, 'vm_uuid': node['qemu_command'][node['qemu_command'].index('-uuid') + 1]}
    return lab, root, record, plan, value


def prepare_keys(root, node_name):
    """Once-only host material creation; baseline may stage only the public key."""
    if os.geteuid() != 0:
        raise ValueError('fixture host must run as root')
    _, root, _, _, who = identity(root, node_name)
    directory = root / 'worker-origin'
    directory.mkdir(mode=0o700)
    names = {name: directory / (PREFIX + name + '.pem') for name in
             ('signing', 'public', 'ca-key', 'ca', 'tls-key', 'tls')}
    run(['openssl', 'genpkey', '-algorithm', 'ED25519', '-out', str(names['signing'])])
    run(['openssl', 'pkey', '-in', str(names['signing']), '-pubout', '-out', str(names['public'])])
    run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '3',
         '-subj', '/CN=CelikPanel disposable worker fixture ' + who['nonce'][:12],
         '-addext', 'basicConstraints=critical,CA:TRUE', '-addext', 'keyUsage=critical,keyCertSign,cRLSign',
         '-keyout', str(names['ca-key']), '-out', str(names['ca'])])
    csr = directory / 'worker-origin-tls.csr'
    run(['openssl', 'req', '-new', '-newkey', 'rsa:2048', '-nodes', '-subj', '/CN=celikpanel.net',
         '-keyout', str(names['tls-key']), '-out', str(csr)])
    extension = directory / 'worker-origin-tls.ext'
    private_write(extension, b'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\nsubjectAltName=DNS:celikpanel.net\n')
    run(['openssl', 'x509', '-req', '-in', str(csr), '-CA', str(names['ca']),
         '-CAkey', str(names['ca-key']), '-set_serial', '1', '-days', '3',
         '-extfile', str(extension), '-out', str(names['tls'])])
    for path in directory.iterdir():
        path.chmod(0o600)
    material = {'schema': SCHEMA, 'identity': who,
                'files': {path.name: digest(path) for path in names.values()}}
    private_write(directory / 'keys.json', (json.dumps(material, sort_keys=True) + '\n').encode())
    return {'public_key': str(names['public']), 'directory': str(directory),
            'public_key_sha256': material['files'][names['public'].name]}


def prepare(root, node_name, archive, version, commit, sequence, repository):
    """Seal a committed local artifact using the already-created test key."""
    _, root, _, _, who = identity(root, node_name)
    directory = root / 'worker-origin'
    material = json.loads(read_file(directory / 'keys.json'))
    if material['schema'] != SCHEMA or material['identity'] != who:
        raise ValueError('fixture key identity differs')
    if set(material['files']) != {PREFIX + name + '.pem' for name in ('signing', 'public', 'ca-key', 'ca', 'tls-key', 'tls')}:
        raise ValueError('fixture material inventory differs')
    for name, expected in material['files'].items():
        if not re.fullmatch(r'worker-origin-[a-z-]+\.pem', name):
            raise ValueError('invalid fixture material name')
        if hashlib.sha256(read_file(directory / name)).hexdigest() != expected:
            raise ValueError('fixture key material changed')
    if (not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?', version)
            or not re.fullmatch(r'[0-9a-f]{40}', commit) or type(sequence) is not int
            or not 0 < sequence < 2**63):
        raise ValueError('invalid fixture release identity')
    archive = Path(archive)
    source = module('candidate_archive')
    candidate = source.inspect_archive(archive, digest(archive))
    if candidate['version'] != version or candidate['commit'] != commit:
        raise ValueError('candidate version/commit differs')
    proof = source.verify_committed_source(candidate, Path(repository))
    target = directory / 'worker-origin-archive.tar.gz'
    private_write(target, read_file(archive, MAX_ARCHIVE, stat.S_IMODE(archive.stat().st_mode)))
    if digest(target) != candidate['archive_sha256']:
        raise ValueError('copied archive changed')
    archive_name = 'celikpanel-' + version + '-linux-amd64.tar.gz'
    fields = [('format', 'celikpanel-release-manifest-v2'), ('sequence', str(sequence)),
              ('version', version), ('commit', commit),
              ('published_at', dt.datetime.now(dt.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')),
              ('os', 'linux'), ('arch', 'amd64'), ('archive', archive_name),
              ('archive_sha256', candidate['archive_sha256']), ('archive_size', str(candidate['archive_size']))]
    manifest = directory / 'worker-origin-manifest'
    private_write(manifest, ''.join(key + '=' + value + '\n' for key, value in fields).encode())
    signature = directory / 'worker-origin-signature'
    run(['openssl', 'pkeyutl', '-sign', '-rawin', '-inkey', str(directory / 'worker-origin-signing.pem'),
         '-in', str(manifest), '-out', str(signature)])
    signature.chmod(0o600)
    run(['openssl', 'pkeyutl', '-verify', '-pubin', '-rawin', '-inkey', str(directory / 'worker-origin-public.pem'),
         '-in', str(manifest), '-sigfile', str(signature)])
    private_write(directory / 'worker-origin-latest', (version + '\n').encode())
    base = '/releases/' + version + '/linux/amd64/'
    routes = {'/releases/latest.txt': 'worker-origin-latest', base + 'release-manifest-v2': manifest.name,
              base + 'release-manifest-v2.sig': signature.name, base + archive_name: target.name}
    served = set(routes.values()) | {'worker-origin-public.pem', 'worker-origin-ca.pem',
                                   'worker-origin-tls.pem', 'worker-origin-tls-key.pem'}
    intent = {'schema': SCHEMA, 'provenance': 'unpublished-local-artifact-with-disposable-fixture-trust',
              'identity': who, 'target': dict(fields), 'source_proof': proof, 'routes': routes,
              'files': {name: {'sha256': digest(directory / name), 'size': (directory / name).stat().st_size}
                        for name in sorted(served)}}
    private_write(directory / 'worker-origin-intent.json', (json.dumps(intent, sort_keys=True) + '\n').encode())
    return intent


def stage(root, node_name):
    """Upload sealed material only to the registered private guest payload root."""
    lab, root, record, plan, who = identity(root, node_name)
    directory = root / 'worker-origin'
    intent = json.loads(read_file(directory / 'worker-origin-intent.json'))
    validate_intent(intent)
    if intent['identity'] != who:
        raise ValueError('origin guest identity differs')
    for name, expected in intent['files'].items():
        path = directory / name
        if digest(path) != expected['sha256'] or path.stat().st_size != expected['size']:
            raise ValueError('origin payload changed')
        lab.put_file(root, record, plan, node_name, path, name)
    lab.put_file(root, record, plan, node_name, directory / 'worker-origin-intent.json', 'worker-origin-intent.json')
    lab.put_file(root, record, plan, node_name, Path(__file__), 'worker-fixture-origin.py')
    return {'intent': str(INTENT), 'helper': str(GUEST_ROOT / 'worker-fixture-origin.py')}


def guest_intent(path, nonce):
    if os.geteuid() != 0 or Path(path) != INTENT or not re.fullmatch('[0-9a-f]{64}', nonce):
        raise ValueError('fixed disposable guest origin is required')
    for parent in (Path('/root'), GUEST_ROOT):
        item = parent.lstat()
        if (not stat.S_ISDIR(item.st_mode) or item.st_uid != 0 or item.st_gid != 0
                or item.st_mode & 0o077):
            raise ValueError('unsafe private origin directory')
    marker = json.loads(read_file(Path('/etc/celikpanel-release-recovery-lab'), 2048, 0o444))
    intent = json.loads(read_file(INTENT))
    if (intent.get('schema') != SCHEMA or intent.get('identity') != marker
            or marker.get('schema') != 'celikpanel-release-recovery-lab/v1'
            or marker.get('nonce') != nonce
            or Path('/sys/class/dmi/id/product_uuid').read_text().strip().lower() != marker.get('vm_uuid')
            or Path('/sys/class/dmi/id/sys_vendor').read_text().strip() != 'QEMU'
            or Path('/proc/1/comm').read_text().strip() != 'systemd'):
        raise ValueError('origin requires exact marked QEMU guest')
    validate_intent(intent)
    for name, wanted in intent['files'].items():
        raw = read_file(GUEST_ROOT / name, MAX_ARCHIVE)
        if len(raw) != wanted['size'] or hashlib.sha256(raw).hexdigest() != wanted['sha256']:
            raise ValueError('sealed origin payload differs')
    return intent


def validate_intent(intent):
    target = intent['target']
    version = target['version']
    if (not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?', version)
            or not re.fullmatch(r'[0-9a-f]{40}', target['commit'])):
        raise ValueError('invalid origin release identity')
    base = '/releases/' + version + '/linux/amd64/'
    expected = {'/releases/latest.txt': 'worker-origin-latest', base + 'release-manifest-v2': 'worker-origin-manifest',
                base + 'release-manifest-v2.sig': 'worker-origin-signature',
                base + 'celikpanel-' + version + '-linux-amd64.tar.gz': 'worker-origin-archive.tar.gz'}
    files = set(expected.values()) | {'worker-origin-public.pem', 'worker-origin-ca.pem',
                                      'worker-origin-tls.pem', 'worker-origin-tls-key.pem'}
    if intent['routes'] != expected or set(intent['files']) != files:
        raise ValueError('origin whitelist differs')
    for item in intent['files'].values():
        if (set(item) != {'sha256', 'size'} or not re.fullmatch('[0-9a-f]{64}', item['sha256'])
                or type(item['size']) is not int or not 0 < item['size'] <= MAX_ARCHIVE):
            raise ValueError('invalid sealed origin file')


def provision(path, nonce):
    """Guest-only trust/network fixture. Does not enroll release trust or update."""
    intent = guest_intent(path, nonce)
    if not Path('/usr/sbin/update-ca-certificates').is_file():
        raise ValueError('this fixture trust setup supports Debian only')
    hosts = Path('/etc/hosts')
    before = read_file(hosts, 65536, 0o644)
    if any('celikpanel.net' in line.split('#', 1)[0].split()[1:] for line in before.decode().splitlines()):
        raise ValueError('origin mapping already exists; no retry mutation')
    private_write(GUEST_ROOT / 'worker-origin-hosts.before', before)
    certificate = Path('/usr/local/share/ca-certificates') / ('celikpanel-worker-fixture-' + nonce[:16] + '.crt')
    raw = read_file(GUEST_ROOT / 'worker-origin-ca.pem')
    private_write(certificate, raw)
    certificate.chmod(0o644)
    run(['/usr/sbin/update-ca-certificates'])
    # The prior exact bytes and intent are durable before this one-time lab-only mapping.
    with hosts.open('ab') as stream:
        stream.write(b'\n127.0.0.1 celikpanel.net # disposable CelikPanel worker fixture\n')
        stream.flush()
        os.fsync(stream.fileno())
    private_write(GUEST_ROOT / 'worker-origin-provisioned.json', (json.dumps({
        'schema': SCHEMA, 'nonce': nonce, 'intent_sha256': digest(INTENT),
        'hosts_before_sha256': hashlib.sha256(before).hexdigest(), 'hosts_after_sha256': digest(hosts),
        'ca_sha256': hashlib.sha256(raw).hexdigest()}) + '\n').encode())
    return {'origin': 'https://celikpanel.net', 'bind': '127.0.0.1:443',
            'provenance': intent['provenance'], 'release_key_enrolled': False}


def make_handler(root, intent):
    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            name = intent['routes'].get(self.path)
            if self.headers.get('Host') not in ('celikpanel.net', 'celikpanel.net:443') or name is None:
                self.send_error(404)
                return
            expected = intent['files'][name]
            try:
                raw = read_file(root / name, MAX_ARCHIVE)
                if len(raw) != expected['size'] or hashlib.sha256(raw).hexdigest() != expected['sha256']:
                    raise ValueError('origin payload changed')
            except (OSError, ValueError):
                self.send_error(503)
                return
            self.send_response(200)
            self.send_header('Content-Type', 'application/octet-stream')
            self.send_header('Content-Length', str(len(raw)))
            self.send_header('Cache-Control', 'no-store')
            self.end_headers()
            self.wfile.write(raw)

        def log_message(self, fmt, *args):
            # Request paths can be arbitrary; never echo them into evidence.
            pass
    return Handler


def serve(path, nonce):
    intent = guest_intent(path, nonce)
    evidence = json.loads(read_file(GUEST_ROOT / 'worker-origin-provisioned.json'))
    if evidence['nonce'] != nonce or evidence['intent_sha256'] != digest(INTENT):
        raise ValueError('origin trust provisioning not confirmed')
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 443), make_handler(GUEST_ROOT, intent))
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(GUEST_ROOT / 'worker-origin-tls.pem', GUEST_ROOT / 'worker-origin-tls-key.pem')
    server.socket = context.wrap_socket(server.socket, server_side=True)
    server.serve_forever()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('keys', 'prepare', 'stage', 'guest-provision', 'guest-serve'))
    parser.add_argument('--work-root')
    parser.add_argument('--node')
    parser.add_argument('--archive')
    parser.add_argument('--version')
    parser.add_argument('--commit')
    parser.add_argument('--sequence', type=int)
    parser.add_argument('--repository')
    parser.add_argument('--intent', default=str(INTENT))
    parser.add_argument('--nonce')
    args = parser.parse_args()
    if args.action == 'keys':
        result = prepare_keys(args.work_root, args.node)
    elif args.action == 'prepare':
        result = prepare(args.work_root, args.node, args.archive, args.version, args.commit, args.sequence, args.repository)
    elif args.action == 'stage':
        result = stage(args.work_root, args.node)
    elif args.action == 'guest-provision':
        result = provision(args.intent, args.nonce)
    else:
        return serve(args.intent, args.nonce)
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    main()
