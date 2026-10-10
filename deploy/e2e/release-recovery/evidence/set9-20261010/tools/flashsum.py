"""set7 cell 5: one line per full load (screens in order with their ms since the document began, the interface's
own session/readiness requests with start, end and status). usage: flashsum.py FLASH_LOADS_JSONL"""
import json
import sys

sys.stdout.reconfigure(encoding="utf-8")
for line in open(sys.argv[1], encoding="utf-8"):
    r = json.loads(line)
    screens = " > ".join(f"{s['ms']}:{s['kind']}" + (f"({s.get('h1')})" if s.get("h1") else "") for s in (r["screens"] or []))
    print(f"{r['label']}: recovery-access {'SHOWN' if r['recovery_access_shown'] else 'not shown'}"
          f" {r['recovery_access_from_ms']}..{r['recovery_access_until_ms']} ms | nav {r['navigation_timing']}")
    print(f"   screens: {screens}")
    wanted = ("auth/me", "panel/availability", "license/access", "setup")
    print("   requests: " + ", ".join(f"{a['path'][8:]} {a['start']}->{a['end']} {a['status']}" for a in (r["api_requests"] or [])
                                    if any(w in a["path"] for w in wanted)))
