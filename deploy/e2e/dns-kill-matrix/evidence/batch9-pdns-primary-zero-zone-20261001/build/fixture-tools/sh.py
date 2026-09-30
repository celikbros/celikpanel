# Read-only: condensed catalog-serial-history.txt (one line per retained DB copy). usage: sh.py FILE...
import json, sys
for p in sys.argv[1:]:
    print("########", p.split("/evidence/")[1])
    for line in open(p):
        line = line.rstrip("\n")
        if not line.startswith("{"): continue
        j = json.loads(line.split("  <-")[0])
        d = j["catalog_domain(id,type,master,account,options,catalog,notified_serial,last_check)"]
        meta = ";".join(f"{k}={v[:8]}" for k, v in j["catalog_metadata"])
        print(" ", j["copy"].split("/")[2], j["copy"].split("/")[-1][:10], "serial", j["catalog_soa_serial"], "notified", d[6] if isinstance(d, list) else d, meta, "members", [m[0] for m in j["member_soa"]], "CHANGE" if "<-" in line else "")
