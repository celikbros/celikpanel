#!/usr/bin/env python3
"""Read-only extraction from a finished upd3 cell's evidence (host only, no guest access).

usage: ext.py LABNAME NODE OUTFILE
"""
import glob, json, re, sys, importlib.util
from pathlib import Path
lab, node, outfile = sys.argv[1:4]
ev = sorted(glob.glob(f"/var/tmp/cp-release-drill-{lab}/evidence/{node}/upd1/*/"))[-1]
ART = json.load(open(open("/var/tmp/cp-upd3-run/ART").read().strip()))
spec = importlib.util.spec_from_file_location("guidance", "/var/tmp/cp-upd3-run/harness/deploy/e2e/dns-pair-acceptance/guidance.py")
g = importlib.util.module_from_spec(spec); spec.loader.exec_module(g)
out = []
P = out.append
P(f"# extraction of {ev}")
res = json.load(open(ev + "result.json")) if Path(ev + "result.json").exists() else {}
P(f"native_evidence={res.get('native_evidence')} overall={res.get('overall')} outcome={json.dumps((res.get('outcome') or {}).get('classification'))}")
P("## steps")
for s in res.get("steps", []):
    P(f"  {s['name']:34s} {s['verdict']:12s} {s.get('started_at')} -> {s.get('finished_at')}  {(s.get('reason') or '')[:300]}")
P("## findings")
for f in res.get("findings", []):
    P("  - " + f)
P("## outcome.attempts / reboot")
o = res.get("outcome") or {}
P("  attempts " + json.dumps(o.get("attempts"))[:1500])
P("  reboot " + json.dumps(o.get("reboot")))
P("  final_status " + json.dumps(o.get("final_status"))[:800])
if res.get("kind"):
    P("## kind judged")
    P("  " + json.dumps(res["kind"], ensure_ascii=False)[:3000])
P("## scope")
P("  " + json.dumps(res.get("scope"))[:1500])
files = sorted(glob.glob(ev + "steps/*-track*/samples/*.json"))
P(f"## track samples ({len(files)})")
tr = g.Translator(g.load_catalog(Path(ART["baseline"]["product_web_src"]) / "i18n"))
seen = {}


def add(k, t, u):
    if (k, t) not in seen:
        seen[(k, t)] = u


for f in files:
    s = json.load(open(f))
    u = (s.get("update_status") or {}).get("body") or {}
    r = (s.get("recovery_api") or {}).get("body") or {}
    try:
        cli = json.loads(s["cli"]["json"]["stdout"] or "null") or {}
    except Exception:
        cli = {}
    ag = s.get("agreement") or {}
    P(f"  {s['utc'][11:22]} U {(s.get('update_status') or {}).get('http')} {u.get('status')} | R {(s.get('recovery_api') or {}).get('http')} "
      f"{r.get('phase')}/{r.get('terminal_proof')} {r.get('automatic_recovery')} fc={r.get('failure_code')} | CLI {cli.get('phase')}/"
      f"{cli.get('terminal_proof')} {cli.get('automatic_recovery')} wait={cli.get('waiting_for')} fc={cli.get('failure_code')} "
      f"ffc={cli.get('first_failure_code')} | perr={str(s.get('panel_error'))[:40]} clierr={str(s.get('cli_error'))[:40]} | "
      f"{ag.get('verdict')} {';'.join(ag.get('reasons') or [])[:120]}")
    for src in ("guidance_api", "guidance_cli", "update_card"):
        gg = s.get(src)
        if not gg:
            continue
        for lang in ("en", "tr"):
            add(f"{src}.{lang}", " || ".join(t for t in gg["texts"][lang] if t), s["utc"][11:19])
    c = s.get("cli") or {}
    for lang in ("en", "tr"):
        t = (c.get(lang) or {}).get("stdout", "")
        t = "\n".join(line for line in t.splitlines()
                      if not re.match(r"^(Request|İşlem|Recorded at|Kayıt zamanı):", line))
        add("cli_raw." + lang, t, s["utc"][11:19])
