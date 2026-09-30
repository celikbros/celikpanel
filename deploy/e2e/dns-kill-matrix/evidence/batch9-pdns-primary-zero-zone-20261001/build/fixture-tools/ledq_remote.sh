# Read-only: ledger entries of the zone requests, DNS answers for the child, agent journal lines after the delete. Arg: none
echo "wall=$(date -u +%FT%T.%NZ)"
python3 - <<'PY'
import json
d = json.load(open("/var/lib/celikpanel-agent-private/service-mutations.json"))
def walk(o):
    if isinstance(o, dict):
        if "request_id" in o and ("status" in o or "phase" in o):
            yield o
        for v in o.values(): yield from walk(v)
    elif isinstance(o, list):
        for v in o: yield from walk(v)
print("top-level keys:", list(d.keys()) if isinstance(d, dict) else type(d))
for j in walk(d):
    print(json.dumps({k: j.get(k) for k in ("request_id","kind","target","status","phase","error_code","error","message","started_at","finished_at","updated_at") if k in j})[:1500])
PY
echo "== agent journal since 01:27 (zone/delete/peer/enroll/inspect/propagation lines)"
journalctl -u celikpanel-agent.service --no-pager -o short-iso-precise --since "2026-09-30 01:27:00" | grep -iE 'zone|delet|peer|enrol|inspect|propagat|pending|recover' | cut -c1-600 | tail -n 60
echo "== dns-engine-state files"; ls -la --time-style=full-iso /var/lib/celikpanel-agent-private/
