import json, sys
s = json.load(open(sys.argv[1]))
only_bad = len(sys.argv) > 2
for c in s["checks"]:
    if only_bad and c["ok"] is True:
        continue
    print(c["ok"], c["name"][:150])
    if c["ok"] is not True:
        print("    ", json.dumps(c["detail"], default=str)[:int(sys.argv[3]) if len(sys.argv) > 3 else 900])
print("error:", s.get("error"))
for n in s.get("notes", []):
    print("note:", n["note"][:200])
