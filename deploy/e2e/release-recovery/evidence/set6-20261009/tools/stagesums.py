# set6: after the digest sweep of a staged cell: rewrite the driver's SHA256SUMS lines of the files the sweep changed,
# and keep the sweep's report (counts only) beside the cell's host-side records.
# usage: stagesums.py STAGED_RUN_DIR SWEEP_REPORT_JSON
import hashlib
import json
import os
import sys

dst, report = sys.argv[1], json.load(open(sys.argv[2]))
changed = sorted(set(report["files_changed"]) | set(report["files_changed_inside_base64_text"]))
driver = sorted(name for name in changed if not name.startswith("host/"))
sums = os.path.join(dst, "SHA256SUMS")
rewritten = []
if driver and os.path.isfile(sums):
    lines = open(sums, encoding="ascii").read().splitlines()
    for index, line in enumerate(lines):
        digest, name = line.split("  ", 1)
        if name in driver:
            lines[index] = hashlib.sha256(open(os.path.join(dst, name), "rb").read()).hexdigest() + "  " + name
            rewritten.append(name)
    open(sums, "w", encoding="ascii", newline="\n").write("\n".join(lines) + "\n")
report["at_staging"] = {"host_side_files_changed": sorted(name for name in changed if name.startswith("host/")),
                        "driver_files_changed": driver, "driver_sha256sums_lines_rewritten": rewritten,
                        "values_learned_from": "the lab's own raw records on the WSL host (not retained here)"}
json.dump(report, open(os.path.join(dst, "host", "digest-sweep-at-staging.json"), "w", newline="\n"), indent=2, sort_keys=True)
print("digest sweep at staging: host-side files changed %d, driver files changed %d, places %s, inside base64 text %d" % (
    len(report["at_staging"]["host_side_files_changed"]), len(driver), json.dumps(report["places_by_class"], sort_keys=True),
    report["places_inside_base64_text"]))
