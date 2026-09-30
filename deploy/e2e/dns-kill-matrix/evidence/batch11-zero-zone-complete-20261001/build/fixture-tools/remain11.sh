# Read-only: what this run left on the host
date -u +%FT%TZ
echo "== QEMU processes"; pgrep -af qemu-system | cut -c1-160 || echo "no qemu process"
for r in /var/tmp/cp-b11-1001/r1 /var/tmp/cp-b11-1001/r2; do echo "== $r/cells"; ls -la $r/cells; for d in $r/cells/*/; do echo "$d: $(ls $d | tr '\n' ' ') $(ls $d/debian13 | tr '\n' ' ')"; done; done
du -sh /var/tmp/cp-b11-1001 /root/cp-b11-src /root/cp-b11-artifacts /root/cp-b11-tools 2>&1
ls -la /var/tmp/cp-b11-build.log /var/tmp/cp-b11-webbuild.log /var/tmp/cp-b11-setup.log /var/tmp/cp-b11-webdist.sha256 /var/tmp/cp-b11-triggertest.log /var/tmp/cp-b11-tcount.log
ls -d /var/tmp/cp-b11* /root/cp-b11*
echo "== other roots untouched (listing only)"; ls -d /var/tmp/cp-pair5-work /var/tmp/cp-b10-1001 /var/tmp/cp-b9-1001 2>&1
free -m | head -2
