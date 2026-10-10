#!/bin/bash
d=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/set8-*/ | tail -1)
python3 -I "$(dirname "$0")/s0ref.py" "$d"
