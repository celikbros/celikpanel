# usage: stop12.sh SHORT CELL ROOT -- stop both guests of the cell (QMP quit via fixture.py stop; overlays kept)
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
echo "### $(date -u +%FT%T.%3NZ) stop guests (fixture.py stop: QMP quit, overlays kept)" >> /var/tmp/cp-b12-1001/logs/$1/driver.log
bash $SP/stoponly.sh "$1" "$2" "$3"
pgrep -af qemu-system | cut -c1-160 || echo "no qemu process"
