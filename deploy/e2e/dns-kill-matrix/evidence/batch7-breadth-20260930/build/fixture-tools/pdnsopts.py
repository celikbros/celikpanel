# Read-only: every pdns_secondary_rows / member_options record in result.json, with its JSON path.
import json, sys
r = json.load(open(sys.argv[1]))
out = []
def walk(o, path):
    if isinstance(o, dict):
        for k, v in o.items():
            if k in ("pdns_secondary_rows", "member_options"):
                out.append({"path": ".".join(path + [k]), "value": v})
            walk(v, path + [k])
    elif isinstance(o, list):
        for i, v in enumerate(o): walk(v, path + [str(i)])
walk(r, [])
json.dump({"source": "result.json (verbatim sub-objects)", "records": out}, sys.stdout, indent=2, sort_keys=True); print()
