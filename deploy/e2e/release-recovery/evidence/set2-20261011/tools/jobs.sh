#!/bin/bash
L=/var/tmp/cp-set2-run/logs
for f in $L/*.start; do n=$(basename $f .start); echo "$n start=$(cat $f) end=$(cat $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null)"; done
for o in /var/tmp/cp-set2-run/overlay-*; do echo "$(basename $o) diff_sha=$(sha256sum $o/harness.diff | cut -c1-16) changed_lines=$(grep -c '^[-+][^-+]' $o/harness.diff) files=$(wc -l < $o/files.sha256)"; done
wc -l /var/tmp/cp-set2-run/runcopy-faa5ef085-files.sha256
