# usage: chain.sh cNN... -- run the given cell launchers one at a time; after each: exit 0 -> stop + teardown; else stop only (overlays kept)
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b9
CL=/var/tmp/cp-b9-1001/logs/chain.log
for c in "$@"; do
  line=$(cat $SP/$c.sh)
  set -- $line
  SHORT=$3 CELL=$4 ROOT=$5
  echo "$(date -u +%FT%TZ) start $c $SHORT $CELL $ROOT" >> $CL
  if pgrep -x qemu-system-x86_64 >/dev/null; then echo "$(date -u +%FT%TZ) QEMU still running before $c; chain stopped" >> $CL; exit 1; fi
  bash $SP/$c.sh
  rc=$(cat /var/tmp/cp-b9-1001/logs/$SHORT/run-prepared.rc 2>/dev/null)
  echo "$(date -u +%FT%TZ) done $c $rc" >> $CL
  if [ "$rc" = "RUN_PREPARED_RC=0" ]; then
    bash $SP/down.sh $SHORT $CELL $ROOT >> $CL 2>&1
  else
    bash $SP/stoponly.sh $SHORT $CELL $ROOT >> $CL 2>&1
  fi
done
echo "$(date -u +%FT%TZ) CHAIN-DONE" >> $CL
