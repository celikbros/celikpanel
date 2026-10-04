# usage: summ.sh SHORT CELL
E=/var/tmp/cp-b6b-0930/evidence/$1
L=/var/tmp/cp-b6b-0930/logs/$1
echo "== collect.log"; cat $L/collect.log | grep -v '^/var/tmp' | head -40
R=$E/raw/results/$2/result.json
python3 - "$R" <<'PY'
import json,sys
r=json.load(open(sys.argv[1]))
def p(k): print(k, "=", json.dumps(r.get(k))[:1500])
for k in ["status","safety_status","kill_proven","verification_failures","safety_failures","diagnostic_failures","failures","complete_verdict","pre_reboot_verdict","recovery_status_read_failures"]: p(k)
print("classification", (r.get("recovery_outcome") or {}).get("classification"))
fp=r.get("fixture_pass_definition") or {}
print("fixture_pass_definition status/failures", json.dumps({k:fp.get(k) for k in ("status","failures","passed")})[:1500])
rb=r.get("reboot_after_recovery") or {}
print("reboot_after_recovery keys", list(rb.keys()))
print("reboot status", json.dumps({k:rb.get(k) for k in ("run","status","reason","reasons","failures","judged","diagnostic_reboot")})[:1500])
rs=r.get("retry_switch_after_rollback") or {}
print("retry_switch", json.dumps({k:rs.get(k) for k in ("run","status","reason","failures")})[:1500])
oi=r.get("owner_inverse_after_restart") or {}
print("owner_inverse status/failures/ambig", json.dumps({k:oi.get(k) for k in ("status","failures","ambiguities")})[:2500])
sr=r.get("recovery_status_reads") or {}
for st,v in sr.items():
    if isinstance(v,dict): print("status read", st, json.dumps({k:v.get(k) for k in ("returncode","mutated","changed_evidence","exit_code")})[:600], "| text:", (v.get("stdout") or v.get("output") or "")[:400].replace("\n"," / "))
bm=r.get("boundary_marker") or {}; k=r.get("kill") or {}
print("marker recorded_at", bm.get("recorded_at"), "| kill keys", list(k.keys())[:20])
PY
