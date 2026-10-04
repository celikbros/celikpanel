E=/var/tmp/cp-b12-1001/evidence/z05-zero-started-zl-rb
python3 - $E/native-and-outage.json <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
def walk(o,p=""):
    if isinstance(o,dict):
        if o.get("rule") in ("post-publication","pre-publication"):
            print(p, json.dumps({k:v for k,v in o.items() if not isinstance(v,(dict,list)) or k in ("members","expected_members")})[:500])
        for k,v in o.items(): walk(v,p+"."+k)
    elif isinstance(o,list):
        for i,v in enumerate(o): walk(v,p+f"[{i}]")
walk(d)
PY
