#!/bin/bash
# upd9 diagnostic probe (fresh disposable Ubuntu 24.04 guest, no CelikPanel): what does the packagekitd that apt's
# hook starts map while idle, and does a read-only PackageKit query load the APT backend? Read-only except the apt
# index refresh that starts the daemon and one read-only Resolve transaction.
set -u
export PYTHONDONTWRITEBYTECODE=1
H=/var/tmp/cp-upd9-run/harness/deploy/e2e/release-recovery
root=/var/tmp/cp-release-drill-upd9-probe-a
python3 $H/lab.py prepare --work-root $root --image-cache /var/tmp/cp-v3n28/images --ssh-port 2997 --platform ubuntu --execute
python3 $H/lab.py start --work-root $root --execute
python3 - $root <<'PY'
import sys, importlib.util, time
spec = importlib.util.spec_from_file_location("lab", "/var/tmp/cp-upd9-run/harness/deploy/e2e/release-recovery/lab.py")
lab = importlib.util.module_from_spec(spec); sys.modules["lab"] = lab; spec.loader.exec_module(lab)
root = lab.checked_root(sys.argv[1]); record, plan = lab.load(root)
def g(body, t=900):
    r = lab.guarded_script(root, record, plan, "ubuntu", body, timeout=t, capture=True)
    return r.stdout
READ = r'''
p=$(pgrep -x packagekitd | head -1)
echo "utc=$(date -u +%FT%T.%3NZ) packagekitd_pid=${p:-none}"
if [ -n "$p" ]; then
  echo "etime_s=$(ps -o etimes= -p $p | tr -d ' ') cmdline=$(tr '\0' ' ' < /proc/$p/cmdline) exe=$(readlink /proc/$p/exe)"
  echo "grep_c_aptcc=$(grep -c aptcc /proc/$p/maps)"
  echo "children=$(ps --ppid $p -o comm= | tr '\n' ',')"
  echo "mapped_so:"; awk '$6 ~ /\.so/ {print $6}' /proc/$p/maps | sort -u | sed 's/^/  /'
fi
echo "locks:"; for f in /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/cache/apt/archives/lock /var/lib/apt/lists/lock; do i=$(stat -c %i $f 2>/dev/null) && grep -E ":$i " /proc/locks | sed "s#^#  $f #"; done
'''
print(g("""date -u +%FT%T.%3NZ; echo "--- before"; pgrep -a packagekitd || echo "no packagekitd"
dpkg-query -W packagekit packagekit-tools gir1.2-packagekitglib-1.0 libpackagekit-glib2-18 apt 2>&1
echo "--- backends"; ls -la /usr/lib/x86_64-linux-gnu/packagekit-backend/ 2>&1
echo "--- conf"; grep -v -E '^(#|$)' /etc/PackageKit/PackageKit.conf 2>&1
echo "--- apt hook"; cat /etc/apt/apt.conf.d/20packagekit 2>&1
echo "--- apt-get update"; apt-get update > /tmp/upd9-apt.log 2>&1; echo "rc=$? $(date -u +%FT%T.%3NZ)"; sleep 8
""" + READ), flush=True)
time.sleep(20)
print("--- idle +28s", flush=True); print(g(READ), flush=True)
print("--- gdbus Properties.GetAll (read-only)", flush=True)
print(g("""gdbus call --system --dest org.freedesktop.PackageKit --object-path /org/freedesktop/PackageKit --method org.freedesktop.DBus.Properties.GetAll org.freedesktop.PackageKit 2>&1 | cut -c1-900; sleep 3
""" + READ), flush=True)
print("--- pkcon resolve bash (read-only transaction, if pkcon exists)", flush=True)
print(g("""if command -v pkcon >/dev/null; then timeout 120 pkcon -p resolve bash 2>&1 | tail -n 5; else echo "pkcon not installed"; fi; sleep 3
""" + READ), flush=True)
time.sleep(30)
print("--- +30s after", flush=True); print(g(READ), flush=True)
print(g("journalctl -u packagekit.service --no-pager -o short-iso-precise | tail -n 30"), flush=True)
PY
python3 $H/lab.py stop --work-root $root --execute
