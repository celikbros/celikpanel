# Read-only digest of one retained cell for README writing.
import json, sys, glob, os, re
E, cell = sys.argv[1], sys.argv[2]
r = json.load(open(f"{E}/raw/results/{cell}/result.json"))
g = lambda o, *ks: (lambda x: x)(__import__('functools').reduce(lambda a, k: (a[k] if isinstance(a, list) and isinstance(k, int) and len(a) > k else (a or {}).get(k) if isinstance(a, dict) else None), ks, o))
def first(s, n=2): return " / ".join((s or "").splitlines()[:n])
print("request", r.get("request_id"), "controller", r.get("started_at"), "->", r.get("finished_at"))
print("status", r.get("status"), "safety", r.get("safety_status"), "class", g(r, "recovery_outcome", "classification"), "fp", (g(r,"recovery_probes",0,"fingerprint") or g(r,"recovery_probes",0,"probe","fingerprint") or "")[:8])
k = r.get("kill") or {}
print("kill pid", k.get("pid"), "state", k.get("state_before_signal"), "at", k.get("delivered_at"), "exit", k.get("exit_code"), "reaped", k.get("proc_entry_absent_after_reap"))
print("marker", g(r, "boundary_marker", "recorded_at"), "journal", g(r, "journal_at_boundary", "observed_phase"), (g(r, "journal_at_boundary", "sha256") or "")[:8])
print("trigger rc", r.get("scenario_trigger_returncode"))
for a in g(r, "recovery", "attempts") or []:
    c = a.get("command") or {}
    print(" retry", a.get("ordinal"), "rc", c.get("returncode"), "dur", c.get("duration_seconds"), "err", a.get("error"))
for i, p in enumerate(r.get("recovery_probes") or []):
    print(" probe", i + 1, p.get("recovery_outcome"), (p.get("fingerprint") or "")[:8], p.get("active_dns_engine"), "|", json.dumps(p)[:0])
st = r.get("stability") or {}
samples = st.get("samples") or []
print("stability", st.get("sample_count"), "ok", sum(1 for s in samples if all((s.get(x) or {}).get("ok", True) for x in ("agent","dns","panel")) and (s.get("dns") or {}).get("ok")), samples[0].get("at") if samples else None, samples[-1].get("at") if samples else None)
for b in r.get("reboots") or []:
    print(" reboot", json.dumps({kk: b.get(kk) for kk in ("ordinal", "point", "boot_id_before", "boot_id_after", "product_uuid")}))
rb = r.get("reboot_after_recovery") or {}
if rb:
    s2 = rb.get("stability") or {}; sm = s2.get("samples") or []
    print("reboot_after_recovery run", rb.get("run"), "status", rb.get("status"), "failures", rb.get("failures"), "unknown", rb.get("unknown"), "window", s2.get("sample_count"), "ok", sum(1 for s in sm if (s.get("dns") or {}).get("ok")), "dns_only", s2.get("dns_only"))
    print("  authority_after_boot", json.dumps(rb.get("authority_after_boot"))[:400])
    if rb.get("management_disabled"): print("  mgmt disabled", json.dumps(rb.get("management_disabled"))[:300])
for stg, v in (r.get("recovery_status_reads") or {}).items():
    c = v.get("command") or {}
    print(f" status-read {stg}: available={v.get('available')} rc={c.get('returncode')} mutated={v.get('mutated')} :: {first(c.get('output'), 2)[:400]}")
asr = r.get("agent_startup_rollback")
if asr: print("agent_startup_rollback", json.dumps({kk: asr.get(kk) for kk in ("status", "failures", "unknown")}), "job", json.dumps({kk: g(asr, "release", "job", kk) or g(asr, "job", kk) for kk in ("status", "phase", "error_code")}))
oi = r.get("owner_inverse_after_restart") or {}
if oi:
    s = oi.get("steps") or {}
    print("OI variant", oi.get("variant"), "status", oi.get("status"), "failures", oi.get("failures"), "ambig", oi.get("ambiguities"))
    print(" release", json.dumps({kk: g(s, "agent_restarted", "release", "job", kk) for kk in ("status", "phase", "error_code")}), "journal", json.dumps(g(s, "agent_restarted", "journal"))[:200])
    for l in g(s, "agent_restarted", "agent_journal_refusal", "matching_lines") or []: print(" refusal:", l[:700])
    print(" status rc", g(s, "status", "command", "returncode"), "names", g(s, "status", "names_owner_command"), "changed", g(s, "status", "changed_evidence"), "::", first(g(s, "status", "command", "output"), 2)[:500])
    oc = g(s, "owner_command", "command") or {}
    print(" owner cmd rc", oc.get("returncode"), "dur", oc.get("duration_seconds")); print("  " + (oc.get("output") or "").replace("\n", "\n  ")[:2500])
    ao = s.get("after_owner_command") or {}
    print(" after: ledger_unchanged", ao.get("ledger_unchanged_since_release"), "state_bytes_unchanged", ao.get("state_bytes_unchanged"), "owner_files_unchanged", ao.get("owner_files_unchanged"), "pdns pid", json.dumps(ao.get("pdns_main_pid")))
    res = ao.get("rollback_end_state")
    if res: print(" rollback_end_state failures", res.get("failures"), "tree_after", res.get("generation_tree_present_after"), "masks", {u: (d.get("properties") or {}).get("LoadState") for u, d in (g(res, "masks", "units") or {}).items()}, "before", g(res, "facts", "target_units_before"))
    rr = s.get("rerun") or {}
    print(" rerun rc", g(rr, "command", "returncode"), "changed", rr.get("changed_evidence"), "::", first(rr.get("stdout"), 1)[:300])
    sa = s.get("status_after_owner_command") or {}
    print(" status after cmd ::", first(g(sa, "command", "output"), 1)[:300])
    ab = s.get("after_reboot")
    if ab: print(" after_reboot(before owner cmd): changed_evidence", ab.get("changed_evidence"), "ledger_unchanged", ab.get("ledger_unchanged_since_release"), "journal", json.dumps(ab.get("journal"))[:160])
if r.get("dns_outage"): print("dns_outage", json.dumps({kk: r["dns_outage"].get(kk) for kk in ("source_stopped_at", "source_serving_again_at", "seconds", "pdns_started_after_stop_at")}))
rs = r.get("retry_switch_after_rollback")
if rs: print("retry_switch run", rs.get("run"), "status", rs.get("status"), "failures", rs.get("failures"), "req", rs.get("request_id"), "trigger rc", g(rs, "trigger", "returncode"), "dur", g(rs, "trigger", "duration_seconds"), "job", g(rs, "job", "status"), "attempt", g(rs, "job", "attempt"), "class", g(rs, "recovery_outcome", "classification"), "window", g(rs, "stability", "sample_count"), "named pid", g(rs, "bind_serving", "named_main_pid"), "units", json.dumps(g(rs, "bind_units", "units"))[:500])
fp = r.get("fixture_pass_definition")
if fp: print("fixture_pass_definition", json.dumps(fp)[:1500])
pb = r.get("provenance_boundary")
if pb: print("provenance_boundary", json.dumps(pb)[:600])
