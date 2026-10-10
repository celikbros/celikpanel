#!/bin/bash
# set8: summary of one lab's driver record. usage: sum.sh LAB NODE [--detail]
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
d=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/set8-*/ | tail -1)
echo "$d"
python3 -I $J/summary.py "$d" $3
