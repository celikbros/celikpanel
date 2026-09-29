#!/bin/bash
# Read-only guest-side timeline sampler. Args: CELL OUTDIR
# Copies the switch journal and the mutation ledger whenever their bytes change.
CELL=$1; OUT=$2
R=/var/lib/celikpanel-dns-kill-matrix/results/$CELL
P=/var/lib/celikpanel-agent-private
mkdir -p "$OUT/copies"
T=$OUT/timeline.log
snap() {
  local tag=$1 f=$OUT/snap-$1.txt
  {
    echo "tag=$tag wall=$(date -u +%FT%T.%NZ) mono=$(cat /proc/uptime) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
    for u in pdns.service named.service bind9.service celikpanel-agent.service celikpanel-panel.service; do
      echo "== $u"; systemctl show "$u" -p LoadState,UnitFileState,ActiveState,SubState,ExecMainStartTimestamp,ExecMainStartTimestampMonotonic,ExecMainPID,MainPID,ExecMainCode,ExecMainStatus,NRestarts,InvocationID,ActiveEnterTimestamp,InactiveEnterTimestamp 2>&1
    done
    echo "== processes"; pgrep -a 'pdns_server|named|agent' 2>&1
    echo "== journal file"; ls -la --time-style=full-iso $P/ 2>&1
    if [ -f $P/dns-engine-switch-journal.json ]; then
      echo "== journal content"; cat $P/dns-engine-switch-journal.json; echo
      cp -p $P/dns-engine-switch-journal.json "$OUT/journal-at-$tag.json" 2>/dev/null
    fi
    echo "== ss 53"; ss -H -lntup 'sport = :53' 2>&1
    echo "== managed BIND roots (lstat, no follow)"; ptrsig
    if [ -f /var/lib/powerdns/pdns.sqlite3 ]; then
      echo "== PowerDNS database (file copy, no lock)"; ls -la --time-style=full-iso /var/lib/powerdns/ 2>&1
      cp -p /var/lib/powerdns/pdns.sqlite3 "$OUT/pdns-at-$tag.sqlite3" 2>/dev/null
      [ -f /var/lib/powerdns/pdns.sqlite3-wal ] && cp -p /var/lib/powerdns/pdns.sqlite3-wal "$OUT/pdns-at-$tag.sqlite3-wal" 2>/dev/null
    fi
    ls -la --time-style=full-iso /var/cache/bind/celikpanel /var/named/celikpanel /var/cache/bind/celikpanel/generations /var/named/celikpanel/generations 2>&1
  } > "$f" 2>&1
}
copy_if_changed() {
  local src=$1 name=$2 var=$3 cur
  if [ -f "$src" ]; then cur=$(sha256sum "$src" | cut -c1-64); else cur=absent; fi
  if [ "$cur" != "${!var}" ]; then
    local ts; ts=$(date -u +%H%M%S.%N | cut -c1-10)
    if [ "$cur" != absent ]; then cp -p "$src" "$OUT/copies/$ts-$name-${cur:0:12}.json" 2>/dev/null; fi
    echo "$(date -u +%T.%N) CHANGE $name ${!var} -> $cur" >> "$T"
    printf -v "$var" '%s' "$cur"
  fi
}
# lstat signature of the managed BIND root and its current pointer (Debian and Arch layouts)
ptrsig() {
  local d
  for d in /var/cache/bind/celikpanel /var/named/celikpanel; do
    if [ -e "$d" ] || [ -L "$d" ]; then stat -c '%n mtime=%y ino=%i' "$d"; fi
    if [ -e "$d/current" ] || [ -L "$d/current" ]; then stat -c '%n mtime=%y ino=%i -> %N' "$d/current"; elif [ -e "$d" ]; then echo "$d/current absent"; fi
  done
}
# PowerDNS database: plain file copy of the main file and its WAL whenever their bytes change (no SQLite lock is taken)
copy_db_if_changed() {
  local src=/var/lib/powerdns/pdns.sqlite3 cur
  if [ -f "$src" ]; then cur=$(cat "$src" "$src-wal" 2>/dev/null | sha256sum | cut -c1-64); else cur=absent; fi
  if [ "$cur" != "$dsha" ]; then
    local ts; ts=$(date -u +%H%M%S.%N | cut -c1-10)
    if [ "$cur" != absent ]; then cp -p "$src" "$OUT/copies/$ts-pdnsdb-${cur:0:12}.sqlite3" 2>/dev/null; [ -f "$src-wal" ] && cp -p "$src-wal" "$OUT/copies/$ts-pdnsdb-${cur:0:12}.sqlite3-wal" 2>/dev/null; fi
    echo "$(date -u +%T.%N) CHANGE pdnsdb $dsha -> $cur" >> "$T"
    dsha=$cur
  fi
}
jsha=none; lsha=none; ssha=none; psig=none; dsha=none
snap start
seen_kill=0; end=$(( $(date +%s) + 4000 ))
while [ "$(date +%s)" -lt "$end" ]; do
  copy_if_changed $P/dns-engine-switch-journal.json journal jsha
  copy_if_changed $P/service-mutations.json ledger lsha
  copy_if_changed $P/dns-engine-state.json state ssha
  copy_db_if_changed
  cur="$(ptrsig 2>&1 | tr '\n' ' ')"
  if [ "$cur" != "$psig" ]; then echo "$(date -u +%T.%N) POINTER $cur" >> "$T"; psig="$cur"; fi
  line="$(date -u +%T.%N) pdns=$(systemctl show pdns.service -p ActiveState,SubState,MainPID,NRestarts --value 2>/dev/null | tr '\n' '/') named=$(systemctl show named.service -p ActiveState,SubState,MainPID --value 2>/dev/null | tr '\n' '/') agent=$(systemctl show celikpanel-agent.service -p ActiveState,MainPID --value 2>/dev/null | tr '\n' '/') jphase=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("phase","?"))' $P/dns-engine-switch-journal.json 2>/dev/null || echo absent) killpid=$(pgrep -x agent.kill | tr '\n' ,)"
  echo "$line" >> "$T"
  if [ $seen_kill = 0 ] && [ -e "$R/kill-proof.json" ]; then seen_kill=1; snap at-kill-proof; fi
  if [ -e "$R/result.json" ]; then sleep 3; copy_if_changed $P/dns-engine-switch-journal.json journal jsha; copy_if_changed $P/service-mutations.json ledger lsha; snap at-result; break; fi
  sleep 0.2
done
echo done >> "$T"
