"""set1 H24: the second cron fault is a relocated spool (dangling symlink), and a fault counts only when the native
read fails before AND after the Panel's calls (cronie's crontab recreates a missing spool directory itself)."""
from pathlib import Path
import sys

ROOT = Path(sys.argv[1])


def sub(t, old, new, count=1):
    assert t.count(old) == count, (t.count(old), old[:70])
    return t.replace(old, new)


p = ROOT / "guest_settings_native.py"
t = p.read_text(encoding="utf-8")
t = sub(t, '''    """Two things an owner can do that stop ``crontab -u <user> -l``: restrict cron to root with /etc/cron.allow
    (a common hardening step), or move the spool directory aside. ``restore`` undoes exactly what ``apply`` did."""''',
        '''    """Two things an owner can do that stop ``crontab -u <user> -l``: restrict cron to root with /etc/cron.allow
    (a common hardening step), or relocate the spool directory to another volume behind a symlink and have that
    volume not mounted (the directory is moved aside and a dangling symlink stands in its place). ``restore``
    undoes exactly what ``apply`` did."""''')
t = sub(t, '''        elif kind == "spool-moved":
            source = next((d for d in SPOOLS if os.path.isdir(d) and not os.path.islink(d)), None)
            if source is None:
                raise Refused("no cron spool directory")
            target = source + ".set1-moved-aside"
            if os.path.lexists(target):
                raise Refused("the moved-aside name exists")
            os.rename(source, target)
            done = {"kind": kind, "moved": [source, target]}''',
        '''        elif kind == "spool-relocated":
            source = next((d for d in SPOOLS if os.path.isdir(d) and not os.path.islink(d)), None)
            if source is None:
                raise Refused("no cron spool directory")
            target = source + ".set1-moved-aside"
            if os.path.lexists(target) or os.path.lexists(RELOCATED_TARGET):
                raise Refused("the moved-aside name or the unmounted volume path exists")
            os.rename(source, target)
            os.symlink(RELOCATED_TARGET, source)
            done = {"kind": kind, "moved": [source, target], "symlink": [source, RELOCATED_TARGET]}''')
t = sub(t, '''    if done["kind"] == "cron-allow":
        os.unlink(done["created"])
    else:
        os.rename(done["moved"][1], done["moved"][0])''',
        '''    if done["kind"] == "cron-allow":
        os.unlink(done["created"])
    else:
        if not os.path.islink(done["symlink"][0]):
            raise Refused("the relocated spool's symlink is no longer a symlink; the owner restores it by hand")
        os.unlink(done["symlink"][0])
        os.rename(done["moved"][1], done["moved"][0])''')
t = sub(t, '''FAULT_RECORD = "set1-cron-fault.json"''',
        '''FAULT_RECORD = "set1-cron-fault.json"
RELOCATED_TARGET = "/mnt/set1-data-volume/cron-spool"   # a volume that is not mounted: the path does not exist''')
p.write_text(t, encoding="utf-8", newline="\n")

p = ROOT / "settings_writes_trial.py"
t = p.read_text(encoding="utf-8")
t = sub(t, '''CRON_FAULTS = ("cron-allow", "spool-moved")''', '''CRON_FAULTS = ("cron-allow", "spool-relocated")''')
old = t[t.index('''            try:
                probe = self.crontab("f-crontab-under-" + kind)'''):t.index('''            finally:
                self.owner("f-restore-" + kind, "owner-cron-fault", action="restore")''')]
new = '''            try:
                probe = self.crontab("f-crontab-under-" + kind)
                native_kind = crontab_native_kind(probe["list"], user)
                record = {"kind": kind, "applied": applied.get("applied"), "native": native_kind,
                          "returncode": probe["list"].get("returncode"), "stderr": probe["list"].get("stderr")}
                self.current.setdefault("cron_faults", []).append(record)
                listing = self.cron_list(f"S1f list under the owner's {kind}")
                record["list_status"] = listing["status"]
                record["list_code"] = (listing["_parsed"] or {}).get("code") if isinstance(listing["_parsed"], dict) else None
                add = None
                if native_kind == "failed":
                    add = self.call(f"S1f add under the owner's {kind}", "POST", path,
                                    {"schedule": "7 7 * * *", "command": "/usr/bin/true set1-fault-job",
                                     "version": final_version}, **stale)
                    record["add_status"] = add["status"]
                # The native read is taken again after the Panel's calls: a cause the platform's own crontab
                # repairs by itself between two reads (cronie recreates a missing spool directory) is not a
                # stable unreadable state, and the Panel's answers are then recorded, not judged.
                after = self.crontab("f-crontab-under-" + kind + "-again")
                record["native_again"] = crontab_native_kind(after["list"], user)
                record["stderr_again"] = after["list"].get("stderr")
                if native_kind != "failed" or record["native_again"] != "failed":
                    self.note(f"{kind} does not keep root's `crontab -u <user> -l` failing on this platform (native "
                              f"answers: {native_kind}, then {record['native_again']}); the list answered HTTP "
                              f"{listing['status']}", record)
                    continue
                used = kind
                self.refused(f"f ({kind}): the list answers 502 CURRENT_SETTINGS_UNREADABLE (scheduled_tasks), not an "
                             "empty list", listing, 502, "CURRENT_SETTINGS_UNREADABLE", "scheduled_tasks")
                self.check(f"f ({kind}): Add is refused", add["status"] != 200,
                           add.get("answer") or add["status"])
                self.refused(f"f ({kind}): Add answers 502 CURRENT_SETTINGS_UNREADABLE (scheduled_tasks)", add, 502,
                             "CURRENT_SETTINGS_UNREADABLE", "scheduled_tasks")
'''
t = t.replace(old, new)
p.write_text(t, encoding="utf-8", newline="\n")
print("patched H24")
