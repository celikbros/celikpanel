# set4, harness defect H43 (found by update-alpha81/upd1-debian13-good/run-a): the added step expected the General
# settings save to change the vhost file. The candidate's Panel renders every hosted vhost when it starts
# ("certificate startup reconcile: restored N hosted vhosts with one nginx validation and reload"), so the file had
# already been rendered by the update itself and the save wrote the same bytes. The rule now is: the vhost in service
# after the update differs from the baseline's, and the save is shown by the file's own inode/mtime and nginx's
# journal instead of by a different digest. usage: patch2.py HARNESS_DIR
import io
import os
import sys

R = sys.argv[1]
p = os.path.join(R, 'guest_set4_native.py')
s = io.open(p, encoding='utf-8', newline='').read()
old = '''                text = data[:16000].decode("utf-8", "replace")
                found.append({"path": path, "kind": "file", "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(),
'''
new = '''                text = data[:16000].decode("utf-8", "replace")
                info = os.stat(path)
                found.append({"path": path, "kind": "file", "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(),
                              "inode": info.st_ino, "mtime_ns": info.st_mtime_ns, "ctime_ns": info.st_ctime_ns,
'''
assert s.count(old) == 1
s = s.replace(old, new)
io.open(p, 'w', encoding='utf-8', newline='').write(s)

p = os.path.join(R, 'set4_trial.py')
s = io.open(p, encoding='utf-8', newline='').read()
old = '''                "vhost": {k: vhost.get(k) for k in ("path", "sha256", "bytes", "includes", "names_the_php_snippet",
                                                    "fastcgi_pass", "php_location_lines")},'''
new = '''                "vhost": {k: vhost.get(k) for k in ("path", "sha256", "bytes", "inode", "mtime_ns", "ctime_ns", "includes",
                                                    "names_the_php_snippet", "fastcgi_pass", "php_location_lines")},'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''        saved = self.api("POST", f"/api/v1/domains/{domain_id}/general", sent, purpose="DomainGeneralSettings save (renders the vhost again)",
                         timeout=300)
'''
new = '''        clock = self.keep4("nginx-unit-before-the-save", "read-units", units=["nginx.service"])
        since = calendar.timegm(time.strptime(clock["at"], "%Y-%m-%dT%H:%M:%SZ"))     # the guest's own clock
        time.sleep(1.5)
        saved = self.api("POST", f"/api/v1/domains/{domain_id}/general", sent, purpose="DomainGeneralSettings save (renders the vhost again)",
                         timeout=300)
        time.sleep(1.5)
        nginx_since = self.keep4("nginx-unit-after-the-save", "read-units", units=["nginx.service"], since_epoch=since)
'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''                      vhost_file_changed_by_the_update_itself=before["vhost"].get("sha256") != untouched["vhost"].get("sha256"),
                      vhost_file_changed_by_the_save=untouched["vhost"].get("sha256") != after["vhost"].get("sha256"),
'''
new = '''                      vhost_file_changed_by_the_update_itself=before["vhost"].get("sha256") != untouched["vhost"].get("sha256"),
                      vhost_file_changed_by_the_save=untouched["vhost"].get("sha256") != after["vhost"].get("sha256"),
                      vhost_file_written_again_by_the_save={k: [untouched["vhost"].get(k), after["vhost"].get(k)]
                                                           for k in ("inode", "mtime_ns", "ctime_ns")},
                      nginx_journal_since_the_save=(nginx_since.get("journal") or {}).get("nginx.service"),
                      product_journal_since_the_save=nginx_since.get("journal_product"),
'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''        if untouched["vhost"].get("sha256") == after["vhost"].get("sha256"):
            failures.append("the save did not change the vhost file (nothing was rendered again)")
'''
new = '''        # H43: the candidate's Panel renders every hosted vhost when it starts, so the update itself may already have
        # written the new text; the save then writes the same bytes. What must hold: the vhost in service is no
        # longer the baseline's, and the save is seen on the file itself (inode or change time) or in nginx's journal.
        if before["vhost"].get("sha256") == after["vhost"].get("sha256"):
            failures.append("the vhost file is still the baseline's after the update and the save (nothing was rendered again)")
        written = any(untouched["vhost"].get(k) != after["vhost"].get(k) for k in ("inode", "mtime_ns", "ctime_ns"))
        reloaded = any("Reload" in line for line in (nginx_since.get("journal") or {}).get("nginx.service") or [])
        checks["the_save_rendered_the_vhost_again"] = {"file_written_again": written, "nginx_reloaded_since_the_save": reloaded}
        if not written and not reloaded:
            failures.append("neither the vhost file nor nginx's journal shows that the save rendered the vhost again")
'''
assert s.count(old) == 1
s = s.replace(old, new)
old = "import argparse" + chr(10) + "import base64" + chr(10) + "import dataclasses" + chr(10)
assert s.count(old) == 1
s = s.replace(old, "import argparse" + chr(10) + "import base64" + chr(10) + "import calendar" + chr(10) + "import dataclasses" + chr(10))
io.open(p, 'w', encoding='utf-8', newline='').write(s)
print("patched H43")
