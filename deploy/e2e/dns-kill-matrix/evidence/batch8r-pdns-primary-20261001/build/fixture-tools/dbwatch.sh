#!/bin/bash
# Read-only PowerDNS database sampler on the primary (batch 8r addition). Args: OUTDIR [SECONDS]
# Plain file copy of /var/lib/powerdns/pdns.sqlite3 and its WAL whenever their bytes change (no SQLite lock is taken).
# Unlike watcher.sh it does not stop at result.json, so it also covers the zone lifecycle after the controller finished.
# Stops when /var/tmp/cp-b8r-dbwatch.stop exists or after SECONDS (default 5400).
OUT=$1; SECS=${2:-5400}
mkdir -p "$OUT/copies"
T=$OUT/timeline.log
src=/var/lib/powerdns/pdns.sqlite3
dsha=none
end=$(( $(date +%s) + SECS ))
echo "$(date -u +%T.%N) start boot_id=$(cat /proc/sys/kernel/random/boot_id)" >> "$T"
while [ "$(date +%s)" -lt "$end" ] && [ ! -e /var/tmp/cp-b8r-dbwatch.stop ]; do
  if [ -f "$src" ]; then cur=$(cat "$src" "$src-wal" 2>/dev/null | sha256sum | cut -c1-64); else cur=absent; fi
  if [ "$cur" != "$dsha" ]; then
    ts=$(date -u +%H%M%S.%N | cut -c1-10)
    if [ "$cur" != absent ]; then cp -p "$src" "$OUT/copies/$ts-${cur:0:12}.sqlite3" 2>/dev/null; [ -f "$src-wal" ] && cp -p "$src-wal" "$OUT/copies/$ts-${cur:0:12}.sqlite3-wal" 2>/dev/null; fi
    echo "$(date -u +%T.%N) CHANGE pdnsdb $dsha -> $cur pdns=$(systemctl show pdns.service -p ActiveState,MainPID --value 2>/dev/null | tr '\n' '/')" >> "$T"
    dsha=$cur
  fi
  sleep 0.5
done
echo done >> "$T"
