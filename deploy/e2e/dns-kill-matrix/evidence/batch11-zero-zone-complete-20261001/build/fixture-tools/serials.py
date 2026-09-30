# Read-only: catalog SOA serial history from the retained sampler copies of the PowerDNS database (with WAL),
# plus the domains row of the catalog (PRODUCER) and CATALOG-HASH metadata, per copy. usage: serials.py EVIDENCE_DIR
import sqlite3, sys, os, glob, shutil, tempfile, json
E = sys.argv[1]
CAT = "catalog-c000020a.celikpanel.invalid"
rows = []
for f in sorted(glob.glob(f"{E}/raw/watch/*/copies/*.sqlite3")):
    td = tempfile.mkdtemp(prefix="ser-")
    shutil.copy(f, f"{td}/db.sqlite3")
    if os.path.exists(f + "-wal"):
        shutil.copy(f + "-wal", f"{td}/db.sqlite3-wal")
    try:
        c = sqlite3.connect(f"file:{td}/db.sqlite3", uri=True, timeout=5)
        d = c.execute("SELECT id,type,master,account,options,catalog,notified_serial,last_check FROM domains WHERE name=?", (CAT,)).fetchone()
        soa = None
        if d:
            s = c.execute("SELECT content FROM records WHERE domain_id=? AND type='SOA'", (d[0],)).fetchone()
            soa = s[0] if s else None
            meta = c.execute("SELECT kind,content FROM domainmetadata WHERE domain_id=?", (d[0],)).fetchall()
        else:
            meta = []
        m = c.execute("SELECT d.name, r.content FROM records r JOIN domains d ON d.id=r.domain_id WHERE r.type='SOA' AND d.name!=? ORDER BY d.name", (CAT,)).fetchall()
        c.close()
        rows.append((os.path.relpath(f, E), d, soa, meta, m))
    except sqlite3.Error as e:
        rows.append((os.path.relpath(f, E), f"sqlite error {e}", None, [], []))
    shutil.rmtree(td, ignore_errors=True)
print(f"catalog {CAT}: one line per retained database copy (watcher dir/copies/<UTC HHMMSS.mmm>-<hash12>.sqlite3)")
last = None
for name, d, soa, meta, members in rows:
    serial = soa.split()[2] if isinstance(soa, str) and len(soa.split()) >= 3 else None
    mark = "" if serial == last else "  <- serial change"
    last = serial
    print(json.dumps({"copy": name, "catalog_domain(id,type,master,account,options,catalog,notified_serial,last_check)": d,
                      "catalog_soa_serial": serial, "catalog_metadata": meta, "member_soa": members}, default=str) + mark)
