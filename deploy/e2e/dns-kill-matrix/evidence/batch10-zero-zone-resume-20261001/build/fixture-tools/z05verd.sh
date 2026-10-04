python3 /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10/z05verd.py /var/tmp/cp-b10-1001/evidence/z05-held-resume 2>&1 | cut -c1-400 | head -120
E=/var/tmp/cp-b10-1001/evidence/z05-held-resume
grep -E '19255434 (running|pending)' $E/raw/watch/cp-b10-watch-led/timeline.log | awk '{print $1}' | sed -n '1p;$p'
grep -oE '^[0-9:.]+|19255434 [a-z]+ [a-z-]+ att=[0-9]+ lease=[^ ]+ code=[a-z_]*' $E/raw/watch/cp-b10-watch-led/timeline.log | paste - - | grep -E '03:07:(4|5)' | sed -n '1,3p;$p'
cat $E/boundary-window.txt 2>/dev/null | head -5
