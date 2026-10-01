#!/usr/bin/env python3
"""Read-only extraction from a finished upd4 cell's evidence (host only, no guest access).

usage: ext4.py LABNAME NODE OUTFILE
"""
import glob, json, re, sys
from pathlib import Path
lab, node, outfile = sys.argv[1:4]
ev = sorted(glob.glob(f"/var/tmp/cp-release-drill-{lab}/evidence/{node}/upd1/*/"))[-1]
out = []
P = out.append
J = lambda v, n=1500: json.dumps(v, ensure_ascii=False, sort_keys=True)[:n]


def step(name):
    f = sorted(glob.glob(ev + f"steps/*-{name}/step.json"))
    return json.load(open(f[0])) if f else {}


P(f"# extraction of {ev}")
res = json.load(open(ev + "result.json")) if Path(ev + "result.json").exists() else {}
P(f"native_evidence={res.get('native_evidence')} overall={res.get('overall')} request_id={res.get('request_id')}")
o = res.get("outcome") or {}
P(f"outcome.classification={J(o.get('classification'))} owner_continuation={o.get('owner_continuation')}")
P("## steps")
for s in res.get("steps", []):
    P(f"  {s['name']:34s} {s['verdict']:12s} {s.get('started_at')} -> {s.get('finished_at')}  {(s.get('reason') or '')[:600]}")
P("## findings")
for f in res.get("findings", []):
    P("  - " + f)
P("## outcome")
for k in ("final_status", "attempts", "reboot", "track_stop", "port_hold", "owner_reboot", "pin_changes"):
    P(f"  {k}: " + J(o.get(k), 3000))
if res.get("kind"):
    P("## kind judged")
    P("  " + J(res["kind"], 5000))
P("## scope")
P("  " + J(res.get("scope"), 2000))
files = sorted(glob.glob(ev + "steps/*-track*/samples/*.json"))
P(f"## status samples ({len(files)})")
seen = {}


def add(k, t, u):
    if t and (k, t) not in seen:
        seen[(k, t)] = u


for f in files:
    s = json.load(open(f))
    u = (s.get("update_status") or {}).get("body") or {}
    r = (s.get("recovery_api") or {}).get("body") or {}
    try:
        cli = json.loads(((s.get("cli") or {}).get("json") or {}).get("stdout") or "null") or {}
    except Exception:
        cli = {}
    ag = s.get("agreement") or {}
    P(f"  {s.get('utc','')[11:22]} {f.split('/')[-3][:8]} U {(s.get('update_status') or {}).get('http')} {u.get('status')} | R {(s.get('recovery_api') or {}).get('http')} "
      f"{r.get('phase')}/{r.get('terminal_proof')} {r.get('automatic_recovery')} fc={r.get('failure_code')} | CLI {cli.get('phase')}/"
      f"{cli.get('terminal_proof')} {cli.get('automatic_recovery')} wait={cli.get('waiting_for')} fc={cli.get('failure_code')} "
      f"ffc={cli.get('first_failure_code')} next={cli.get('next_attempt_at') or cli.get('retry_at')} | perr={str(s.get('panel_error'))[:40]} clierr={str(s.get('cli_error'))[:40]} | "
      f"{ag.get('verdict')} {';'.join(ag.get('reasons') or [])[:120]}")
    for src in ("guidance_api", "guidance_cli", "update_card", "recovery_screen"):
        gg = s.get(src)
        if not isinstance(gg, dict) or not isinstance(gg.get("texts"), dict):
            continue
        for lang in ("en", "tr"):
            add(f"{src}.{lang}", " || ".join(t for t in (gg["texts"].get(lang) or []) if t), s.get("utc", "")[11:19])
    c = s.get("cli") or {}
    for lang in ("en", "tr"):
        t = (c.get(lang) or {}).get("stdout", "")
        t = "\n".join(line for line in t.splitlines() if not re.match(r"^(Request|İşlem|Recorded at|Kayıt zamanı):", line))
        add("cli_raw." + lang, t, s.get("utc", "")[11:19])
P("## guidance texts seen (first time, per source)")
for (k, t), u in sorted(seen.items(), key=lambda x: (x[0][0], x[1])):
    P(f"[{u}] {k}:\n    " + t.replace("\n", "\n    "))
tr = step("track")
if tr:
    c = tr.get("checks") or {}
    P("## track checks (keys) " + J(sorted(c)))
    for k in ("samples", "agreement", "settled_failure", "paused", "final"):
        if k in c:
            P(f"  {k}: " + J(c[k], 1500))
oc = step("owner-continuation-required")
if oc:
    c = oc.get("checks") or {}
    P("## owner-continuation (required): " + oc.get("verdict", "") + " " + str(oc.get("reason") or ""))
    for k in ("hold_at_pause", "panel_log", "printed_retry", "release", "retry", "result", "at_pause_snapshot"):
        if k in c:
            P(f"  {k}: " + J(c[k], 4000))
    views = c.get("views") or {}
    for k, v in views.items():
        P(f"  view {k}: " + J(v, 6000))
