# Guest body (runs as root after the lab guard). Network is still open here; it is
# closed (QEMU restrict=on) before any contract test runs.
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
date -u +%FT%TZ
cat /etc/debian_version; uname -r
apt-get update -qq
apt-get install -y -qq git make php-cli php-sqlite3 nodejs python3 openssl curl ca-certificates tar \
    xz-utils nftables iproute2 sqlite3 dash file gpg rsync bsdextrautils >/root/apt.log 2>&1 || { tail -30 /root/apt.log; exit 1; }
install -d -m 0755 /srv/rt /srv/rt/out /opt/celikpanel-test-toolchains/go1.26.5
L=/root/celikpanel-release-recovery-lab
tar -C /opt/celikpanel-test-toolchains/go1.26.5 --no-same-owner -xzf $L/go1.26.5.tar.gz
chmod -R go+rX /opt/celikpanel-test-toolchains
install -m 0644 $L/src-48d264d5.tar /srv/rt/src.tar
sha256sum /srv/rt/src.tar
id rtuser >/dev/null 2>&1 || useradd -m -s /bin/bash rtuser
install -d -o rtuser -g rtuser -m 0755 /srv/rt/out/unpriv
install -d -m 0755 /srv/rt/out/root
G=/opt/celikpanel-test-toolchains/go1.26.5/go/bin
"$G/go" version
rm -rf /srv/rt/modprep && mkdir -p /srv/rt/modprep && tar -C /srv/rt/modprep -xf /srv/rt/src.tar go.mod go.sum
chmod -R a+rX /srv/rt/modprep
(cd /srv/rt/modprep && env HOME=/root PATH="$G:/usr/bin:/bin" GOTOOLCHAIN=local "$G/go" mod download all)
runuser -u rtuser -- env -i HOME=/home/rtuser PATH="$G:/usr/bin:/bin" GOTOOLCHAIN=local \
    sh -c 'cd /srv/rt/modprep && go mod download all'
rm -rf /srv/rt/modprep
du -sh /root/go/pkg/mod /home/rtuser/go/pkg/mod
dpkg-query -W -f '${Package} ${Version}\n' git make php8.4-cli php8.4-sqlite3 nodejs python3 openssl curl sqlite3 dash bash coreutils systemd util-linux 2>/dev/null || true
df -h /
date -u +%FT%TZ
echo PROVISION-DONE
