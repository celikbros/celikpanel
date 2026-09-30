E=/var/tmp/cp-b10-1001/evidence/z05-held-resume
echo "== notify capture"; grep -vE '^ +\[' $E/pdns-notify-journal-and-also-notify.txt | grep -E 'pdns_server|matching|also-notify=' | cut -c1-230
echo "spurious: $(grep -ci spurious $E/pdns-notify-journal-and-also-notify.txt)  ':0 ' failed: $(grep -c ':0 failed after retries' $E/pdns-notify-journal-and-also-notify.txt)  failed after retries: $(grep -c 'failed after retries' $E/pdns-notify-journal-and-also-notify.txt)  192.0.2.11:53 lines: $(grep -c '192.0.2.11:53' $E/pdns-notify-journal-and-also-notify.txt)"
echo "== CATALOG-HASH / serial lines in pdns journal"; grep -iE 'catalog-hash|serial' $E/raw/journald/pdns.service.txt | cut -c1-230
echo "== catalog serial history"; python3 - $E/catalog-serial-history.txt <<'PY'
import json,sys
for l in open(sys.argv[1]):
    l=l.strip()
    if not l.startswith("{"): continue
    j=json.loads(l.split("}  <-")[0]+"}" if "}  <-" in l else l)
    print(j["copy"].split("/")[-1][:19], j.get("catalog_soa_serial"), j.get("catalog_metadata"), "members=",[m for m in j.get("member_soa",[])], "<- change" if "<-" in l else "")
PY
