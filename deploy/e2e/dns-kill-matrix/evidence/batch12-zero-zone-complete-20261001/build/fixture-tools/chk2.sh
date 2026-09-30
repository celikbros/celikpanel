for s in z04-zero-committed-zl z05-zero-started-zl-rb; do E=/var/tmp/cp-b12-1001/evidence/$s
echo "== $s"
python3 - $E/native-and-outage.json <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
def walk(o,p=""):
    if isinstance(o,dict):
        for k,v in o.items():
            q=p+"."+k
            if any(t in k for t in ("serial_rule","catalog_restamp","verdict","failures","complete","after_reboot","zero_zone_restamp","management")) and not isinstance(v,(dict,list)):
                print(q.lstrip("."),"=",json.dumps(v)[:200])
            walk(v,q)
    elif isinstance(o,list):
        for i,v in enumerate(o[:50]): walk(v,p+f"[{i}]")
walk(d)
PY
done 2>&1 | head -80
