# set4: one-off edits of the two new harness files while they were being written (kept as a record of the session).
import io
import os
import sys

R = sys.argv[1]
p = os.path.join(R, 'guest_set4_native.py')
s = io.open(p, encoding='utf-8', newline='').read()
old_start = s.index('def read_isolation(args: dict) -> dict:')
old_end = s.index('MODES = {')
new = '''def read_isolation(args: dict) -> dict:
    """Lab isolation as this guest's own hosts file answers it: the product's name and the certificate authority's
    two directory names. Asked with ``getent -s files``, which reads /etc/hosts only: no DNS question is sent for
    any of these names, and nothing is connected to."""
    names = ("celikpanel.net", "acme-v02.api.letsencrypt.org", "acme-staging-v02.api.letsencrypt.org")
    resolves = {name: (run(["getent", "-s", "files", "hosts", name], timeout=20).get("stdout") or "").strip() for name in names}
    return {"asked_with": "getent -s files hosts NAME", "resolves": resolves,
            "loopback_only": {name: rid.loopback_only(answer) for name, answer in resolves.items()},
            "hosts_lines": [l for l in (text_of("/etc/hosts") or "").splitlines() if l.strip() and not l.startswith("#")],
            "at": utc()}


def read_spa(args: dict) -> dict:
    """What the installed web build holds: for every ``web/dist`` on this guest, how many of its files contain each
    of the given sentences; and where the running Panel's process lives."""
    needles = [n for n in args.get("needles", []) if isinstance(n, str) and 8 <= len(n) <= 200][:12]
    found = run(["find", "/opt", "/usr/local", "/var/lib", "/srv", "/root", "/home", "-xdev", "-type", "f", "-path",
                 "*/web/dist/index.html"], timeout=180)
    builds = []
    for index in sorted((found.get("stdout") or "").splitlines()):
        root = os.path.dirname(index)
        counts, files = {needle: 0 for needle in needles}, 0
        for directory, _dirs, names in os.walk(root):
            for name in names:
                if not name.endswith((".js", ".html", ".json", ".mjs")):
                    continue
                files += 1
                try:
                    data = Path(os.path.join(directory, name)).read_bytes()
                except OSError:
                    continue
                for needle in needles:
                    if needle.encode() in data:
                        counts[needle] += 1
        builds.append({"dist": root, "real_path": os.path.realpath(root), "files_read": files, "files_containing": counts,
                       "index_sha256": sha256_file(index)})
    unit = run(["systemctl", "show", "celikpanel-panel.service", "-p", "MainPID", "-p", "ExecStart", "-p", "WorkingDirectory",
                "-p", "ActiveState"], timeout=30)
    shown = dict(line.split("=", 1) for line in (unit.get("stdout") or "").splitlines() if "=" in line)
    process = {}
    pid = shown.get("MainPID")
    if pid and pid.isdigit() and int(pid) > 1:
        for name in ("exe", "cwd"):
            try:
                process[name] = os.readlink(f"/proc/{pid}/{name}")
            except OSError as exc:
                process[name] = type(exc).__name__
    return {"needles": needles, "web_builds": builds, "panel_unit": shown, "panel_process": process, "at": utc()}


'''
s = s[:old_start] + new + s[old_end:]
a = '''         "read-units": read_units, "read-isolation": read_isolation,'''
assert s.count(a) == 1
s = s.replace(a, '''         "read-units": read_units, "read-isolation": read_isolation, "read-spa": read_spa,''')
io.open(p, 'w', encoding='utf-8', newline='').write(s)

p = os.path.join(R, 'set4_trial.py')
s = io.open(p, encoding='utf-8', newline='').read()
a = s.index('SECTIONS = (\n    ("M0-prepare"')
b = s.index('CELLS = {\n    "set4-arch"')
new = '''# M6 runs while no PHP site exists (after M1 deleted its own, before the imports create theirs): its owner action
# moves a file that every PHP vhost in service reads, and `nginx -t` must still pass with the configuration in service.
SECTIONS = (
    ("M0-prepare", "the platform, the lab's isolation, the nginx package's PHP snippet (item 7)", None),
    ("M1-php-site", "a PHP site: created, nginx -t, executed as the site's account, PATH_INFO, a missing script, the recorded "
                    "PHP version and socket, deleted (items 1, 3, 8)", None),
    ("M6-site-refused", "a new site whose configuration the web server refuses (item 6)", None),
    ("M2-import", "the cPanel-archive import of set3's fixture; the imported site served (items 2, 3)", None),
    ("M5-import-absolute", "an archive with a member named by an absolute path (item 5)", None),
    ("M10-postfix-stop", "Postfix stopped through the Panel while `postfix check` refuses main.cf (item 10)", "mail"),
    ("M4-reload-stopped", "Reload of a stopped nginx, MariaDB and PostgreSQL (item 4)", None),
)
'''
s = s[:a] + new + s[b:]
old = '''            needs = ("site",) if key == "M0-prepare" else ("site", "M0-prepare")
            if key == "M0-prepare":'''
assert s.count(old) == 1
s = s.replace(old, '''            if key == "M0-prepare":''')
old = '''``fastcgi.conf`` away, one with a long site name and no owner action.
'''
assert s.count(old) == 1
s = s.replace(old, '''``fastcgi.conf`` away, one with a long site name and no owner action. M6 runs right after M1, while no PHP
      site exists.
''')
io.open(p, 'w', encoding='utf-8', newline='').write(s)
print("patched")
