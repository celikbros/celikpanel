# Derived, read-only: copies selected sub-objects of result.json verbatim (native-state records,
# outage, status reads, complete verdict, reboot and retry records, kill proof and boundary marker).
import json, sys
r = json.load(open(sys.argv[1]))
keys = ["status", "safety_status", "request_id", "recovery_outcome", "complete_verdict", "pre_reboot_verdict",
        "recovery_status_reads", "recovery_status_read_failures", "reboot_after_recovery",
        "management_disabled_before_reboot", "retry_switch_after_rollback", "agent_startup_rollback",
        "owner_inverse_native_at_boundary", "dns_outage", "owner_inverse_failures", "boundary_marker",
        "kill", "kill_proven", "provenance_boundary", "journal_at_boundary", "verification_failures",
        "safety_failures", "diagnostic_failures", "failures"]
out = {"source": "result.json (verbatim sub-objects)"}
for k in keys:
    out[k] = r.get(k)
f = r.get("owner_inverse_after_restart") or {}
out["variant"] = f.get("variant"); out["expectation"] = f.get("expectation")
out["flow_status"] = f.get("status"); out["flow_failures"] = f.get("failures"); out["flow_ambiguities"] = f.get("ambiguities")
steps = {}
def walk(obj, path):
    if isinstance(obj, dict):
        for k, v in obj.items():
            if k in ("native", "pdns_main_pid", "bind_units", "rollback_end_state"):
                steps[".".join(path + [k])] = v
            walk(v, path + [k])
    elif isinstance(obj, list):
        for i, v in enumerate(obj): walk(v, path + [str(i)])
walk(f.get("steps") or {}, ["steps"])
out["step_records"] = steps
json.dump(out, sys.stdout, indent=2, sort_keys=True); print()
