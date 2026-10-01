#!/bin/bash
# upd10 cell C (fresh disposable Debian 13 guest of the existing debian13-arch lab kind, no CelikPanel): after an apt
# operation, is packagekitd present or running, and if so which pathnames does it map under packagekit-backend?
# Reads /proc and the package database; the only changes are apt-get update and one small apt-get install on the
# disposable guest (the apt operation the question needs) and a download of the packagekit .deb into /tmp to list its
# files (not installed). The Arch node of the lab kind boots and is not used.
set -u
export PYTHONDONTWRITEBYTECODE=1
H=/var/tmp/cp-upd10-run/harness/deploy/e2e/release-recovery
root=/var/tmp/cp-release-drill-upd10-deb-probe-c
python3 $H/lab.py prepare --work-root $root --image-cache /var/tmp/cp-v3n28/images --ssh-port 3017 --platform debian13-arch --execute
python3 $H/lab.py start --work-root $root --execute
python3 - $root <<'PY'
import sys, importlib.util, time
spec = importlib.util.spec_from_file_location("lab", "/var/tmp/cp-upd10-run/harness/deploy/e2e/release-recovery/lab.py")
lab = importlib.util.module_from_spec(spec); sys.modules["lab"] = lab; spec.loader.exec_module(lab)
root = lab.checked_root(sys.argv[1]); record, plan = lab.load(root)
def g(body, t=1200):
    r = lab.guarded_script(root, record, plan, "debian13", body, timeout=t, capture=True)
    return r.stdout + (("\n[stderr] " + r.stderr) if r.stderr.strip() else "")
READ = r'''
set +e
echo "utc=$(date -u +%FT%T.%3NZ)"
pids=$(pgrep -x packagekitd)
echo "packagekitd_pids=${pids:-none}"
for p in $pids; do
  echo "pid=$p etime_s=$(ps -o etimes= -p $p | tr -d ' ') exe=$(readlink /proc/$p/exe) cmdline=$(tr '\0' ' ' < /proc/$p/cmdline)"
  echo "children=$(ps --ppid $p -o comm= | tr '\n' ',')"
  echo "mapped pathnames under packagekit-backend (raw maps lines):"
  grep -E '/packagekit-backend/' /proc/$p/maps | sed 's/^/  /'
  echo "grep_c_aptcc=$(grep -c aptcc /proc/$p/maps)"
done
echo "package-manager processes:"; ps -eo pid,ppid,etimes,comm,args | awk 'NR==1 || $4 ~ /^(apt|apt-get|dpkg|dpkg-deb|packagekitd|pkcon|unattended-upgr|aptd|http|https|store|gpgv|needrestart)$/' | cut -c1-200 | sed 's/^/  /'
echo "lock lines:"; for f in /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/cache/apt/archives/lock /var/lib/apt/lists/lock; do i=$(stat -c %i $f 2>/dev/null) && grep -E ":$i " /proc/locks | sed "s#^#  $f #"; done
'''
print(g("""set +e; date -u +%FT%T.%3NZ; echo "--- os"; cat /etc/os-release | grep -E '^(PRETTY_NAME|VERSION_ID|VERSION_CODENAME)='; uname -r
echo "--- packages"; dpkg-query -W -f='${Package} ${Version} ${db:Status-Abbrev}\\n' packagekit packagekit-tools libpackagekit-glib2-18 apt unattended-upgrades needrestart 2>&1
echo "--- packagekit files"; ls -la /usr/lib/x86_64-linux-gnu/packagekit-backend/ 2>&1; ls -la /etc/PackageKit 2>&1 | head; ls -la /etc/apt/apt.conf.d/ 2>&1
echo "--- packagekit units"; systemctl list-unit-files 'packagekit*' --no-pager 2>&1; systemctl show packagekit.service -p LoadState -p ActiveState -p SubState --no-pager 2>&1
echo "--- dbus activation"; ls -la /usr/share/dbus-1/system-services/ 2>&1 | grep -i packagekit || echo "no PackageKit D-Bus activation file"
echo "--- before"
""" + READ), flush=True)
print("--- apt-get update", flush=True)
print(g("""set +e; apt-get update > /tmp/upd10-apt-update.log 2>&1; echo "rc=$? $(date -u +%FT%T.%3NZ)"; tail -n 4 /tmp/upd10-apt-update.log; sleep 8
""" + READ), flush=True)
print("--- apt-get install -y --no-install-recommends tree (one small package; dpkg runs)", flush=True)
print(g("""set +e; DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends tree > /tmp/upd10-apt-install.log 2>&1; echo "rc=$? $(date -u +%FT%T.%3NZ)"; tail -n 6 /tmp/upd10-apt-install.log; sleep 8
""" + READ), flush=True)
time.sleep(30)
print("--- +38 s after the install", flush=True); print(g(READ), flush=True)
print("--- packagekit journal (all boots of this guest)", flush=True)
print(g("set +e; journalctl -u packagekit.service --no-pager -o short-iso-precise 2>&1 | tail -n 20; journalctl --no-pager -o short-iso-precise 2>&1 | grep -i packagekit | tail -n 10 || true"), flush=True)
print("--- what Debian 13 would install as PackageKit's backend (packagekit .deb downloaded to /tmp, NOT installed)", flush=True)
print(g("""set +e; apt-cache policy packagekit 2>&1 | head -n 8
mkdir -p /tmp/upd10-pk && cd /tmp/upd10-pk && apt-get download packagekit > /tmp/upd10-pk/download.log 2>&1; echo "download rc=$?"
for d in /tmp/upd10-pk/*.deb; do [ -f "$d" ] || continue; echo "deb=$(basename $d) sha256=$(sha256sum $d | cut -c1-64)"; dpkg-deb -c "$d" | grep -E 'packagekit-backend|apt.conf.d|packagekitd$' | sed 's/^/  /'; done
dpkg-query -W packagekit 2>&1 || true; pgrep -x packagekitd || echo "no packagekitd after the download"
"""), flush=True)
PY
python3 $H/lab.py stop --work-root $root --execute
