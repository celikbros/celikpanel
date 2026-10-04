#!/bin/bash
# Read-only: find the arch-oc-a guest serial log and show I/O error / pacman / systemd lines (prints to stdout only).
L=/var/tmp/cp-release-drill-upd5-arch-oc-a
mount | grep ' / '
ls -la "$L" "$L"/cells/*/ "$L"/cells/*/arch/ 2>&1 | head -40
for f in "$L"/cells/*/arch/*.log "$L"/cells/*/arch/serial* "$L"/*serial*; do
  [ -f "$f" ] || continue
  echo "== $f"
  grep -naE 'I/O error|EXT4-fs|blk_update|Buffer I/O|critical|pacman|upgrad|systemd\[1\]: (Reexec|Reload)' "$f" 2>&1 | tail -n 40 | cut -c1-220
done
