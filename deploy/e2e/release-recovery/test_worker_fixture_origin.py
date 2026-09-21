#!/usr/bin/env python3
"""Offline trust and network-scope checks, not native recovery evidence."""
import hashlib
import http.client
import http.server
import importlib.util
import os
import re
import shlex
from pathlib import Path
import socket
import ssl
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('tested_worker_origin', HERE / 'worker_fixture_origin.py')
f = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = f
spec.loader.exec_module(f)


def fixture_intent():
    version = 'v0.1.0-alpha.82'
    base = '/releases/' + version + '/linux/amd64/'
    routes = {'/releases/latest.txt': 'worker-origin-latest', base + 'release-manifest-v2': 'worker-origin-manifest',
              base + 'release-manifest-v2.sig': 'worker-origin-signature',
              base + 'celikpanel-' + version + '-linux-amd64.tar.gz': 'worker-origin-archive.tar.gz',
              base + 'celikpanel-' + version + '-linux-amd64.tar.gz.sha256': 'worker-origin-archive.sha256'}
    names = set(routes.values()) | {'worker-origin-public.pem', 'worker-origin-ca.pem',
                                    'worker-origin-tls.pem', 'worker-origin-tls-key.pem'}
    target = {'version': version, 'sequence': '82', 'commit': 'a' * 40, 'archive': 'celikpanel-' + version + '-linux-amd64.tar.gz', 'archive_sha256': 'b' * 64}
    files = {name: {'sha256': 'b' * 64, 'size': 1} for name in names}
    checksum = f.checksum_bytes(target)
    files['worker-origin-archive.sha256'] = {'sha256': hashlib.sha256(checksum).hexdigest(), 'size': len(checksum)}
    policy = {'format': 'celikpanel-release-sequence-policy-v1', 'version': version, 'current': 82, 'previous': 81, 'previous_version': 'v0.1.0-alpha.81', 'previous_commit': 'd' * 40}
    policy['sha256'] = hashlib.sha256(''.join(key + '=' + str(value) + '\n' for key, value in policy.items()).encode()).hexdigest()
    return {'schema': f.SCHEMA, 'target': target, 'source_proof': {'release_policy': policy}, 'routes': routes, 'files': files}


class WhitelistTests(unittest.TestCase):
    def test_exact_routes(self):
        f.validate_intent(fixture_intent())

    def test_origin_policy_is_bound_to_target_and_canonical_bytes(self):
        for key, wrong in [('version', 'v0.1.0-alpha.80'), ('current', 80), ('previous', 80),
                           ('previous_version', 'v0.1.0-alpha.80'), ('previous_commit', 'a' * 40), ('sha256', 'a' * 64)]:
            with self.subTest(key=key):
                value = fixture_intent()
                value['source_proof']['release_policy'][key] = wrong
                with self.assertRaises(ValueError):
                    f.validate_intent(value)

    def test_checksum_sidecar_is_bound_to_target(self):
        for key, changed in [('size', 1), ('sha256', 'f' * 64)]:
            value = fixture_intent()
            value['files']['worker-origin-archive.sha256'][key] = changed
            with self.assertRaises(ValueError):
                f.validate_intent(value)
        value = fixture_intent()
        value['target']['archive_sha256'] = 'c' * 64
        with self.assertRaises(ValueError):
            f.validate_intent(value)

    def test_no_private_key_or_arbitrary_route(self):
        for route, name in [('/key', 'worker-origin-signing.pem'),
                            ('/releases/latest.txt', '../../etc/passwd'),
                            ('https://evil.invalid', 'worker-origin-manifest')]:
            value = fixture_intent()
            value['routes'][route] = name
            with self.assertRaises(ValueError):
                f.validate_intent(value)

    def test_no_extra_payload(self):
        value = fixture_intent()
        value['files']['worker-origin-signing.pem'] = {'sha256': 'b' * 64, 'size': 1}
        with self.assertRaises(ValueError):
            f.validate_intent(value)

    def test_release_and_digest_bounds(self):
        for key, bad in [('version', '../../evil'), ('commit', 'unknown')]:
            value = fixture_intent()
            value['target'][key] = bad
            with self.assertRaises(ValueError):
                f.validate_intent(value)
        for bad in [0, -1, f.MAX_ARCHIVE + 1, True]:
            value = fixture_intent()
            value['files']['worker-origin-latest']['size'] = bad
            with self.assertRaises(ValueError):
                f.validate_intent(value)

    def test_keys_require_root_before_any_lab_access(self):
        with mock.patch.object(f.os, 'geteuid', return_value=1000), mock.patch.object(f, 'identity') as identity:
            with self.assertRaises(ValueError):
                f.prepare_keys(Path('/unused'), 'debian13')
            identity.assert_not_called()

    def test_guest_requires_fixed_path_and_nonce(self):
        for path, nonce in [('/tmp/fake.json', 'a' * 64), (str(f.INTENT), 'bad')]:
            with self.assertRaises(ValueError):
                f.guest_intent(path, nonce)


class CryptoAndServerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.root = Path(cls.temp.name)
        cls.identity = {'schema': 'celikpanel-release-recovery-lab/v1', 'nonce': 'a' * 64,
                        'node': 'debian', 'cell_id': 'release-recovery-test',
                        'vm_uuid': '11111111-1111-1111-1111-111111111111'}
        # Generate real fixture keys as the CI user; no real guest or host guard is bypassed.
        with mock.patch.object(f, 'identity', return_value=(None, cls.root, None, None, cls.identity)), mock.patch.object(f.os, 'geteuid', return_value=0):
            cls.result = f.prepare_keys(cls.root, 'debian')
        cls.directory = cls.root / 'worker-origin'

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def test_real_key_and_tls_material(self):
        self.assertEqual(f.digest(Path(self.result['public_key'])), self.result['public_key_sha256'])
        subprocess.run(['openssl', 'verify', '-CAfile', str(self.directory / 'worker-origin-ca.pem'),
                        '-verify_hostname', 'celikpanel.net', str(self.directory / 'worker-origin-tls.pem')],
                       check=True, capture_output=True)
        message = self.directory / 'crypto-test-message'
        f.private_write(message, b'fixture manifest\n')
        signature = self.directory / 'crypto-test-signature'
        f.run(['openssl', 'pkeyutl', '-sign', '-rawin', '-inkey', str(self.directory / 'worker-origin-signing.pem'),
               '-in', str(message), '-out', str(signature)])
        f.run(['openssl', 'pkeyutl', '-verify', '-pubin', '-rawin', '-inkey', str(self.directory / 'worker-origin-public.pem'),
               '-in', str(message), '-sigfile', str(signature)])
        self.assertEqual(len(signature.read_bytes()), 64)
        for name in ('signing', 'ca-key', 'tls-key', 'public'):
            self.assertEqual((self.directory / ('worker-origin-' + name + '.pem')).stat().st_mode & 0o777, 0o600)

    def test_keys_refuse_recreation(self):
        with mock.patch.object(f, 'identity', return_value=(None, self.root, None, None, self.identity)), mock.patch.object(f.os, 'geteuid', return_value=0):
            with self.assertRaises(FileExistsError):
                f.prepare_keys(self.root, 'debian')

    @unittest.skipUnless(os.geteuid() == 0, 'real root ownership is exercised by the explicit sudo CI run')
    def test_reader_refuses_symlinks_and_permissions(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            p = root / 'file'
            f.private_write(p, b'sealed')
            self.assertEqual(f.read_file(p), b'sealed')
            link = root / 'link'
            link.symlink_to(p)
            with self.assertRaises(OSError):
                f.read_file(link)
            p.chmod(0o644)
            with self.assertRaises(ValueError):
                f.read_file(p)

    def test_https_routes_are_closed_and_payload_pinned(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            value = fixture_intent()
            value['target']['archive_sha256'] = hashlib.sha256(b'test worker-origin-archive.tar.gz').hexdigest()
            for name in value['files']:
                raw = f.checksum_bytes(value['target']) if name == 'worker-origin-archive.sha256' else b'test ' + name.encode()
                f.private_write(root / name, raw)
                value['files'][name] = {'size': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}
            # Non-root TLS/route coverage isolates only the root-owned filesystem
            # admission boundary. The sudo CI run uses the unmodified reader.
            def fixture_read(path, maximum, mode=0o600):
                if path.parent != root or path.name not in value['files']:
                    raise AssertionError('HTTP handler escaped its fixture root')
                data = path.read_bytes()
                if len(data) > maximum:
                    raise ValueError('fixture payload exceeds bound')
                return data
            reader_patch = mock.patch.object(f, 'read_file', side_effect=fixture_read) if os.geteuid() != 0 else None
            if reader_patch is not None:
                reader_patch.start()
                self.addCleanup(reader_patch.stop)
            server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), f.make_handler(root, value))
            tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            tls.load_cert_chain(self.directory / 'worker-origin-tls.pem', self.directory / 'worker-origin-tls-key.pem')
            server.socket = tls.wrap_socket(server.socket, server_side=True)
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            try:
                def request(path, host='celikpanel.net'):
                    context = ssl.create_default_context(cafile=str(self.directory / 'worker-origin-ca.pem'))
                    connection = http.client.HTTPConnection('127.0.0.1', server.server_port, timeout=3)
                    connection.sock = context.wrap_socket(socket.create_connection(('127.0.0.1', server.server_port)), server_hostname='celikpanel.net')
                    connection.request('GET', path, headers={'Host': host})
                    response = connection.getresponse()
                    status, body = response.status, response.read()
                    connection.close()
                    return status, body
                self.assertEqual(request('/releases/latest.txt')[0], 200)
                checksum_path = next(path for path in value['routes'] if path.endswith('.tar.gz.sha256'))
                self.assertEqual(request(checksum_path), (200, f.checksum_bytes(value['target'])))
                # Exercise the real installed updater's signed_fetch function and
                # every update-branch release_url fetch line. Only TLS transport
                # address/CA are redirected to this temporary loopback server.
                get_sh = (HERE.parents[2] / 'download-portal/get.sh').read_text()
                function = re.search(r'(?ms)^signed_fetch\(\) \{\n.*?^\}', get_sh)
                self.assertIsNotNone(function)
                fetches = re.findall(r'^  signed_fetch "\$release_url/[^\n]+', get_sh, flags=re.MULTILINE)
                self.assertEqual(len(fetches), 4)
                downloads = root / 'downloads'
                downloads.mkdir()
                quote = shlex.quote
                base = 'https://celikpanel.net:' + str(server.server_port) + '/releases/' + value['target']['version'] + '/linux/amd64'
                wrapper = 'curl() { command /usr/bin/curl --noproxy "*" --resolve ' + quote('celikpanel.net:' + str(server.server_port) + ':127.0.0.1') + ' --cacert ' + quote(str(self.directory / 'worker-origin-ca.pem')) + ' --header "Host: celikpanel.net" "$@"; }\n'
                script = ('set -eu\n' + wrapper + function.group() + '\nrelease_url=' + quote(base) +
                          '\narchive=' + quote(value['target']['archive']) + '\nworkdir=' + quote(str(downloads)) +
                          '\nsigned_archive_size=' + str(value['files']['worker-origin-archive.tar.gz']['size']) + '\n' +
                          '\n'.join(fetches) + '\ncd "$workdir"\nsha256sum -c "$archive.sha256"\n')
                completed = subprocess.run(['/bin/bash', '-c', script], capture_output=True, text=True, timeout=10)
                self.assertEqual(completed.returncode, 0, completed.stderr)
                self.assertIn(value['target']['archive'] + ': OK', completed.stdout)
                for path in ('/get.sh', '/worker-origin-tls-key.pem', '/../etc/passwd', '/releases/latest.txt?x=1', '/releases/%2e%2e/key'):
                    self.assertEqual(request(path)[0], 404)
                self.assertEqual(request('/releases/latest.txt', 'other.invalid')[0], 404)
                (root / 'worker-origin-latest').write_bytes(b'tampered')
                self.assertEqual(request('/releases/latest.txt')[0], 503)
            finally:
                server.shutdown()
                server.server_close()
                thread.join(3)


if __name__ == '__main__':
    unittest.main()
