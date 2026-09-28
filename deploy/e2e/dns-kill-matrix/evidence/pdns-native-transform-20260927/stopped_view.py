import hashlib
import json
import os
import sqlite3
import stat
import subprocess
from pathlib import Path
node = __import__("sys").argv[1]
marker = Path("/etc/celikpanel-dns-kill-matrix").read_text()
if node not in ("debian13","arch") or f"node={node}\n" not in marker or "cell_id=pdns-switch__intent__after-write__paired-primary__peer-reachable\n" not in marker:
    raise SystemExit("foreign guest")
def command(*args):
    p=subprocess.run(args,capture_output=True,text=True)
    return {"code":p.returncode,"stdout":p.stdout.strip(),"stderr":p.stderr.strip()}
unit=command("systemctl","show","pdns.service","-p","ActiveState","-p","SubState","-p","MainPID","-p","ControlGroup")
if "ActiveState=inactive" not in unit["stdout"] or "MainPID=0" not in unit["stdout"]:
    raise SystemExit("PowerDNS not fully stopped")
dbpath=Path("/var/lib/powerdns/pdns.sqlite3")
def info(p):
    try: s=p.lstat()
    except FileNotFoundError: return {"exists":False}
    if not stat.S_ISREG(s.st_mode): raise SystemExit("nonregular DB/sidecar")
    return {"exists":True,"dev":s.st_dev,"inode":s.st_ino,"mode":stat.S_IMODE(s.st_mode),"uid":s.st_uid,"gid":s.st_gid,"size":s.st_size,"sha256":hashlib.sha256(p.read_bytes()).hexdigest()}
before={s:info(Path(str(dbpath)+s)) for s in ("","-wal","-shm","-journal")}
db=sqlite3.connect(f"file:{dbpath}?mode=ro",uri=True)
try:
    tables={}
    for (name,) in db.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name"):
        if name.startswith("sqlite_"): continue
        rows=list(db.execute('SELECT * FROM "'+name.replace('"','""')+'"'))
        tables[name]=sorted([list(r) for r in rows],key=lambda x:json.dumps(x,sort_keys=True))
    integrity=db.execute("PRAGMA quick_check").fetchone()[0]
finally: db.close()
after={s:info(Path(str(dbpath)+s)) for s in ("","-wal","-shm","-journal")}
print(json.dumps({"schema":"celikpanel/pdns-stopped-wal-view/v1","node":node,"unit":unit,"files_before_read":before,"files_after_read":after,"integrity":integrity,"tables":tables},sort_keys=True,separators=(",",":")))
