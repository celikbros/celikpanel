#!/bin/bash
L=/var/tmp/cp-upd3-run/logs
for f in $L/*.start; do n=$(basename $f .start); echo "$n start=$(cat $f) end=$(cat $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null)"; done
for n in "$@"; do echo "--- $n.out"; tail -${TAILN:-15} $L/$n.out; echo "--- $n.err"; tail -${TAILN:-8} $L/$n.err; done
pgrep -a qemu | cut -c1-80
