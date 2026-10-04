#!/bin/bash
S=/var/tmp/cp-upd3-run/stage/upd3-20261001
for c in upd1-arch-good upd1-arch-defective upd1-arch-startcheck upd1-arch-realstart upd1-debian13-defective upd1-debian13-startcheck upd1-debian13-realstart upd1-debian13-good; do
  d=$S/$c/run-a/side
  nb=$(ls $d/inspect-before-site-* 2>/dev/null | wc -l)
  last=$(ls $d/inspect-before-site-* 2>/dev/null | tail -1)
  echo "== $c before-site probes=$nb last=$(basename "$last")"
  [ -n "$last" ] && grep -E '/var/www$|/var/www/celikpanel$|^absent|rc=' "$last" | head -4
  a=$(ls $d/inspect-after-seed-* | head -1)
  echo "   after-seed $(basename $a):"; grep -E ' /var/www$| /var/www/celikpanel$|"path"|host-header' $a | tr -s ' ' | head -6
  t=$(ls $d/inspect-after-track-* 2>/dev/null | head -1)
  [ -n "$t" ] && { echo "   after-track:"; grep -E 'host-header|upd1-cron-stamp' $t | head -3; }
done
