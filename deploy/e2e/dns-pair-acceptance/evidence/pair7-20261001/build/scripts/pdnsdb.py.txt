import json, sqlite3, sys, time
out = {"t": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
try:
    c = sqlite3.connect("file:/var/lib/powerdns/pdns.sqlite3?mode=ro", uri=True, timeout=1)
    out["domains"] = [list(r) for r in c.execute(
        "SELECT id, name, type, notified_serial, COALESCE(catalog,''), COALESCE(account,'') FROM domains ORDER BY id")]
    meta = []
    for d, k, v in c.execute("SELECT domain_id, kind, COALESCE(content,'') FROM domainmetadata ORDER BY domain_id, kind"):
        meta.append([d, k, "<not read>" if "TSIG" in k.upper() else v])
    out["metadata"] = meta
    out["catalog_records"] = [list(r) for r in c.execute(
        "SELECT d.name, r.name, r.type, r.content FROM records r JOIN domains d ON d.id = r.domain_id "
        "WHERE d.type IN ('PRODUCER','CONSUMER') ORDER BY r.name, r.type, r.content")]
    c.close()
except Exception as exc:
    out["error"] = type(exc).__name__ + ": " + str(exc)[:200]
print(json.dumps(out, sort_keys=True))
