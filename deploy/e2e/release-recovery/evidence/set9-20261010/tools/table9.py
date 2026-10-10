"""set9 cell 1: set7 (alpha.82 interface, cell 5) against set9 (7c3a05809 interface), per route and network profile, on
set7's time base: ms since the probe started in the new document ("doc ms"; performance.now() minus the probe's t0).
usage: table9.py SET7_FLASH_LOADS_JSONL SET9_LOADS_JSONL [--detail]"""
import json
import sys

sys.stdout.reconfigure(encoding="utf-8")
APP = ("app", "app-shell-page-loading")


def load(path, which):
    out = []
    for line in open(path, encoding="utf-8"):
        r = json.loads(line)
        if which == "set7":
            runs = [{"kind": f["kind"], "h1": f.get("h1"), "from": f["from"], "until": f["until"], "frames": f["frames"],
                     "worded": None} for f in (r.get("painted_frames") or [])]
        else:
            t0 = r.get("probe_t0_ms") or 0
            runs = [{"kind": f["kind"], "h1": f.get("h1"), "from": f["from"] - t0, "until": f["until"] - t0, "frames": f["frames"],
                     "worded": bool(f.get("sentence") or f.get("buttons")), "text": f.get("text", "")[:80]}
                    for f in (r.get("painted") or [])]
        out.append({"label": r["label"], "route": r["route"], "profile": r.get("profile") or ("throttled" if r.get("throttle") else "plain"),
                    "runs": runs, "rec": r})
    return out


def name(run):
    if run is None:
        return "nothing painted yet"
    k = run["kind"]
    return k + (f"({run['h1']})" if run.get("h1") and k in ("recovery-access", "access-hold", "other") else "")


def at(runs, ms):
    found = None
    for run in runs:
        if run["from"] <= ms:
            found = run
        else:
            break
    return found


def early(runs):
    seen = []
    for run in runs:
        if run["from"] <= 200 and name(run) not in seen:
            seen.append(name(run))
    return " > ".join(seen) or "nothing painted"


def summary(load_):
    runs = load_["runs"]
    inter = [r for r in runs if r["kind"] == "recovery-access"]
    app = next((r for r in runs if r["kind"] in APP), None)
    return {"0-200": early(runs), "1s": name(at(runs, 1000)), "2s": name(at(runs, 2000)),
            "interstitial": (f"yes {inter[0]['from']:.0f}-{inter[-1]['until']:.0f}" if inter else "no"),
            "app": None if app is None else round(app["from"])}


def main():
    set7 = load(sys.argv[1], "set7")
    set9 = load(sys.argv[2], "set9")
    print("| route | profile | run | 0-200 doc ms | at 1 s | at 2 s | 'Checking panel access' interstitial | app from (doc ms) |")
    print("| --- | --- | --- | --- | --- | --- | --- | --- |")
    for route in ("/setup", "/", "/settings?section=updates"):
        for profile in ("plain", "throttled"):
            for which, loads in (("set7", set7), ("set9", set9)):
                chosen = [l for l in loads if l["route"] == route and l["profile"] == profile]
                if not chosen:
                    continue
                sums = [summary(l) for l in chosen]
                def merge(key):
                    values = []
                    for s in sums:
                        if str(s[key]) not in values:
                            values.append(str(s[key]))
                    return " / ".join(values)
                apps = [s["app"] for s in sums if s["app"] is not None]
                print(f"| `{route}` | {profile} | {which} ({len(chosen)} loads) | {merge('0-200')} | {merge('1s')} | {merge('2s')} | "
                      f"{merge('interstitial')} | {min(apps) if apps else '-'}-{max(apps) if apps else '-'} |")
    if "--detail" in sys.argv:
        for l in set9:
            r = l["rec"]
            print(f"\n{l['label']}: t0 {r.get('probe_t0_ms')} ms after timeOrigin; timeOrigin {r.get('time_origin_after_goto_ms')} ms after goto; "
                  f"first read answered {r.get('first_read_answered_goto_ms')} goto-ms; rule-breaking runs {len(r.get('rule_breaking_runs') or [])}")
            print("   " + " > ".join(f"{run['from']:.0f}-{run['until']:.0f}:{name(run)}{'+worded' if run['worded'] else ''}" for run in l["runs"]))


main()