P("## guidance texts seen (first time)")
for (k, t), u in sorted(seen.items(), key=lambda x: (x[0][0], x[1])):
    P(f"[{u}] {k}:\n    " + t.replace("\n", "\n    "))
term = sorted(glob.glob(ev + "steps/*-terminal/step.json"))
if term:
    t = json.load(open(term[0]))
    card = (t.get("checks") or {}).get("update_card") or {}
    P("## terminal: update card (harness model) and product-rendered card (systemUpdateOutcome.ts at the build commit)")
    P("  harness model: " + json.dumps(card.get("update_card"), ensure_ascii=False)[:1200])
    st = (card.get("status") or {}).get("body") or {}
    rc = (card.get("recovery") or {}).get("body") or {}
    ck = (card.get("check") or {}).get("body") or {}
    P("  status body: " + json.dumps(st, ensure_ascii=False)[:1200])
    P("  recovery body: " + json.dumps(rc, ensure_ascii=False)[:1200])
    P("  check body: " + json.dumps(ck, ensure_ascii=False)[:1500])
    versions = {"target": "v0.1.0-alpha.82", "previous": "v0.1.0-alpha.81"}
    codes = ("candidate_panel_startup_check_failed", "panel_start_unverified")
    if rc.get("observation") == "known" and rc.get("phase") == "recovered" and rc.get("terminal_proof") == "rollback_verified":
        fc = rc.get("failure_code") if rc.get("previous_failure") == "update_failed" else None
        keys = [("panelUpdate.outcome.rolledBackTitle", {}), ("panelUpdate.outcome.rolledBack", versions),
                (f"panelUpdate.outcome.cause.{fc}" if fc in codes else "panelUpdate.outcome.cause.generic", {}),
                ("panelUpdate.outcome.rolledBackNext", versions), ("panelUpdate.outcome.rolledBackResume", versions)]
        if st.get("summary"):
            keys.append(("panelUpdate.outcome.serverMessage", {"message": st.get("summary")}))
        for lang in ("en", "tr"):
            P(f"  CARD[{lang}] " + " || ".join(tr.text(k, v, language=lang) for k, v in keys))
        P("  CARD keys " + json.dumps([k for k, _ in keys]) + " missing " + json.dumps([k for k, _ in keys if not tr.has(k)]))
    pa = ck.get("previous_attempt")
    if pa:
        kk = "panelUpdate.previousAttempt.recovered" if pa.get("phase") == "recovered" else "panelUpdate.previousAttempt.failed"
        for lang in ("en", "tr"):
            parts = [tr.text("panelUpdate.previousAttempt.title", language=lang),
                     tr.text(kk, {"version": "v0.1.0-alpha.82", "time": pa.get("finished_at"), "current": "v0.1.0-alpha.81"},
                             language=lang)]
            if pa.get("failure_code"):
                parts.append(tr.text("panelUpdate.previousAttempt.cause",
                                     {"cause": tr.text("recovery.reason." + pa["failure_code"], language=lang)}, language=lang))
            P(f"  PREVIOUS-ATTEMPT[{lang}] " + " || ".join(parts))
    else:
        P("  previous_attempt: absent in check body")
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
    if Path(c + "observation-records.json").exists():
        P("## observation records")
        P("  " + json.dumps(json.load(open(c + "observation-records.json")))[:2500])
    if Path(c + "budget.json").exists():
        P("## budget receipts")
        b = json.load(open(c + "budget.json"))
        for r in b.get("receipts", []):
            P("  " + json.dumps(r)[:400])
    P("## product journal key lines")
    pat = re.compile(r"CELIKPANEL_UPDATE|startup check|start check|stay running|did not stay|held:|unsafe directory|Restored release|"
                     r"source commit|Recovery dispatch|Rollback complete|recovery_required|paused|--retry|panel_start_unverified|"
                     r"candidate_panel_startup|hosting root|Recovery waiting|Artifact source|did not come up|verified panel", re.I)
    for line in open(c + "journal-product.txt", errors="replace"):
        if pat.search(line) and "GET /api" not in line:
            P("  " + line.rstrip()[:420])
Path(outfile).write_text("\n".join(out) + "\n")
print(outfile, len(out), "lines")
