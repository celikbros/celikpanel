for s in c08-fresh-pdns-in c09-fresh-pdns-cm c10-fresh-pdns-tv c12-sec-pdns-in c13-sec-pdns-cm c14-sec-pdns-rb; do
E=/var/tmp/cp-b7-0930/evidence/$s
echo "== $s"
grep -h "CHANGE pdnsdb" $E/raw/watch/cp-b7-watch/timeline.log | head -4 | cut -c1-80
awk '{for(i=1;i<=NF;i++) if($i ~ /^jphase=/) print $1, $i}' $E/raw/watch/cp-b7-watch/timeline.log | uniq -f1 | head -12 | tr '\n' ' '; echo
grep -E 'domainmetadata rows|records per domain:' $E/pdns-db-post-collect.txt | head -4
done
for s in c05-v2-ss-aw-rb2; do python3 -c "
import json,sys;r=json.load(open('/var/tmp/cp-b7-0930/evidence/$s/raw/results/bind__source-stopped__after-write__standalone__peer-reachable/result.json'))
print(json.dumps(r.get('native_versions'))[:600]); rs=r.get('retry_switch_after_rollback') or {}; print([k for k in rs.keys()])"; done