tac = step("track-after-owner-continuation")
if tac:
    P("## track-after-owner-continuation: " + tac.get("verdict", "") + " " + J({k: (tac.get('checks') or {}).get(k) for k in ('samples', 'agreement', 'final')}, 1500))
t = step("terminal")
if t:
    c = t.get("checks") or {}
    P("## terminal: " + t.get("verdict", "") + " " + str(t.get("reason") or ""))
    for k in ("final", "outcome", "build_identity_ok", "installed", "floor", "foundation", "transaction", "firewall_equal",
              "site_marker", "smtp", "mailbox", "login_ok", "login", "seeded_rows", "timers", "panel_service", "cron"):
        if k in c:
            P(f"  {k}: " + J(c[k], 1500))
    db = c.get("database") or c.get("database_vs_pre_update") or {}
    P(f"  database: {db.get('verdict')} unexpected={db.get('unexpected')} excluded={db.get('volatile_excluded') or db.get('excluded')}")
    card = c.get("update_card") or {}
    P("  UPDATE CARD (product rules): " + J(card.get("update_card"), 5000))
    P("  card judged: " + J(card.get("update_card_judged"), 1500))
    P("  RECOVERY SCREEN: " + J(card.get("recovery_guidance"), 4000))
    P("  status body: " + J((card.get("status") or {}).get("body"), 1500))
    P("  recovery body: " + J((card.get("recovery") or {}).get("body"), 1500))
    P("  check body: " + J((card.get("check") or {}).get("body"), 2500))
    if "texts_after_retry" in c:
        P("  texts_after_retry: " + J(c["texts_after_retry"], 6000))
    if "views_at_pause" in c:
        P("  views_at_pause: " + J(c["views_at_pause"], 6000))
for name in ("management-off", "owner-reboot", "management-off-measure", "management-return"):
    s = step(name)
    if s:
        c = dict(s.get("checks") or {})
        c.pop("services", None)
        P(f"## {name}: {s.get('verdict')} {s.get('reason') or ''}")
        P("  " + J(c, 8000))
v = step("verdicts")
if v:
    c = v.get("checks") or {}
    P("## verdicts: " + v.get("verdict", "") + " " + str(v.get("reason") or ""))
    for k, w in (c.get("workloads") or {}).items():
        P(f"  workload {k}: {w.get('verdict')} windows={J(w.get('windows'), 800)}")
    P("  host_panel_windows " + J(c.get("host_panel_windows"), 600))
    P("  host_ssh_windows " + J(c.get("host_ssh_windows"), 400))
    P("  agreement " + J(c.get("agreement"), 600) + " skew " + str(c.get("clock_skew_seconds")))
coll = sorted(glob.glob(ev + "steps/*-collect/"))
if coll:
    c = coll[0]
    P("## observer events")
    try:
        for e in json.load(open(c + "observer-events.json")):
            P("  " + str(e.get("at") or e.get("utc")) + " " + str(e.get("event")) + " "
              + json.dumps({k: v for k, v in e.items() if k not in ("schema", "identity", "operation_id", "event", "at", "utc")})[:260])
    except Exception as exc:  # noqa: BLE001
        P(f"  unavailable {exc}")
    if Path(c + "recovery-fault-events.jsonl").exists():
        P("## recovery-fault events")
        for line in open(c + "recovery-fault-events.jsonl"):
            if line.strip():
                e = json.loads(line)
                P("  " + str(e.get("at") or e.get("utc")) + " " + str(e.get("event")) + " "
                  + json.dumps({k: v for k, v in e.items() if k in ("checkpoint", "phase", "pid", "boot_id")})[:200])
    if Path(c + "budget.json").exists():
        P("## budget receipts")
        b = json.load(open(c + "budget.json"))
        for r in b.get("receipts", []):
            P("  " + json.dumps(r)[:400])
    P("## product journal key lines")
    pat = re.compile(r"CELIKPANEL_UPDATE|startup check|start check|stay running|did not stay|held:|Restored release|"
                     r"Recovery dispatch|Rollback complete|recovery_required|paused|--retry|panel_start_unverified|"
                     r"candidate_panel_startup|recovery_runtime_preflight|retry_scheduled|Recovery waiting|did not come up|"
                     r"verified panel|certbot|renewal|reset-failed|start-limit|unit_start_limit|address already in use|"
                     r"stopped before|compatib|preflight|!!", re.I)
    for line in open(c + "journal-product.txt", errors="replace"):
        if pat.search(line) and "GET /api" not in line:
            P("  " + line.rstrip()[:500])
for f in sorted(glob.glob(ev + "steps/*/owner-port-hold-*.jsonl")) + sorted(glob.glob(ev + "**/owner-port-hold-*.jsonl", recursive=True))[:1]:
    P("## port hold events " + f.split(ev)[-1])
    for line in open(f):
        P("  " + line.rstrip()[:300])
    break
Path(outfile).write_text("\n".join(out) + "\n")
print(outfile, len(out), "lines")
