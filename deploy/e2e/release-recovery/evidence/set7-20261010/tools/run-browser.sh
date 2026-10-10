#!/bin/bash
# set7: the browser on Windows. usage: run-browser.sh LAB CELLS   (LAB: a82 | a81)
S=<scratchpad>/set7
export BROWSER_INSPECT_MODULES=$(cygpath -w $S/browser-modules)
mkdir -p $S/browser/$1
node "/c/CELIKBROS PROJECTS/celikpanel/web/tools/browser-inspect/live-restart.mjs" //wsl.localhost/archlinux/var/tmp/cp-set7-run/hand/hand-$1 "$(cygpath -w $S/browser/$1)" "$2" >> $S/browser/$1/runner.log 2>&1
echo "rc=$?" >> $S/browser/$1/runner.log
tail -n 6 $S/browser/$1/runner.log
