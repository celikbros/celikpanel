# set6: derive secretscan.py from set5's (evidence/set5-20261009/tools/secretscan.py): the same classes, the harness
# module found through SET6_HARNESS, and class 10 (base64 text decoded and searched). usage: mkscan.py SET5_SCAN OUT
import sys

src = open(sys.argv[1], encoding="utf-8").read()


def rep(old, new):
    global src
    assert src.count(old) == 1, (src.count(old), old[:70])
    src = src.replace(old, new)


rep('"""set5: secret scan over the staged evidence folder (run on the QEMU host, where the lab key files and the labs\' own raw\n'
    'records are). set3\'s scan, with three classes added: digests of tokens (class 7), HTTP credential headers (class 8)\n'
    'and the local scratch-folder path (class 9, informational).\n',
    '"""set6: secret scan over the staged evidence folder (run on the QEMU host, where the lab key files and the labs\' own raw\n'
    'records are). set5\'s scan (set3\'s classes 1 to 6; digests of tokens, class 7; HTTP credential headers, class 8; the\n'
    'local scratch-folder path, class 9, informational), with one class added: class 10, base64 text. Every run of 40 or\n'
    'more base64 characters that decodes strictly to UTF-8 text is decoded (three levels deep) and the decoded text is\n'
    'searched with the rules of classes 1, 2, 3, 5, 6, 7 and 8. The classes are listed at the top of the report.\n')

HEAD = 'print(f"# secret scan over {os.path.basename(E.rstrip(\'/\'))} ({datetime.datetime.now(datetime.timezone.utc).strftime(\'%Y-%m-%dT%H:%M:%SZ\')}); files: {len(files)}")\n'
rep(HEAD, '''

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
        if not inner or sum(ch.isprintable() or ch in "\\r\\n\\t" for ch in inner) < 0.95 * len(inner):
            continue
        found.append(inner)
        if depth < 2:
            found += decoded_texts(inner, depth + 1)
    return found


''' + HEAD + '''print("""# classes searched:
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
''')

rep('sys.path.insert(0, os.environ["SET5_HARNESS"])\nimport set5_redact  # noqa: E402\n',
    'sys.path.insert(0, os.environ["SET6_HARNESS"])\nimport set6_redact  # noqa: E402\nset5_redact = set6_redact.set5_redact\n')
rep("# the same two patterns the driver applies at collection time (the harness module set5_redact.py, found through\n"
    "# SET5_HARNESS: the run copy's deploy/e2e/release-recovery directory)\n",
    "# the same two patterns the driver applies at collection time (the harness modules set5_redact.py and set6_redact.py,\n"
    "# found through SET6_HARNESS: the run copy's deploy/e2e/release-recovery directory)\n")
rep('            known.update(m.group(3).lower() for m in TOKEN_PAIR.finditer(raw))\n'
    '            known.update(m.group(2).lower() for m in MIGRATION_DIR.finditer(raw))\n',
    '            for piece in [raw] + decoded_texts(raw):     # the labs\' raw records hold such values inside base64 text too\n'
    '                known.update(m.group(3).lower() for m in TOKEN_PAIR.finditer(piece))\n'
    '                known.update(m.group(2).lower() for m in MIGRATION_DIR.finditer(piece))\n')
rep('PLAIN_HEADER = re.compile(r"(?im)^(?:cookie|set-cookie|authorization):\\s*(?!\\[REDACTED\\])\\S")',
    'PLAIN_HEADER = re.compile(r"(?im)^(?:[<>] )?(?:cookie|set-cookie|authorization):\\s*(?!\\[REDACTED\\])\\S")')
rep('# 9. informational, not a secret: the local scratch-folder path and the Windows profile path\n', '''# 10. base64 text, decoded: the rules of classes 1, 2, 3, 5 (fields), 6, 7 and 8 over every decoded text
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
        for match in FIELD.finditer(piece.replace('\\\\"', '"')):
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
''')
open(sys.argv[2], "w", encoding="utf-8", newline="\n").write(src)
