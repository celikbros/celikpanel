#!/bin/bash
# set8: the disk rule before a guest start. Reads the newest Windows C: reading written by cwatch.ps1 (PowerShell
# Get-PSDrive C, every 30 s; WSL cannot run PowerShell on this host). Exit 0 only when the reading is younger than
# 120 s and at least 40 GiB and no below-40 flag was written; every decision is appended to c-drive-cells.txt.
# usage: gate.sh LABEL
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
R=/var/tmp/cp-set8-run
label=${1:-guest start}
reading=$(tail -n 1 $J/c-drive-watch.txt 2>/dev/null | tr -d '\r')
stamp=$(echo "$reading" | cut -d' ' -f1)
gib=$(echo "$reading" | sed -n 's/.*free_GiB=\([0-9.]*\).*/\1/p')
bytes=$(echo "$reading" | sed -n 's/.*free_bytes=\([0-9]*\).*/\1/p')
age=$(( $(date -u +%s) - $(date -u -d "$stamp" +%s 2>/dev/null || echo 0) ))
ok=yes
[ -n "$bytes" ] && [ "$bytes" -ge 42949672960 ] || ok=no
[ "$age" -le 120 ] || ok=no
[ ! -e $J/c-drive.below40 ] || ok=no
echo "$(date -u +%FT%TZ) before $label free_GiB=$gib free_bytes=$bytes (Get-PSDrive C reading of $stamp, ${age}s old) allowed=$ok" >> $R/c-drive-cells.txt
[ $ok = yes ]
