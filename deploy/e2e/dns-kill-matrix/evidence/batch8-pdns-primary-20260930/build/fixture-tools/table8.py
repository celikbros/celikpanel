# Read-only per-cell summary over retained evidence (README drafting). usage: table8.py [SHORT...]
import json, os, sys, glob
ROOT = "/var/tmp/cp-b8-0930/evidence"
cells = [l.split() for l in open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "cells.txt")) if l.strip()]
want = set(sys.argv[1:])
for s, c in cells:
    if want and s not in want:
        continue
    E = f"{ROOT}/{s}"
    rp = f"{E}/raw/results/{c}/result.json"
    print(f"== {s} {c}")
    if not os.path.exists(rp):
        print("  no result.json"); continue
    r = json.load(open(rp))
    g = lambda *ks: __import__("functools").reduce(lambda a, k: a.get(k) if isinstance(a, dict) else None, ks, r)
    rc = open(f"{E}/run-prepared.rc").read().strip() if os.path.exists(f"{E}/run-prepared.rc") else "?"
    k = r.get("kill") or {}
    print(f"  status={r.get('status')} safety={r.get('safety_status')} class={g('recovery_outcome','classification')} complete={g('complete_verdict','passed')} {rc} kill_proven={r.get('kill_proven')}")
    print(f"  request={r.get('request_id')} controller {r.get('started_at')} -> {r.get('finished_at')} gate={g('gate_probe','gate')} agent_error={g('gate_probe','agent_error')!r}")
    print(f"  boundary={g('boundary','journal_phase')} disk={g('journal_at_boundary','observed_phase')} sha={str(g('journal_at_boundary','sha256'))[:12]} marker={g('boundary_marker','recorded_at')} kill={k.get('delivered_at')} pid={k.get('pid')} state={k.get('state_before_signal')} exit={k.get('exit_code')} trigger_rc={r.get('scenario_trigger_returncode')}")
    print(f"  failures v={r.get('verification_failures')} s={r.get('safety_failures')} d={r.get('diagnostic_failures')}")
    fp = r.get("fresh_primary_v3") or {}
    if fp:
        rs = fp.get("restart") or {}
        job = (rs.get("release") or {}).get("job") or {}
        print(f"  fp.failures={fp.get('failures')} fp.unknown={fp.get('unknown')}")
        print(f"  restart: variant={rs.get('variant')} code={job.get('error_code')} status={job.get('status')} attempt={job.get('attempt')} retired={rs.get('journal_retired')} msg={job.get('error_message')!r}")
        pi = rs.get("pre_install")
        if pi: print(f"  pre_install failures={pi.get('failures')} unknown={pi.get('unknown')} unit={ {x: (pi.get('pdns_unit') or {}).get(x) for x in ('LoadState','UnitFileState','ActiveState','MainPID')} } db={pi.get('database_paths_present')}")
        fw = rs.get("forward")
        if fw: print(f"  forward failures={fw.get('failures')} unknown={fw.get('unknown')} pid={fw.get('pdns_pid_history')} restamp={json.dumps(fw.get('catalog_restamp'))}")
        oc = rs.get("agent_journal_owner_command")
        if oc: print(f"  agent names owner command: {oc.get('observed')}")
        pr = fp.get("pair") or {}
        print(f"  pair primary={json.dumps(pr.get('primary'))} secondary_equal={pr.get('secondary') == pr.get('primary')} state_serial={pr.get('state_catalog_serial')}")
    cut = r.get("fresh_primary_v3_cut") or {}
    if cut:
        j = cut.get("journal") or {}
        print(f"  cut: variant={cut.get('variant')} pid={cut.get('pdns_main_pid')} files={ {x: v.get('exists') for x, v in (cut.get('files') or {}).items()} } sealed={j.get('candidate_sealed')} staged_serial={j.get('primary_catalog_serial')} native_serial={j.get('native_serial')} unit_before={j.get('target_unit_before')}")
    h = r.get("fresh_primary_v3_hold")
    if h:
        rj = (h.get("release") or {}).get("job") or {}
        print(f"  hold: status={h.get('status')} failures={h.get('failures')} unknown={h.get('unknown')} dns_judged={h.get('dns_judged')}")
        print(f"  hold release: code={rj.get('error_code')} status={rj.get('status')} phase={rj.get('phase')}")
        print(f"  hold release message: {rj.get('error_message')!r}")
        print(f"  hold journal_after_release={json.dumps(h.get('journal_after_release'))[:300]} unrelated={ (h.get('unrelated_begin') or {}).get('verdict') } {json.dumps((h.get('unrelated_begin') or {}).get('result'))[:400]}")
        print(f"  hold edit={json.dumps(h.get('edit'))[:500]}")
        o = h.get("owner_release_recovery")
        if o:
            st = o.get("status") or {}
            print(f"  owner status rc={(st.get('command') or {}).get('returncode')} mutated={st.get('mutated')}")
            print("  owner status output: " + repr((st.get('command') or {}).get('output'))[:3000])
            cm = o.get("command") or {}
            print(f"  owner command rc={cm.get('returncode')} dur={cm.get('duration_seconds')} output={cm.get('output')!r}"[:3000])
            rr = o.get("rerun") or {}
            print(f"  owner rerun rc={rr.get('returncode')} output={rr.get('output')!r}"[:1500])
            pi = o.get("pre_install") or {}
            print(f"  owner pre_install failures={pi.get('failures')} unknown={pi.get('unknown')} ledger={json.dumps(o.get('ledger'))[:400]}")
    for st, v in (r.get("recovery_status_reads") or {}).items():
        cm = v.get("command") or {}
        print(f"  status-read {st} avail={v.get('available')} rc={cm.get('returncode')} mutated={v.get('mutated')} :: {(cm.get('output') or '')[:160]!r}")
    ra = r.get("reboot_after_recovery") or {}
    if ra: print(f"  reboot_after_recovery run={ra.get('run')} status={ra.get('status')} reason={ra.get('reason')!r}"[:400])
    for a in g("recovery", "attempts") or []:
        cm = a.get("command") or {}
        print(f"  retry {a.get('ordinal')} rc={cm.get('returncode')} dur={cm.get('duration_seconds')}")
    for i, p in enumerate(r.get("recovery_probes") or []):
        print(f"  probe {i+1} {p.get('recovery_outcome')} {(p.get('fingerprint') or '')[:12]}")
    stb = r.get("stability") or {}
    print(f"  stability n={len(stb.get('samples') or [])}")
    pv = f"{E}/fresh-primary-peer/peer-verdict.json"
    if os.path.exists(pv):
        v = json.load(open(pv))
        print(f"  peer status={v.get('status')} combined={v.get('combined_exit')} guest_exit={v.get('guest_controller_exit')} error={v.get('error')} serial={(v.get('observation') or {}).get('catalog_serial')} members={(v.get('observation') or {}).get('catalog_members')}")
    else:
        print("  peer verdict: none")
    sm = f"{E}/service-mutation-status-post-collect.json"
    try:
        txt = open(sm).read(); js = json.loads(txt[txt.index("{"):])
        job = (js.get("response") or {}).get("job") or {}
        print(f"  smstatus: status={job.get('status')} code={job.get('error_code')} attempt={job.get('attempt')} msg={job.get('error_message')!r}")
    except Exception as e:
        print(f"  smstatus: {e}")
