#!/bin/bash
# usage: cell.sh CELL LABNAME SSHPORT
set -u
cell=$1 name=$2 port=$3 draft=${4:-}
export UPD1_SETUP_DRAFT_JSON=$draft
art=$(cat /var/tmp/cp-upd1-run/artifacts.path)
log=/var/tmp/cp-upd1-run/$cell.$name
if pgrep -x qemu-system-x86_64 >/dev/null || pgrep -f qemu-system >/dev/null; then echo "QEMU already running; refusing"; exit 3; fi
cd "/mnt/c/CELIKBROS PROJECTS/celikpanel"  # repository root as the README says; harness from the run copy
date -u +%FT%TZ > $log.start
PYTHONDONTWRITEBYTECODE=1 bash /var/tmp/cp-upd1-run/run-upd1.v3.sh cell "$cell" "$art" "$name" "$port" > $log.out 2> $log.err
echo $? > $log.rc
date -u +%FT%TZ > $log.end
echo "rc=$(cat $log.rc)"; tail -5 $log.out
