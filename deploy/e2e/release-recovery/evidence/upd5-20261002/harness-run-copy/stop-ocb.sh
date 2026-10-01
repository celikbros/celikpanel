#!/bin/bash
# Stop only the upd5-arch-oc-b run (wrapper, lab start, its two QEMU guests) with SIGTERM; nothing is deleted.
for pid in 637 653 658 670; do
  c=$(tr '\0' ' ' < /proc/$pid/cmdline 2>/dev/null) || { echo "$pid gone"; continue; }
  case "$c" in
    *upd5-arch-oc-b*) kill -TERM "$pid" && echo "TERM sent $pid: ${c:0:120}";;
    *) echo "skip $pid (not upd5-arch-oc-b): ${c:0:80}";;
  esac
done
sleep 5
for pid in 637 653 658 670; do [ -e /proc/$pid ] && echo "$pid still present" || echo "$pid ended"; done
