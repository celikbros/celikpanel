# Derived, read-only: copies the controller's native-state records, PowerDNS PID record and dns_outage out of result.json.
import json, sys
r = json.load(open(sys.argv[1]))
out = {"source": "result.json (verbatim sub-objects)", "status": r.get("status"), "safety_status": r.get("safety_status"),
       "request_id": r.get("request_id"), "recovery_outcome": r.get("recovery_outcome"),
       "owner_inverse_native_at_boundary": r.get("owner_inverse_native_at_boundary"),
       "dns_outage": r.get("dns_outage"), "owner_inverse_failures": r.get("owner_inverse_failures")}
f = r.get("owner_inverse_after_restart") or {}
out["variant"] = f.get("variant"); out["expectation"] = f.get("expectation")
out["flow_status"] = f.get("status"); out["flow_failures"] = f.get("failures"); out["flow_ambiguities"] = f.get("ambiguities")
steps = {}
def walk(obj, path):
    if isinstance(obj, dict):
        for k, v in obj.items():
            if k in ("native", "pdns_main_pid", "bind_units"):
                steps[".".join(path + [k])] = v
            walk(v, path + [k])
    elif isinstance(obj, list):
        for i, v in enumerate(obj): walk(v, path + [str(i)])
walk(f.get("steps") or {}, ["steps"])
out["step_records"] = steps
json.dump(out, sys.stdout, indent=2, sort_keys=True); print()
