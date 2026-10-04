# Read-only: what this run left on the host
date -u +%FT%TZ
pgrep -a qemu || echo "no qemu process"
for r in /var/tmp/cp-b8r-1001 /var/tmp/cp-b8r-1001/r2 /var/tmp/cp-b8r-1001/r3; do
  echo "== $r/cells"; ls $r/cells
  for d in $r/cells/*/; do [ -d "$d" ] && echo "$d: $(cat $d/cell-id 2>/dev/null || grep -o '"cell_id": *"[^"]*"' $d/plan.json 2>/dev/null | head -1) $(ls $d | tr '\n' ' ')"; done
done
du -sh /var/tmp/cp-b8r-1001 /root/cp-b8r-src /root/cp-b8r-artifacts /root/cp-b8r-tools 2>&1
ls -la /var/tmp/cp-b8r-build.log /var/tmp/cp-b8r-webbuild.log /var/tmp/cp-b8r-setup.log /var/tmp/cp-b8r-webdist.sha256
ls -d /var/tmp/cp-b8* /root/cp-b8*
