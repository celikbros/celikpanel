#!/usr/bin/env python3
"""Per-cell summary of the staged upd5 evidence (host only, read-only). usage: summary4.py STAGE OUTDIR"""
import datetime as dt, glob, json, sys
from pathlib import Path
stage, outdir = Path(sys.argv[1]), Path(sys.argv[2])
ABBR = {"passed": "P", "observed": "O", "failed": "F", "inconclusive": "I", "skipped": "S", "not-run": "N"}


def hm(s):
    return (s or "")[11:19]


def u(x):
    return dt.datetime.fromtimestamp(x, dt.timezone.utc).strftime("%H:%M:%S") if isinstance(x, (int, float)) else str(x)


def step(run, name):
    f = sorted(run.glob(f"steps/*-{name}/step.json"))
    return json.load(open(f[0])) if f else {}


rows, summ, wins = [], [], []
for run in sorted(stage.glob("upd1-*/run-*")):
    res = json.load(open(run / "result.json"))
    cell = run.parent.name + ("" if run.name == "run-a" else f" ({run.name})")
    steps = {s["name"]: s for s in res["steps"]}
    o = res.get("outcome") or {}
    att = o.get("attempts") or {}
    kind = ((res.get("kind") or {}).get("judged") or {})
    summ.append(f"== {cell}: overall={res.get('overall')} outcome={o.get('classification')} native_evidence={res.get('native_evidence')} "
                f"request={res.get('request_id')}")
    summ.append(f"   final={json.dumps(o.get('final_status'))}")
    summ.append(f"   automatic attempts={att.get('automatic_count')} "
                + json.dumps([(a.get('attempt'), a.get('operation'), a.get('phase'), a.get('direction'), a.get('at')) for a in att.get('automatic') or []])
                + f" owner={att.get('owner_count')} " + json.dumps([(a.get('operation'), a.get('phase'), a.get('at')) for a in att.get('owner') or []]))
    if o.get("reboot"):
        summ.append(f"   second fault reset: {json.dumps(o['reboot'])}")
    if o.get("track_stop"):
        summ.append(f"   track_stop: {json.dumps(o['track_stop'])[:600]}")
    if o.get("port_hold"):
        summ.append(f"   port_hold: {json.dumps(o['port_hold'])}")
    if o.get("owner_reboot"):
        summ.append(f"   owner_reboot: {json.dumps(o['owner_reboot'])}")
    if kind:
        summ.append(f"   kind {kind.get('kind')}: {kind.get('verdict')} findings={len(kind.get('findings') or [])} unknown={kind.get('unknown')}")
        for f in kind.get("findings") or []:
            summ.append(f"     - {f[:400]}")
    for s in res["steps"]:
        summ.append(f"   {s['name']:34s} {s['verdict']:12s} {hm(s.get('started_at'))}-{hm(s.get('finished_at'))} {(s.get('reason') or '')[:300]}")
    tr = step(run, "track").get("checks") or {}
    v = step(run, "verdicts").get("checks") or {}
    summ.append(f"   agreement(track)={json.dumps(tr.get('agreement'))} agreement(verdicts)={json.dumps(v.get('agreement'))}")
    # guest samples after the start
    gs = sorted(run.glob("steps/*-collect/guest-samples.jsonl"))
    st = steps.get("owner-start", {}).get("started_at")
    if gs:
        sm = [json.loads(line) for line in open(gs[0]) if line.strip()]
        after = [r for r in sm if str(r.get("utc", "")) >= (st or "")]
        def ok(r, k):
            val = r.get(k)
            return val.get("ok") if isinstance(val, dict) else None
        cnt = {k: (sum(1 for r in after if ok(r, k) is True), sum(1 for r in after if ok(r, k) is False)) for k in ("web", "smtp", "panel", "db")}
        stamps = sorted({(r.get("cron") or {}).get("content") for r in after if isinstance(r.get("cron"), dict)} - {None})
        summ.append(f"   guest samples total={len(sm)} after-start={len(after)} ok/fail web={cnt['web']} smtp={cnt['smtp']} panel={cnt['panel']} db={cnt['db']} "
                    f"cron distinct stamps after start={len(stamps)} first={stamps[:1]} last={stamps[-1:]}")
    wins.append(f"== {cell}")
    for k, w in (v.get("workloads") or {}).items():
        for x in w.get("windows") or []:
            wins.append(f"  {k:6s} {w.get('verdict')}: {u(x.get('from'))}-{u(x.get('to'))} bound {x.get('lower_bound_s')}-{x.get('upper_bound_s')} s kind={x.get('kind')} cause={x.get('cause')}")
        if not w.get("windows"):
            wins.append(f"  {k:6s} {w.get('verdict')}")
    for x in v.get("host_panel_windows") or []:
        wins.append(f"  host-panel: {u(x.get('from'))}-{u(x.get('to'))} bound {x.get('lower_bound_s')}-{x.get('upper_bound_s')} s")
    for x in v.get("host_ssh_windows") or []:
        wins.append(f"  host-ssh: {u(x.get('from'))}-{u(x.get('to'))} bound {x.get('lower_bound_s')}-{x.get('upper_bound_s')} s")
    order = ["preflight", "origin", "baseline-install", "owner-login", "license", "setup", "seed", "pre-state", "arm", "owner-start",
             "track", "owner-continuation (required)", "track-after-owner-continuation", "terminal", "management-off", "owner-reboot",
             "management-off-measure", "management-return", "collect", "verdicts", "kind-expectation"]
    rows.append((cell, {n: (ABBR.get(steps[n]["verdict"], steps[n]["verdict"]) + " " + hm(steps[n].get("started_at"))[:5] + "-" + hm(steps[n].get("finished_at"))[:8])
                        if n in steps else "-" for n in order}, order))
(outdir / "summary-per-cell.txt").write_text("\n".join(summ) + "\n")
(outdir / "outage-windows.txt").write_text("\n".join(wins) + "\n")
order = rows[0][2]
md = ["| Step | " + " | ".join(r[0].replace("upd1-", "") for r in rows) + " |", "| --- |" + " --- |" * len(rows)]
for n in order:
    md.append(f"| {n} | " + " | ".join(r[1][n] for r in rows) + " |")
(outdir / "step-table.md").write_text("\n".join(md) + "\n")
print("\n".join(summ))
