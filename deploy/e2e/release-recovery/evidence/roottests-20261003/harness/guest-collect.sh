if systemctl is-active --quiet rt-rerun; then echo busy; exit 1; fi
P=/srv/rt/out/guest-provenance.txt
{
  echo "collected=$(date -u +%FT%TZ)"
  cat /etc/os-release; echo "debian_version=$(cat /etc/debian_version)"; uname -a
  echo "product_uuid=$(cat /sys/class/dmi/id/product_uuid)"
  echo "lab_marker=$(cat /etc/celikpanel-release-recovery-lab)"
  nproc; free -m
  for m in / /run /tmp; do findmnt -no TARGET,FSTYPE,OPTIONS "$m" || true; done
  echo "note: /var/tmp and /srv are on /"
  echo "src.tar sha256=$(sha256sum < /srv/rt/src.tar)"
  /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
  /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go env GOROOT GOVERSION
  echo "## go toolchain tree digest (sorted sha256sum of every file)"
  (cd /opt/celikpanel-test-toolchains/go1.26.5/go && find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | sha256sum)
  echo "## packages"
  dpkg-query -W -f '${Package} ${Version}\n' | sort
} > "$P" 2>&1
cp /root/apt.log /srv/rt/out/guest-apt-install.log 2>/dev/null || true
tar -C /srv/rt -czf /srv/rt/evidence.tgz out out-attempt1-crlf
sha256sum /srv/rt/evidence.tgz
ls -la /srv/rt/evidence.tgz
