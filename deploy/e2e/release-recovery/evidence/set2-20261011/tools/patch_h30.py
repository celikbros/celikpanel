"""set2 H30: signs of a reload are looked for only after a Reload (a restart also changes the unit's ExecReload record
and PostgreSQL's load time, which is not a reload)."""
import sys
p = sys.argv[1] + "/settings_writes_trial.py"
s = open(p, encoding="utf-8", newline="").read()
old = '''        reloaded_by = [m for m in RELOAD_MARKERS.get(service, ()) if m in log]
        if d0["exec_reload"] != d1["exec_reload"] and d1["exec_reload"]:
            reloaded_by.append("the unit's ExecReload ran")
        if service == "postgresql" and d0["conf_load_time"] and d1["conf_load_time"] and d0["conf_load_time"] != d1["conf_load_time"]:
            reloaded_by.append("pg_conf_load_time() moved")
'''
new = '''        reloaded_by = []
        if action == "reload":     # H30: a restart also changes these; they are signs of a reload only after a Reload
            reloaded_by = [m for m in RELOAD_MARKERS.get(service, ()) if m in log]
            if d0["exec_reload"] != d1["exec_reload"] and d1["exec_reload"]:
                reloaded_by.append("the unit's ExecReload ran")
            if service == "postgresql" and d0["conf_load_time"] and d1["conf_load_time"] and d0["conf_load_time"] != d1["conf_load_time"]:
                reloaded_by.append("pg_conf_load_time() moved")
'''
assert s.count(old) == 1
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="").write(s)
print("H30 patched")
