#!/usr/bin/env python3
"""Per-cell verdict summary from the driver's own step records (host only, read-only)."""
import glob, json, sys
CELLS = [("upd3-d13-good-a", "debian13"), ("upd3-d13-def-a", "debian13"), ("upd3-d13-sc-a", "debian13"),
         ("upd3-d13-rs-a", "debian13"), ("upd3-arch-good-a", "arch"), ("upd3-arch-def-a", "arch"),
         ("upd3-arch-sc-a", "arch"), ("upd3-arch-rs-a", "arch")]


def step(ev, name):
    f = glob.glob(ev + f"steps/*-{name}/step.json")
    return json.load(open(f[0])) if f else {}


for lab, node in CELLS:
    ev = sorted(glob.glob(f"/var/tmp/cp-release-drill-{lab}/evidence/{node}/upd1/*/"))[-1]
    print(f"==================== {lab} {ev.split('/')[-2]}")
    v = step(ev, "verdicts").get("checks", {})
    for k, w in (v.get("workloads") or {}).items():
        wins = [(x.get("start_utc") or x.get("start"), x.get("end_utc") or x.get("end"), x.get("min_seconds"), x.get("max_seconds"), x.get("label"))
                for x in (w.get("windows") or [])]
        print(f"  workload {k}: {w.get('verdict')} windows={json.dumps(wins)[:400]}")
    print("  host_panel_windows", json.dumps(v.get("host_panel_windows"))[:400])
    print("  host_ssh_windows", json.dumps(v.get("host_ssh_windows"))[:300])
    print("  agreement", json.dumps(v.get("agreement")), "skew", v.get("clock_skew_seconds"))
    t = step(ev, "terminal").get("checks", {})
    keys = ("outcome", "build_identity_ok", "installed", "floor", "foundation", "transaction", "firewall_equal", "site_marker",
            "smtp", "login_ok", "seeded_rows", "login")
    print("  terminal", json.dumps({k: t.get(k) for k in keys if k in t})[:900])
    db = t.get("database") or t.get("database_vs_pre_update") or {}
    print("  database", db.get("verdict"), "differing", db.get("differing"), "unexpected", db.get("unexpected"), "excluded", db.get("volatile_excluded"))
    print("  timers", json.dumps(t.get("timers"))[:300])
    k = step(ev, "kind-expectation").get("checks", {})
    obs = k.get("observations") or {}
    if obs:
        print("  views", json.dumps(obs.get("views"))[:700])
        print("  check_reasons", obs.get("check_reasons"), "update_failure_codes", obs.get("update_failure_codes"),
              "sidecar", obs.get("sidecar"), "completion_marker_seen", obs.get("completion_marker_seen"))
        cli = obs.get("cli") or {}
        print("  cli by_key", json.dumps(cli.get("by_key")), "mismatches", len(cli.get("mismatches") or []),
              "panel_log_seen", cli.get("panel_log_command_seen"))
        print("  cli_text_source", json.dumps(k.get("cli_text_source")))
    oc = step(ev, "owner-continuation-required").get("checks", {})
    if oc:
        print("  continuation", json.dumps({x: oc.get(x) for x in ("required", "printed_retry", "owner_retry")})[:700])
        views = oc.get("views") or {}
        print("  views_at_pause api", json.dumps(views.get("api"))[:300])
        print("  views_at_pause offline", json.dumps((views.get("offline_shell") or {}).get("status_command")))
    tr = step(ev, "track").get("checks", {})
    print("  track", json.dumps({x: tr.get(x) for x in ("samples", "agreement", "h8_settled_failed")})[:400])
    # guest sampler during the whole period (real-start: after the start)
    gs = glob.glob(ev + "steps/*-collect/guest-samples.jsonl")
    if gs:
        rows = [json.loads(line) for line in open(gs[0]) if line.strip()]
        st = step(ev, "owner-start").get("started_at")
        after = [r for r in rows if str(r.get("utc", "")) >= (st or "")]
        def ok(r, key):
            val = r.get(key)
            return (val or {}).get("ok") if isinstance(val, dict) else val
        cnt = {key: (sum(1 for r in after if ok(r, key) is True), sum(1 for r in after if ok(r, key) is False)) for key in ("web", "smtp", "panel")}
        stamps = sorted({(r.get("cron") or {}).get("content") for r in after if isinstance(r.get("cron"), dict)} - {None})
        print(f"  guest samples total={len(rows)} after-start={len(after)} ok/fail web={cnt['web']} smtp={cnt['smtp']} panel={cnt['panel']}"
              f" cron distinct stamps after start={len(stamps)} first={stamps[:1]} last={stamps[-1:]}")
        if rows:
            print("  sample keys", sorted(rows[0].keys()))
