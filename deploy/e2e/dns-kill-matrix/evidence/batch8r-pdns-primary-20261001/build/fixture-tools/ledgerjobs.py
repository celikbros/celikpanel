# Read-only: zone-sync and switch jobs from the collected ledger. usage: ledgerjobs.py SHORT
import json, sys
E = f"/var/tmp/cp-b8r-1001/evidence/{sys.argv[1]}/raw/state/service-mutations.json"
d = json.load(open(E))
jobs = d.get("jobs") if isinstance(d, dict) else None
if jobs is None:
    print("top keys", list(d.keys())[:20]); jobs = []
items = jobs.values() if isinstance(jobs, dict) else jobs
for j in sorted(items, key=lambda x: x.get("started_at", "")):
    print(j.get("kind"), j.get("target"), j.get("request_id"), j.get("status"), "start", j.get("started_at"), "fin", j.get("finished_at"), "phase", (j.get("phase") or "")[:60], j.get("error_code"))
