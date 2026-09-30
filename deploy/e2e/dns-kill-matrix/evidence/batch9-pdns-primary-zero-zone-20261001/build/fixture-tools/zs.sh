# Read-only: serial history, pdns catalog/notify journal lines, peer-sampler cat serial transitions for one cell. usage: zs.sh SHORT
E=/var/tmp/cp-b9-1001/evidence/$1
echo "== catalog-serial-history (condensed)"
python3 - $E/catalog-serial-history.txt <<'PY'
import json,sys
for line in open(sys.argv[1]):
    line=line.rstrip("\n")
    if not line.startswith("{"): print(line); continue
    mark = "<-" in line
    j = json.loads(line.split("  <-")[0])
    d = j["catalog_domain(id,type,master,account,options,catalog,notified_serial,last_check)"]
    print(j["copy"].split("/")[-1][:17], j["copy"].split("/")[2], "serial", j["catalog_soa_serial"], "notified", d[6] if isinstance(d,list) else d, "meta", j["catalog_metadata"], "members", len(j["member_soa"]), "CHANGE" if mark else "")
PY
echo "== pdns journal: catalog/hash/notify lines"
grep -iE 'catalog|hash|notif|spurious|failed after|does not resolve|start|ready' $E/raw/journald/pdns.service.txt | cut -c1-260 | head -60
echo "== peer sampler cat serial transitions (primary .10 udp, secondary .11 udp)"
grep -o '^[^ ]*\|10:cat:udp=[^ ]*\|11:cat:udp=[^ ]*\|^########.*' $E/peer-dns-sampler.log | paste -d' ' - - - | awk '{k=$2" "$3; if (k!=last) print; last=k}' | head -40
echo "last sample:"; grep -v '^####' $E/peer-dns-sampler.log | tail -1 | cut -c1-600
