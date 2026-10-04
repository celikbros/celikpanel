# Batch 11, read-only pacing gate before the z05 resume (no product, harness or guest change).
# z04 resumed ~57 s after its delete and the PowerDNS daemon's periodic catalog re-stamp fell inside the resumed attempt.
# This waits until at least 90 s have passed since the pending delete job started (the daemon's check period is ~60 s),
# then lists the primary's CATALOG-HASH lines so the record shows whether the post-delete re-stamp had already happened.
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b11
ROOT=/var/tmp/cp-b11-1001/r2; CELL=pdns-switch__target-started__after-write__paired-primary__peer-reachable
LOG=/var/tmp/cp-b11-1001/logs/z05-zero-started-zl-rb
OUT=$LOG/resume-pacing-gate.txt
G="python3 $SP/gssh.py $ROOT $CELL debian13"
ST=$($G "sudo python3 -c 'import json;d=json.load(open(\"/var/lib/celikpanel-agent-private/service-mutations.json\"));print([j[\"started_at\"] for j in d[\"jobs\"].values() if j.get(\"kind\")==\"dns_zone_sync\" and j.get(\"status\")==\"pending\"][0])'" < /dev/null | tr -d '\r\n')
T0=$(date -u -d "$ST" +%s)
{
echo "pending delete job started_at=$ST"
echo "gate opens at $(date -u -d @$((T0 + 90)) +%FT%TZ) (started_at + 90 s); now $(date -u +%FT%T.%3NZ)"
while [ "$(date +%s)" -lt $((T0 + 90)) ]; do sleep 2; done
echo "gate open at $(date -u +%FT%T.%3NZ)"
echo "== primary pdns.service CATALOG-HASH lines (this boot)"
$G "sudo journalctl -b -u pdns.service --no-pager -o short-iso-precise | grep 'CATALOG-HASH'" < /dev/null
echo "== served catalog SOA (primary, UDP) now"
$G "sudo python3 -" < $SP/pairq.py 2>/dev/null | grep -m1 '"server": "192.0.2.10", "qname": "catalog' | cut -c1-300
} > $OUT 2>&1
cat $OUT
