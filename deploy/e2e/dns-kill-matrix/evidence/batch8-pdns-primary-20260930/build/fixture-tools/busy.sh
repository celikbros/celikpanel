# usage: busy.sh SHORT -- read-only: what was busy around the begin refusal
E=/var/tmp/cp-b8-0930/evidence/$1
echo "== agent journal 20:59:30-21:00:30"; grep -E 'T2[01]:(59|00):' $E/raw/journald/celikpanel-agent.service.txt | cut -c1-600 | tail -30
echo "== driver timestamps"; grep '###' $E/driver.log | tail -12
echo "== pre-run units (processes)"; grep -iE 'apt|dpkg|unattended' $E/pre-run-units.txt | head
echo "== watcher snap start processes"; sed -n '/== processes/,/== journal file/p' $E/raw/watch/cp-b8-watch/snap-start.txt 2>/dev/null | head -20
echo "== unit list in journald dir"; ls $E/raw/journald/
echo "== owner-post apt history"; grep -A40 'apt history' $E/owner-post-state.txt | head -20
