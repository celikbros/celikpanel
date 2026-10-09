"""set4b (the scan of set3 and set4, unchanged but for the folder name): secret scan over the staged evidence folder (run on the QEMU host, where the lab key files are).

usage: secretscan.py EVIDENCE_DIR LAB_ROOT...
Prints the report; exits 1 when a hit that is not explained is found.
"""
import base64
import datetime
import glob
import json
import os
import re
import sys

E = sys.argv[1]
labs = sys.argv[2:]
files = []
for directory, _, names in os.walk(E):
    for name in names:
        path = os.path.join(directory, name)
        if os.path.basename(path) in ("SHA256SUMS", "secret-scan.txt") and os.path.dirname(path) == E.rstrip("/"):
            continue
        files.append(path)
files.sort()
# The harness's own source (the run-copy diff and the tools) names the words it searches for; it holds no secret of a run.
SOURCE = ("/harness-run-copy/", "/tools/")


def is_source(path):
    return any(part in path.replace("\\", "/") for part in SOURCE)


texts = {}
for path in files:
    with open(path, "rb") as stream:
        texts[path] = stream.read().decode("utf-8", "replace")
print(f"# secret scan over set4c-20261009 ({datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')}); files: {len(files)}")
bad = 0

# 1. PEM private keys
pem = sum(len(re.findall(r"-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----", t)) for t in texts.values())
print(f"PEM private-key blocks: {pem}")
bad += pem

# 2. the labs' own key files, line by line
key_files = []
for lab in labs:
    key_files += [p for p in [os.path.join(lab, "key")] + glob.glob(os.path.join(lab, "worker-origin", "*.pem"))
                  + glob.glob(os.path.join(lab, "**", "*.key"), recursive=True) if os.path.isfile(p)]
print(f"key files checked: {len(key_files)}")
lines = set()
for path in key_files:
    print("  " + path)
    for line in open(path, errors="replace").read().splitlines():
        line = line.strip()
        if len(line) >= 40 and not line.startswith("-----"):
            lines.add(line)
hits = sum(1 for t in texts.values() for line in lines if line in t)
print(f"key body lines checked: {len(lines)}, hits: {hits}")
bad += hits

# 3. licence keys
literal = "CPK-acce57f1c7" + "0" * 54
hits = sum(t.count(literal) for t in texts.values())
shape = sum(len(re.findall(r"CPK-[0-9a-f]{20,}", t)) for t in texts.values())
print(f"fixture licence literal hits: {hits}")
print(f"CPK- key shape hits: {shape}")
bad += hits + shape

# 4. WireGuard: 44-character base64 keys. Public keys are evidence (peers are identified by them); every key of that
# shape must be a public key that the evidence itself names as one.
public = set()
shaped = {}
KEY = re.compile(r"(?<![A-Za-z0-9+/=])[A-Za-z0-9+/]{43}=(?![A-Za-z0-9+/=])")
for path, text in texts.items():
    for match in KEY.finditer(text):
        try:
            if len(base64.b64decode(match.group(0), validate=True)) != 32:
                continue
        except ValueError:
            continue
        shaped.setdefault(match.group(0), set()).add(path)
    for match in re.finditer(r'"(?:public_key|server_public_key|answer_public_key)":\s*"([A-Za-z0-9+/]{43}=)"', text):
        public.add(match.group(1))
    for block in re.finditer(r'"(?:wg_peers|panel_active_rows|new_wg_peers|new_active_rows|wg0)":\s*\[([^\]]*)\]', text):
        public.update(re.findall(r"[A-Za-z0-9+/]{43}=", block.group(1)))
unexplained = {k: v for k, v in shaped.items() if k not in public}
print(f"32-byte base64 keys found: {len(shaped)}; named as public keys by the evidence: {len(public & set(shaped))}; others: {len(unexplained)}")
for key, where in list(unexplained.items())[:8]:
    print(f"  UNEXPLAINED {key[:6]}...{key[-4:]} in {sorted(where)[0][len(E):]}")
bad += len(unexplained)

# 5. WireGuard configuration words and password fields outside the harness's own source
words = ("PrivateKey", "PresharedKey", "[Interface]")
REPORT = ("README.md",)     # the report names these words when it says what was searched for; it quotes no configuration
for word in words:
    count = sum(t.count(word) for p, t in texts.items() if not is_source(p) and os.path.basename(p) not in REPORT)
    named = sum(t.count(word) for p, t in texts.items() if os.path.basename(p) in REPORT)
    print(f"`{word}` outside the harness source and the report's own text: {count} (named in README.md: {named})")
    bad += count
FIELD = re.compile(r'"([A-Za-z_]*(?:password|passwd|secret|token|private|preshared|cookie)[A-Za-z_]*)"\s*:\s*"((?:[^"\\]|\\.)*)"', re.I)
PUBLIC_SUFFIX = ("_state", "_status", "_reason", "_required", "_id", "_sha256", "_count", "_at", "_enabled", "_configured", "_present",
                 "_kind", "_type", "_b64")
