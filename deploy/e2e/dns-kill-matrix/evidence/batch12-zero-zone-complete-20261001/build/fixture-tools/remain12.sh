# Read-only: what this run left on the host
date -u +%FT%TZ
echo "== QEMU processes"; pgrep -af qemu-system | cut -c1-160 || echo "no qemu process"
for r in /var/tmp/cp-b12-1001/r1 /var/tmp/cp-b12-1001/r2; do echo "== $r/cells"; ls -la $r/cells; for d in $r/cells/*/; do echo "$d: $(ls $d | tr '\n' ' ') $(ls $d/debian13 | tr '\n' ' ')"; done; done
du -sh /var/tmp/cp-b12-1001 /root/cp-b12-src /root/cp-b12-artifacts /root/cp-b12-tools 2>&1
ls -la /var/tmp/cp-b12-build.log /var/tmp/cp-b12-setup.log /var/tmp/cp-b12-webdist.sha256 /var/tmp/cp-b12-triggertest.log /var/tmp/cp-b12-tcount.log
ls -d /var/tmp/cp-b12* /root/cp-b12*
echo "== other roots untouched (listing only)"; ls -d /var/tmp/cp-pair7-work /var/tmp/cp-b11-1001 /var/tmp/cp-b10-1001 /var/tmp/cp-pair6-work 2>&1
free -m | head -2
