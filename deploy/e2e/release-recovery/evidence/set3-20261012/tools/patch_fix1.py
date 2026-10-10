# set3: offline-test pins and the loopback reader without an HTTP client library.
ROOT = r'C:\CELIKBROS PROJECTS\celikpanel\deploy\e2e\release-recovery' + '\\'


class Patch:
    def __init__(self, name):
        self.path = ROOT + name
        self.s = open(self.path, encoding='utf-8', newline='').read()
        assert '\r\n' not in self.s, name

    def rep(self, a, b, n=1):
        assert self.s.count(a) == n, (a[:90], self.s.count(a))
        self.s = self.s.replace(a, b)

    def save(self):
        open(self.path, 'w', encoding='utf-8', newline='').write(self.s)


h = Patch('guest_request_identity_native.py')
h.rep('''import http.client
import imaplib
''', '''import imaplib
''')
h.rep('''import secrets as secrets_module
import shutil
import sqlite3
import ssl
''', '''import secrets as secrets_module
import shutil
import socket
import sqlite3
import ssl
''')
h.rep('''    try:
        connection = http.client.HTTPConnection("127.0.0.1", 80, timeout=20)
        connection.request("GET", path, headers={"Host": host, "Connection": "close"})
        response = connection.getresponse()
        body = response.read(65536)
        headers = {k.lower(): v for k, v in response.getheaders() if k.lower() in ("content-type", "server", "x-powered-by")}
        connection.close()
    except Exception as exc:  # noqa: BLE001 - reported as data
        return {"host": host, "path": path, "error": type(exc).__name__ + ": " + str(exc)[:200], "at": utc()}
    return {"host": host, "path": path, "status": response.status, "headers": headers, "bytes": len(body),
            "body": body[:600].decode("utf-8", "replace"), "at": utc()}''', '''    # A plain socket to 127.0.0.1:80 and nothing else: this helper holds no client that could name another host.
    try:
        with socket.create_connection(("127.0.0.1", 80), timeout=20) as connection:
            connection.sendall(("GET " + path + " HTTP/1.0\\r\\nHost: " + host + "\\r\\nConnection: close\\r\\n\\r\\n").encode())
            raw = b""
            while len(raw) < 131072:
                chunk = connection.recv(65536)
                if not chunk:
                    break
                raw += chunk
    except Exception as exc:  # noqa: BLE001 - reported as data
        return {"host": host, "path": path, "error": type(exc).__name__ + ": " + str(exc)[:200], "at": utc()}
    head, _, body = raw.partition(b"\\r\\n\\r\\n")
    lines = head.decode("latin-1").split("\\r\\n")
    status = int(lines[0].split()[1]) if len(lines[0].split()) > 1 and lines[0].split()[1].isdigit() else None
    headers = {}
    for line in lines[1:]:
        name, _, value = line.partition(":")
        if name.lower() in ("content-type", "server", "x-powered-by", "transfer-encoding"):
            headers[name.lower()] = value.strip()
    return {"host": host, "path": path, "status": status, "headers": headers, "bytes": len(body),
            "body": body[:600].decode("utf-8", "replace"), "at": utc()}''')
h.save()

t = Patch('test_request_identity_trial.py')
t.rep('''                         ["lab-isolate-acme", "lab-kill-panel", "owner-change-site", "owner-cpmove-fixture",
                          "owner-restart-panel", "owner-seed-rows", "owner-seed-site"])''',
      '''                         ["lab-isolate-acme", "lab-kill-panel", "owner-change-site", "owner-cpmove-fixture",
                          "owner-php-probe", "owner-restart-panel", "owner-seed-rows", "owner-seed-site"])''')
t.rep('''        self.assertIn('"authority_unreachable"', (REPO / "cmd" / "agent" / "certbot_failure.go").read_text(encoding="utf-8"))''',
      '''        self.assertIn('"authority_unreachable"', (REPO / "internal" / "transport" / "ssl_contracts.go").read_text(encoding="utf-8"))''')
t.save()

o = Patch('test_owner_update_trial.py')
o.rep('''        self.assertIn('g_json=$(build "$good" v0.1.0-alpha.81)', text)''',
      '''        self.assertIn('g_json=$(build "$good" "$c_version")', text)
        self.assertIn("c_version=v0.1.0-alpha.81 b_seq=80 c_seq=81", text)
        # set3: the published v0.1.0-alpha.81 is built unpatched (the tag commit itself); candidates are alpha.82.
        self.assertIn("c_version=v0.1.0-alpha.82 b_seq=81 c_seq=82 b_parent=", text)
        self.assertIn('baseline=$tag_commit', text)
        self.assertEqual(t.baseline_ref_patched("v0.1.0-alpha.81"), ())
        self.assertEqual(t.baseline_ref_patched("v0.1.0-alpha.80"), t.BASELINE_REF_PATCHED)''')
o.save()
print('ok')
