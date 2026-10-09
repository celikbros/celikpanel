#!/bin/bash
# usage: peek.sh JOB LAB NODE -> processes and the newest setup evidence (read-only)
L=/var/tmp/cp-set2-run/logs
pgrep -a qemu | cut -c1-90
ps -eo pid,etimes,pcpu,args | grep -E "trial.py|lab.py" | grep -v grep | cut -c1-150
ev=$(ls -d /var/tmp/cp-release-drill-$2/evidence/$3/upd1/*/ 2>/dev/null | tail -1)
ls -t $ev/steps/*/ 2>/dev/null | head -5
ls -t $ev/steps/06-setup 2>/dev/null | head -4
date -u +%T
