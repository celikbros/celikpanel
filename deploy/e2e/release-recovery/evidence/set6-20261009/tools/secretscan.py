"""set6: secret scan over the staged evidence folder (run on the QEMU host, where the lab key files and the labs' own raw
records are). set5's scan (set3's classes 1 to 6; digests of tokens, class 7; HTTP credential headers, class 8; the
local scratch-folder path, class 9, informational), with one class added: class 10, base64 text. Every run of 40 or
more base64 characters that decodes strictly to UTF-8 text is decoded (three levels deep) and the decoded text is
searched with the rules of classes 1, 2, 3, 5, 6, 7 and 8. The classes are listed at the top of the report.

usage: secretscan.py EVIDENCE_DIR LAB_ROOT...
Prints the report; exits 1 when a hit that is not explained is found. No value of a hit is printed.
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


def decoded_texts(text, depth=0):
    """Every UTF-8 text that a base64 run of 40 or more characters of ``text`` holds, three levels deep."""
    import binascii
    found = []
    for match in re.finditer(r"(?<![A-Za-z0-9+/=])[A-Za-z0-9+/]{40,}={0,2}(?![A-Za-z0-9+/=])", text):
        run = match.group(0)
        if len(run) % 4:
            continue
        try:
            inner = base64.b64decode(run, validate=True).decode("utf-8")
        except (binascii.Error, ValueError, UnicodeDecodeError):
            continue
        if not inner or sum(ch.isprintable() or ch in "\r\n\t" for ch in inner) < 0.95 * len(inner):
            continue
        found.append(inner)
        if depth < 2:
            found += decoded_texts(inner, depth + 1)
    return found


print(f"# secret scan over {os.path.basename(E.rstrip('/'))} ({datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')}); files: {len(files)}")
print("""# classes searched:
#  1  PEM private-key blocks
#  2  the body lines of every key file of this run's labs (read on this host)
#  3  licence keys (the acceptance fixture literal, and the CPK- shape)
#  4  WireGuard 32-byte base64 keys that the evidence does not itself name as public keys
#  5  WireGuard configuration words; secret-named JSON fields (password, passwd, secret, token, private, preshared, cookie)
#  6  hash-shaped credential values (crypt, Dovecot schemes, SCRAM verifiers, MariaDB native hashes); password assignments
#  7  digests of tokens: by shape (a hexadecimal value under a token-named key, or the directory under
#     .release-db-migrations/) and by value (every such value of the labs' raw records, plain and inside base64 text)
#  8  HTTP credential headers recorded with a value (Cookie, Set-Cookie, Authorization, CSRF)
#  9  local user-profile and scratch-folder paths (informational, not a secret)
#  10 base64 text: every run of 40 or more base64 characters that decodes to UTF-8 text is decoded, three levels
#     deep, and the decoded text is searched with the rules of classes 1, 2, 3, 5, 6, 7 and 8""")
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

# 7. digests of tokens. set3 and set4 retained `transaction_token_sha256` and the same digest as a directory name under
# `.release-db-migrations/` (a sudo journal line). (a) by shape: a hexadecimal value of 32 to 128 characters under a
# key whose name contains "token", or directly under `.release-db-migrations/`; (b) by value: every such value found
# in the labs' own raw records on this host (which are not part of the evidence) is searched for in every retained
# file, whatever stands before it.
# the same two patterns the driver applies at collection time (the harness modules set5_redact.py and set6_redact.py,
# found through SET6_HARNESS: the run copy's deploy/e2e/release-recovery directory)
sys.path.insert(0, os.environ["SET6_HARNESS"])
import set6_redact  # noqa: E402
set5_redact = set6_redact.set5_redact
TOKEN_PAIR, MIGRATION_DIR = set5_redact.TOKEN_PAIR, set5_redact.MIGRATION_DIR
shape = {}
for p, t in texts.items():
    if is_source(p) or os.path.basename(p) in REPORT:
        continue
    n = len(TOKEN_PAIR.findall(t)) + len(MIGRATION_DIR.findall(t))
    if n:
        shape[p] = n
print(f"token digests by shape (a hexadecimal value under a token-named key, or as the directory under .release-db-migrations/) outside the harness source: {sum(shape.values())} in {len(shape)} file(s)")
for p, n in list(shape.items())[:8]:
    print(f"  TOKEN-DIGEST {n} in {p[len(E):]}")
bad += sum(shape.values())
known, raw_files = set(), 0
for lab in labs:
    for directory, _, names in os.walk(os.path.join(lab, "evidence")):
        for name in names:
            try:
                raw = open(os.path.join(directory, name), "rb").read().decode("utf-8")
            except (OSError, UnicodeDecodeError):
                continue
            raw_files += 1
            for piece in [raw] + decoded_texts(raw):     # the labs' raw records hold such values inside base64 text too
                known.update(m.group(3).lower() for m in TOKEN_PAIR.finditer(piece))
                known.update(m.group(2).lower() for m in MIGRATION_DIR.finditer(piece))
