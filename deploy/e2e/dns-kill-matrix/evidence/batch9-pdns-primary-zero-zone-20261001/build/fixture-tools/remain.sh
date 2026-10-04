# Read-only: what this run left on the host
date -u +%FT%TZ
pgrep -a qemu || echo "no qemu process"
for r in $(cat /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b9/roots.txt); do
  echo "== $r/cells"; ls $r/cells
  for d in $r/cells/*/; do [ -d "$d" ] && echo "$d: $(cat $d/cell-id 2>/dev/null || grep -o '"cell_id": *"[^"]*"' $d/plan.json 2>/dev/null | head -1) $(ls $d | tr '\n' ' ')"; done
done
du -sh /var/tmp/cp-b9-1001 /root/cp-b9-src /root/cp-b9-artifacts /root/cp-b9-tools 2>&1
ls -la /var/tmp/cp-b9-build.log /var/tmp/cp-b9-webbuild.log /var/tmp/cp-b9-setup.log /var/tmp/cp-b9-webdist.sha256
ls -d /var/tmp/cp-b9* /root/cp-b9*
