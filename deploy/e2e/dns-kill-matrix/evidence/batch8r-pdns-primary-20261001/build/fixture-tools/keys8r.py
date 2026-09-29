import json,sys
r=json.load(open(sys.argv[1]))
def walk(o,p,d):
    if d>2: return
    if isinstance(o,dict):
        for k,v in o.items():
            t=type(v).__name__
            s=json.dumps(v)[:120] if not isinstance(v,(dict,list)) else f"<{t} {len(v)}>"
            print("  "*d+f"{k}: {s}")
            walk(v,p+[k],d+1)
walk(r,[],0)
