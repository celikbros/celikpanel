#!/usr/bin/env python3
"""Make the H8 run copy: harness-h8 = harness (git archive 94be6b6e) + the H8 track settle rule."""
import shutil, subprocess, sys
from pathlib import Path
R = Path("/var/tmp/cp-upd3-run")
src, dst = R / "harness", R / "harness-h8"
if dst.exists():
    sys.exit("refusing: harness-h8 exists")
shutil.copytree(src, dst, symlinks=True)
f = dst / "deploy/e2e/release-recovery/owner_update_trial.py"
text = f.read_text()
old_loop_head = """        deadline = time.monotonic() + 5400
        final = None
        while time.monotonic() < deadline:"""
new_loop_head = """        deadline = time.monotonic() + 5400
        final = None
        # H8 (upd3 run copy): a failure before any change (phase failed, proof none, no automatic
        # recovery, no wait) that the product keeps unchanged has no terminal or paused state; the
        # 90-minute deadline then only delays the same inconclusive verdict. Stop after 600 s of it.
        settled_since = None
        settled_samples = 0
        while time.monotonic() < deadline:"""
old_classify = """            observed = sample.get("observed")
            state = classify_status(observed)
            if state in ("terminal", "paused"):
                final = observed
                break
"""
new_classify = """            observed = sample.get("observed")
            state = classify_status(observed)
            if state in ("terminal", "paused"):
                final = observed
                break
            if (isinstance(observed, dict) and observed.get("observation") == "known"
                    and observed.get("phase") == "failed" and observed.get("terminal_proof") == "none"
                    and not observed.get("automatic_recovery") and not observed.get("waiting_for")):
                settled_since = settled_since or time.monotonic()
                settled_samples += 1
                if time.monotonic() - settled_since >= 600 and settled_samples >= 3:
                    checks["h8_settled_failed"] = {"seconds": round(time.monotonic() - settled_since, 1),
                                                   "samples": settled_samples, "observed": observed,
                                                   "update_status": (sample.get("update_status") or {}).get("body")}
                    break
            else:
                settled_since, settled_samples = None, 0
"""
old_end = """        if final is None:
            raise StepInconclusive("no terminal or paused state was observed within 90 minutes")"""
new_end = """        if final is None and checks.get("h8_settled_failed"):
            raise StepInconclusive("H8: the update failed before any change and its recovery status stayed "
                                   "failed/none without automatic recovery for 600 s; no terminal or paused state")
        if final is None:
            raise StepInconclusive("no terminal or paused state was observed within 90 minutes")"""
for old, new in ((old_loop_head, new_loop_head), (old_classify, new_classify), (old_end, new_end)):
    if text.count(old) != 1:
        sys.exit(f"patch anchor not unique/found: {old[:60]!r}")
    text = text.replace(old, new)
f.write_text(text)
diff = subprocess.run(["diff", "-u", str(src / "deploy/e2e/release-recovery/owner_update_trial.py"), str(f)],
                      capture_output=True, text=True).stdout
(R / "H8-owner_update_trial.py.diff").write_text(diff)
print(diff)
