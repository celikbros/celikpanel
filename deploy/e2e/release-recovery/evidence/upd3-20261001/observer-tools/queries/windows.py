import glob, json, datetime as dt
S = "/var/tmp/cp-upd3-run/stage/upd3-20261001"
def u(x):
    return dt.datetime.fromtimestamp(x, dt.timezone.utc).strftime("%H:%M:%S") if isinstance(x, (int, float)) else x
for f in sorted(glob.glob(S + "/*/run-a/steps/*-verdicts/step.json")):
    c = json.load(open(f))["checks"]
    print("==", f.split("/")[-5])
    for k, w in (c.get("workloads") or {}).items():
        for x in w.get("windows") or []:
            print(f"  {k:6s} {w.get('verdict')}: {u(x.get('from'))}-{u(x.get('to'))} bound {x.get('lower_bound_s')}-{x.get('upper_bound_s')} s kind={x.get('kind')} label={x.get('label')}")
    for x in c.get("host_panel_windows") or []:
        print(f"  host-panel: {u(x.get('from'))}-{u(x.get('to'))} bound {x.get('lower_bound_s')}-{x.get('upper_bound_s')} s")
    for x in c.get("host_ssh_windows") or []:
        print(f"  host-ssh: {u(x.get('from'))}-{u(x.get('to'))} bound {x.get('lower_bound_s')}-{x.get('upper_bound_s')} s")
    print("  resets", [u(r) for r in (json.load(open(f.replace('/steps/', '/../').rsplit('/steps', 1)[0] + '/result.json')) if False else [])])
