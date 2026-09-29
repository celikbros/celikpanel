# Read-only full dump of a PowerDNS SQLite database (every table; records also counted per domain).
# usage: python3 pdnsdb.py PATH [immutable|copy]
#   live DB: mode=ro; "copy": a retained file copy is opened from a temporary copy together with its PATH-wal (if retained);
#   "immutable": main file only, WAL ignored.
import sqlite3, sys, json, os, hashlib, shutil, tempfile
p = sys.argv[1]; mode = sys.argv[2] if len(sys.argv) > 2 else ""; imm = mode == "immutable"
if not os.path.exists(p):
    print(f"== {p}: absent"); sys.exit(0)
st = os.stat(p)
print(f"== {p} size={st.st_size} mtime_ns={st.st_mtime_ns} sha256={hashlib.sha256(open(p,'rb').read()).hexdigest()}")
for s in ("-journal", "-wal", "-shm"):
    if os.path.exists(p + s): print(f"   sibling {p+s} size={os.stat(p+s).st_size}")
uri = f"file:{p}?" + ("immutable=1" if imm else "mode=ro")
if mode == "copy":
    td = tempfile.mkdtemp(prefix="pdnsdb-")
    shutil.copy(p, os.path.join(td, "db.sqlite3"))
    if os.path.exists(p + "-wal"):
        shutil.copy(p + "-wal", os.path.join(td, "db.sqlite3-wal")); print(f"   with retained WAL {p}-wal size={os.stat(p+'-wal').st_size}")
    else:
        print("   no retained WAL copy")
    uri = f"file:{os.path.join(td, 'db.sqlite3')}"
try:
    c = sqlite3.connect(uri, uri=True, timeout=5)
    tables = [r[0] for r in c.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")]
    print("tables:", tables)
    for t in tables:
        cols = [r[1] for r in c.execute(f'PRAGMA table_info("{t}")')]
        try:
            rows = c.execute(f'SELECT * FROM "{t}" ORDER BY 1').fetchall()
        except sqlite3.Error as e:
            print(f"-- table {t}: sqlite error {e}"); continue
        print(f"-- table {t} rows={len(rows)} columns={cols}")
        if t == "records":
            for r in c.execute("SELECT d.name, COUNT(*) FROM records r LEFT JOIN domains d ON d.id=r.domain_id GROUP BY d.name ORDER BY d.name"):
                print("   records per domain:", json.dumps(list(r)))
            for r in c.execute("SELECT d.name, r.type, COUNT(*) FROM records r LEFT JOIN domains d ON d.id=r.domain_id GROUP BY d.name, r.type ORDER BY d.name, r.type"):
                print("   records per domain/type:", json.dumps(list(r)))
        for row in rows:
            print(f"   {t}:", json.dumps(dict(zip(cols, row)), default=str))
    c.close()
except sqlite3.Error as e:
    print("sqlite error:", e)
