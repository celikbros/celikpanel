# Cell 2: extra read-only observations after the pending resume (not a harness verdict): observe --zero-zones, observe-child --step delete --zero-zones, pairq both guests
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b11
ROOT=/var/tmp/cp-b11-1001/r2; CELL=pdns-switch__target-started__after-write__paired-primary__peer-reachable
LOG=/var/tmp/cp-b11-1001/logs/z05-zero-started-zl-rb
cd /root/cp-b11-src
echo "### $(date -u +%FT%T.%3NZ) extra read-only observations after the pending resume" >> $LOG/driver.log
PB=deploy/e2e/dns-kill-matrix/native_pdns_bind_peer.py
PC="--work-root $ROOT --cell-id $CELL --identity-file $ROOT/id_ed25519 --source-fixture uninitialized"
{
echo "### $(date -u +%FT%T.%3NZ) native_pdns_bind_peer.py observe --zero-zones --execute (read-only)"
python3 $PB observe $PC --zero-zones --execute 2>&1; echo "[rc=$?]"
echo "### $(date -u +%FT%T.%3NZ) native_pdns_bind_peer.py observe-child --step delete --zero-zones --execute (read-only)"
python3 $PB observe-child $PC --step delete --zero-zones --execute 2>&1; echo "[rc=$?]"
echo "### $(date -u +%FT%T.%3NZ) end"
} > $LOG/extra-observe-after-pending.txt
python3 $SP/gssh.py $ROOT $CELL debian13 'sudo python3 -' < $SP/pairq.py > $LOG/dns-after-pending-guest.txt 2>&1
python3 $SP/gssh.py $ROOT $CELL arch 'sudo python3 -' < $SP/pairq.py > $LOG/dns-after-pending-peer.txt 2>&1
cut -c1-1800 $LOG/extra-observe-after-pending.txt
grep -i 's2.s1' $LOG/dns-after-pending-guest.txt $LOG/dns-after-pending-peer.txt | cut -c1-260 | head -12
