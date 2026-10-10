#!/bin/bash
# set4b: the browser inspection of the scenarios this run changed, on the final web build (loopback mock only).
S=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "/c/CELIKBROS PROJECTS/celikpanel" || exit 2
export BROWSER_INSPECT_MODULES="$S/browser/tools" BROWSER_INSPECT_OUT="$S/set4b/shots-final"
mkdir -p "$BROWSER_INSPECT_OUT"
: > "$S/set4b/shots-final.log"
port=4881
for c in "desktop en light" "desktop tr light" "phone en light" "phone tr light" "desktop tr dark" "phone en dark"; do
  set -- $c
  node web/tools/browser-inspect/run.mjs $1 $2 $3 $port updaterolledback,importentries,importleftout,stopnote > "$S/set4b/shots-final-$1-$3-$2.log" 2>&1
  echo "$1-$3-$2 rc=$? $(tail -n 1 "$S/set4b/shots-final-$1-$3-$2.log")" >> "$S/set4b/shots-final.log"
  port=$((port + 1))
done
echo done >> "$S/set4b/shots-final.log"
