#!/bin/bash
# set9: the guest timeline of one lab (read only): probe transitions, Panel unit stop/start lines, version after.
lab=/var/tmp/cp-release-drill-$1
d=$(ls -d $lab/evidence/debian13/upd1/upd1-debian13-good-*/ | tail -1)
s=$(ls -d $d/steps/*set7-guest-timeline* | tail -1)
python3 -I -c "import json,sys; c=json.load(open(sys.argv[1])); ch=c.get('checks',c); print('skew', ch.get('guest_minus_host_seconds')); print('version_after', json.dumps(ch.get('panel_version_after'))[:300]); [print('probe', t) for t in ch.get('probe_transitions',[])]" $s/step.json
grep -E "Stopping CelikPanel|Stopped celikpanel-panel|Started celikpanel-panel|startup listener|Starting CelikPanel Backend|SIGSTOP|frozen|self-update" $s/journal-celikpanel-units.txt | grep -v "^--" | tail -n 25 | cut -c1-220
w=$(ls -d $d/steps/*set7-browser-window* | tail -1)
python3 -I -c "import json,sys; c=json.load(open(sys.argv[1])); ch=c.get('checks',c); [print('restart', r.get('request'), r.get('guest_output','').replace(chr(10),' | ')[:400]) for r in ch.get('restarts',[])]; print('password_file_removed', ch.get('password_file_removed'))" $w/step.json
