#!/usr/bin/env python3
# Batch 10 secret scan over the evidence folder (read-only). Arg: folder
import os, re, sys, sqlite3, shutil, tempfile
D = sys.argv[1]
pat = [
    (re.compile(rb"-----BEGIN [A-Z ]*PRIVATE KEY-----"), "private key block"),
    (re.compile(rb"openssh-key-v1"), "openssh private key (binary marker)"),
    (re.compile(rb"(?i)\b(password|passwd|api[_-]?key|secret|token)\s*[=:]\s*['\"]?[A-Za-z0-9+/_\-]{12,}"), "credential assignment"),
    (re.compile(rb"(?i)license[_-]?key\s*[=:]"), "license key assignment"),
    (re.compile(rb"(?i)\balgorithm\s+hmac-[a-z0-9]+;\s*secret\s+\""), "TSIG/rndc secret"),
]
hits = []; files = 0; dbs = 0; dbrows = []
for root, _, names in os.walk(D):
    for n in names:
        p = os.path.join(root, n); files += 1
        b = open(p, "rb").read()
        for rx, label in pat:
            for m in rx.finditer(b):
                hits.append((os.path.relpath(p, D), label, b[max(0, m.start()-40):m.end()+40][:160]))
        if n.endswith(".sqlite3"):
            dbs += 1
            t = tempfile.mkdtemp()
            try:
                shutil.copy(p, os.path.join(t, "db.sqlite3"))
                if os.path.exists(p + "-wal"):
                    shutil.copy(p + "-wal", os.path.join(t, "db.sqlite3-wal"))
                c = sqlite3.connect(os.path.join(t, "db.sqlite3"))
                for tab in ("tsigkeys", "cryptokeys"):
                    try:
                        r = c.execute(f"select count(*) from {tab}").fetchone()[0]
                    except Exception as e:
                        r = f"err {e}"
                    if r != 0:
                        dbrows.append((os.path.relpath(p, D), tab, r))
                c.close()
            finally:
                shutil.rmtree(t)
print(f"files scanned: {files}; sqlite copies checked: {dbs}")
print(f"sqlite tsigkeys/cryptokeys non-empty or unreadable: {dbrows if dbrows else 'none (all rows=0)'}")
print(f"pattern hits: {len(hits)}")
for h in hits:
    print("  ", h[0], "|", h[1], "|", h[2])
