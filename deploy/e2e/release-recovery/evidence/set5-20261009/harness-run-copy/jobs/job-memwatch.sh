#!/bin/bash
# set5: the WSL host's memory every 30 s while the cells run (MemAvailable, swap in use, tmpfs in use under the labs);
# ends when /var/tmp/cp-set5-run/hold.stop exists or after 14 h. Read-only.
for i in $(seq 1 1680); do
  [ -e /var/tmp/cp-set5-run/hold.stop ] && exit 0
  echo "$(date -u +%FT%TZ) $(awk '/^MemAvailable:/ {a=int($2/1024)} /^SwapTotal:/ {t=$2} /^SwapFree:/ {f=$2} END {printf "mem_available_MiB=%d swap_used_MiB=%d", a, (t-f)/1024}' /proc/meminfo) lab_tmpfs_used=$(findmnt -rn -t tmpfs -o TARGET,USED | awk '$1 ~ /^\/var\/tmp\/cp-release-drill-s5-/ {printf "%s%s", sep, $2; sep="+"} END {if (!sep) printf "none"}') qemu=$(pgrep -c qemu)" >> /var/tmp/cp-set5-run/mem-watch.txt
  sleep 30
done
