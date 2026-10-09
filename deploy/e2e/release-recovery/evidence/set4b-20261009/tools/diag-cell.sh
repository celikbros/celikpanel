#!/bin/bash
# set4b read-only: what the running cell's processes are and when its files were last written.
date -u +%FT%TZ; uptime
ps -eo pid,ppid,etimes,stat,args --sort=etimes | grep -E 'set4b_trial|run-set4b|qemu|ssh |lab.py' | grep -v grep | cut -c1-230
L=/var/tmp/cp-release-drill-$1
find $L/evidence -type f -printf '%TT %p\n' 2>/dev/null | sort | tail -n 6 | cut -c1-200
ls -la --time-style=+%T $L/evidence/*/upd1/*/steps/ 2>/dev/null | tail -n 6
journalctl -k --since "-60 min" --no-pager 2>/dev/null | grep -iE 'suspend|resume|hibern|clock|time jump' | tail -n 5
dmesg 2>/dev/null | tail -n 4
