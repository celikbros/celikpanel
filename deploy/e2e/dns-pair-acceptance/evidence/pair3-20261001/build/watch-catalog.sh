#!/bin/bash
# Read-only catalog watcher for one cell. usage: watch-catalog.sh CELL PRIMARY_PORT SECONDARY_PORT PRIMARY_IP CATALOG LABEL PRIMARY_ENGINE
CELL=$1; PP=$2; SP=$3; PIP=$4; CAT=$5; LABEL=$6; PENG=$7
H=$(printf %s "$CELL" | sha256sum | cut -c1-24)
KH=/var/tmp/cp-pair3-work/cells/$H/ssh-known-hosts
PROBE=/root/cp-pair3/driver/deploy/e2e/dns-pair-acceptance/guest_probe.py
DB=/root/cp-pair3/pdnsdb.py
LOG=/root/cp-pair3/logs/$LABEL-catalog-watch.jsonl
sshn() { port=$1; shift; timeout 60 ssh -o ServerAliveInterval=5 -o ServerAliveCountMax=3 -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=yes -o UserKnownHostsFile=$KH -i /var/tmp/cp-pair3-key/id_ed25519 -p $port celik@127.0.0.1 "$@"; }
prev=""; dbreads=0
end=$(( $(date +%s) + 3*3600 ))
while [[ $(date +%s) -lt $end ]]; do
  grep -q 'run end' /root/cp-pair3/logs/$LABEL-runner.log 2>/dev/null && { echo "{\"t\":\"$(date -u +%FT%TZ)\",\"watch\":\"run ended\"}" >> $LOG; break; }
  obs=$(sshn $SP sudo -n /usr/bin/python3 - catalog --server $PIP --catalog $CAT < $PROBE 2>&1 | tail -1)
  if [[ "$obs" != "$prev" ]]; then
    echo "{\"t\":\"$(date -u +%FT%TZ)\",\"catalog_axfr_from_secondary\":$obs}" >> $LOG 2>/dev/null || echo "{\"t\":\"$(date -u +%FT%TZ)\",\"raw\":\"$(echo $obs | tr -d '"\')\"}" >> $LOG
    if [[ $PENG == pdns && $dbreads -lt 12 && "$obs" == *'"transferred": true'* ]]; then
      d=$(sshn $PP sudo -n /usr/bin/python3 - < $DB 2>&1 | tail -1)
      echo "{\"t\":\"$(date -u +%FT%TZ)\",\"primary_pdns_db\":$d}" >> $LOG
      dbreads=$((dbreads+1))
    fi
    prev="$obs"
  fi
  sleep 10
done
