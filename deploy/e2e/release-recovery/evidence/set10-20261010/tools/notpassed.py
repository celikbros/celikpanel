#!/usr/bin/env python3
"""set10: every step whose verdict is not passed/observed/skipped and every section check that is not true, from the
staged driver records, each with its class: product (a D-031 expectation measured and not held), not-measured,
harness (a check of an earlier set's driver whose expectation the D-031 release changes), lab (the guest, not the
product), or aborted (a run stopped after a harness error)."""
import glob, json, os, sys
E = sys.argv[1]
HARNESS = {"post-update-facts": "set3's post-update check pins the released ledger 43; this candidate migrates to 44 (D-031, migration 044)",
           "set4-php-site-after-the-update": "set4 item 9 expects a General save to rewrite the vhost; under D-031 identical bytes are not rewritten and nginx is not reloaded",
           "set6-headers-after-the-update": "set6's header reading expects the candidate's header; after an automatic return the returned alpha.81 answers (includeSubDomains), as set6 measured for alpha.81"}
out = []
for run in sorted(glob.glob(E + "/*/*/driver")):
    rel = os.path.relpath(os.path.dirname(run), E)
    aborted = os.path.exists(os.path.join(os.path.dirname(run), "host", "driver-not-finalized.txt"))
    for f in sorted(glob.glob(run + "/steps/*/step.json")):
        s = json.load(open(f))
        if s.get("verdict") in ("passed", "observed", "skipped"):
            continue
        name = s.get("name")
        cls = ("aborted" if aborted else "harness" if name in HARNESS else
               "lab" if name == "setup" else "product" if s.get("verdict") == "failed" else "not-measured")
        out.append(f"{rel}\tstep\t{name}\t{s.get('verdict')}\t{cls}\t{(s.get('reason') or '')[:300]}\t{HARNESS.get(name, '')}")
    for f in sorted(glob.glob(run + "/steps/*/section.json")):
        s = json.load(open(f))
        for c in s.get("checks") or []:
            if c["ok"] is True:
                continue
            cls = "aborted" if aborted else "product" if c["ok"] is False else "not-measured"
            out.append(f"{rel}\tcheck\t{os.path.basename(os.path.dirname(f))}\t{c['ok']}\t{cls}\t{c['name'][:200]}\t")
print("run\tkind\tname\tverdict\tclass\tdetail\twhy (harness class)")
print("\n".join(out))
