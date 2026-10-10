#!/bin/bash
# read-only: processes of this run that are still alive on the QEMU host
pgrep -a qemu | cut -c1-80 || true
ps -eo pid,etimes,args | grep -E "trial.py|lab.py|run-set2|run-upd1|watch.sh|tail -n 0 -F|waitstep|sleep 28800|debug" | grep -v grep | cut -c1-140
echo "--- end of list"
