#!/usr/bin/env python3
"""Exercise the production SMTP client against an isolated TLS SMTP server."""
import base64
import email
import hashlib
import json
import socket
import ssl
import subprocess
import tempfile
import threading
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class SMTPTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='celikpanel-smtp-')
        self.work = Path(self.temp.name)
        self.cert, self.key = self.work/'cert.pem', self.work/'key.pem'
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
                        '-subj', '/CN=localhost', '-addext', 'subjectAltName=DNS:localhost',
                        '-keyout', str(self.key), '-out', str(self.cert)],
                       check=True, capture_output=True)
        self.runner = self.work/'send.php'
        self.runner.write_text('<?php require '+json.dumps(str(ROOT/'portal-membership/src/Mail.php'))+'; '
                               '$c=json_decode(file_get_contents($argv[1]),true); '
                               'try { \\CelikPanel\\Membership\\Mail::send($c,"recipient@example.test",'
                               '"CelikPanel test", "Test body\\n.leading dot\\n"); echo "sent"; } '
                               'catch (\\Throwable $e) { echo $e->getMessage(); exit(1); }')

    def tearDown(self):
        self.temp.cleanup()

    def probe(self, *, trusted=True, host='localhost', reject_auth=False):
        listener = socket.socket()
        listener.bind(('127.0.0.1', 0))
        listener.listen(1)
        listener.settimeout(15)
        port = listener.getsockname()[1]
        state = {'commands': [], 'message': b'', 'error': None}

        def serve():
            try:
                context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
                context.load_cert_chain(self.cert, self.key)
                with listener.accept()[0] as raw:
                    raw.settimeout(15)
                    with context.wrap_socket(raw, server_side=True) as client:
                        stream = client.makefile('rwb', buffering=0)
                        def reply(value): stream.write(value+b'\r\n')
                        def read():
                            line = stream.readline(8192)
                            if not line:
                                raise EOFError('Peer closed before an SMTP command')
                            return line.rstrip(b'\r\n')
                        reply(b'220 localhost ESMTP test')
                        state['commands'].append(read())
                        reply(b'250-localhost\r\n250 AUTH LOGIN')
                        state['commands'].append(read())
                        reply(b'334 VXNlcm5hbWU6')
                        state['username'] = base64.b64decode(read())
                        reply(b'334 UGFzc3dvcmQ6')
                        state['password'] = base64.b64decode(read())
                        if reject_auth:
                            reply(b'535 5.7.8 test password rejection')
                            if read() == b'QUIT': reply(b'221 bye')
                            return
                        reply(b'235 authenticated')
                        state['commands'].append(read()); reply(b'250 sender ok')
                        state['commands'].append(read()); reply(b'250 recipient ok')
                        state['commands'].append(read()); reply(b'354 end with dot')
                        lines = []
                        while True:
                            line = read()
                            if line == b'.': break
                            lines.append(line[1:] if line.startswith(b'..') else line)
                        state['message'] = b'\r\n'.join(lines)
                        reply(b'250 queued')
                        if read() == b'QUIT': reply(b'221 bye')
            except (ssl.SSLError, BrokenPipeError, ConnectionResetError, EOFError):
                state['tls_rejected'] = True
            except Exception as problem:
                state['error'] = type(problem).__name__
            finally:
                listener.close()

        worker = threading.Thread(target=serve, daemon=True)
        worker.start()
        config = {'sender':'sender@example.test', 'smtp':{'host':host, 'port':port,
                  'username':'sender@example.test', 'password':'isolated-test-password'}}
        path = self.work/'config.json'
        path.write_text(json.dumps(config))
        command = ['php', '-d', 'display_errors=0', '-d', 'log_errors=0']
        if trusted: command += ['-d', 'openssl.cafile='+str(self.cert)]
        result = subprocess.run(command+[str(self.runner), str(path)], capture_output=True, text=True, timeout=25)
        worker.join(20)
        self.assertFalse(worker.is_alive())
        self.assertIsNone(state['error'])
        self.assertNotIn('isolated-test-password', result.stdout+result.stderr)
        return result, state

    def test_verified_tls_authentication_and_message(self):
        result, state = self.probe()
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        self.assertEqual(result.stdout, 'sent')
        self.assertEqual(state['username'], b'sender@example.test')
        self.assertEqual(state['password'], b'isolated-test-password')
        self.assertIn(b'MAIL FROM:<sender@example.test>', state['commands'])
        self.assertIn(b'RCPT TO:<recipient@example.test>', state['commands'])
        message = email.message_from_bytes(state['message'])
        self.assertEqual(message['Subject'], 'CelikPanel test')
        self.assertIn(b'.leading dot', message.get_payload(decode=True))

    def test_untrusted_certificate_fails_before_authentication(self):
        result, state = self.probe(trusted=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, 'mail_unavailable')
        self.assertEqual(state['commands'], [])

    def test_wrong_certificate_hostname_fails_before_authentication(self):
        result, state = self.probe(host='127.0.0.1')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, 'mail_unavailable')
        self.assertEqual(state['commands'], [])

    def test_authentication_rejection_has_no_delivery_or_diagnostics(self):
        result, state = self.probe(reject_auth=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, 'mail_unavailable')
        self.assertEqual(state['message'], b'')
        self.assertNotIn('535', result.stdout+result.stderr)

    def test_vendored_files_match_pinned_upstream(self):
        vendor = ROOT/'portal-membership/vendor/phpmailer'
        manifest = json.loads((vendor/'UPSTREAM.json').read_text())
        for name, digest in manifest['sha256'].items():
            self.assertEqual(hashlib.sha256((vendor/name).read_bytes()).hexdigest(), digest, name)


if __name__ == '__main__':
    unittest.main(verbosity=2)
