# Read-only: what this run left on the host
date -u +%FT%TZ
pgrep -a qemu || echo "no qemu process"
for r in /var/tmp/cp-b8-0930 /var/tmp/cp-b8-0930/r2 /var/tmp/cp-b8-0930/r3; do
  echo "== $r/cells"; ls $r/cells
  for d in $r/cells/*/; do [ -d "$d" ] && echo "$d: $(cat $d/cell-id 2>/dev/null || grep -o '"cell_id": *"[^"]*"' $d/plan.json 2>/dev/null | head -1) $(ls $d | tr '\n' ' ')"; done
done
du -sh /var/tmp/cp-b8-0930 /root/cp-b8-src /root/cp-b8-artifacts /root/cp-b8-tools 2>&1
ls -la /var/tmp/cp-b8-build.log /var/tmp/cp-b8-webbuild.log /var/tmp/cp-b8-setup.log /var/tmp/cp-b8-webdist.sha256
ls -d /var/tmp/cp-b8* /root/cp-b8*
