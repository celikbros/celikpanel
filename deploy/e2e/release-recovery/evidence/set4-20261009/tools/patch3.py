# set4, harness defect H44 (found by update-alpha81/upd1-arch-defective/run-a): the reader of the installed web build
# looked for web/dist/index.html; the installer copies the build to <prefix>/web (index.html directly there), so the
# build the running Panel serves was not read. The reader now finds both layouts and names the one the running
# Panel's process serves (its CELIKPANEL_WEB_DIR, or ./web/dist below its working directory; only that one variable
# of the process environment is read). usage: patch3.py HARNESS_DIR
import io
import os
import sys

R = sys.argv[1]
p = os.path.join(R, 'guest_set4_native.py')
s = io.open(p, encoding='utf-8', newline='').read()
old = '''    found = run(["find", "/opt", "/usr/local", "/var/lib", "/srv", "/root", "/home", "-xdev", "-type", "f", "-path",
                 "*/web/dist/index.html"], timeout=180)
'''
new = '''    found = run(["find", "/opt", "/usr/local", "/var/lib", "/srv", "/root", "/home", "-xdev", "-type", "f", "(", "-path",
                 "*/web/dist/index.html", "-o", "-path", "*/web/index.html", ")"], timeout=180)
'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''    return {"needles": needles, "web_builds": builds, "panel_unit": shown, "panel_process": process, "at": utc()}
'''
new = '''    served = None
    if pid and pid.isdigit() and int(pid) > 1:
        try:
            for entry in Path(f"/proc/{pid}/environ").read_bytes().split(bytes([0])):
                if entry.startswith(b"CELIKPANEL_WEB_DIR="):
                    served = entry.split(b"=", 1)[1].decode("utf-8", "replace")
        except OSError:
            pass
        if served is None and isinstance(process.get("cwd"), str) and process["cwd"].startswith("/"):
            served = os.path.join(process["cwd"], "web", "dist")
        if served is not None and not os.path.isabs(served) and isinstance(process.get("cwd"), str):
            served = os.path.join(process["cwd"], served)
    for build in builds:
        build["served_by_the_running_panel"] = served is not None and os.path.realpath(served) == build["real_path"]
    return {"needles": needles, "web_builds": builds, "panel_unit": shown, "panel_process": process,
            "web_directory_of_the_running_panel": served, "at": utc()}
'''
assert s.count(old) == 1
s = s.replace(old, new)
io.open(p, 'w', encoding='utf-8', newline='').write(s)
print("patched H44")
