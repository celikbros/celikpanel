#!/bin/bash
# Read-only: list run processes (qemu, driver, wrapper, lab) via /proc; no external binaries needed.
for d in /proc/[0-9]*; do
  c=$(tr '\0' ' ' < "$d/cmdline" 2>/dev/null) || continue
  case "$c" in
    *qemu-system*|*owner_update_trial*|*run-upd1*|*lab.py*|*bg.sh*) echo "${d#/proc/} ${c:0:300}";;
  esac
done