ALLOWED_VALUES = {"", "[REDACTED]", "[REDACTED client configuration]"}
open_fields, redacted = {}, 0
for path, text in texts.items():
    if is_source(path):
        continue
    for match in FIELD.finditer(text):
        name, value = match.group(1), match.group(2)
        if value in ALLOWED_VALUES or "[REDACTED]" in value:
            redacted += 1
            continue
        lowered = name.lower()
        if lowered.endswith(PUBLIC_SUFFIX) or lowered in ("password_source", "private_ip", "token_type"):
            continue
        open_fields.setdefault(name, []).append((path, value[:40]))
print(f"secret-named JSON fields holding `[REDACTED]`: {redacted}")
print(f"secret-named JSON fields holding another value: {sum(len(v) for v in open_fields.values())}")
for name, where in open_fields.items():
    kinds = sorted({v for _, v in where})[:4]
    print(f"  {name}: {len(where)} (first in {where[0][0][len(E):]}); values: {kinds}")
# values that describe instead of disclose are listed above and judged by the reader; counted as hits unless descriptive
descriptive = re.compile(r"^(minted by the Panel|sent by the owner|\(not recorded|[0-9]+$|true$|false$|null$)")
real = sum(1 for where in open_fields.values() for _, v in where if not descriptive.match(v))
bad += real

# 6. hash-shaped credential values (crypt, Dovecot schemes, SCRAM verifiers, MariaDB native hashes): the same shapes
# the drivers redact at collection time (owner_update_trial.HASH_SHAPED). None may be in a retained file; the harness's
# own source (the run-copy diffs and the tools) writes the patterns and test strings and is not a value of a run.
HASH_SHAPED = re.compile(
    r"(?:\{[A-Z][A-Z0-9.-]{1,24}\})?\$(?:1|2[abxy]?|5|6|7|y|gy|sha1|argon2(?:id|i|d)|scrypt|pbkdf2(?:-sha(?:1|256|512))?)"
    r"\$[./A-Za-z0-9$=,+-]{8,}"
    r"|\{(?:SSHA(?:256|512)?|SHA(?:256|512)?|SMD5|PLAIN|CRYPT|CRAM-MD5|[A-Z0-9]+-CRYPT|ARGON2ID?|PBKDF2)\}[^\s\"'<>\\]{4,}"
    r"|SCRAM-SHA-256\$\d+:[A-Za-z0-9+/=]+\$[A-Za-z0-9+/=]+:[A-Za-z0-9+/=]+"
    r"|(?<![0-9A-Za-z])\*[0-9A-F]{40}(?![0-9A-Fa-f])")
shaped_hits = {}
for p, t in texts.items():
    if is_source(p):
        continue
    found = HASH_SHAPED.findall(t)
    if found:
        shaped_hits[p] = len(found)
print(f"hash-shaped credential values outside the harness source: {sum(shaped_hits.values())} in {len(shaped_hits)} file(s)")
for p, n in list(shaped_hits.items())[:8]:
    print(f"  HASH-SHAPED {n} in {p[len(E):]}")
bad += sum(shaped_hits.values())
print(f"`[REDACTED hash-shaped value]` markers outside the harness source (values the drivers removed at collection time): {sum(t.count('[REDACTED hash-shaped value]') for p, t in texts.items() if not is_source(p))}; the marker's own text in the harness source: {sum(t.count('[REDACTED hash-shaped value]') for p, t in texts.items() if is_source(p))}")
print(f"`crypt_hash` field names outside the harness source: {sum(t.count('crypt_hash') for p, t in texts.items() if not is_source(p) and os.path.basename(p) not in REPORT)}")
for pattern, label in ((r"(?:MYSQL_PWD|PGPASSWORD)=[^\s\"']+", "MYSQL_PWD= / PGPASSWORD= assignments"),
                       (r"IDENTIFIED BY '[^']+'", "SQL password literals (IDENTIFIED BY '...')"),
                       (r"PASSWORD '[^']+'", "SQL password literals (PASSWORD '...')"),
                       (r"password_b64\"?\s*[:=]\s*\"?[A-Za-z0-9+/=]{8,}", "base64 passwords handed to a guest helper (password_b64)")):
    count = sum(len(re.findall(pattern, t)) for p, t in texts.items() if not is_source(p))
    print(f"{label}: {count}")
    bad += count

markers = sum(t.count("[REDACTED]") for t in texts.values())
print(f"`[REDACTED]` markers in the evidence: {markers}")
print("RESULT: " + ("no unexplained hit" if bad == 0 else f"{bad} hit(s) to review"))
sys.exit(0 if bad == 0 else 1)
