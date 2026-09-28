import hashlib
import json
import os
import pwd
import grp
import sqlite3
import stat
import subprocess
import sys
import time
from pathlib import Path

node = sys.argv[1]
if node not in ("debian13", "arch"):
    raise SystemExit("invalid node")
marker = Path("/etc/celikpanel-dns-kill-matrix").read_text()
want = ("schema=celikpanel/dns-kill-fixture-plan/v1\n"
        "cell_id=pdns-switch__intent__after-write__paired-primary__peer-reachable\n"
        f"node={node}\n")
if marker != want:
    raise SystemExit("foreign guest marker")
schema = Path("/usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql" if node == "debian13" else "/usr/share/doc/powerdns/schema.sqlite3.sql")
user = "pdns" if node == "debian13" else "powerdns"
ident = pwd.getpwnam(user)
group = grp.getgrnam(user)
root = Path("/var/lib/powerdns")
root.mkdir(mode=0o755, exist_ok=True)
os.chown(root, ident.pw_uid, group.gr_gid)
live = root / "pdns.sqlite3"
candidate = root / ".celikpanel-native-transform-candidate.sqlite3"
catalog = "catalog-c000020a.celikpanel.invalid"
member = "s1-kill.test"
def call(*args, check=True):
    p = subprocess.run(args, text=True, capture_output=True)
    if check and p.returncode:
        raise RuntimeError(f"{args!r}: {p.returncode}: {p.stdout} {p.stderr}")
    return {"exit": p.returncode, "stdout": p.stdout.strip(), "stderr": p.stderr.strip()}
def file_info(p):
    try: s = p.lstat()
    except FileNotFoundError: return {"exists": False}
    if not stat.S_ISREG(s.st_mode):
        return {"exists": True, "kind": "nonregular", "mode": stat.S_IMODE(s.st_mode)}
    return {"exists": True, "dev": s.st_dev, "inode": s.st_ino,
            "size": s.st_size, "mode": stat.S_IMODE(s.st_mode),
            "uid": s.st_uid, "gid": s.st_gid,
            "sha256": hashlib.sha256(p.read_bytes()).hexdigest()}
def sql_info(path, active):
    if not path.exists(): return {"exists": False}
    uri = f"file:{path}?mode=ro" + ("" if active else "&immutable=1")
    db = sqlite3.connect(uri, uri=True)
    try:
        names = [r[0] for r in db.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")]
        tables = {}
        for name in names:
            if name.startswith("sqlite_"): continue
            rows = list(db.execute('SELECT * FROM "' + name.replace('"','""') + '"'))
            tables[name] = sorted([list(r) for r in rows], key=lambda x: json.dumps(x, sort_keys=True))
        sql = sorted([list(r) for r in db.execute("SELECT type,name,tbl_name,sql FROM sqlite_master ORDER BY type,name")])
        return {"tables": tables, "schema": sql, "integrity": db.execute("PRAGMA quick_check").fetchone()[0]}
    finally: db.close()
def snap(label, path, active=False):
    return {"phase": label, "main": file_info(path),
            "sidecars": {suffix: file_info(Path(str(path)+suffix)) for suffix in ("-wal","-shm","-journal")},
            "sqlite": sql_info(path, active)}
if call("systemctl","is-active","pdns.service",check=False)["exit"] == 0:
    raise SystemExit("PowerDNS already active")
if live.exists() or candidate.exists():
    raise SystemExit("database pre-existed")
out = {"schema": "celikpanel/pdns-native-transform-observation/v1",
       "node": node, "package": call("dpkg-query","-W","pdns-server","pdns-backend-sqlite3") if node == "debian13" else call("pacman","-Q","powerdns"),
       "binary": file_info(Path("/usr/sbin/pdns_server" if node=="debian13" else "/usr/bin/pdns_server")),
       "schema_file": file_info(schema),
       "before": {"unit_active": call("systemctl","is-active","pdns.service",check=False),
                  "unit_enabled": call("systemctl","is-enabled","pdns.service",check=False),
                  "database": snap("absent", live)}}
db = sqlite3.connect(candidate)
db.execute("PRAGMA journal_mode=DELETE")
db.execute("PRAGMA synchronous=FULL")
db.executescript(schema.read_text())
db.execute("INSERT INTO domains(name,type,account) VALUES (?,?,?)", (catalog,"PRODUCER","celikpanel-bind-catalog-v1"))
catalog_id = db.execute("SELECT id FROM domains WHERE name=?", (catalog,)).fetchone()[0]
db.execute("INSERT INTO domains(name,type,catalog) VALUES (?,?,?)", (member,"MASTER",catalog))
member_id = db.execute("SELECT id FROM domains WHERE name=?", (member,)).fetchone()[0]
for name, kind, content, ttl in ((catalog,"SOA","invalid. invalid. 1 60 30 3600 30",60),
                                (catalog,"NS","invalid.",60)):
    db.execute("INSERT INTO records(domain_id,name,type,content,ttl,prio,disabled,auth) VALUES (?,?,?,?,?,0,0,1)", (catalog_id,name,kind,content,ttl))
for name, kind, content, ttl in ((member,"SOA","ns1.s1-kill.test hostmaster.s1-kill.test 2026083101 10800 3600 604800 3600",3600),
                                (member,"NS","ns1.s1-kill.test",3600),
                                (member,"NS","ns2.s1-kill.test",3600),
                                ("ns1."+member,"A","192.0.2.10",300),
                                ("ns2."+member,"A","192.0.2.11",300),
                                ("www."+member,"A","192.0.2.10",300)):
    db.execute("INSERT INTO records(domain_id,name,type,content,ttl,prio,disabled,auth) VALUES (?,?,?,?,?,0,0,1)", (member_id,name,kind,content,ttl))
db.commit()
if db.execute("PRAGMA quick_check").fetchone()[0] != "ok": raise SystemExit("candidate failed integrity")
db.close()
os.chmod(candidate,0o640)
os.chown(candidate,ident.pw_uid,group.gr_gid)
out["staged"] = snap("staged", candidate)
os.rename(candidate, live)
out["renamed"] = snap("renamed_before_start", live)
conf = Path("/etc/powerdns/pdns.conf")
conf.write_text("# Disposable native transformation witness\n"
                "launch=gsqlite3\ngsqlite3-dnssec=yes\n"
                "gsqlite3-database=/var/lib/powerdns/pdns.sqlite3\n"
                "local-address=127.0.0.1,192.0.2.10\nzone-cache-refresh-interval=0\n"
                "webserver=no\napi=no\nprimary=yes\nsecondary=yes\n"
                "allow-axfr-ips=127.0.0.1,192.0.2.11\nalso-notify=192.0.2.11\n", encoding="ascii")
os.chmod(conf,0o640)
os.chown(conf,0,group.gr_gid)
out["config"] = file_info(conf)
out["start"] = call("systemctl","start","pdns.service",check=False)
time.sleep(3)
out["running_unit"] = call("systemctl","is-active","pdns.service",check=False)
out["running"] = snap("running", live, active=True)
out["catalog_soa"] = call("dig","+time=2","+tries=1","+norecurse","+noall","+comments","+answer","@127.0.0.1",catalog,"SOA",check=False)
out["catalog_axfr"] = call("dig","+time=2","+tries=1","+norecurse","+noall","+answer","@127.0.0.1",catalog,"AXFR",check=False)
print(json.dumps(out, sort_keys=True, separators=(",",":")))
