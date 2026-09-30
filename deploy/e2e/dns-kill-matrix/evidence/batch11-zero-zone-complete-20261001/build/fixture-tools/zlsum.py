#!/usr/bin/env python3
# Batch 10: summary of the zone lifecycle evidence of one collected cell (read-only). Arg: evidence dir
import json, sys, glob, os, datetime
E = sys.argv[1]
def t(s):
    return datetime.datetime.fromisoformat(s.replace("Z", "+00:00")[:26] + "+00:00") if s else None
for f in sorted(glob.glob(os.path.join(E, "fresh-primary-peer", "zone-lifecycle*.json"))):
    d = json.load(open(f))
    print("##", os.path.basename(f), "status=", d.get("status"), "recover_delete=", d.get("recover_delete"), "zero_zones=", d.get("zero_zones"))
    for s in d.get("steps", []):
        r = s.get("result", {})
        print("  step", s["step"], "recover=", s["recover"], "rc=", s["returncode"], "outcome=", r.get("outcome"), "verdict=", s["verdict"],
              "job=", r.get("request_id", "")[:8], r.get("job_status"), (r.get("job_phase") or "").split("/")[3:4], r.get("job_error_code"), "heartbeats=", r.get("heartbeats"),
              "obs_err=", s.get("observation_error"))
    if "outcome" in d or "lifecycle_status" in d:
        print("  ", {k: d[k] for k in d if k in ("outcome", "lifecycle_status", "peer_verdict_exit", "lifecycle_exit", "guest")})
led = os.path.join(E, "raw", "state", "service-mutations.json")
if os.path.exists(led):
    d = json.load(open(led))
    print("## ledger (collected copy)")
    for rid, j in sorted(d["jobs"].items(), key=lambda kv: kv[1].get("started_at", "")):
        st, fi = t(j.get("started_at")), t(j.get("finished_at"))
        dur = round((fi - st).total_seconds(), 2) if st and fi and j.get("finished_at", "").startswith("20") else None
        print("  ", rid[:8], j["kind"], j.get("target"), j["status"], j["phase"].split("/")[3] if j["kind"] == "dns_zone_sync" else j["phase"].split("/")[3],
              "att=", j.get("attempt"), "code=", j.get("error_code"), "started=", j.get("started_at"), "finished=", j.get("finished_at"), "dur=", dur)
