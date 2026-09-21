#!/usr/bin/env python3
"""Offline trust and network-scope checks, not native recovery evidence."""
import copy
import hashlib
import http.client
import http.server
import importlib.util
import json
import os
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
              base + 'celikpanel-' + version + '-linux-amd64.tar.gz': 'worker-origin-archive.tar.gz'}
    names = set(routes.values()) | {'worker-origin-public.pem', 'worker-origin-ca.pem',
                                    'worker-origin-tls.pem', 'worker-origin-tls-key.pem'}
    return {'schema': f.SCHEMA, 'target': {'version': version, 'commit': 'a' * 40}, 'routes': routes,
            'files': {name: {'sha256': 'b' * 64, 'size': 1} for name in names}}


class WhitelistTests(unittest.TestCase):
    def test_exact_routes(self):
        f.validate_intent(fixture_intent())

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

    def test_guest_requires_fixed_path_and_nonce(self):
        for path, nonce in [('/tmp/fake.json', 'a' * 64), (str(f.INTENT), 'bad')]:
            with self.assertRaises(ValueError):
                f.guest_intent(path, nonce)


@unittest.skipUnless(os.geteuid() == 0, 'production metadata contract requires root')
class CryptoAndServerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.root = Path(cls.temp.name)
        cls.identity = {'schema': 'celikpanel-release-recovery-lab/v1', 'nonce': 'a' * 64,
                        'node': 'debian', 'cell_id': 'release-recovery-test',
                        'vm_uuid': '11111111-1111-1111-1111-111111111111'}
        with mock.patch.object(f, 'identity', return_value=(None, cls.root, None, None, cls.identity)):
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
        with mock.patch.object(f, 'identity', return_value=(None, self.root, None, None, self.identity)):
            with self.assertRaises(FileExistsError):
                f.prepare_keys(self.root, 'debian')

    def test_reader_refuses_symlinks_and_permissions(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            p = root / 'file'
            f.private_write(p, b'sealed')
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
            for name in value['files']:
                raw = b'test ' + name.encode()
                f.private_write(root / name, raw)
                value['files'][name] = {'size': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}
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
                for path in ('/worker-origin-tls-key.pem', '/../etc/passwd', '/releases/latest.txt?x=1', '/releases/%2e%2e/key'):
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
