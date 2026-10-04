SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
for s in z04-zero-committed-zl z05-zero-started-zl-rb; do
E=/var/tmp/cp-b12-1001/evidence/$s
echo "################ $s"
python3 $SP/facts.py $E 2>&1 | cut -c1-900
echo "== notify"
N=$E/pdns-notify-journal-and-also-notify.txt
echo "matching: $(grep -E '^matching lines' $N)  spurious=$(grep -ci spurious $N) ':0 '=$(grep -c ':0 ' $N) failed-after-retries=$(grep -c 'failed after retries' $N) notif-request-53=$(grep -c 'Notification request to host 192.0.2.11:53' $N) unsuccessful=$(grep -c 'unsuccessful notification' $N) does-not-resolve=$(grep -c 'does not resolve' $N)"
grep -E 'also-notify' $N | head -3
echo "== catalog serial history (changes)"
grep -E 'serial change' $E/catalog-serial-history.txt | python3 -c '
import sys,json
for l in sys.stdin:
    j=json.loads(l.split("  <-")[0]); print(j["copy"].split("/")[-1][:10], j["catalog_soa_serial"], [m[0] for m in j["member_soa"]], [x for x in j["catalog_metadata"] if x[0]=="CATALOG-HASH"])'
echo "== sampler changes"; cut -c1-330 $E/peer-dns-sampler-changes.txt
done
