#!/bin/bash
# upd8 probe 2 (fresh disposable probe guest): does a real dpkg install start packagekitd, and for how long does it run?
set -u
export PYTHONDONTWRITEBYTECODE=1
H=/var/tmp/cp-upd8-run/harness/deploy/e2e/release-recovery
root=/var/tmp/cp-release-drill-upd8-probe-c
python3 $H/lab.py prepare --work-root $root --image-cache /var/tmp/cp-v3n28/images --ssh-port 2897 --platform ubuntu --execute
python3 $H/lab.py start --work-root $root --execute
python3 - $root <<'PY'
import sys, importlib.util, time
spec = importlib.util.spec_from_file_location("lab", "/var/tmp/cp-upd8-run/harness/deploy/e2e/release-recovery/lab.py")
lab = importlib.util.module_from_spec(spec); sys.modules["lab"] = lab; spec.loader.exec_module(lab)
root = lab.checked_root(sys.argv[1]); record, plan = lab.load(root)
def g(body, t=900):
    return lab.guarded_script(root, record, plan, "ubuntu", body, timeout=t, capture=True).stdout
print(g("""date -u +%T; apt-get update >/tmp/apt1.log 2>&1; echo update rc=$?; date -u +%T; DEBIAN_FRONTEND=noninteractive apt-get install -y nginx >/tmp/apt2.log 2>&1; echo apt rc=$?; grep -i -E "restart|packagekit|Setting up nginx" /tmp/apt2.log | head; tail -3 /tmp/apt2.log; date -u +%T
systemctl show packagekit.service -p ActiveState -p ExecMainStartTimestamp; ps -eo pid,etimes,comm | grep -E 'packagekit|apt|dpkg' || true"""), flush=True)
for i in range(30):
    out = g("date -u +%T; systemctl show packagekit.service -p ActiveState --value; ps -eo pid,etimes,comm | grep -E 'packagekit|apt-get|dpkg' || echo none")
    print(out.strip().replace("\n"," | "), flush=True)
    if "none" in out: break
    time.sleep(30)
PY
python3 $H/lab.py stop --work-root $root --execute
