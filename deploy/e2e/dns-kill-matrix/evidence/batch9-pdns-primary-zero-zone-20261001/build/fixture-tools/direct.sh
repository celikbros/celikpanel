# usage: direct.sh zNN -- run one launcher directly (no stop/teardown afterwards; decided after inspection)
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b9
CL=/var/tmp/cp-b9-1001/logs/chain.log
set -- $(cat $SP/$1.sh) ; SHORT=$3
if pgrep -x qemu-system-x86_64 >/dev/null; then echo "$(date -u +%FT%TZ) QEMU still running before $SHORT; not started" >> $CL; exit 1; fi
echo "$(date -u +%FT%TZ) start $SHORT (direct runcell; stop/teardown decided after inspection)" >> $CL
"$@"
echo "$(date -u +%FT%TZ) done $SHORT $(cat /var/tmp/cp-b9-1001/logs/$SHORT/run-prepared.rc 2>/dev/null) (direct; guests left running for inspection)" >> $CL
