#!/bin/bash
L=/var/tmp/cp-release-drill-upd3-d13-def-a
find $L -maxdepth 3 \( -path "$L/images" -o -path "$L/evidence" \) -prune -o -print | head -60
echo; find $L -type f -newer $L/key ! -path "*/evidence/*" ! -name "*.qcow2" -printf '%s %p\n' | head -60
du -sh /var/tmp/cp-release-drill-upd3-* /var/tmp/cp-upd3-run /var/tmp/cp-upd1-build/20260930t174023z
ls -d /var/tmp/cp-pair-accept/dist/*acceptance-license | xargs -n1 basename
