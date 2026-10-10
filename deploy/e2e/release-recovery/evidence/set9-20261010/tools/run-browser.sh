#!/bin/bash
# set9: the browser on Windows. usage: run-browser.sh SCRIPT CELLS   (SCRIPT: cold | live); env LIVE_* passed through
S=<scratchpad>/set9
export BROWSER_INSPECT_MODULES=$(cygpath -w $S/../set7/browser-modules)
case $1 in
  cold) JS="/c/CELIKBROS PROJECTS/celikpanel/web/tools/browser-inspect/cold-load-set9.mjs";;
  live) JS="$S/tools/live-restart-7c3a05809.mjs";;
  *) echo "usage"; exit 2;;
esac
mkdir -p $S/browser
echo "$(date -u +%FT%TZ) start $1 $2 tag=${LIVE_TAG:-} lang=${LIVE_LANG:-en}" >> $S/browser/runner.log
node "$JS" //wsl.localhost/archlinux/var/tmp/cp-set9-run/hand/${HAND:-hand-s9} "$(cygpath -w $S/browser)" "$2" >> $S/browser/runner.log 2>&1
echo "$(date -u +%FT%TZ) rc=$? $1 $2" >> $S/browser/runner.log
tail -n 4 $S/browser/runner.log
