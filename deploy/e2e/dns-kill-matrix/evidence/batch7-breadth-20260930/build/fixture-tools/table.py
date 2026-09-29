# Read-only per-cell summary over retained evidence (for the README).
import json, os, re, sys, glob
ROOT = "/var/tmp/cp-b7-0930/evidence"
for line in open(os.path.join(os.path.dirname(__file__), "cells.txt")):
    s, c = line.split()
    E = f"{ROOT}/{s}"
    rp = f"{E}/raw/results/{c}/result.json"
    r = json.load(open(rp))
    g = lambda *ks: __import__("functools").reduce(lambda a, k: a.get(k) if isinstance(a, dict) else None, ks, r)
    pv = {}
    if os.path.exists(f"{E}/paired-secondary-peer/peer-verdict.json"):
        pv = json.load(open(f"{E}/paired-secondary-peer/peer-verdict.json"))
    rc = open(f"{E}/run-prepared.rc").read().strip()
    ra = r.get("reboot_after_recovery") or {}
    rs = r.get("retry_switch_after_rollback") or {}
    oi = r.get("owner_inverse_after_restart") or {}
    k = r.get("kill") or {}
    print(f"== {s}")
    print(f"  status={r.get('status')} safety={r.get('safety_status')} class={g('recovery_outcome','classification')} complete={g('complete_verdict','passed')} kill_proven={r.get('kill_proven')} {rc}")
    print(f"  request={r.get('request_id')} controller {r.get('started_at')} -> {r.get('finished_at')}")
    print(f"  marker={g('boundary_marker','recorded_at')} phase_on_disk={g('journal_at_boundary','observed_phase')} kill={k.get('delivered_at')} pid={k.get('pid')} state={k.get('state_before_signal')} exit={k.get('exit_code')} trigger_rc={r.get('scenario_trigger_returncode')}")
    print(f"  failures v={r.get('verification_failures')} s={r.get('safety_failures')} d={r.get('diagnostic_failures')}")
    if ra: print(f"  reboot_after_recovery run={ra.get('run')} status={ra.get('status')} dns_only={(ra.get('stability') or {}).get('dns_only')} n={(ra.get('stability') or {}).get('sample_count')}")
    for b in r.get("reboots") or []: print(f"  reboot {b.get('ordinal')} {b.get('boot_id_before')} -> {b.get('boot_id_after')}")
    if oi: print(f"  OI variant={oi.get('variant')} status={oi.get('status')} fail={oi.get('failures')} amb={oi.get('ambiguities')}")
    if rs: print(f"  retry_switch run={rs.get('run')} status={rs.get('status')} req={rs.get('request_id')} job={g('retry_switch_after_rollback','job','status')} class={g('retry_switch_after_rollback','recovery_outcome','classification')}")
    if r.get("dns_outage"): print("  dns_outage", json.dumps({x: r["dns_outage"].get(x) for x in ("source_stopped_at","source_serving_again_at","seconds")}))
    for att in g("recovery", "attempts") or []:
        cmd = att.get("command") or {}
        print(f"  retry {att.get('ordinal')} rc={cmd.get('returncode')} dur={cmd.get('duration_seconds')}")
    job = None
    for st, v in (r.get("recovery_status_reads") or {}).items():
        cmd = v.get("command") or {}
        print(f"  status-read {st} avail={v.get('available')} rc={cmd.get('returncode')} mutated={v.get('mutated')}")
    nv = r.get("native_versions") or {}
    print("  native_versions keys", list(nv.keys()))
    if pv: print(f"  peer status={pv.get('status')} combined={pv.get('combined_exit')} fmt={pv.get('peer_catalog_format')} engine={pv.get('peer_engine')} producer={g2 if (g2:=(pv.get('after_recovery') or {}).get('catalog_producer')) else None} serial={(pv.get('after_recovery') or {}).get('catalog_serial')} labels={(pv.get('after_recovery') or {}).get('catalog_member_labels')}")
    so = r.get("dpkg_statoverride")
    if so: print("  dpkg_statoverride", json.dumps(so)[:300])
    # pdns metadata counts after collect
    p = f"{E}/pdns-db-post-collect.txt"
    if os.path.exists(p):
        t = open(p).read()
        m = re.findall(r"-- table (\w+) rows=(\d+)", t)
        print("  pdns tables", dict(m) if m else t.splitlines()[0][:120])
