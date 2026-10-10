# set6: one bounded summary of a staged cell's result.json (read-only).
import json
import sys

try:
    r = json.load(open(sys.argv[1], encoding="utf-8"))
except Exception as exc:  # noqa: BLE001
    print("overall: (no result.json:", type(exc).__name__ + ")")
    raise SystemExit(0)
outcome = r.get("outcome") or {}
final = outcome.get("final_status") or {}
print("overall:", r.get("overall"), "| native_evidence:", r.get("native_evidence"), "| outcome:", outcome.get("classification"),
      "| final:", (final.get("status") if isinstance(final, dict) else final), (final.get("phase") if isinstance(final, dict) else ""))
for s in r.get("steps", []):
    if s.get("verdict") not in ("passed", "observed"):
        print("  step", s.get("name"), s.get("verdict"), "|", str(s.get("reason"))[:900])
for k, v in ((r.get("sections") or (r.get("set5") or {}).get("sections") or {})).items():
    print("  section", k, v.get("verdict"), "| checks:", v.get("checks"), "| failed:", [f[:220] for f in (v.get("failed") or [])][:14],
          "| unknown:", [u[:120] for u in (v.get("unknown") or [])][:4], "| error:", str(v.get("error"))[:500])
for f in r.get("findings") or []:
    print("  finding:", str(f)[:300])
