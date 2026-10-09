#!/bin/bash
# read-only: every step verdict and wrapper end line of the cell jobs, as they are written
L=/var/tmp/cp-set2-run/logs
tail -n 0 -F $L/cell-*.out 2>/dev/null | grep --line-buffered -E '^\{"step"|^SET2 |^==> |Traceback|lab refused' | cut -c1-420
