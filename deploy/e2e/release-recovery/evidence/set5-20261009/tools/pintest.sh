#!/bin/bash
# set5 read-only smoke test of the driver's guest script (read mode) on the WSL host itself: nothing is written
cd "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery"
PYTHONDONTWRITEBYTECODE=1 python3 -c "
import set5_trial, subprocess, json, sys
out = subprocess.run([sys.executable, '-I', '-', 'read', json.dumps(list(set5_trial.CA_NAMES)), set5_trial.PIN_MARK, 'celikpanel.net'], input=set5_trial.PIN_SCRIPT, capture_output=True, text=True)
print(out.returncode, out.stderr[-300:])
v = json.loads(out.stdout); print({k: v[k] for k in ('packages_installed', 'kernel', 'os_release', 'mode')}); print(set5_trial.pin_verdict(v, set5_trial.CA_NAMES))
"