value_hits = {}
for p, t in texts.items():
    lowered = t.lower()
    n = sum(lowered.count(v) for v in known)
    if n:
        value_hits[p] = n
print(f"token digests by value: {len(known)} distinct value(s) read from {raw_files} raw file(s) of {len(labs)} lab(s) on this host; occurrences in the evidence: {sum(value_hits.values())} in {len(value_hits)} file(s)")
for p, n in list(value_hits.items())[:8]:
    print(f"  TOKEN-DIGEST-VALUE {n} in {p[len(E):]}")
bad += sum(value_hits.values())
print(f"`[REDACTED-SHA256]` markers outside the harness source (token digests removed at collection time): {sum(t.count('[REDACTED-SHA256]') for p, t in texts.items() if not is_source(p) and os.path.basename(p) not in REPORT)}")
print(f"`transaction_token_sha256` field names outside the harness source and the report: {sum(t.count('transaction_token_sha256') for p, t in texts.items() if not is_source(p) and os.path.basename(p) not in REPORT)} (each must hold the marker; counted by shape above)")

# 8. HTTP credential headers recorded with a value (the pair redactor replaces the value of each of them)
HEADER = re.compile(r'"(?i:cookie|set-cookie|authorization|proxy-authorization|x-csrf-token|x-xsrf-token)"\s*[,:]\s*"(?!\[REDACTED\])[^"]')
PLAIN_HEADER = re.compile(r"(?im)^(?:[<>] )?(?:cookie|set-cookie|authorization):\s*(?!\[REDACTED\])\S")
headers = {}
for p, t in texts.items():
    if is_source(p):
        continue
    n = len(HEADER.findall(t)) + len(PLAIN_HEADER.findall(t))
    if n:
        headers[p] = n
print(f"Cookie / Set-Cookie / Authorization / CSRF headers recorded with a value other than `[REDACTED]`: {sum(headers.values())} in {len(headers)} file(s)")
for p, n in list(headers.items())[:8]:
    print(f"  HEADER {n} in {p[len(E):]}")
bad += sum(headers.values())

# 10. base64 text, decoded: the rules of classes 1, 2, 3, 5 (fields), 6, 7 and 8 over every decoded text
with_text = texts_decoded = 0
inside = {}
for p, t in texts.items():
    if is_source(p):
        continue
    pieces = decoded_texts(t)
    if not pieces:
        continue
    with_text += 1
    texts_decoded += len(pieces)
    n = 0
    for piece in pieces:
        n += len(re.findall(r"-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----", piece))
        n += sum(1 for line in lines if line in piece)
        n += piece.count(literal) + len(re.findall(r"CPK-[0-9a-f]{20,}", piece))
        for match in FIELD.finditer(piece.replace('\\"', '"')):
            name, value = match.group(1), match.group(2)
            lowered = name.lower()
            if (value in ALLOWED_VALUES or "[REDACTED" in value or lowered.endswith(PUBLIC_SUFFIX)
                    or lowered in ("password_source", "private_ip", "token_type") or descriptive.match(value)):
                continue
            n += 1
        n += len(HASH_SHAPED.findall(piece))
        n += len(TOKEN_PAIR.findall(piece)) + len(MIGRATION_DIR.findall(piece))
        n += sum(piece.lower().count(v) for v in known)
        n += len(HEADER.findall(piece)) + len(PLAIN_HEADER.findall(piece))
    if n:
        inside[p] = n
print(f"base64 text: {texts_decoded} decoded text(s) in {with_text} file(s) outside the harness source; hits of classes 1, 2, 3, 5, 6, 7 and 8 inside decoded text: {sum(inside.values())} in {len(inside)} file(s)")
for p, n in list(inside.items())[:8]:
    print(f"  INSIDE-BASE64 {n} in {p[len(E):]}")
bad += sum(inside.values())

# 9. informational, not a secret: the local scratch-folder path and the Windows profile path
local = sum(len(re.findall(r"AppData/Local/Temp/claude|AppData.Local.Temp.claude|/mnt/c/Users/|C:.Users.", t)) for p, t in texts.items())
print(f"occurrences of a local user-profile or scratch-folder path (informational): {local}")

markers = sum(t.count("[REDACTED]") for t in texts.values())
print(f"`[REDACTED]` markers in the evidence: {markers}")
print("RESULT: " + ("no unexplained hit" if bad == 0 else f"{bad} hit(s) to review"))
sys.exit(0 if bad == 0 else 1)
