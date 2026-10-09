# set4: one bounded summary of a staged cell's result.json (read-only).
import json
import sys

try:
    r = json.load(open(sys.argv[1], encoding="utf-8"))
except Exception as exc:  # noqa: BLE001
    print("no result.json:", type(exc).__name__)
    raise SystemExit(0)
print("overall:", r.get("overall"), "| native_evidence:", r.get("native_evidence"), "| outcome:", (r.get("outcome") or {}).get("classification"))
for s in r.get("steps", []):
    if s.get("verdict") not in ("passed", "observed"):
        print("  step", s.get("name"), s.get("verdict"), "|", str(s.get("reason"))[:900])
for k, v in (r.get("sections") or {}).items():
    if v.get("verdict") != "passed":
        print("  section", k, v.get("verdict"), "| failed:", [f[:220] for f in (v.get("failed") or [])][:14],
              "| unknown:", [u[:120] for u in (v.get("unknown") or [])][:4], "| error:", str(v.get("error"))[:500])
