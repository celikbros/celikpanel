#!/bin/bash
# set4c read-only: print chosen readings.
cd /var/tmp/cp-set4c-run/reading || exit 2
for f in "$@"; do
  echo "== $f exit $(cat $f.exit)"; echo "-- stdout"; cat -A $f.stdout | cut -c1-220; echo "-- stderr"; cat -A $f.stderr | cut -c1-220; echo "-- combined"; cat -A $f.combined | cut -c1-220
done
