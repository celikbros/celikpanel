#!/usr/bin/env python3
"""Secret scan over the staged upd11 evidence folder (host only). usage: scan11.py STAGE OUT"""
import glob, re, sys, datetime as dt
from pathlib import Path
stage, out = Path(sys.argv[1]), Path(sys.argv[2])
files = [p for p in stage.rglob("*") if p.is_file() and p.resolve() != out.resolve()]
texts = {}
for p in files:
    try:
        texts[p] = p.read_bytes().decode("utf-8", "replace")
    except OSError:
        pass
L = [f"# secret scan over {stage.name} ({dt.datetime.now(dt.timezone.utc):%Y-%m-%dT%H:%M:%SZ}); files: {len(texts)}"]
pem = sum(len(re.findall(r"-----BEGIN [A-Z ]*PRIVATE KEY-----", t)) for t in texts.values())
L.append(f"PEM private-key blocks: {pem}")
keyfiles = sorted(glob.glob("/var/tmp/cp-release-drill-upd11*/key")
                  + glob.glob("/var/tmp/cp-release-drill-upd11*/worker-origin/worker-origin-signing.pem")
                  + glob.glob("/var/tmp/cp-release-drill-upd11*/worker-origin/worker-origin-tls-key.pem")
                  + glob.glob("/var/tmp/cp-release-drill-upd11*/worker-origin/worker-origin-ca-key.pem"))
L.append(f"key files checked: {len(keyfiles)}")
lines = set()
for k in keyfiles:
    L.append("  " + k)
    for line in Path(k).read_text(errors="replace").splitlines():
        line = line.strip()
        if len(line) >= 20 and not line.startswith("-----"):
            lines.add(line)
hits = [(str(p), l[:12]) for l in lines for p, t in texts.items() if l in t]
L.append(f"key body lines checked: {len(lines)}, hits: {len(hits)}")
for h in hits[:10]:
    L.append(f"  HIT {h}")
lic = "CPK-acce57f1c7" + "0" * 54
L.append(f"fixture licence literal hits: {sum(t.count(lic) for t in texts.values())}")
L.append(f"CPK- key shape hits: {sum(len(re.findall(r'CPK-[0-9a-f]{20,}', t)) for t in texts.values())}")
b43 = set()
for t in texts.values():
    b43.update(re.findall(r"(?<![A-Za-z0-9_-])[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])", t))
b43 = sorted(x for x in b43 if re.search(r"[A-Z]", x) and re.search(r"[a-z]", x) and re.search(r"[0-9]", x))
L.append(f"43-char base64url tokens with mixed case and a digit (admin password shape): {len(b43)}")
for x in b43[:10]:
    L.append(f"  sample: {x}")
b32 = set()
for t in texts.values():
    b32.update(re.findall(r"(?<![A-Za-z0-9_-])[A-Za-z0-9]{32}(?![A-Za-z0-9_-])", t))
b32 = sorted(x for x in b32 if re.search(r"[A-Z]", x) and re.search(r"[a-z]", x) and re.search(r"[0-9]", x))
L.append(f"32-char alnum tokens with mixed case and a digit (mailbox/db password shape): {len(b32)}")
for x in b32[:10]:
    L.append(f"  sample: {x}")
field = re.compile(r'"(password|new_password|db_password|secret|token|csrf_token|session|api_key|tsig_secret|private_key|license_key)"\s*:\s*"([^"]*)"', re.I)
bad = []
for p, t in texts.items():
    for m in field.finditer(t):
        v = m.group(2)
        if v and v != "[REDACTED]" and not v.startswith("[REDACTED"):
            bad.append((str(p.relative_to(stage)), m.group(1), v[:6] + "..."))
L.append(f"password/secret/token fields with non-redacted values: {len(bad)}")
for b in bad[:20]:
    L.append(f"  {b}")
ck = []
for p, t in texts.items():
    for m in re.finditer(r'(?i)"?(cookie|set-cookie)"?\s*[:=]\s*"?([^"\n]{0,80})', t):
        if "[REDACTED" not in m.group(2) and m.group(2).strip() not in ("", "null", "[]", "{}"):
            ck.append((str(p.relative_to(stage)), m.group(2)[:40]))
L.append(f"cookie headers not redacted: {len(ck)}")
for c in ck[:20]:
    L.append(f"  {c}")
L.append(f"[REDACTED] markers: {sum(t.count('[REDACTED') for t in texts.values())}")
L.append("# real origin")
L.append(f"185.95. (address the real name resolved to in upd1): {sum(1 for t in texts.values() if '185.95.' in t)} files")
ctx = {}
for p, t in texts.items():
    for line in t.splitlines():
        if "celikpanel.net" in line and ("systemd" in line or "journal" in str(p)):
            key = re.sub(r"^\S+ \S+ ", "", line)[:200]
            key = re.sub(r"[0-9a-f]{32}", "<rid>", key)
            ctx[key] = ctx.get(key, 0) + 1
L.append(f"journal lines naming celikpanel.net (distinct contexts): {len(ctx)}")
for k, v in sorted(ctx.items(), key=lambda x: -x[1])[:25]:
    L.append(f"  {v:5d} {k}")
lic_lines = sum(t.count("not contacted") for t in texts.values())
L.append(f"'not contacted' occurrences (licence step): {lic_lines}")
longest = max((len("deploy/e2e/release-recovery/evidence/") + len(str(p.relative_to(stage.parent))) for p in files), default=0)
L.append(f"longest repo-relative path length: {longest}")
out.write_text("\n".join(L) + "\n")
print("\n".join(L[:16]))
