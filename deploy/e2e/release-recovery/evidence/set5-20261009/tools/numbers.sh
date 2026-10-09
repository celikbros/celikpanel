#!/bin/bash
# set5 read-only: the numbers the README quotes about disk and memory, from the run's own logs
R=/var/tmp/cp-set5-run
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
echo "gate decisions: $(grep -c . $R/c-drive-cells.txt), allowed=yes $(grep -c 'allowed=yes' $R/c-drive-cells.txt), allowed=no $(grep -c 'allowed=no' $R/c-drive-cells.txt)"
echo "gate readings (free_GiB), in order: $(sed -n 's/.*free_GiB=\([0-9.]*\).*/\1/p' $R/c-drive-cells.txt | tr '\n' ' ')"
echo "oldest reading used by the gate (s): $(sed -n 's/.*, \([0-9]*\)s old.*/\1/p' $R/c-drive-cells.txt | sort -n | tail -1)"
echo "watcher: $(grep -c . $J/c-drive-watch.txt) readings, first $(head -n 1 $J/c-drive-watch.txt | cut -d' ' -f1,4), last $(tail -n 1 $J/c-drive-watch.txt | cut -d' ' -f1,4)"
echo "watcher lowest: $(sed 's/free_GiB=//' $J/c-drive-watch.txt | sort -k4 -n | head -n 1 | cut -d' ' -f1,4)"
echo "below-40 flag file: $(ls $J/c-drive.below40 2>/dev/null || echo absent)"
echo "ram-nodes MemAvailable before the mounts (MiB): $(sed -n 's/.*MemAvailable \([0-9]*\) MiB.*/\1/p' $R/ram-nodes.txt | tr '\n' ' ')"
echo "mem-watch: $(grep -c . $R/mem-watch.txt) lines; lowest mem_available: $(sed 's/mem_available_MiB=//' $R/mem-watch.txt | sort -k2 -n | head -n 1 | cut -c1-120)"
echo "mem-watch: highest swap_used_MiB: $(sed -n 's/.*swap_used_MiB=\([0-9]*\).*/\1/p' $R/mem-watch.txt | sort -n | tail -1)"
echo "mem-watch: largest lab tmpfs use seen: $(grep -o 'lab_tmpfs_used=[^ ]*' $R/mem-watch.txt | sort -u | tr '\n' ' ' | cut -c1-600)"
cat $J/c-drive.txt
