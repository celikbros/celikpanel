#!/bin/bash
# set3: the checks that did not pass in one staged or live section. usage: checks.sh PATH_TO_section.json [all]
python3 - "$1" "${2:-}" <<'PY'
import json, sys
s = json.load(open(sys.argv[1]))
print(s.get("section"), s.get("verdict"), "checks:", len(s.get("checks", [])), "error:", str(s.get("error"))[:600])
for c in s.get("checks", []):
    if c["ok"] is not True or sys.argv[2] == "all":
        print(("PASS " if c["ok"] else "FAIL " if c["ok"] is False else "UNKN ") + c["name"][:230])
        if c["ok"] is not True:
            print("     ", json.dumps(c.get("detail"), sort_keys=True)[:1500])
PY
